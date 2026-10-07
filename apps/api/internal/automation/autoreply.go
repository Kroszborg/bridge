package automation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
	"bridge/internal/webhook"
)

// Match types and actions of auto-reply rules.
const (
	MatchExact      = "exact"
	MatchContains   = "contains"
	MatchStartsWith = "starts_with"

	ActionNone   = "none"
	ActionOptOut = "opt_out"
	ActionOptIn  = "opt_in"
)

const (
	maxAutoReplyRules = 50
	maxKeywords       = 20
	maxKeywordLength  = 50
	maxReplyLength    = 480
	// ReplyCooldown: a rule replies to the same number at most once in this
	// long, so two automated senders cannot keep answering each other.
	ReplyCooldown = 10 * time.Minute
	// minReplyDigits: senders with fewer digits are short codes, which do not
	// read replies; alphanumeric sender IDs cannot receive any.
	minReplyDigits = 7
)

// RuleFields are an auto-reply rule's settings.
type RuleFields struct {
	Name     string
	Match    string
	Keywords []string
	Reply    *string
	Action   string
	Enabled  bool
	Priority int
}

// defaultRules are created for every project the first time its rules are
// used, so STOP works without any setup.
var defaultRules = []RuleFields{
	{Name: "Unsubscribe", Match: MatchExact, Keywords: []string{"STOP", "UNSUBSCRIBE", "CANCEL", "END", "QUIT"},
		Reply: ptr("You are unsubscribed. Reply START to subscribe again."), Action: ActionOptOut, Enabled: true, Priority: 10},
	{Name: "Subscribe again", Match: MatchExact, Keywords: []string{"START", "UNSTOP"},
		Reply: ptr("You are subscribed again."), Action: ActionOptIn, Enabled: true, Priority: 20},
	{Name: "Help", Match: MatchExact, Keywords: []string{"HELP"},
		Reply: ptr("Reply STOP to unsubscribe."), Action: ActionNone, Enabled: true, Priority: 30},
}

