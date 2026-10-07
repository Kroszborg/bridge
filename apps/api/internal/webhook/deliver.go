package webhook

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
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
)

// QueueWebhooks keeps slow endpoints from delaying message dispatch.
const QueueWebhooks = "webhooks"

const (
	requestTimeout = 15 * time.Second
	// DisableAfter: an endpoint that has failed every delivery for this long is disabled.
	DisableAfter     = 5 * 24 * time.Hour
	maxResponseBytes = 1024
	userAgent        = "Bridge-Webhooks/1 (+https://www.standardwebhooks.com)"
)

// retrySchedule is the wait before each retry: about 2.8 days in total, so an
// endpoint can be down for a weekend without losing events.
var retrySchedule = []time.Duration{
	5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour,
	10 * time.Hour, 10 * time.Hour, 10 * time.Hour, 10 * time.Hour, 10 * time.Hour, 10 * time.Hour,
}

// MaxAttempts is the first try plus every retry.
var MaxAttempts = len(retrySchedule) + 1

// RetryDelay is the wait after the given failed attempt (1-based), with up to
// 10% jitter so endpoints coming back are not hit by every retry at once.
func RetryDelay(attempt int) time.Duration {
	d := retrySchedule[min(max(attempt, 1), len(retrySchedule))-1]
	return d + time.Duration(rand.Int64N(int64(d/10)+1))
}

// DeliverArgs sends one event to one endpoint.
type DeliverArgs struct {
	EndpointID string `json:"endpoint_id"`
	EventID    string `json:"event_id"`
}

func (DeliverArgs) Kind() string { return "webhook.deliver" }

func (DeliverArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueWebhooks, MaxAttempts: MaxAttempts}
}

type DeliverWorker struct {
	river.WorkerDefaults[DeliverArgs]
	svc *Service
}

func (w *DeliverWorker) NextRetry(job *river.Job[DeliverArgs]) time.Time {
	return time.Now().Add(RetryDelay(job.Attempt))
}

func (w *DeliverWorker) Timeout(*river.Job[DeliverArgs]) time.Duration {
	return requestTimeout + 15*time.Second
}

func (w *DeliverWorker) Work(ctx context.Context, job *river.Job[DeliverArgs]) error {
	err := w.svc.Deliver(ctx, job.Args.EndpointID, job.Args.EventID, job.Attempt)
	if errors.Is(err, errEndpointGone) {
		return river.JobCancel(err)
	}
	return err
}

// Register adds the webhook workers.
func Register(workers *river.Workers, svc *Service) {
	river.AddWorker(workers, &DeliverWorker{svc: svc})
	river.AddWorker(workers, &PresenceWorker{svc: svc})
}

// DeliveryError is a failed attempt; River retries it on the schedule.
type DeliveryError struct {
	Status int
	Err    string
}

func (e *DeliveryError) Error() string {
	if e.Status != 0 {
		return "endpoint responded " + strconv.Itoa(e.Status)
	}
	return e.Err
}

// Deliver makes one delivery attempt and logs it. A nil error means the
// endpoint answered 2xx.
func (s *Service) Deliver(ctx context.Context, endpointID, eventID string, attempt int) error {
	ep, err := s.q.GetWebhookEndpointByID(ctx, endpointID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !ep.Enabled) {
		return errEndpointGone
	}
	if err != nil {
		return err
	}
	ev, err := s.q.GetWebhookEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errEndpointGone // removed by retention
	}
	if err != nil {
		return err
	}

	body, err := json.Marshal(Envelope{Type: ev.Type, Timestamp: ev.CreatedAt, Data: ev.Payload})
	if err != nil {
		return err
	}
	now := s.now()
	sig, err := Sign(ep.Secret, ev.ID, now, body)
	if err != nil {
		return err
	}
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return s.record(ctx, ep, ev, attempt, 0, "", "The endpoint URL is invalid.", 0)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(HeaderID, ev.ID)
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(HeaderSignature, sig)

	start := time.Now()
	resp, err := s.client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return s.record(ctx, ep, ev, attempt, 0, "", describe(err), elapsed)
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) // let the connection be reused
	return s.record(ctx, ep, ev, attempt, resp.StatusCode, cleanSnippet(snippet), "", elapsed)
}

// record logs an attempt and updates the endpoint's health.
func (s *Service) record(ctx context.Context, ep dbq.WebhookEndpoint, ev dbq.WebhookEvent, attempt, status int, respBody, errMsg string, elapsed time.Duration) error {
	ok := status >= 200 && status < 300
	params := dbq.InsertWebhookDeliveryParams{
		ID: id.New(id.Delivery), EndpointID: ep.ID, EventID: ev.ID, Attempt: int32(attempt),
		Succeeded: ok, DurationMs: int32(elapsed / time.Millisecond),
	}
	if status != 0 {
		st := int32(status)
		params.ResponseStatus = &st
	}
	if respBody != "" {
		params.ResponseBody = &respBody
	}
	if errMsg != "" {
		params.Error = &errMsg
	}
	// The log entry and the endpoint's health change together, so readers never
	// see an attempt without its effect.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	if err := q.InsertWebhookDelivery(ctx, params); err != nil {
		return err
	}
	if ok {
		if err := q.MarkWebhookSuccess(ctx, ep.ID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		s.log.Info("webhook delivered", "endpoint_id", ep.ID, "event_id", ev.ID, "type", ev.Type, "status", status, "attempt", attempt)
		return nil
	}

	failing, err := q.MarkWebhookFailure(ctx, ep.ID)
	if err != nil {
		return err
	}
	derr := &DeliveryError{Status: status, Err: errMsg}
	disable := failing.FailingSince != nil && s.now().Sub(*failing.FailingSince) >= DisableAfter
	if disable {
		reason := fmt.Sprintf("Disabled after every delivery failed for %d days (last: %s). Fix the endpoint, then enable it again.",
			int(DisableAfter.Hours()/24), derr.Error())
		if err := q.DisableWebhookEndpoint(ctx, dbq.DisableWebhookEndpointParams{ID: ep.ID, DisabledReason: &reason}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.log.Warn("webhook delivery failed", "endpoint_id", ep.ID, "event_id", ev.ID, "type", ev.Type,
		"attempt", attempt, "error", derr.Error())
	if disable {
		s.log.Warn("webhook endpoint disabled", "endpoint_id", ep.ID, "project_id", ep.ProjectID)
		return errEndpointGone
	}
	return derr
}

// describe turns a transport error into a short message for the delivery log.
func describe(err error) string {
	var dnsErr interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &dnsErr) && dnsErr.Timeout()):
		return "Timed out after " + requestTimeout.String() + " waiting for the endpoint."
	case strings.Contains(err.Error(), "non-public address"):
		return "Refused: the URL resolves to a private or local address."
	case strings.Contains(err.Error(), "no such host"):
		return "The endpoint's hostname does not resolve."
	case strings.Contains(err.Error(), "connection refused"):
		return "Connection refused by the endpoint."
	case strings.Contains(err.Error(), "certificate"):
		return "TLS certificate error: " + clip(err.Error(), 200)
	}
	return clip(err.Error(), 300)
}

func cleanSnippet(b []byte) string {
	s := strings.ToValidUTF8(string(b), "�")
	if !utf8.ValidString(s) {
		return ""
	}
	return clip(strings.TrimSpace(s), 500)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
