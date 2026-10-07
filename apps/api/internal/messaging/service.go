// Package messaging owns the outbound SMS pipeline: accepting messages,
// choosing a device, delivering jobs to phones, and applying the status
// reports they send back. Every status change goes through
// message.Transition and the TransitionMessage query.
package messaging

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nyaruka/phonenumbers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/id"
	"bridge/internal/message"
	"bridge/internal/push"
	"bridge/internal/ratelimit"
)

const (
	ProviderAndroid   = "android"
	ProviderSimulator = "simulator"

	MaxBodyLength   = 1600 // characters; about 10 GSM-7 segments
	maxMetadataSize = 4096 // bytes of JSON
	maxMetadataKeys = 32
)

// Config tunes the pipeline. Zero values take the defaults.
type Config struct {
	MaxAttempts   int           // device assignments per message before it fails
	AssignTimeout time.Duration // how long a phone has to accept a job
	QueueTimeout  time.Duration // how long a message may wait for any phone
	Retention     time.Duration // message bodies are redacted after this
}

func (c Config) withDefaults() Config {
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 3
	}
	if c.AssignTimeout == 0 {
		c.AssignTimeout = 2 * time.Minute
	}
	if c.QueueTimeout == 0 {
		c.QueueTimeout = time.Hour
	}
	if c.Retention == 0 {
		c.Retention = 30 * 24 * time.Hour
	}
	return c
}

// JobInserter is satisfied by *river.Client.
type JobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// Publisher delivers a frame to a device wherever it is connected.
type Publisher func(ctx context.Context, deviceID string, frame gateway.Outbound) error

// Waker wakes an offline device through push.
type Waker interface {
	Wake(ctx context.Context, t push.Target, reason string) error
}

type Service struct {
	pool    *pgxpool.Pool
	q       *dbq.Queries
	log     *slog.Logger
	jobs    JobInserter
	publish Publisher
	waker   Waker
	emitter Emitter
	limiter ratelimit.Limiter
	// providers is nil when the server cannot use SMS providers.
	providers Providers
	cfg       Config
	now       func() time.Time
}

type Options struct {
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Publisher Publisher
	Waker     Waker   // optional
	Emitter   Emitter // optional: webhook events
	Limiter   ratelimit.Limiter
	Providers Providers // optional: SMS providers as a fallback for phones
	Config    Config
}

func New(o Options) *Service {
	q := dbq.New(o.Pool)
	limiter := o.Limiter
	if limiter == nil {
		limiter = ratelimit.NewPostgres(q)
	}
	return &Service{
		pool: o.Pool, q: q, log: o.Logger, publish: o.Publisher, waker: o.Waker, emitter: o.Emitter, providers: o.Providers,
		limiter: limiter, cfg: o.Config.withDefaults(), now: time.Now,
	}
}

// SetJobInserter wires the River client. The worker creates its client after
// the service, so this cannot be a constructor argument.
func (s *Service) SetJobInserter(j JobInserter) { s.jobs = j }

// Retention is how long message bodies are kept.
func (s *Service) Retention() time.Duration { return s.cfg.Retention }

// SweepInterval is how often unaccepted assignments are reclaimed.
func (s *Service) SweepInterval() time.Duration {
	return max(time.Second, min(s.cfg.AssignTimeout/4, 30*time.Second))
}

// ValidationError reports an invalid request field.
type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// RateLimitError reports which limit was hit.
type RateLimitError struct {
	Scope      string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "rate limited: " + e.Scope }

// ErrIdempotencyConflict means the key was used with a different request.
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

// SendRequest is a request to send one SMS.
type SendRequest struct {
	ProjectID      string
	Environment    dbq.APIEnvironment
	APIKeyID       *string
	To             string
	Body           string
	DeviceID       *string
	SimSlot        *int16
	Metadata       map[string]any
	IdempotencyKey string

	// Purpose is "otp" for one-time passwords; empty means an ordinary message.
	Purpose string
	// DisplayBody, when set, is what the API and webhooks show instead of Body.
	DisplayBody *string
	// BodyVars are template variables for providers that send registered
	// templates (MSG91), such as {"code": "482913"}. Redacted with the body.
	BodyVars map[string]string
	// OnCreate runs inside the transaction that inserts the message, so a
	// caller's own rows commit or roll back together with it.
	OnCreate func(ctx context.Context, q *dbq.Queries, msg dbq.Message) error
}

// Message purposes.
const (
	PurposeMessage = "message"
	PurposeOTP     = "otp"
)

// Rate limits on accepting messages. Device capacity is enforced at dispatch.
const (
	projectHourlyLimit     = 1000
	destinationHourlyLimit = 20
)