// EnsureDefaults creates the default rules once per project.
func (s *Service) EnsureDefaults(ctx context.Context, projectID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	n, err := q.ClaimAutomationDefaults(ctx, projectID)
	if err != nil || n == 0 {
		return err
	}
	for _, f := range defaultRules {
		if _, err := q.InsertAutoReplyRule(ctx, insertRuleParams(projectID, f)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertRuleParams(projectID string, f RuleFields) dbq.InsertAutoReplyRuleParams {
	return dbq.InsertAutoReplyRuleParams{
		ID: id.New(id.AutoReplyRule), ProjectID: projectID, Name: f.Name, MatchType: f.Match, Keywords: f.Keywords,
		Reply: f.Reply, Action: f.Action, Enabled: f.Enabled, Priority: int32(f.Priority),
	}
}

// normalizeRule validates rule settings.
func normalizeRule(f *RuleFields) error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len([]rune(f.Name)) > 60 {
		return fieldErr("name", "Use a name of 1 to 60 characters.")
	}
	switch f.Match {
	case MatchExact, MatchContains, MatchStartsWith:
	default:
		return fieldErr("match", "Use exact, contains or starts_with.")
	}
	var kws []string
	for _, k := range f.Keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if len([]rune(k)) > maxKeywordLength {
			return fieldErr("keywords", "Keywords can be at most 50 characters.")
		}
		if !slices.ContainsFunc(kws, func(x string) bool { return strings.EqualFold(x, k) }) {
			kws = append(kws, k)
		}
	}
	if len(kws) == 0 || len(kws) > maxKeywords {
		return fieldErr("keywords", "Give 1 to 20 keywords.")
	}
	f.Keywords = kws
	if f.Reply != nil {
		r := strings.TrimSpace(*f.Reply)
		if r == "" {
			f.Reply = nil
		} else if len([]rune(r)) > maxReplyLength {
			return fieldErr("reply", "Keep the reply to 480 characters.")
		} else {
			f.Reply = &r
		}
	}
	if f.Action == "" {
		f.Action = ActionNone
	}
	switch f.Action {
	case ActionNone, ActionOptOut, ActionOptIn:
	default:
		return fieldErr("action", "Use none, opt_out or opt_in.")
	}
	if f.Reply == nil && f.Action == ActionNone {
		return fieldErr("reply", "A rule needs a reply, an action, or both.")
	}
	if f.Priority < 0 || f.Priority > 10000 {
		return fieldErr("priority", "Use a priority from 0 to 10000.")
	}
	return nil
}

// ListAutoReplies returns a project's rules in the order they are tried.
func (s *Service) ListAutoReplies(ctx context.Context, projectID string) ([]dbq.AutoReplyRule, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	return s.q.ListAutoReplyRules(ctx, projectID)
}

func (s *Service) GetAutoReply(ctx context.Context, projectID, ruleID string) (dbq.AutoReplyRule, error) {
	r, err := s.q.GetAutoReplyRule(ctx, dbq.GetAutoReplyRuleParams{ID: ruleID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Service) CreateAutoReply(ctx context.Context, projectID string, f RuleFields) (dbq.AutoReplyRule, error) {
	if err := normalizeRule(&f); err != nil {
		return dbq.AutoReplyRule{}, err
	}
	rules, err := s.ListAutoReplies(ctx, projectID)
	if err != nil {
		return dbq.AutoReplyRule{}, err
	}
	if len(rules) >= maxAutoReplyRules {
		return dbq.AutoReplyRule{}, &LimitError{"A project can have at most 50 auto-reply rules. Remove one first."}
	}
	return s.q.InsertAutoReplyRule(ctx, insertRuleParams(projectID, f))
}

// RuleFieldsOf reads a stored rule's settings.
func RuleFieldsOf(r dbq.AutoReplyRule) RuleFields {
	return RuleFields{Name: r.Name, Match: r.MatchType, Keywords: r.Keywords, Reply: r.Reply, Action: r.Action, Enabled: r.Enabled, Priority: int(r.Priority)}
}

func (s *Service) UpdateAutoReply(ctx context.Context, cur dbq.AutoReplyRule, f RuleFields) (dbq.AutoReplyRule, error) {
	if err := normalizeRule(&f); err != nil {
		return cur, err
	}
	return s.q.UpdateAutoReplyRule(ctx, dbq.UpdateAutoReplyRuleParams{
		ID: cur.ID, ProjectID: cur.ProjectID, Name: f.Name, MatchType: f.Match, Keywords: f.Keywords, Reply: f.Reply,
		Action: f.Action, Enabled: f.Enabled, Priority: int32(f.Priority),
	})
}

func (s *Service) DeleteAutoReply(ctx context.Context, projectID, ruleID string) error {
	n, err := s.q.DeleteAutoReplyRule(ctx, dbq.DeleteAutoReplyRuleParams{ID: ruleID, ProjectID: projectID})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// MatchRule returns the first enabled rule (rules are in priority order) that
// matches body, and the keyword that matched. Matching ignores case and
// surrounding whitespace; exact compares the whole message.
func MatchRule(rules []dbq.AutoReplyRule, body string) (*dbq.AutoReplyRule, string) {
	text := strings.ToLower(strings.TrimSpace(body))
	if text == "" {
		return nil, ""
	}
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			continue
		}
		for _, k := range r.Keywords {
			kw := strings.ToLower(strings.TrimSpace(k))
			if kw == "" {
				continue
			}
			var ok bool
			switch r.MatchType {
			case MatchExact:
				ok = text == kw
			case MatchStartsWith:
				ok = strings.HasPrefix(text, kw)
			case MatchContains:
				ok = strings.Contains(text, kw)
			}
			if ok {
				return r, k
			}
		}
	}
	return nil, ""
}

// ReplyNumber returns the E.164 number to answer a sender at. ok is false for
// alphanumeric sender IDs, short codes and numbers not in international form.
func ReplyNumber(sender string) (string, bool) {
	s := strings.TrimSpace(sender)
	if strings.HasPrefix(s, "00") {
		s = "+" + s[2:]
	}
	digits := 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", false // a letter: an alphanumeric sender ID
		}
	}
	if digits < minReplyDigits || !strings.HasPrefix(s, "+") {
		return "", false
	}
	n, err := messaging.NormalizeE164(s)
	return n, err == nil
}

// AutoReplied is the data of the message.auto_replied event.
type AutoReplied struct {
	Environment    string            `json:"environment" enum:"live,test"`
	Message        messaging.Message `json:"message" doc:"The incoming SMS."`
	RuleID         string            `json:"rule_id"`
	RuleName       string            `json:"rule_name"`
	Keyword        string            `json:"keyword" doc:"The keyword that matched."`
	Action         string            `json:"action" enum:"none,opt_out,opt_in"`
	ReplyMessageID *string           `json:"reply_message_id" doc:"The reply sent, if any."`
}

