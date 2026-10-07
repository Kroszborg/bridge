package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/message"
	"bridge/internal/messaging"
	"bridge/internal/webhook"
)

const (
	requestTimeout = 15 * time.Second
	// echoWindow: an incoming SMS whose text equals a forward Bridge sent this
	// recently is that forward arriving back, and is not processed again.
	echoWindow = time.Hour
	// maxForwardSegments bounds the SMS a phone destination receives.
	maxForwardSegments = 3
	userAgent          = "Bridge-Forwarding/1"
)

// InboundWorker runs automation for an incoming SMS.
type InboundWorker struct {
	river.WorkerDefaults[messaging.InboundArgs]
	svc *Service
}

func (w *InboundWorker) Work(ctx context.Context, job *river.Job[messaging.InboundArgs]) error {
	return w.svc.ProcessInbound(ctx, job.Args.MessageID)
}

// ProcessInbound runs the auto-reply rules and forwarding rules for an
// incoming SMS. It is safe to run again: a reply and each forward happen once.
func (s *Service) ProcessInbound(ctx context.Context, messageID string) error {
	m, err := s.q.GetMessageByID(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.Direction != dbq.MessageDirectionInbound || m.Sender == nil {
		return nil
	}
	echo, err := s.q.RecentForwardWithBody(ctx, dbq.RecentForwardWithBodyParams{
		ProjectID: m.ProjectID, BodySha256: m.BodySha256, Since: s.now().Add(-echoWindow),
	})
	if err != nil {
		return err
	}
	if echo {
		// One of this project's forwards, received by another of its phones:
		// handling it again could forward it in a loop.
		s.log.Info("incoming SMS is a forward sent by Bridge; skipping rules", "message_id", m.ID)
		if done, _ := s.q.HasMessageEvent(ctx, dbq.HasMessageEventParams{MessageID: m.ID, Type: "automation_skipped"}); !done {
			return s.msgs.Event(ctx, nil, m, "automation_skipped", map[string]any{"reason": "forwarded_by_bridge"})
		}
		return nil
	}
	if err := s.autoReply(ctx, m); err != nil {
		return err
	}
	return s.fanOut(ctx, m)
}

// fanOut queues a delivery to every destination of every matching rule.
// Messages from opted-out numbers are forwarded too.
func (s *Service) fanOut(ctx context.Context, m dbq.Message) error {
	rules, err := s.ListForwarding(ctx, m.ProjectID)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	queued := 0
	for _, r := range rules {
		if !r.Enabled || !Matches(r.ForwardingRule, *m.Sender, m.Body) {
			continue
		}
		for _, d := range r.Destinations {
			deliveryID, err := q.InsertForwardingDelivery(ctx, dbq.InsertForwardingDeliveryParams{
				ID: id.New(id.ForwardDelivery), RuleID: r.ID, DestinationID: d.ID, ProjectID: m.ProjectID, MessageID: m.ID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				continue // queued by an earlier run
			}
			if err != nil {
				return err
			}
			if _, err := s.jobs.InsertTx(ctx, tx, ForwardArgs{DeliveryID: deliveryID}, nil); err != nil {
				return err
			}
			queued++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if queued > 0 {
		s.log.Info("incoming SMS queued for forwarding", "message_id", m.ID, "deliveries", queued)
	}
	return nil
}

// ForwardArgs delivers one incoming SMS to one destination.
type ForwardArgs struct {
	DeliveryID string `json:"delivery_id"`
}

func (ForwardArgs) Kind() string { return "forwarding.deliver" }

// ForwardMaxAttempts is the first try plus retries over about four hours.
const ForwardMaxAttempts = 8

var forwardRetries = []time.Duration{
	10 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour,
}

func (ForwardArgs) InsertOpts() river.InsertOpts {
	// Slow destinations share the webhooks queue, away from message dispatch.
	return river.InsertOpts{Queue: webhook.QueueWebhooks, MaxAttempts: ForwardMaxAttempts}
}

type ForwardWorker struct {
	river.WorkerDefaults[ForwardArgs]
	svc *Service
}

func (w *ForwardWorker) NextRetry(job *river.Job[ForwardArgs]) time.Time {
	d := forwardRetries[min(max(job.Attempt, 1), len(forwardRetries))-1]
	return time.Now().Add(d + time.Duration(rand.Int64N(int64(d/10)+1)))
}

func (w *ForwardWorker) Timeout(*river.Job[ForwardArgs]) time.Duration {
	return requestTimeout + 30*time.Second
}

func (w *ForwardWorker) Work(ctx context.Context, job *river.Job[ForwardArgs]) error {
	err := w.svc.Deliver(ctx, job.Args.DeliveryID, job.Attempt, job.Attempt >= job.MaxAttempts)
	var fe *forwardError
	if errors.As(err, &fe) && fe.permanent {
		return river.JobCancel(err)
	}
	return err
}

// forwardError is a failed attempt. Permanent failures are not retried.
type forwardError struct {
	msg       string
	status    int
	permanent bool
	skipped   bool // nothing to deliver: recorded as skipped, not failed
}

func (e *forwardError) Error() string { return e.msg }

func retryable(msg string, status int) error { return &forwardError{msg: msg, status: status} }
func permanent(msg string, status int) error {
	return &forwardError{msg: msg, status: status, permanent: true}
}
func skip(msg string) error { return &forwardError{msg: msg, permanent: true, skipped: true} }

// Deliver makes one attempt and records it on the delivery.
func (s *Service) Deliver(ctx context.Context, deliveryID string, attempt int, last bool) error {
	d, err := s.q.GetForwardingDelivery(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the rule or destination was deleted
	}
	if err != nil {
		return err
	}
	if d.Status == "succeeded" || d.Status == "failed" || d.Status == "skipped" {
		return nil
	}
	dest, err := s.q.GetForwardingDestination(ctx, d.DestinationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rule, err := s.q.GetForwardingRuleByID(ctx, d.RuleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	m, err := s.q.GetMessageByID(ctx, d.MessageID)
	if err != nil {
		return err
	}

	var forwardedID *string
	var ferr error
	switch {
	case !rule.Enabled:
		ferr = skip("The rule was disabled before delivery.")
	case m.BodyRedactedAt != nil:
		ferr = permanent("The message text was removed by the retention policy before it could be forwarded.", 0)
	default:
		forwardedID, ferr = s.forward(ctx, rule, dest, d, m)
	}

	params := dbq.RecordForwardingAttemptParams{ID: d.ID, Attempts: int32(attempt), ForwardedMessageID: forwardedID, Status: "succeeded"}
	var fe *forwardError
	if errors.As(ferr, &fe) {
		params.Error = ptr(clip(fe.msg, 500))
		if fe.status != 0 {
			params.ResponseStatus = ptr(int32(fe.status))
		}
		switch {
		case fe.skipped:
			params.Status = "skipped"
		case fe.permanent || last:
			params.Status = "failed"
		default:
			params.Status = "retrying"
		}
	} else if ferr != nil {
		params.Status, params.Error = "retrying", ptr("Internal error; retrying.")
		if last {
			params.Status = "failed"
		}
	}
	if err := s.q.RecordForwardingAttempt(ctx, params); err != nil {
		return err
	}
	if ferr == nil {
		s.log.Info("incoming SMS forwarded", "delivery_id", d.ID, "rule_id", rule.ID, "type", dest.Type, "attempt", attempt)
	} else {
		s.log.Warn("forwarding attempt failed", "delivery_id", d.ID, "rule_id", rule.ID, "type", dest.Type, "attempt", attempt, "error", ferr)
	}
	if params.Status == "skipped" {
		return nil
	}
	return ferr
}

func (s *Service) forward(ctx context.Context, rule dbq.ForwardingRule, dest dbq.ForwardingDestination, d dbq.ForwardingDelivery, m dbq.Message) (*string, error) {
	sender := deref(m.Sender)
	switch dest.Type {
	case DestPhone:
		return s.forwardSMS(ctx, rule, dest, d, m)
	case DestTelegram:
		return nil, s.forwardTelegram(ctx, dest, plainText(sender, m.Body, 4000))
	case DestWebhook:
		return nil, s.forwardWebhook(ctx, rule, dest, d, m)
	case DestEmail:
		return nil, s.sendEmail(ctx, dest.Target, "SMS from "+sender, emailBody(rule, m))
	}
	return nil, permanent("Unknown destination type "+dest.Type+".", 0)
}

// plainText is the forwarded text for chat destinations.
func plainText(sender, body string, limit int) string {
	return clip("SMS from "+sender+"\n\n"+body, limit)
}

func (s *Service) forwardSMS(ctx context.Context, rule dbq.ForwardingRule, dest dbq.ForwardingDestination, d dbq.ForwardingDelivery, m dbq.Message) (*string, error) {
	sender := deref(m.Sender)
	if from, ok := ReplyNumber(sender); (ok && from == dest.Target) || sender == dest.Target {
		return nil, skip("The destination is the sender of the message.")
	}
	text := TruncateSegments("From "+sender+": "+m.Body, maxForwardSegments)
	fwd, _, err := s.msgs.Send(ctx, messaging.SendRequest{
		ProjectID: m.ProjectID, Environment: m.Environment, To: dest.Target, Body: text,
		Metadata:       map[string]any{"forwarded_from": m.ID, "forwarding_rule_id": rule.ID},
		IdempotencyKey: "forward:" + d.ID,
	})
	var ve *messaging.ValidationError
	var oe *messaging.OptedOutError
	var rl *messaging.RateLimitError
	switch {
	case err == nil:
		return &fwd.ID, nil
	case errors.As(err, &oe):
		return nil, permanent("The destination number is on this project's opt-out list.", 0)
	case errors.As(err, &ve):
		return nil, permanent(ve.Message, 0)
	case errors.As(err, &rl):
		return nil, retryable("The project's hourly sending limit was reached; retrying later.", 0)
	}
	return nil, err
}

// TruncateSegments shortens text to fit in n SMS segments, ending it with "...".
func TruncateSegments(text string, n int) string {
	if _, segs := message.Segments(text); segs <= n {
		return text
	}
	r := []rune(text)
	lo, hi := 0, len(r) // the longest prefix that fits is in [lo, hi)
	for lo < hi-1 {
		mid := (lo + hi) / 2
		if _, segs := message.Segments(string(r[:mid]) + "..."); segs <= n {
			lo = mid
		} else {
			hi = mid
		}
	}
	return strings.TrimRightFunc(string(r[:lo]), func(c rune) bool { return c == ' ' || c == '\n' }) + "..."
}

func (s *Service) forwardTelegram(ctx context.Context, dest dbq.ForwardingDestination, text string) error {
	token, err := s.box.Open(dest.ID, dest.Secret)
	if err != nil {
		return permanent("The bot token cannot be read: "+err.Error()+" Enter it again.", 0)
	}
	payload, _ := json.Marshal(map[string]any{"chat_id": dest.Target, "text": text, "disable_web_page_preview": true})
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, s.telegramURL+"/bot"+string(token)+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return permanent("Could not build the Telegram request.", 0)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.telegram.Do(req)
	if err != nil {
		// Transport errors quote the URL, which contains the token.
		return retryable("Could not reach Telegram: "+strings.ReplaceAll(err.Error(), string(token), "<token>"), 0)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var tr struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &tr)
	msg := "Telegram answered " + strconv.Itoa(resp.StatusCode)
	if tr.Description != "" {
		msg += ": " + strings.ReplaceAll(tr.Description, string(token), "<token>")
	}
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return permanent(msg+". Check the bot token and chat ID, and that the bot was added to the chat.", resp.StatusCode)
	}
	return retryable(msg, resp.StatusCode)
}

func (s *Service) forwardWebhook(ctx context.Context, rule dbq.ForwardingRule, dest dbq.ForwardingDestination, d dbq.ForwardingDelivery, m dbq.Message) error {
	var payload any
	switch deref(dest.Format) {
	case FormatSlack:
		payload = map[string]string{"text": plainText(deref(m.Sender), m.Body, 3000)}
	case FormatDiscord:
		payload = map[string]string{"content": plainText(deref(m.Sender), m.Body, 2000)}
	default:
		payload = messaging.View(m)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := s.now()
	sig, err := webhook.Sign(rule.SigningSecret, d.ID, now, body)
	if err != nil {
		return err
	}
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, dest.Target, bytes.NewReader(body))
	if err != nil {
		return permanent("The destination URL is invalid.", 0)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(webhook.HeaderID, d.ID)
	req.Header.Set(webhook.HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(webhook.HeaderSignature, sig)
	resp, err := s.hooks.Do(req)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "non-public address") {
			return permanent("Refused: the URL resolves to a private or local address.", 0)
		}
		return retryable(clip(msg, 300), 0)
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	msg := fmt.Sprintf("The endpoint answered %d", resp.StatusCode)
	if t := strings.TrimSpace(strings.ToValidUTF8(string(snippet), "")); t != "" {
		msg += ": " + t
	}
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return permanent(msg, resp.StatusCode)
	}
	return retryable(msg, resp.StatusCode)
}

func emailBody(rule dbq.ForwardingRule, m dbq.Message) string {
	var b strings.Builder
	b.WriteString(m.Body)
	b.WriteString("\n\n--\nFrom: " + deref(m.Sender) + "\nReceived: " + m.CreatedAt.UTC().Format(time.RFC1123) + "\n")
	b.WriteString("Forwarded by Bridge (rule \"" + rule.Name + "\", message " + m.ID + ")\n")
	return b.String()
}
