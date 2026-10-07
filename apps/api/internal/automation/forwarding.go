package automation

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
	"bridge/internal/webhook"
)

// Destination types and webhook formats.
const (
	DestPhone    = "phone"
	DestTelegram = "telegram"
	DestWebhook  = "webhook"
	DestEmail    = "email"

	FormatJSON    = "json"
	FormatSlack   = "slack"
	FormatDiscord = "discord"
)

const (
	maxForwardingRules = 20
	maxDestinations    = 5
	maxSenderPatterns  = 20
)

var (
	telegramTokenRE = regexp.MustCompile(`^[0-9]{3,15}:[A-Za-z0-9_-]{20,64}$`)
	telegramChatRE  = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{3,31})$`)
)

// DestinationInput is a destination as submitted. ID names an existing
// destination of the rule to keep its stored bot token when BotToken is empty.
type DestinationInput struct {
	ID       string
	Type     string
	To       string // phone number or email address
	ChatID   string // telegram
	BotToken string // telegram; write-only
	URL      string // webhook
	Format   string // webhook: json, slack or discord
}

// ForwardingFields are a forwarding rule's settings.
type ForwardingFields struct {
	Name         string
	Enabled      bool
	Senders      []string
	Contains     *string
	Destinations []DestinationInput
}

// ForwardingRule is a rule with its destinations.
type ForwardingRule struct {
	dbq.ForwardingRule
	Destinations []dbq.ForwardingDestination
}

// normalizeForwarding validates a rule's match settings.
func normalizeForwarding(f *ForwardingFields) error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len([]rune(f.Name)) > 60 {
		return fieldErr("name", "Use a name of 1 to 60 characters.")
	}
	var senders []string
	for _, p := range f.Senders {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > 64 || strings.Contains(strings.TrimSuffix(p, "*"), "*") || p == "*" {
			return fieldErr("match.senders", "Use exact senders, or a prefix ending in * such as +9198*. A * may only end a pattern.")
		}
		senders = append(senders, p)
	}
	if len(senders) > maxSenderPatterns {
		return fieldErr("match.senders", "Use at most 20 sender patterns.")
	}
	if senders == nil {
		senders = []string{}
	}
	f.Senders = senders
	if f.Contains != nil {
		c := strings.TrimSpace(*f.Contains)
		if c == "" {
			f.Contains = nil
		} else if len([]rune(c)) > 100 {
			return fieldErr("match.contains", "Keep the keyword to 100 characters.")
		} else {
			f.Contains = &c
		}
	}
	if len(f.Destinations) == 0 || len(f.Destinations) > maxDestinations {
		return fieldErr("destinations", "Give 1 to 5 destinations.")
	}
	return nil
}

// checkDestination validates one destination and returns its stored form.
// The sealed token is filled in later, once the row ID is known.
func (s *Service) checkDestination(i int, d DestinationInput, existing map[string]dbq.ForwardingDestination) (dbq.ForwardingDestination, string, error) {
	field := fmt.Sprintf("destinations[%d]", i)
	out := dbq.ForwardingDestination{Type: d.Type, Position: int32(i)}
	if d.ID != "" {
		prev, ok := existing[d.ID]
		if !ok {
			return out, "", fieldErr(field+".id", "No destination with this ID in the rule. Leave id out to add a new destination.")
		}
		out.ID, out.Secret = prev.ID, prev.Secret
		if prev.Type != d.Type {
			out.Secret = nil
		}
	}
	switch d.Type {
	case DestPhone:
		n, err := messaging.NormalizeE164(d.To)
		if err != nil {
			return out, "", fieldErr(field+".to", "Use an E.164 number such as +919876543210.")
		}
		out.Target = n
	case DestEmail:
		if s.smtp == nil {
			return out, "", ErrNoSMTP
		}
		a, err := mail.ParseAddress(strings.TrimSpace(d.To))
		if err != nil || strings.ContainsAny(a.Address, "\r\n") {
			return out, "", fieldErr(field+".to", "Use an email address such as alerts@example.com.")
		}
		out.Target = a.Address
	case DestTelegram:
		chat := strings.TrimSpace(d.ChatID)
		if !telegramChatRE.MatchString(chat) {
			return out, "", fieldErr(field+".chat_id", "Use the numeric chat ID (e.g. -1001234567890) or a public channel's @username.")
		}
		out.Target = chat
		token := strings.TrimSpace(d.BotToken)
		if token == "" && out.Secret == nil {
			return out, "", fieldErr(field+".bot_token", "Give the bot token from @BotFather.")
		}
		if token != "" {
			if !telegramTokenRE.MatchString(token) {
				return out, "", fieldErr(field+".bot_token", "This does not look like a bot token from @BotFather (123456:ABC…).")
			}
			if !s.box.Ready() {
				return out, "", ErrNoSecretKey
			}
			return out, token, nil
		}
	case DestWebhook:
		u, err := webhook.CheckURL(d.URL, s.allowPrivate)
		if err != nil {
			var ve *webhook.ValidationError
			if errors.As(err, &ve) {
				return out, "", fieldErr(field+".url", ve.Message)
			}
			return out, "", err
		}
		out.Target = u
		format := cmpOr(d.Format, FormatJSON)
		if format != FormatJSON && format != FormatSlack && format != FormatDiscord {
			return out, "", fieldErr(field+".format", "Use json, slack or discord.")
		}
		out.Format = &format
	default:
		return out, "", fieldErr(field+".type", "Use phone, telegram, webhook or email.")
	}
	return out, "", nil
}

// ListForwarding returns a project's rules with their destinations.
func (s *Service) ListForwarding(ctx context.Context, projectID string) ([]ForwardingRule, error) {
	rules, err := s.q.ListForwardingRules(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.withDestinations(ctx, rules)
}

func (s *Service) withDestinations(ctx context.Context, rules []dbq.ForwardingRule) ([]ForwardingRule, error) {
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	dests, err := s.q.ListForwardingDestinations(ctx, ids)
	if err != nil {
		return nil, err
	}
	byRule := map[string][]dbq.ForwardingDestination{}
	for _, d := range dests {
		byRule[d.RuleID] = append(byRule[d.RuleID], d)
	}
	out := make([]ForwardingRule, 0, len(rules))
	for _, r := range rules {
		ds := byRule[r.ID]
		if ds == nil {
			ds = []dbq.ForwardingDestination{}
		}
		out = append(out, ForwardingRule{ForwardingRule: r, Destinations: ds})
	}
	return out, nil
}

func (s *Service) GetForwarding(ctx context.Context, projectID, ruleID string) (ForwardingRule, error) {
	r, err := s.q.GetForwardingRule(ctx, dbq.GetForwardingRuleParams{ID: ruleID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ForwardingRule{}, ErrNotFound
	}
	if err != nil {
		return ForwardingRule{}, err
	}
	out, err := s.withDestinations(ctx, []dbq.ForwardingRule{r})
	if err != nil {
		return ForwardingRule{}, err
	}
	return out[0], nil
}

// CreateForwarding validates and stores a rule with a new signing secret.
func (s *Service) CreateForwarding(ctx context.Context, projectID string, f ForwardingFields) (ForwardingRule, error) {
	if err := normalizeForwarding(&f); err != nil {
		return ForwardingRule{}, err
	}
	existing, err := s.q.ListForwardingRules(ctx, projectID)
	if err != nil {
		return ForwardingRule{}, err
	}
	if len(existing) >= maxForwardingRules {
		return ForwardingRule{}, &LimitError{"A project can have at most 20 forwarding rules. Remove one first."}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ForwardingRule{}, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	r, err := q.InsertForwardingRule(ctx, dbq.InsertForwardingRuleParams{
		ID: id.New(id.ForwardingRule), ProjectID: projectID, Name: f.Name, Enabled: f.Enabled, Senders: f.Senders,
		Contains: f.Contains, SigningSecret: webhook.NewSecret(),
	})
	if err != nil {
		return ForwardingRule{}, err
	}
	if err := s.saveDestinations(ctx, q, r, f.Destinations, nil); err != nil {
		return ForwardingRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ForwardingRule{}, err
	}
	return s.GetForwarding(ctx, projectID, r.ID)
}

// UpdateForwarding replaces a rule's settings. With destinations nil the
// destinations stay as they are; otherwise the list replaces them, keeping
// the destinations whose ID is given (and their bot tokens).
func (s *Service) UpdateForwarding(ctx context.Context, cur ForwardingRule, f ForwardingFields) (ForwardingRule, error) {
	keep := f.Destinations == nil
	if keep {
		f.Destinations = []DestinationInput{{Type: "keep"}} // satisfies the count check
	}
	if err := normalizeForwarding(&f); err != nil {
		return cur, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return cur, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	r, err := q.UpdateForwardingRule(ctx, dbq.UpdateForwardingRuleParams{
		ID: cur.ID, ProjectID: cur.ProjectID, Name: f.Name, Enabled: f.Enabled, Senders: f.Senders, Contains: f.Contains,
	})
	if err != nil {
		return cur, err
	}
	if !keep {
		if err := s.saveDestinations(ctx, q, r, f.Destinations, cur.Destinations); err != nil {
			return cur, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return cur, err
	}
	return s.GetForwarding(ctx, cur.ProjectID, cur.ID)
}

// saveDestinations writes a rule's destination list: updates those named by
// ID, inserts new ones and deletes the rest (with their delivery logs).
func (s *Service) saveDestinations(ctx context.Context, q *dbq.Queries, r dbq.ForwardingRule, in []DestinationInput, current []dbq.ForwardingDestination) error {
	existing := map[string]dbq.ForwardingDestination{}
	for _, d := range current {
		existing[d.ID] = d
	}
	kept := map[string]bool{}
	for i, d := range in {
		row, token, err := s.checkDestination(i, d, existing)
		if err != nil {
			return err
		}
		if row.ID != "" && kept[row.ID] {
			return fieldErr(fmt.Sprintf("destinations[%d].id", i), "Each destination ID may appear once.")
		}
		isNew := row.ID == ""
		if isNew {
			row.ID = id.New(id.ForwardingDest)
		}
		if token != "" {
			if row.Secret, err = s.box.Seal(row.ID, []byte(token)); err != nil {
				return err
			}
		}
		if row.Type != DestTelegram {
			row.Secret = nil
		}
		if isNew {
			_, err = q.InsertForwardingDestination(ctx, dbq.InsertForwardingDestinationParams{
				ID: row.ID, RuleID: r.ID, ProjectID: r.ProjectID, Type: row.Type, Target: row.Target, Format: row.Format,
				Secret: row.Secret, Position: row.Position,
			})
		} else {
			kept[row.ID] = true
			_, err = q.UpdateForwardingDestination(ctx, dbq.UpdateForwardingDestinationParams{
				ID: row.ID, RuleID: r.ID, Type: row.Type, Target: row.Target, Format: row.Format, Secret: row.Secret, Position: row.Position,
			})
		}
		if err != nil {
			return err
		}
	}
	for _, d := range current {
		if !kept[d.ID] {
			if err := q.DeleteForwardingDestination(ctx, dbq.DeleteForwardingDestinationParams{ID: d.ID, RuleID: r.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) DeleteForwarding(ctx context.Context, projectID, ruleID string) error {
	n, err := s.q.DeleteForwardingRule(ctx, dbq.DeleteForwardingRuleParams{ID: ruleID, ProjectID: projectID})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// Deliveries lists a rule's latest deliveries, newest first.
func (s *Service) Deliveries(ctx context.Context, ruleID string, limit int) ([]dbq.ListForwardingDeliveriesRow, error) {
	return s.q.ListForwardingDeliveries(ctx, dbq.ListForwardingDeliveriesParams{RuleID: ruleID, RowLimit: int32(limit)})
}

// Matches reports whether an incoming SMS matches a rule: its sender is one
// of the rule's senders (exact, or prefix with *), and its body contains the
// keyword. Comparisons ignore case; empty settings match everything.
func Matches(r dbq.ForwardingRule, sender, body string) bool {
	if len(r.Senders) > 0 {
		from := strings.ToLower(strings.TrimSpace(sender))
		ok := false
		for _, p := range r.Senders {
			p = strings.ToLower(p)
			if prefix, wild := strings.CutSuffix(p, "*"); wild {
				ok = strings.HasPrefix(from, prefix)
			} else {
				ok = from == p
			}
			if ok {
				break
			}
		}
		if !ok {
			return false
		}
	}
	if r.Contains != nil && !strings.Contains(strings.ToLower(body), strings.ToLower(*r.Contains)) {
		return false
	}
	return true
}