// EventAutoReply is the timeline event recorded on the incoming message.
const EventAutoReply = "auto_reply"

// autoReply runs the first matching rule for an incoming SMS: the opt-out or
// opt-in action, then the reply through the phone that received it.
func (s *Service) autoReply(ctx context.Context, m dbq.Message) error {
	if done, err := s.q.HasMessageEvent(ctx, dbq.HasMessageEventParams{MessageID: m.ID, Type: EventAutoReply}); err != nil || done {
		return err
	}
	if err := s.EnsureDefaults(ctx, m.ProjectID); err != nil {
		return err
	}
	rules, err := s.q.ListAutoReplyRules(ctx, m.ProjectID)
	if err != nil {
		return err
	}
	rule, keyword := MatchRule(rules, m.Body)
	if rule == nil {
		return nil
	}
	detail := map[string]any{"rule_id": rule.ID, "rule_name": rule.Name, "keyword": keyword, "action": rule.Action}
	number, ok := ReplyNumber(deref(m.Sender))
	if !ok {
		// Sender IDs and short codes are not people; nothing to opt out or answer.
		detail["skipped"] = "sender_not_a_phone_number"
		return s.msgs.Event(ctx, nil, m, EventAutoReply, detail)
	}

	switch rule.Action {
	case ActionOptOut:
		if _, _, err := s.AddOptOut(ctx, m.ProjectID, number, SourceKeyword, &keyword); err != nil {
			return err
		}
	case ActionOptIn:
		if _, err := s.RemoveOptOut(ctx, m.ProjectID, number); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	var replyID *string
	recorded := false
	if rule.Reply != nil {
		switch reason, err := s.replyBlocked(ctx, m, rule, number); {
		case err != nil:
			return err
		case reason != "":
			detail["reply_skipped"] = reason
		default:
			reply, _, err := s.msgs.Send(ctx, messaging.SendRequest{
				ProjectID: m.ProjectID, Environment: m.Environment, To: number, Body: *rule.Reply, DeviceID: m.DeviceID,
				Metadata:       map[string]any{"auto_reply_rule_id": rule.ID, "in_reply_to": m.ID},
				IdempotencyKey: "auto-reply:" + m.ID, AllowOptedOut: true,
				OnCreate: func(ctx context.Context, q *dbq.Queries, reply dbq.Message) error {
					detail["reply_message_id"] = reply.ID
					return s.msgs.Event(ctx, q, m, EventAutoReply, detail)
				},
			})
			if err != nil {
				delete(detail, "reply_message_id")
				detail["reply_error"] = err.Error()
				s.log.Warn("auto-reply not sent", "message_id", m.ID, "rule_id", rule.ID, "error", err)
			} else {
				replyID, recorded = &reply.ID, true
			}
		}
	}
	if !recorded {
		if err := s.msgs.Event(ctx, nil, m, EventAutoReply, detail); err != nil {
			return err
		}
	}
	s.log.Info("auto-reply rule ran", "message_id", m.ID, "rule_id", rule.ID, "action", rule.Action, "replied", replyID != nil)
	s.msgs.Emit(ctx, m.ProjectID, webhook.EventMessageAutoReplied, AutoReplied{
		Environment: string(m.Environment), Message: messaging.View(m), RuleID: rule.ID, RuleName: rule.Name,
		Keyword: keyword, Action: rule.Action, ReplyMessageID: replyID,
	})
	return nil
}

// replyBlocked explains why a rule must not reply now, or returns "".
func (s *Service) replyBlocked(ctx context.Context, m dbq.Message, rule *dbq.AutoReplyRule, number string) (string, error) {
	if m.DeviceID == nil {
		return "device_removed", nil
	}
	d, err := s.q.GetDevice(ctx, dbq.GetDeviceParams{ID: *m.DeviceID, ProjectID: m.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && d.RevokedAt != nil) {
		return "device_removed", nil
	}
	if err != nil {
		return "", err
	}
	recent, err := s.q.RecentAutoReply(ctx, dbq.RecentAutoReplyParams{
		ProjectID: m.ProjectID, Recipient: number, RuleID: rule.ID, Since: s.now().Add(-ReplyCooldown),
	})
	if err != nil {
		return "", err
	}
	if recent {
		return "loop_protection", nil
	}
	return "", nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
