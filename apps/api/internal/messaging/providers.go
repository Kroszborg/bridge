package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/message"
	"bridge/internal/provider"
)

// ProviderFallback marks a live message handed to the project's providers
// that none has accepted yet. Dispatch leaves such messages alone.
const ProviderFallback = "fallback"

// Routing modes (project_routing.mode).
const (
	RoutePhones          = "phones"
	RoutePhonesProviders = "phones_then_providers"
	RouteProviders       = "providers"
)

// Providers resolves a project's provider accounts. *provider.Router satisfies it.
type Providers interface {
	Accounts(ctx context.Context, projectID string) ([]provider.Account, error)
	Record(ctx context.Context, accountID string, err error)
}

type route struct {
	fallback bool          // providers may take messages phones cannot send
	only     bool          // providers take every message
	wait     time.Duration // how long a message may wait for a phone first
}

// routeFor reports how a message may use providers. Only live messages that
// did not ask for a specific phone, in projects with an enabled provider, can.
func (s *Service) routeFor(ctx context.Context, m dbq.Message) route {
	if s.providers == nil || m.Environment != dbq.ApiEnvironmentLive || m.RequestedDeviceID != nil {
		return route{}
	}
	r, err := s.q.GetRouting(ctx, m.ProjectID)
	if err != nil || r.Mode == RoutePhones {
		return route{}
	}
	accounts, err := s.q.EnabledProviderAccounts(ctx, m.ProjectID)
	if err != nil || len(accounts) == 0 {
		return route{}
	}
	return route{fallback: true, only: r.Mode == RouteProviders, wait: time.Duration(r.FallbackAfterSeconds) * time.Second}
}

// handoff releases a message from any phone and queues it for the providers.
func (s *Service) handoff(ctx context.Context, m dbq.Message, reason string) error {
	return s.requeueWith(ctx, m, "provider_fallback", map[string]any{"reason": reason}, ProviderSendArgs{MessageID: m.ID},
		func(q *dbq.Queries) error {
			_, err := q.SetMessageProvider(ctx, dbq.SetMessageProviderParams{ID: m.ID, Provider: ProviderFallback})
			return err
		})
}

// ProviderSendArgs sends a message through the project's providers.
type ProviderSendArgs struct {
	MessageID string `json:"message_id"`
}

func (ProviderSendArgs) Kind() string { return "message.provider_send" }

func (ProviderSendArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 6, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour}}
}

type ProviderSendWorker struct {
	river.WorkerDefaults[ProviderSendArgs]
	Service *Service
}

func (w *ProviderSendWorker) Work(ctx context.Context, job *river.Job[ProviderSendArgs]) error {
	return w.Service.ProviderSend(ctx, job.Args.MessageID, job.Attempt >= job.MaxAttempts)
}

// ProviderSend tries the project's providers in priority order. A retryable
// failure everywhere returns an error so the job runs again later, unless
// this is the last attempt; any other outcome is final.
func (s *Service) ProviderSend(ctx context.Context, messageID string, lastAttempt bool) error {
	m, err := s.q.GetMessageByID(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.Status != message.Queued || m.Provider != ProviderFallback {
		return nil // already sent, or taken back
	}
	accounts, err := s.providers.Accounts(ctx, m.ProjectID)
	if err != nil {
		return err
	}
	out := provider.Outgoing{MessageID: m.ID, To: m.Recipient, Body: m.Body, Purpose: m.Purpose,
		Unicode: m.Encoding != nil && *m.Encoding == "ucs2"}
	if len(m.BodyVars) > 0 {
		_ = json.Unmarshal(m.BodyVars, &out.Vars)
	}

	var last *provider.Error
	retry := false
	for _, a := range accounts {
		out.CallbackURL = a.CallbackURL
		acc, err := a.Client.Send(ctx, out)
		s.providers.Record(ctx, a.ID, err)
		if err == nil {
			return s.providerAccepted(ctx, m, a, acc.ExternalID)
		}
		var pe *provider.Error
		if !errors.As(err, &pe) {
			return err // context canceled and the like: let River retry
		}
		last, retry = pe, retry || pe.Retryable
		s.log.Warn("provider refused message", "message_id", m.ID, "provider", a.Kind, "code", pe.Code, "retryable", pe.Retryable)
	}
	if retry && !lastAttempt {
		return errors.New("every provider failed; retrying: " + last.Error())
	}
	code, text := "no_provider", "No SMS provider is enabled for this project."
	if last != nil {
		code, text = clipText(last.Code, 40, "provider_failed"), clipText(last.Message, 300, "The provider could not send the message.")
	}
	_, _, err = s.transition(ctx, s.q, m, message.Failed, "failed", map[string]any{"provider_error": code}, code, text, nil)
	return err
}

func (s *Service) providerAccepted(ctx context.Context, m dbq.Message, a provider.Account, externalID string) error {
	m, err := s.q.SetMessageProvider(ctx, dbq.SetMessageProviderParams{
		ID: m.ID, Provider: string(a.Kind), ProviderAccountID: &a.ID, ProviderMessageID: &externalID,
	})
	if err != nil {
		return err
	}
	detail := map[string]any{"provider": string(a.Kind), "provider_account_id": a.ID}
	if m, _, err = s.transition(ctx, s.q, m, message.Sending, "provider_accepted", detail, "", "", nil); err != nil {
		return err
	}
	// Accepted by a provider counts as sent, as a phone's "sent" report does;
	// a delivery report, if the provider sends one, finishes the message.
	_, _, err = s.transition(ctx, s.q, m, message.Sent, "sent", detail, "", "", m.Segments)
	return err
}

// ProviderReport applies delivery reports from a provider account's callback.
func (s *Service) ProviderReport(ctx context.Context, accountID string, updates []provider.Update) error {
	for _, u := range updates {
		m, err := s.q.GetMessageByProviderID(ctx, dbq.GetMessageByProviderIDParams{ProviderAccountID: &accountID, ProviderMessageID: &u.ExternalID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		detail := map[string]any{"provider": m.Provider}
		switch {
		case u.Status == provider.StatusDelivered && m.Status == message.Sent:
			_, _, err = s.transition(ctx, s.q, m, message.Delivered, "delivered", detail, "", "", nil)
		case u.Status == provider.StatusFailed && (m.Status == message.Sent || m.Status == message.Sending):
			_, _, err = s.transition(ctx, s.q, m, message.Failed, "delivery_failed", detail,
				sanitizeCode(u.ErrorCode, "delivery_failed"), clipText(u.ErrorMessage, 300, "The provider reported that the message was not delivered."), nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
