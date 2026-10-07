// Package automation reacts to incoming SMS and keeps the opt-out list:
//
//   - Opt-outs: numbers that asked not to be messaged. messaging.Send refuses
//     ordinary messages to them; one-time passwords and auto-replies still go.
//   - Auto-reply rules: keyword rules (STOP, START, HELP by default) that
//     answer through the phone that received the SMS and can opt the sender
//     out or back in.
//   - Forwarding rules: copy matching incoming SMS to a phone number, a
//     Telegram chat, a webhook (JSON, Slack or Discord) or an email address.
//
// Incoming SMS are processed by a job queued by messaging.DeviceInbound, so
// the phone's acknowledgement never waits for rules or slow destinations.
package automation

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"bridge/internal/config"
	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
	"bridge/internal/push"
	"bridge/internal/secretbox"
)

// DefaultTelegramURL is Telegram's Bot API.
const DefaultTelegramURL = "https://api.telegram.org"

var (
	ErrNotFound = errors.New("not found")
	// ErrNoSecretKey: a Telegram bot token cannot be stored without BRIDGE_SECRET_KEY.
	ErrNoSecretKey = errors.New("BRIDGE_SECRET_KEY is not set")
	// ErrNoSMTP: email destinations need BRIDGE_SMTP_HOST and friends.
	ErrNoSMTP = errors.New("SMTP is not configured")
)

// LimitError reports that a project has as many of something as allowed.
type LimitError struct{ Message string }

func (e *LimitError) Error() string { return e.Message }

// JobInserter is satisfied by *river.Client.
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

type Service struct {
	pool         *pgxpool.Pool
	q            *dbq.Queries
	log          *slog.Logger
	msgs         *messaging.Service
	box          *secretbox.Box
	jobs         JobInserter
	smtp         *config.SMTPConfig
	telegramURL  string
	telegram     *http.Client // Telegram's fixed host
	hooks        *http.Client // user-chosen URLs: refuses private addresses unless allowed
	allowPrivate bool
	now          func() time.Time
}

type Options struct {
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Messaging *messaging.Service
	// Box seals Telegram bot tokens; without a key they cannot be stored.
	Box *secretbox.Box
	// AllowPrivate lets webhook destinations resolve to private addresses
	// (BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS).
	AllowPrivate bool
	SMTP         *config.SMTPConfig
	TelegramURL  string // defaults to DefaultTelegramURL
}

func New(o Options) *Service {
	box := o.Box
	if box == nil {
		box, _ = secretbox.New(nil)
	}
	hooks := push.SafeClient(o.AllowPrivate)
	hooks.Timeout = requestTimeout
	return &Service{
		pool: o.Pool, q: dbq.New(o.Pool), log: o.Logger, msgs: o.Messaging, box: box, smtp: o.SMTP,
		telegramURL: strings.TrimRight(cmpOr(o.TelegramURL, DefaultTelegramURL), "/"),
		telegram:    &http.Client{Timeout: requestTimeout}, hooks: hooks, allowPrivate: o.AllowPrivate, now: time.Now,
	}
}

// SetJobInserter wires the River client, which is created after the service.
func (s *Service) SetJobInserter(j JobInserter) { s.jobs = j }

// SMTPReady reports whether email destinations can be used.
func (s *Service) SMTPReady() bool { return s.smtp != nil }

// SecretKeyReady reports whether Telegram bot tokens can be stored.
func (s *Service) SecretKeyReady() bool { return s.box.Ready() }

// Register adds the automation workers.
func Register(workers *river.Workers, s *Service) {
	river.AddWorker(workers, &InboundWorker{svc: s})
	river.AddWorker(workers, &ForwardWorker{svc: s})
}

// DeleteOld removes forwarding delivery logs older than before.
func (s *Service) DeleteOld(ctx context.Context, before time.Time) (int64, error) {
	return s.q.DeleteOldForwardingDeliveries(ctx, before)
}

func fieldErr(field, msg string) error { return &messaging.ValidationError{Field: field, Message: msg} }

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func ptr[T any](v T) *T { return &v }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
