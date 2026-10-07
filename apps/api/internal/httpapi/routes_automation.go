package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/automation"
	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
)

// ---- Opt-outs ---------------------------------------------------------------

// OptOut is a number that asked not to receive messages from the project.
type OptOut struct {
	ID        string    `json:"id" example:"uns_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Number    string    `json:"number" example:"+919876543210"`
	Source    string    `json:"source" enum:"keyword,manual,api" doc:"keyword: the person texted STOP (or another opt-out keyword); manual: added in the dashboard; api: added with an API key."`
	Keyword   *string   `json:"keyword" nullable:"true" example:"STOP"`
	CreatedAt time.Time `json:"created_at"`
}

type OptOutList struct {
	Data    []OptOut `json:"data"`
	HasMore bool     `json:"has_more" doc:"Pass the last entry's ID as starting_after to fetch the next page."`
}

type OptOutInput struct {
	Number string `json:"number" minLength:"3" maxLength:"32" example:"+919876543210" doc:"E.164 number."`
}

type OptOutPath struct {
	Number string `path:"number" maxLength:"64" example:"+919876543210" doc:"The number, URL-encoded (%2B for +) or as is."`
}

type ProjectOptOutPath struct {
	ProjectPath
	OptOutPath
}

type ListOptOutsQuery struct {
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	StartingAfter string `query:"starting_after" doc:"An opt-out ID; returns entries created before it."`
	Source        string `query:"source" enum:"keyword,manual,api"`
}

func toOptOut(o dbq.OptOut) OptOut {
	return OptOut{ID: o.ID, Number: o.Number, Source: o.Source, Keyword: o.Keyword, CreatedAt: o.CreatedAt}
}

// pathNumber reads a number from the path, decoding %2B and normalizing it.
func pathNumber(raw string) string {
	if dec, err := url.PathUnescape(raw); err == nil {
		raw = dec
	}
	return automation.LookupNumber(raw)
}

// ---- Auto-replies -----------------------------------------------------------

// AutoReplyRule answers incoming SMS that match its keywords.
type AutoReplyRule struct {
	ID        string    `json:"id" example:"arr_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Name      string    `json:"name" example:"Unsubscribe"`
	Match     string    `json:"match" enum:"exact,contains,starts_with" doc:"How keywords are compared with the trimmed message, ignoring case. exact compares the whole message."`
	Keywords  []string  `json:"keywords" example:"[\"STOP\",\"UNSUBSCRIBE\"]"`
	Reply     *string   `json:"reply" nullable:"true" doc:"Sent back through the phone that received the SMS."`
	Action    string    `json:"action" enum:"none,opt_out,opt_in" doc:"opt_out adds the sender to the opt-out list; opt_in removes them."`
	Enabled   bool      `json:"enabled"`
	Priority  int       `json:"priority" doc:"Lower is tried first; only the first matching enabled rule runs."`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AutoReplyRuleList struct {
	Data []AutoReplyRule `json:"data" doc:"In the order they are tried."`
}

type AutoReplyRuleCreateInput struct {
	Name     string   `json:"name" minLength:"1" maxLength:"60"`
	Match    string   `json:"match,omitempty" enum:"exact,contains,starts_with" doc:"Default exact."`
	Keywords []string `json:"keywords" minItems:"1" maxItems:"20"`
	Reply    *string  `json:"reply,omitempty" maxLength:"480"`
	Action   string   `json:"action,omitempty" enum:"none,opt_out,opt_in" doc:"Default none."`
	Enabled  *bool    `json:"enabled,omitempty" doc:"Default true."`
	Priority *int     `json:"priority,omitempty" minimum:"0" maximum:"10000" doc:"Default 100."`
}

type AutoReplyRuleUpdateInput struct {
	Name     *string  `json:"name,omitempty" minLength:"1" maxLength:"60"`
	Match    *string  `json:"match,omitempty" enum:"exact,contains,starts_with"`
	Keywords []string `json:"keywords,omitempty" maxItems:"20"`
	Reply    *string  `json:"reply,omitempty" maxLength:"480" doc:"An empty string removes the reply."`
	Action   *string  `json:"action,omitempty" enum:"none,opt_out,opt_in"`
	Enabled  *bool    `json:"enabled,omitempty"`
	Priority *int     `json:"priority,omitempty" minimum:"0" maximum:"10000"`
}

type AutoReplyRulePath struct {
	ProjectPath
	RuleID string `path:"ruleId" pattern:"^arr_[0-9a-z]{26}$" example:"arr_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

func toAutoReplyRule(r dbq.AutoReplyRule) AutoReplyRule {
	kw := r.Keywords
	if kw == nil {
		kw = []string{}
	}
	return AutoReplyRule{
		ID: r.ID, Name: r.Name, Match: r.MatchType, Keywords: kw, Reply: r.Reply, Action: r.Action, Enabled: r.Enabled,
		Priority: int(r.Priority), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// ---- Forwarding -------------------------------------------------------------

// ForwardingDestination is where a forwarding rule sends incoming SMS.
type ForwardingDestination struct {
	ID          string  `json:"id" example:"fwd_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Type        string  `json:"type" enum:"phone,telegram,webhook,email"`
	To          *string `json:"to" nullable:"true" doc:"phone: the E.164 number; email: the address."`
	ChatID      *string `json:"chat_id" nullable:"true" doc:"telegram: the chat."`
	BotTokenSet bool    `json:"bot_token_set" doc:"telegram: whether a bot token is stored. Tokens are never returned."`
	URL         *string `json:"url" nullable:"true" doc:"webhook: the endpoint."`
	Format      *string `json:"format" nullable:"true" enum:"json,slack,discord" doc:"webhook: json posts the message; slack posts {\"text\"}; discord posts {\"content\"}."`
}

type ForwardingMatch struct {
	Senders  []string `json:"senders" doc:"Exact senders, or prefixes ending in * (e.g. +9198*, AX-*). Empty matches every sender."`
	Contains *string  `json:"contains" nullable:"true" doc:"Only messages containing this text (ignoring case)."`
}

// ForwardingRule copies matching incoming SMS to its destinations.
type ForwardingRule struct {
	ID           string                  `json:"id" example:"fwr_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Name         string                  `json:"name"`
	Enabled      bool                    `json:"enabled"`
	Match        ForwardingMatch         `json:"match"`
	Destinations []ForwardingDestination `json:"destinations"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

type CreatedForwardingRule struct {
	ForwardingRule
	SigningSecret string `json:"signing_secret" example:"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw" doc:"Signs requests to webhook destinations (Standard Webhooks). Reveal it again later."`
}

type ForwardingRuleList struct {
	Data              []ForwardingRule `json:"data"`
	EmailAvailable    bool             `json:"email_available" doc:"Whether the server has SMTP configured for email destinations."`
	TelegramAvailable bool             `json:"telegram_available" doc:"Whether the server can store Telegram bot tokens (BRIDGE_SECRET_KEY)."`
}

type ForwardingDestinationInput struct {
	ID       string `json:"id,omitempty" pattern:"^fwd_[0-9a-z]{26}$" doc:"Keep this existing destination (and its stored bot token when bot_token is left out)."`
	Type     string `json:"type" enum:"phone,telegram,webhook,email"`
	To       string `json:"to,omitempty" maxLength:"254" doc:"phone: an E.164 number; email: an address."`
	ChatID   string `json:"chat_id,omitempty" maxLength:"64" doc:"telegram: the numeric chat ID or a channel's @username."`
	BotToken string `json:"bot_token,omitempty" maxLength:"100" writeOnly:"true" doc:"telegram: the token from @BotFather. Write-only; stored encrypted."`
	URL      string `json:"url,omitempty" maxLength:"2048" doc:"webhook: the endpoint URL."`
	Format   string `json:"format,omitempty" enum:"json,slack,discord" doc:"webhook: default json."`
}

type ForwardingMatchInput struct {
	Senders  []string `json:"senders,omitempty" maxItems:"20"`
	Contains string   `json:"contains,omitempty" maxLength:"100"`
}

type ForwardingRuleCreateInput struct {
	Name         string                       `json:"name" minLength:"1" maxLength:"60"`
	Enabled      *bool                        `json:"enabled,omitempty" doc:"Default true."`
	Match        *ForwardingMatchInput        `json:"match,omitempty" doc:"Leave out to forward every incoming SMS."`
	Destinations []ForwardingDestinationInput `json:"destinations" minItems:"1" maxItems:"5"`
}

type ForwardingRuleUpdateInput struct {
	Name         *string                      `json:"name,omitempty" minLength:"1" maxLength:"60"`
	Enabled      *bool                        `json:"enabled,omitempty"`
	Match        *ForwardingMatchInput        `json:"match,omitempty" doc:"Replaces the whole match."`
	Destinations []ForwardingDestinationInput `json:"destinations,omitempty" maxItems:"5" doc:"Replaces the list. Include a destination's id to keep it."`
}

type ForwardingRulePath struct {
	ProjectPath
	RuleID string `path:"ruleId" pattern:"^fwr_[0-9a-z]{26}$" example:"fwr_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

// ForwardingDelivery is one incoming SMS sent (or being sent) to one destination.
type ForwardingDelivery struct {
	ID                 string    `json:"id" example:"fdl_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	DestinationID      string    `json:"destination_id"`
	DestinationType    string    `json:"destination_type" enum:"phone,telegram,webhook,email"`
	MessageID          string    `json:"message_id" doc:"The incoming SMS."`
	Status             string    `json:"status" enum:"pending,retrying,succeeded,failed,skipped"`
	Attempts           int       `json:"attempts"`
	ResponseStatus     *int32    `json:"response_status" nullable:"true" doc:"HTTP status from Telegram or the webhook."`
	Error              *string   `json:"error" nullable:"true"`
	ForwardedMessageID *string   `json:"forwarded_message_id" nullable:"true" doc:"phone: the SMS Bridge sent."`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ForwardingDeliveryList struct {
	Data []ForwardingDelivery `json:"data"`
}

type ForwardingSecret struct {
	SigningSecret string `json:"signing_secret"`
}

func toForwardingRule(r automation.ForwardingRule) ForwardingRule {
	senders := r.Senders
	if senders == nil {
		senders = []string{}
	}
	out := ForwardingRule{
		ID: r.ID, Name: r.Name, Enabled: r.Enabled, Match: ForwardingMatch{Senders: senders, Contains: r.Contains},
		Destinations: make([]ForwardingDestination, 0, len(r.Destinations)), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	for _, d := range r.Destinations {
		fd := ForwardingDestination{ID: d.ID, Type: d.Type, BotTokenSet: d.Secret != nil}
		target := d.Target
		switch d.Type {
		case automation.DestPhone, automation.DestEmail:
			fd.To = &target
		case automation.DestTelegram:
			fd.ChatID = &target
		case automation.DestWebhook:
			fd.URL, fd.Format = &target, d.Format
		}
		out.Destinations = append(out.Destinations, fd)
	}
	return out
}

func (in *ForwardingMatchInput) apply(f *automation.ForwardingFields) {
	if in == nil {
		f.Senders, f.Contains = []string{}, nil
		return
	}
	f.Senders = in.Senders
	f.Contains = optString(in.Contains)
}

func destinationsIn(in []ForwardingDestinationInput) []automation.DestinationInput {
	if in == nil {
		return nil
	}
	out := make([]automation.DestinationInput, 0, len(in))
	for _, d := range in {
		out = append(out, automation.DestinationInput{
			ID: d.ID, Type: d.Type, To: d.To, ChatID: d.ChatID, BotToken: d.BotToken, URL: d.URL, Format: d.Format,
		})
	}
	return out
}

func automationError(err error, what string) error {
	var le *automation.LimitError
	switch {
	case errors.Is(err, automation.ErrNotFound):
		return notFound(what)
	case errors.Is(err, automation.ErrNoSMTP):
		return Errorf(http.StatusConflict, CodeConflict, "Email forwarding is not set up on this server. Set BRIDGE_SMTP_HOST, BRIDGE_SMTP_FROM "+
			"(and BRIDGE_SMTP_USERNAME/PASSWORD if the server needs them), then restart Bridge.")
	case errors.Is(err, automation.ErrNoSecretKey):
		return Errorf(http.StatusConflict, CodeConflict, "This server cannot store Telegram bot tokens yet. Set BRIDGE_SECRET_KEY (openssl rand -base64 32) and restart Bridge.")
	case errors.As(err, &le):
		return Errorf(http.StatusConflict, CodeConflict, le.Message)
	}
	return messagingError(err)
}

func (s *Server) registerAutomation(api huma.API) {
	s.registerOptOuts(api)
	s.registerAutoReplies(api)
	s.registerForwarding(api)
}

func (s *Server) registerOptOuts(api huma.API) {
	type optOutOutput struct {
		Status int
		Body   OptOut
	}
	add := func(ctx context.Context, projectID, raw, source string) (*optOutOutput, bool, error) {
		n, err := messaging.NormalizeE164(raw)
		if err != nil {
			var ve *messaging.ValidationError
			if errors.As(err, &ve) {
				return nil, false, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.number", Message: ve.Message})
			}
			return nil, false, err
		}
		o, created, err := s.tools.Automation.AddOptOut(ctx, projectID, n, source, nil)
		if err != nil {
			return nil, false, err
		}
		out := &optOutOutput{Status: http.StatusOK, Body: toOptOut(o)}
		if created {
			out.Status = http.StatusCreated
		}
		return out, created, nil
	}

	// ---- Developer API -------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "listOptOuts", Method: http.MethodGet, Path: "/v1/opt-outs", Tags: []string{"Automation"},
		Summary: "List opted-out numbers", Description: "Newest first. The list is shared by live and test.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *ListOptOutsQuery) (*struct{ Body OptOutList }, error) {
		return s.listOptOuts(ctx, principalFrom(ctx).APIKey.ProjectID, in)
	})

	huma.Register(api, huma.Operation{
		OperationID: "addOptOut", Method: http.MethodPost, Path: "/v1/opt-outs", Tags: []string{"Automation"},
		Summary:     "Opt a number out",
		Description: "Ordinary messages, broadcasts and schedules to the number are refused with `opted_out`; one-time passwords still go. Returns 200 with the existing entry if the number was already opted out.",
		Security:    apiKeyAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *struct{ Body OptOutInput }) (*optOutOutput, error) {
		out, _, err := add(ctx, principalFrom(ctx).APIKey.ProjectID, in.Body.Number, automation.SourceAPI)
		return out, err
	})

	huma.Register(api, huma.Operation{
		OperationID: "getOptOut", Method: http.MethodGet, Path: "/v1/opt-outs/{number}", Tags: []string{"Automation"},
		Summary: "Check whether a number opted out", Description: "404 when the number may be messaged.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *OptOutPath) (*struct{ Body OptOut }, error) {
		n := pathNumber(in.Number)
		o, err := s.tools.Automation.GetOptOut(ctx, principalFrom(ctx).APIKey.ProjectID, n)
		if err != nil {
			return nil, automationError(err, "Opt-out for "+n)
		}
		return &struct{ Body OptOut }{Body: toOptOut(o)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "removeOptOut", Method: http.MethodDelete, Path: "/v1/opt-outs/{number}", Tags: []string{"Automation"},
		Summary: "Opt a number back in", Description: "Only do this when the person asked to receive messages again.",
		Security: apiKeyAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *OptOutPath) (*struct{}, error) {
		n := pathNumber(in.Number)
		if _, err := s.tools.Automation.RemoveOptOut(ctx, principalFrom(ctx).APIKey.ProjectID, n); err != nil {
			return nil, automationError(err, "Opt-out for "+n)
		}
		return nil, nil
	})

	// ---- Dashboard ------------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "listProjectOptOuts", Method: http.MethodGet, Path: "/v1/projects/{projectId}/opt-outs", Tags: []string{"Automation"},
		Summary: "List opted-out numbers", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		ListOptOutsQuery
	}) (*struct{ Body OptOutList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.listOptOuts(ctx, in.ProjectID, &in.ListOptOutsQuery)
	})

	huma.Register(api, huma.Operation{
		OperationID: "exportProjectOptOuts", Method: http.MethodGet, Path: "/v1/projects/{projectId}/opt-outs/export", Tags: []string{"Automation"},
		Summary: "Export opted-out numbers as CSV", Description: "Columns: number, source, keyword, created_at.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
		Responses: map[string]*huma.Response{"200": {Description: "CSV file", Content: map[string]*huma.MediaType{"text/csv": {Schema: &huma.Schema{Type: "string"}}}}},
	}, func(ctx context.Context, in *ProjectPath) (*struct {
		ContentType        string `header:"Content-Type"`
		ContentDisposition string `header:"Content-Disposition"`
		Body               []byte
	}, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"number", "source", "keyword", "created_at"})
		after := ""
		for {
			rows, more, err := s.tools.Automation.ListOptOuts(ctx, automation.ListOptOutsRequest{ProjectID: in.ProjectID, StartingAfter: after, Limit: 1000})
			if err != nil {
				return nil, err
			}
			for _, o := range rows {
				_ = w.Write([]string{csvSafe(o.Number), o.Source, csvSafe(deref(o.Keyword)), o.CreatedAt.UTC().Format(time.RFC3339)})
			}
			if !more || len(rows) == 0 {
				break
			}
			after = rows[len(rows)-1].ID
		}
		w.Flush()
		return &struct {
			ContentType        string `header:"Content-Type"`
			ContentDisposition string `header:"Content-Disposition"`
			Body               []byte
		}{ContentType: "text/csv; charset=utf-8", ContentDisposition: `attachment; filename="opt-outs.csv"`, Body: buf.Bytes()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "addProjectOptOut", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/opt-outs", Tags: []string{"Automation"},
		Summary: "Opt a number out", Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body OptOutInput
	}) (*optOutOutput, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out, created, err := add(ctx, p.ID, in.Body.Number, automation.SourceManual)
		if err != nil || !created {
			return out, err
		}
		return out, s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "opt_out.added", TargetType: "opt_out", TargetID: out.Body.ID,
			Metadata: map[string]any{"number": out.Body.Number},
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "removeProjectOptOut", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/opt-outs/{number}", Tags: []string{"Automation"},
		Summary: "Opt a number back in", Description: "Only do this when the person asked to receive messages again.",
		Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectOptOutPath) (*struct{}, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		n := pathNumber(in.Number)
		o, err := s.tools.Automation.RemoveOptOut(ctx, p.ID, n)
		if err != nil {
			return nil, automationError(err, "Opt-out for "+n)
		}
		return nil, s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "opt_out.removed", TargetType: "opt_out", TargetID: o.ID,
			Metadata: map[string]any{"number": o.Number, "source": o.Source},
		})
	})
}