// Send validates and queues a message. replayed is true when an earlier
// request with the same idempotency key is returned instead.
func (s *Service) Send(ctx context.Context, r SendRequest) (msg dbq.Message, replayed bool, err error) {
	to, err := NormalizeE164(r.To)
	if err != nil {
		return msg, false, err
	}
	if strings.TrimSpace(r.Body) == "" {
		return msg, false, &ValidationError{"message", "The message cannot be empty."}
	}
	if n := len([]rune(r.Body)); n > MaxBodyLength {
		return msg, false, &ValidationError{"message", fmt.Sprintf("The message is %d characters; the limit is %d (about 10 SMS segments).", n, MaxBodyLength)}
	}
	if r.SimSlot != nil && *r.SimSlot != 1 && *r.SimSlot != 2 {
		return msg, false, &ValidationError{"sim_slot", "Use 1 or 2, or leave it out to use the phone's default SIM."}
	}
	meta := []byte("{}")
	if r.Metadata != nil {
		if len(r.Metadata) > maxMetadataKeys {
			return msg, false, &ValidationError{"metadata", fmt.Sprintf("Use at most %d keys.", maxMetadataKeys)}
		}
		if meta, err = json.Marshal(r.Metadata); err != nil || len(meta) > maxMetadataSize {
			return msg, false, &ValidationError{"metadata", fmt.Sprintf("Metadata must be a JSON object of at most %d bytes.", maxMetadataSize)}
		}
	}

	var reqHash []byte
	if r.IdempotencyKey != "" {
		reqHash = requestHash(to, r)
		existing, err := s.q.GetMessageByIdempotencyKey(ctx, dbq.GetMessageByIdempotencyKeyParams{ProjectID: r.ProjectID, IdempotencyKey: &r.IdempotencyKey})
		if err == nil {
			if string(existing.IdempotencyHash) != string(reqHash) {
				return msg, false, ErrIdempotencyConflict
			}
			return existing, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return msg, false, err
		}
	}

	if r.DeviceID != nil {
		d, err := s.q.GetDevice(ctx, dbq.GetDeviceParams{ID: *r.DeviceID, ProjectID: r.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && d.RevokedAt != nil) {
			return msg, false, &ValidationError{"device_id", "No active device with this ID in the project."}
		}
		if err != nil {
			return msg, false, err
		}
	}

	if err := s.limit(ctx, "msg:project:"+r.ProjectID, projectHourlyLimit, "project"); err != nil {
		return msg, false, err
	}
	if err := s.limit(ctx, "msg:dest:"+r.ProjectID+":"+to, destinationHourlyLimit, "destination"); err != nil {
		return msg, false, err
	}

	encoding, segments := message.Segments(r.Body)
	seg := int16(segments)
	sum := sha256.Sum256([]byte(r.Body))
	provider := ProviderAndroid
	if r.Environment == dbq.ApiEnvironmentTest {
		provider = ProviderSimulator
	}
	params := dbq.InsertMessageParams{
		ID: id.New(id.Message), ProjectID: r.ProjectID, Environment: r.Environment, Provider: provider,
		APIKeyID: r.APIKeyID, RequestedDeviceID: r.DeviceID, Recipient: to, Body: r.Body,
		Segments: &seg, Encoding: &encoding, Metadata: meta, SimSlot: r.SimSlot,
		BodySha256: sum[:], BodyLength: ptr(int32(len([]rune(r.Body)))),
		Purpose: cmp.Or(r.Purpose, PurposeMessage), DisplayBody: r.DisplayBody,
	}
	if len(r.BodyVars) > 0 {
		params.BodyVars, _ = json.Marshal(r.BodyVars)
	}
	if r.IdempotencyKey != "" {
		params.IdempotencyKey, params.IdempotencyHash = &r.IdempotencyKey, reqHash
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return msg, false, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	msg, err = q.InsertMessage(ctx, params)
	if db.IsUniqueViolation(err, "") && r.IdempotencyKey != "" {
		// A concurrent request with the same key won; return its message.
		_ = tx.Rollback(ctx)
		existing, getErr := s.q.GetMessageByIdempotencyKey(ctx, dbq.GetMessageByIdempotencyKeyParams{ProjectID: r.ProjectID, IdempotencyKey: &r.IdempotencyKey})
		if getErr != nil {
			return msg, false, getErr
		}
		if string(existing.IdempotencyHash) != string(reqHash) {
			return msg, false, ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	if err != nil {
		return msg, false, err
	}
	created := message.Created
	if err := s.event(ctx, q, msg, "created", nil, &created, map[string]any{"encoding": encoding, "segments": segments}); err != nil {
		return msg, false, err
	}
	if err := s.event(ctx, q, msg, "queued", &created, ptr(message.Queued), nil); err != nil {
		return msg, false, err
	}
	if r.OnCreate != nil {
		if err := r.OnCreate(ctx, q, msg); err != nil {
			return msg, false, err
		}
	}
	var job river.JobArgs = DispatchArgs{MessageID: msg.ID}
	if provider == ProviderSimulator {
		job = SimulateArgs{MessageID: msg.ID}
	}
	if _, err := s.jobs.InsertTx(ctx, tx, job, nil); err != nil {
		return msg, false, fmt.Errorf("queue message job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return msg, false, err
	}
	s.log.Info("message queued", "message_id", msg.ID, "project_id", msg.ProjectID, "environment", msg.Environment,
		"to", maskNumber(to), "segments", segments)
	return msg, false, nil
}

// NormalizeE164 validates a destination and returns it in E.164 form.
func NormalizeE164(raw string) (string, error) {
	s := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '(' || r == ')' || r == '.' {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	if !strings.HasPrefix(s, "+") {
		return "", &ValidationError{"to", "Use international E.164 format with a country code, for example +919876543210."}
	}
	num, err := phonenumbers.Parse(s, "")
	if err != nil || !phonenumbers.IsPossibleNumber(num) {
		return "", &ValidationError{"to", "This is not a possible phone number. Check the country code and the number of digits."}
	}
	return phonenumbers.Format(num, phonenumbers.E164), nil
}

func requestHash(to string, r SendRequest) []byte {
	payload, _ := json.Marshal(map[string]any{
		"to": to, "body": r.Body, "device_id": r.DeviceID, "sim_slot": r.SimSlot, "metadata": r.Metadata,
	})
	sum := sha256.Sum256(payload)
	return sum[:]
}

func (s *Service) limit(ctx context.Context, key string, n int, scope string) error {
	res, err := s.limiter.Hit(ctx, key, n, time.Hour)
	if err != nil {
		s.log.Warn("rate limiter unavailable; allowing message", "error", err)
	}
	if !res.Allowed {
		return &RateLimitError{Scope: scope, RetryAfter: res.RetryAfter}
	}
	return nil
}

// transition moves a message from its current status to `to`, recording an
// event in the same transaction. ok is false when the message changed
// concurrently. Pass s.q to run in a transaction of its own, after which the
// change is announced to webhooks; pass a transaction's queries to make the
// change part of it (used only for returns to the queue, which have no webhook).
func (s *Service) transition(ctx context.Context, q *dbq.Queries, m dbq.Message, to message.Status, eventType string,
	detail map[string]any, failCode, failMessage string, segments *int16,
) (dbq.Message, bool, error) {
	if err := message.Transition(m.Status, to); err != nil {
		return m, false, err
	}
	if q != s.q {
		return s.applyTransition(ctx, q, m, to, eventType, detail, failCode, failMessage, segments)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return m, false, err
	}
	defer tx.Rollback(ctx)
	updated, ok, err := s.applyTransition(ctx, s.q.WithTx(tx), m, to, eventType, detail, failCode, failMessage, segments)
	if err != nil || !ok {
		return m, ok, err
	}
	if err := tx.Commit(ctx); err != nil {
		return m, false, err
	}
	if ev := statusEvent(to); ev != "" {
		s.emit(ctx, updated, ev)
	}
	return updated, true, nil
}

func (s *Service) applyTransition(ctx context.Context, q *dbq.Queries, m dbq.Message, to message.Status, eventType string,
	detail map[string]any, failCode, failMessage string, segments *int16,
) (dbq.Message, bool, error) {
	params := dbq.TransitionMessageParams{ID: m.ID, ToStatus: to, FromStatuses: []string{string(m.Status)}, Segments: segments}
	if to == message.Failed {
		params.ErrorCode, params.ErrorMessage = &failCode, &failMessage
	}
	updated, err := q.TransitionMessage(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	from := m.Status
	if to == message.Failed {
		if detail == nil {
			detail = map[string]any{}
		}
		detail["error_code"], detail["error_message"] = failCode, failMessage
	}
	if err := s.event(ctx, q, updated, eventType, &from, &to, detail); err != nil {
		return updated, true, err
	}
	s.log.Info("message status changed", "message_id", m.ID, "from", from, "to", to, "event", eventType)
	return updated, true, nil
}

func (s *Service) event(ctx context.Context, q *dbq.Queries, m dbq.Message, typ string, from, to *message.Status, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return q.InsertMessageEvent(ctx, dbq.InsertMessageEventParams{
		ID: id.New(id.MessageEvent), MessageID: m.ID, ProjectID: m.ProjectID, Type: typ,
		FromStatus: from, ToStatus: to, Detail: raw,
	})
}

func ptr[T any](v T) *T { return &v }

// maskNumber keeps only the last three digits for logs.
func maskNumber(n string) string {
	if len(n) <= 4 {
		return "***"
	}
	return n[:3] + strings.Repeat("•", len(n)-6) + n[len(n)-3:]
}