// csvSafe stops spreadsheet apps from running a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) && !isPhoneNumber(v) {
		return "'" + v
	}
	return v
}

func isPhoneNumber(v string) bool {
	return len(v) > 1 && v[0] == '+' && strings.Trim(v[1:], "0123456789") == ""
}

func (s *Server) listOptOuts(ctx context.Context, projectID string, in *ListOptOutsQuery) (*struct{ Body OptOutList }, error) {
	rows, more, err := s.tools.Automation.ListOptOuts(ctx, automation.ListOptOutsRequest{
		ProjectID: projectID, Source: in.Source, StartingAfter: in.StartingAfter, Limit: cmpOrInt(in.Limit, 50),
	})
	if err != nil {
		return nil, queryError(err)
	}
	out := OptOutList{Data: make([]OptOut, 0, len(rows)), HasMore: more}
	for _, o := range rows {
		out.Data = append(out.Data, toOptOut(o))
	}
	return &struct{ Body OptOutList }{Body: out}, nil
}

func (s *Server) registerAutoReplies(api huma.API) {
	ruleForUser := func(ctx context.Context, in *AutoReplyRulePath) (dbq.GetProjectForUserRow, dbq.AutoReplyRule, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return p, dbq.AutoReplyRule{}, err
		}
		r, err := s.tools.Automation.GetAutoReply(ctx, p.ID, in.RuleID)
		if err != nil {
			return p, r, automationError(err, "Auto-reply rule "+in.RuleID)
		}
		return p, r, nil
	}
	audit := func(ctx context.Context, p dbq.GetProjectForUserRow, action string, r dbq.AutoReplyRule) error {
		return s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: action, TargetType: "auto_reply_rule", TargetID: r.ID,
			Metadata: map[string]any{"name": r.Name, "keywords": r.Keywords, "action": r.Action, "enabled": r.Enabled},
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "listAutoReplyRules", Method: http.MethodGet, Path: "/v1/projects/{projectId}/auto-replies", Tags: []string{"Automation"},
		Summary: "List auto-reply rules",
		Description: "In the order they are tried. Every project starts with STOP/UNSUBSCRIBE/CANCEL/END/QUIT (opt out), START/UNSTOP (opt back in) " +
			"and HELP rules. Replies are never sent to sender IDs or short codes, and each rule answers a number at most once every 10 minutes.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body AutoReplyRuleList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		rows, err := s.tools.Automation.ListAutoReplies(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out := AutoReplyRuleList{Data: make([]AutoReplyRule, 0, len(rows))}
		for _, r := range rows {
			out.Data = append(out.Data, toAutoReplyRule(r))
		}
		return &struct{ Body AutoReplyRuleList }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createAutoReplyRule", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/auto-replies", Tags: []string{"Automation"},
		Summary: "Add an auto-reply rule", Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body AutoReplyRuleCreateInput
	}) (*struct{ Body AutoReplyRule }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		b := in.Body
		f := automation.RuleFields{Name: b.Name, Match: cmpOrStr(b.Match, automation.MatchExact), Keywords: b.Keywords, Reply: b.Reply,
			Action: cmpOrStr(b.Action, automation.ActionNone), Enabled: b.Enabled == nil || *b.Enabled, Priority: 100}
		if b.Priority != nil {
			f.Priority = *b.Priority
		}
		r, err := s.tools.Automation.CreateAutoReply(ctx, p.ID, f)
		if err != nil {
			return nil, automationError(err, "")
		}
		if err := audit(ctx, p, "auto_reply.created", r); err != nil {
			return nil, err
		}
		return &struct{ Body AutoReplyRule }{Body: toAutoReplyRule(r)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getAutoReplyRule", Method: http.MethodGet, Path: "/v1/projects/{projectId}/auto-replies/{ruleId}", Tags: []string{"Automation"},
		Summary: "Get an auto-reply rule", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *AutoReplyRulePath) (*struct{ Body AutoReplyRule }, error) {
		_, r, err := ruleForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		return &struct{ Body AutoReplyRule }{Body: toAutoReplyRule(r)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateAutoReplyRule", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/auto-replies/{ruleId}", Tags: []string{"Automation"},
		Summary: "Update an auto-reply rule", Description: "Omitted fields stay unchanged.", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		AutoReplyRulePath
		Body AutoReplyRuleUpdateInput
	}) (*struct{ Body AutoReplyRule }, error) {
		p, r, err := ruleForUser(ctx, &in.AutoReplyRulePath)
		if err != nil {
			return nil, err
		}
		f := automation.RuleFieldsOf(r)
		b := in.Body
		if b.Name != nil {
			f.Name = *b.Name
		}
		if b.Match != nil {
			f.Match = *b.Match
		}
		if b.Keywords != nil {
			f.Keywords = b.Keywords
		}
		if b.Reply != nil {
			f.Reply = b.Reply
		}
		if b.Action != nil {
			f.Action = *b.Action
		}
		if b.Enabled != nil {
			f.Enabled = *b.Enabled
		}
		if b.Priority != nil {
			f.Priority = *b.Priority
		}
		if r, err = s.tools.Automation.UpdateAutoReply(ctx, r, f); err != nil {
			return nil, automationError(err, "Auto-reply rule "+in.RuleID)
		}
		if err := audit(ctx, p, "auto_reply.updated", r); err != nil {
			return nil, err
		}
		return &struct{ Body AutoReplyRule }{Body: toAutoReplyRule(r)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteAutoReplyRule", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/auto-replies/{ruleId}", Tags: []string{"Automation"},
		Summary: "Delete an auto-reply rule", Description: "Default rules can be deleted too; they are not created again.",
		Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *AutoReplyRulePath) (*struct{}, error) {
		p, r, err := ruleForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if err := s.tools.Automation.DeleteAutoReply(ctx, p.ID, r.ID); err != nil {
			return nil, automationError(err, "Auto-reply rule "+in.RuleID)
		}
		return nil, audit(ctx, p, "auto_reply.deleted", r)
	})
}

func cmpOrStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func (s *Server) registerForwarding(api huma.API) {
	ruleForUser := func(ctx context.Context, in *ForwardingRulePath) (dbq.GetProjectForUserRow, automation.ForwardingRule, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return p, automation.ForwardingRule{}, err
		}
		r, err := s.tools.Automation.GetForwarding(ctx, p.ID, in.RuleID)
		if err != nil {
			return p, r, automationError(err, "Forwarding rule "+in.RuleID)
		}
		return p, r, nil
	}
	audit := func(ctx context.Context, p dbq.GetProjectForUserRow, action string, r automation.ForwardingRule) error {
		types := make([]string, 0, len(r.Destinations))
		for _, d := range r.Destinations {
			types = append(types, d.Type)
		}
		return s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: action, TargetType: "forwarding_rule", TargetID: r.ID,
			Metadata: map[string]any{"name": r.Name, "enabled": r.Enabled, "destinations": types},
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "listForwardingRules", Method: http.MethodGet, Path: "/v1/projects/{projectId}/forwarding-rules", Tags: []string{"Automation"},
		Summary: "List forwarding rules",
		Description: "Rules copy incoming SMS to a phone number, a Telegram chat, a webhook (JSON, Slack or Discord) or an email address. " +
			"Every matching rule runs; SMS from opted-out numbers are forwarded too.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body ForwardingRuleList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		rows, err := s.tools.Automation.ListForwarding(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out := ForwardingRuleList{Data: make([]ForwardingRule, 0, len(rows)), EmailAvailable: s.tools.Automation.SMTPReady(),
			TelegramAvailable: s.tools.Automation.SecretKeyReady()}
		for _, r := range rows {
			out.Data = append(out.Data, toForwardingRule(r))
		}
		return &struct{ Body ForwardingRuleList }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createForwardingRule", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/forwarding-rules", Tags: []string{"Automation"},
		Summary: "Add a forwarding rule", Description: "Returns the signing secret for webhook destinations; reveal it again later.",
		Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body ForwardingRuleCreateInput
	}) (*struct{ Body CreatedForwardingRule }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		f := automation.ForwardingFields{Name: in.Body.Name, Enabled: in.Body.Enabled == nil || *in.Body.Enabled,
			Destinations: destinationsIn(in.Body.Destinations)}
		in.Body.Match.apply(&f)
		r, err := s.tools.Automation.CreateForwarding(ctx, p.ID, f)
		if err != nil {
			return nil, automationError(err, "")
		}
		if err := audit(ctx, p, "forwarding_rule.created", r); err != nil {
			return nil, err
		}
		return &struct{ Body CreatedForwardingRule }{Body: CreatedForwardingRule{ForwardingRule: toForwardingRule(r), SigningSecret: r.SigningSecret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getForwardingRule", Method: http.MethodGet, Path: "/v1/projects/{projectId}/forwarding-rules/{ruleId}", Tags: []string{"Automation"},
		Summary: "Get a forwarding rule", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ForwardingRulePath) (*struct{ Body ForwardingRule }, error) {
		_, r, err := ruleForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		return &struct{ Body ForwardingRule }{Body: toForwardingRule(r)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateForwardingRule", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/forwarding-rules/{ruleId}", Tags: []string{"Automation"},
		Summary: "Update a forwarding rule", Description: "Omitted fields stay unchanged.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ForwardingRulePath
		Body ForwardingRuleUpdateInput
	}) (*struct{ Body ForwardingRule }, error) {
		p, r, err := ruleForUser(ctx, &in.ForwardingRulePath)
		if err != nil {
			return nil, err
		}
		f := automation.ForwardingFields{Name: r.Name, Enabled: r.Enabled, Senders: r.Senders, Contains: r.Contains,
			Destinations: destinationsIn(in.Body.Destinations)}
		if in.Body.Name != nil {
			f.Name = *in.Body.Name
		}
		if in.Body.Enabled != nil {
			f.Enabled = *in.Body.Enabled
		}
		if in.Body.Match != nil {
			in.Body.Match.apply(&f)
		}
		if r, err = s.tools.Automation.UpdateForwarding(ctx, r, f); err != nil {
			return nil, automationError(err, "Forwarding rule "+in.RuleID)
		}
		if err := audit(ctx, p, "forwarding_rule.updated", r); err != nil {
			return nil, err
		}
		return &struct{ Body ForwardingRule }{Body: toForwardingRule(r)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteForwardingRule", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/forwarding-rules/{ruleId}", Tags: []string{"Automation"},
		Summary: "Delete a forwarding rule", Description: "Pending deliveries are dropped.",
		Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ForwardingRulePath) (*struct{}, error) {
		p, r, err := ruleForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if err := s.tools.Automation.DeleteForwarding(ctx, p.ID, r.ID); err != nil {
			return nil, automationError(err, "Forwarding rule "+in.RuleID)
		}
		return nil, audit(ctx, p, "forwarding_rule.deleted", r)
	})

	huma.Register(api, huma.Operation{
		OperationID: "listForwardingDeliveries", Method: http.MethodGet, Path: "/v1/projects/{projectId}/forwarding-rules/{ruleId}/deliveries", Tags: []string{"Automation"},
		Summary: "List a rule's deliveries", Description: "Newest first; one entry per incoming SMS and destination, updated on every attempt. Failed deliveries are retried for about four hours.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ForwardingRulePath
		Limit int `query:"limit" minimum:"1" maximum:"100" default:"50"`
	}) (*struct{ Body ForwardingDeliveryList }, error) {
		_, r, err := ruleForUser(ctx, &in.ForwardingRulePath)
		if err != nil {
			return nil, err
		}
		rows, err := s.tools.Automation.Deliveries(ctx, r.ID, cmpOrInt(in.Limit, 50))
		if err != nil {
			return nil, err
		}
		out := make([]ForwardingDelivery, 0, len(rows))
		for _, d := range rows {
			out = append(out, ForwardingDelivery{
				ID: d.ID, DestinationID: d.DestinationID, DestinationType: d.DestinationType, MessageID: d.MessageID, Status: d.Status,
				Attempts: int(d.Attempts), ResponseStatus: d.ResponseStatus, Error: d.Error, ForwardedMessageID: d.ForwardedMessageID,
				CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
			})
		}
		return &struct{ Body ForwardingDeliveryList }{Body: ForwardingDeliveryList{Data: out}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getForwardingRuleSecret", Metadata: adminOnly, Method: http.MethodGet, Path: "/v1/projects/{projectId}/forwarding-rules/{ruleId}/secret", Tags: []string{"Automation"},
		Summary: "Reveal the signing secret", Description: "Every reveal is recorded in the audit log.", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ForwardingRulePath) (*struct{ Body ForwardingSecret }, error) {
		p, r, err := ruleForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "forwarding_rule.secret_revealed", TargetType: "forwarding_rule", TargetID: r.ID,
		}); err != nil {
			return nil, err
		}
		return &struct{ Body ForwardingSecret }{Body: ForwardingSecret{SigningSecret: r.SigningSecret}}, nil
	})
}
