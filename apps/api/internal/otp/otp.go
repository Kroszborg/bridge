// Package otp sends one-time passwords as SMS and checks them.
//
// Bridge generates the code, sends it through the messaging pipeline, and
// stores only an HMAC of it under a key generated once per installation. A
// code can be checked a limited number of times before it expires; once a
// verification finishes, its hash is erased and the message body (which
// contains the code) is redacted as soon as the phone is done with it.
//
// Codes belong to Verify apps (apps.go): each has its own template, limits,
// fraud protection (fraud.go), delivery failover (failover.go) and drop-in
// widget, whose successful checks are proven with signed tokens (token.go).
package otp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
	"bridge/internal/ratelimit"
	"bridge/internal/secretbox"
	"bridge/internal/turnstile"
)

const (
	// ResendCooldown is the minimum time between two codes to one number.
	ResendCooldown = 30 * time.Second
	// DestinationHourlyLimit caps codes per number per hour, which also caps
	// how many guesses an attacker gets: attempts × this.
	DestinationHourlyLimit = 5
	// HistoryRetention is how long finished verifications are kept.
	HistoryRetention = 30 * 24 * time.Hour

	DefaultCodeLength  = 6
	DefaultTTL         = 10 * time.Minute
	DefaultMaxAttempts = 5
	DefaultTemplate    = "{code} is your {app} code. It expires in {minutes} minutes. Do not share it."

	maxTemplateLength = 300
	maxAppNameLength  = 40
	hmacKeyName       = "otp_hmac"
)

var (
	// ErrNotFound means no verification matched, or none is pending for the number.
	ErrNotFound = errors.New("verification not found")

	androidHashPattern = regexp.MustCompile(`^[A-Za-z0-9+/]{11}$`)
	domainPattern      = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	placeholderPattern = regexp.MustCompile(`\{[^{}]*\}`)
)

// Service sends and verifies codes.
type Service struct {
	pool      *pgxpool.Pool
	q         *dbq.Queries
	msgs      *messaging.Service
	limiter   ratelimit.Limiter
	log       *slog.Logger
	emitter   Emitter
	box       *secretbox.Box
	turnstile *turnstile.Verifier
	issuer    string
	now       func() time.Time

	keyMu sync.Mutex
	key   []byte
}

// Emitter announces verification events (otp.verified, otp.failed,
// otp.expired, otp.blocked) to webhooks and the event stream.
// *webhook.Service satisfies it.
type Emitter interface {
	Emit(ctx context.Context, projectID, eventType string, data any) error
}

// Options configure the service. Limiter, Emitter and Box may be nil.
type Options struct {
	Pool      *pgxpool.Pool
	Messaging *messaging.Service
	Limiter   ratelimit.Limiter
	Emitter   Emitter
	Logger    *slog.Logger
	// Box seals apps' signing and Turnstile secrets (BRIDGE_SECRET_KEY).
	Box *secretbox.Box
	// Issuer is the iss claim of verification tokens: the public API URL.
	Issuer string
	// TurnstileURL replaces Cloudflare's siteverify endpoint (tests).
	TurnstileURL string
	HTTP         *http.Client
}

// New builds the service.
func New(o Options) *Service {
	q := dbq.New(o.Pool)
	limiter := o.Limiter
	if limiter == nil {
		limiter = ratelimit.NewPostgres(q)
	}
	box := o.Box
	if box == nil {
		box, _ = secretbox.New(nil)
	}
	return &Service{
		pool: o.Pool, q: q, msgs: o.Messaging, limiter: limiter, emitter: o.Emitter, log: o.Logger, box: box,
		turnstile: turnstile.New(o.TurnstileURL, o.HTTP), issuer: o.Issuer, now: time.Now,
	}
}

// ---- Settings --------------------------------------------------------------

// Settings controls how an app's codes look and behave.
type Settings struct {
	AppName      string        // shown as {app}; the project name when empty
	Template     string        // DefaultTemplate when empty
	CodeLength   int           // 4 to 10 digits
	TTL          time.Duration // 1 to 60 minutes
	MaxAttempts  int           // 1 to 10
	WebOTPDomain string        // optional: adds "@domain #code" for browser autofill
	Custom       bool          // false while the project uses the defaults
}

// EffectiveAppName and EffectiveTemplate resolve the defaults.
func (s Settings) EffectiveAppName(projectName string) string {
	if s.AppName != "" {
		return s.AppName
	}
	return projectName
}

func (s Settings) EffectiveTemplate() string {
	if s.Template != "" {
		return s.Template
	}
	return DefaultTemplate
}

// Defaults are the settings of an app that never changed them.
func Defaults() Settings {
	return Settings{CodeLength: DefaultCodeLength, TTL: DefaultTTL, MaxAttempts: DefaultMaxAttempts}
}

// LoadSettings returns the code settings of the project's default app.
func (s *Service) LoadSettings(ctx context.Context, projectID string) (Settings, error) {
	app, err := s.DefaultApp(ctx, projectID)
	if err != nil {
		return Settings{}, err
	}
	return SettingsOf(app), nil
}

// Validate checks settings before they are saved.
func (s Settings) Validate() error {
	if n := len([]rune(s.AppName)); n > maxAppNameLength {
		return &messaging.ValidationError{Field: "app_name", Message: fmt.Sprintf("Use at most %d characters.", maxAppNameLength)}
	}
	if s.Template != "" {
		if err := ValidateTemplate(s.Template); err != nil {
			return err
		}
	}
	if s.CodeLength < 4 || s.CodeLength > 10 {
		return &messaging.ValidationError{Field: "code_length", Message: "Use 4 to 10 digits."}
	}
	if s.TTL < time.Minute || s.TTL > time.Hour {
		return &messaging.ValidationError{Field: "ttl_seconds", Message: "Use between 60 seconds and 1 hour."}
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > 10 {
		return &messaging.ValidationError{Field: "max_attempts", Message: "Use 1 to 10 attempts."}
	}
	if s.WebOTPDomain != "" && !domainPattern.MatchString(s.WebOTPDomain) {
		return &messaging.ValidationError{Field: "web_otp_domain", Message: "Use a bare domain such as example.com, without https:// or a path."}
	}
	return nil
}

// SaveSettings validates and stores the code settings of the project's
// default app, keeping its other configuration.
func (s *Service) SaveSettings(ctx context.Context, projectID string, in Settings) (Settings, error) {
	app, err := s.DefaultApp(ctx, projectID)
	if err != nil {
		return Settings{}, err
	}
	c := ConfigOf(app)
	c.Settings = in
	if app, err = s.UpdateApp(ctx, app, c, nil); err != nil {
		return Settings{}, err
	}
	return SettingsOf(app), nil
}

// ValidateTemplate requires exactly one {code} and only known placeholders.
func ValidateTemplate(tpl string) error {
	if n := len([]rune(tpl)); n > maxTemplateLength {
		return &messaging.ValidationError{Field: "template", Message: fmt.Sprintf("Use at most %d characters.", maxTemplateLength)}
	}
	if c := strings.Count(tpl, "{code}"); c != 1 {
		return &messaging.ValidationError{Field: "template", Message: "The template must contain {code} exactly once."}
	}
	for _, p := range placeholderPattern.FindAllString(tpl, -1) {
		if p != "{code}" && p != "{app}" && p != "{minutes}" {
			return &messaging.ValidationError{Field: "template", Message: "Unknown placeholder " + p + ". Use {code}, {app} and {minutes}."}
		}
	}
	return nil
}

func minutesOf(ttl time.Duration) int { return int((ttl + time.Minute - 1) / time.Minute) }

// Render builds the SMS text. An Android app hash (SMS Retriever) goes on the
// last line; otherwise a WebOTP domain adds "@domain #code" for browser
// autofill. Both require being last, so the app hash wins.
func Render(tpl, app, code string, ttl time.Duration, androidHash, domain string) string {
	minutes := minutesOf(ttl)
	body := strings.NewReplacer("{code}", code, "{app}", app, "{minutes}", strconv.Itoa(minutes)).Replace(tpl)
	switch {
	case androidHash != "":
		body += "\n" + androidHash
	case domain != "":
		body += "\n\n@" + domain + " #" + code
	}
	return body
}

// ---- Sending ---------------------------------------------------------------

// SendRequest asks for a new code to one number.
type SendRequest struct {
	ProjectID   string
	ProjectName string
	Environment dbq.APIEnvironment
	APIKeyID    *string
	// App is an app ID (vap_...) or slug; empty means the default app.
	App            string
	To             string
	AndroidAppHash string
	Metadata       map[string]any
	// ClientIP is the end user's address, for the per-IP limit; zero skips it.
	ClientIP netip.Addr
	// Widget marks requests from the drop-in widget or hosted page, which must
	// pass the app's Turnstile check when it has one.
	Widget         bool
	TurnstileToken string
}

// Send generates a code, cancels any code of the same app still pending for
// the number, and queues the SMS. Fraud protection runs first and may refuse
// with a *BlockedError. The returned code is only meant for test keys.
func (s *Service) Send(ctx context.Context, r SendRequest) (Verification, error) {
	to, err := messaging.NormalizeE164(r.To)
	if err != nil {
		return Verification{}, err
	}
	if r.AndroidAppHash != "" && !androidHashPattern.MatchString(r.AndroidAppHash) {
		return Verification{}, &messaging.ValidationError{Field: "android_app_hash", Message: "Use the 11-character hash from your app's SMS Retriever setup."}
	}
	meta := []byte("{}")
	if r.Metadata != nil {
		if meta, err = json.Marshal(r.Metadata); err != nil || len(meta) > 4096 || len(r.Metadata) > 32 {
			return Verification{}, &messaging.ValidationError{Field: "metadata", Message: "Metadata must be a JSON object of at most 32 keys and 4 KB."}
		}
	}
	appRow, err := s.ResolveApp(ctx, r.ProjectID, r.App)
	if err != nil {
		return Verification{}, err
	}
	settings := SettingsOf(appRow)
	if err := s.checkFraud(ctx, fraudCheck{
		app: appRow, env: r.Environment, to: to, clientIP: r.ClientIP,
		captcha: r.Widget && appRow.TurnstileSecret != nil, turnstileToken: r.TurnstileToken,
	}); err != nil {
		return Verification{}, err
	}

	// A short cooldown per number, then an hourly cap.
	latest, err := s.q.LatestOTPForRecipient(ctx, dbq.LatestOTPForRecipientParams{ProjectID: r.ProjectID, AppID: appRow.ID, Environment: r.Environment, Recipient: to})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Verification{}, err
	}
	if err == nil {
		if wait := latest.CreatedAt.Add(ResendCooldown).Sub(s.now()); wait > 0 {
			return Verification{}, &messaging.RateLimitError{Scope: "otp_resend", RetryAfter: wait}
		}
	}
	res, err := s.limiter.Hit(ctx, "otp:dest:"+appRow.ID+":"+string(r.Environment)+":"+to, DestinationHourlyLimit, time.Hour)
	if err != nil {
		s.log.Warn("rate limiter unavailable; allowing code", "error", err)
	} else if !res.Allowed {
		return Verification{}, &messaging.RateLimitError{Scope: "otp_destination", RetryAfter: res.RetryAfter}
	}

	code, err := generateCode(settings.CodeLength)
	if err != nil {
		return Verification{}, err
	}
	key, err := s.hmacKey(ctx)
	if err != nil {
		return Verification{}, err
	}
	otpID := id.New(id.OTP)
	app := settings.EffectiveAppName(r.ProjectName)
	tpl := settings.EffectiveTemplate()
	body := Render(tpl, app, code, settings.TTL, r.AndroidAppHash, settings.WebOTPDomain)
	masked := Render(tpl, app, strings.Repeat("•", len(code)), settings.TTL, r.AndroidAppHash, settings.WebOTPDomain)
	var testCode *string
	if r.Environment == dbq.ApiEnvironmentTest {
		testCode = &code
	}

	// Live codes are checked for delivery after the app's failover delay.
	var followUps []messaging.FollowUp
	if r.Environment == dbq.ApiEnvironmentLive && appRow.FailoverAfterSeconds > 0 {
		followUps = append(followUps, messaging.FollowUp{
			Args: messaging.OTPFailoverArgs{VerificationID: otpID},
			Opts: &river.InsertOpts{ScheduledAt: s.now().Add(time.Duration(appRow.FailoverAfterSeconds) * time.Second)},
		})
	}

	var row dbq.OtpVerification
	msg, _, err := s.msgs.Send(ctx, messaging.SendRequest{
		ProjectID: r.ProjectID, Environment: r.Environment, APIKeyID: r.APIKeyID, To: to, Body: body,
		Purpose: messaging.PurposeOTP, DisplayBody: &masked, Metadata: map[string]any{"otp_id": otpID},
		BodyVars:  map[string]string{"code": code, "app": app, "minutes": strconv.Itoa(minutesOf(settings.TTL))},
		FollowUps: followUps,
		OnCreate: func(ctx context.Context, q *dbq.Queries, m dbq.Message) error {
			if err := q.CancelPendingOTPs(ctx, dbq.CancelPendingOTPsParams{ProjectID: r.ProjectID, AppID: appRow.ID, Environment: r.Environment, Recipient: to}); err != nil {
				return err
			}
			var insertErr error
			row, insertErr = q.InsertOTP(ctx, dbq.InsertOTPParams{
				ID: otpID, ProjectID: r.ProjectID, AppID: appRow.ID, Environment: r.Environment, APIKeyID: r.APIKeyID, Recipient: to,
				CodeHash: hashCode(key, otpID, code), TestCode: testCode, CodeLength: int16(len(code)),
				MaxAttempts: int16(settings.MaxAttempts), MessageID: &m.ID, Metadata: meta,
				ExpiresAt: s.now().Add(settings.TTL),
			})
			return insertErr
		},
	})
	if err != nil {
		return Verification{}, err
	}
	s.log.Info("verification code queued", "otp_id", otpID, "project_id", r.ProjectID, "app_id", appRow.ID, "environment", r.Environment, "message_id", msg.ID)
	status := msg.Status
	return s.view(row, &status), nil
}

// DeliverRequest sends a code that another system generated and will check,
// such as Supabase Auth's phone login.
type DeliverRequest struct {
	ProjectID   string
	ProjectName string
	Environment dbq.APIEnvironment
	To          string
	Code        string
	Metadata    map[string]any
}

var externalCodePattern = regexp.MustCompile(`^[0-9A-Za-z]{4,12}$`)

// DeliverCode sends an externally generated code with the default app's
// message template. The message is masked like Bridge's own codes; nothing is stored
// to verify it.
func (s *Service) DeliverCode(ctx context.Context, r DeliverRequest) (dbq.Message, error) {
	if !externalCodePattern.MatchString(r.Code) {
		return dbq.Message{}, &messaging.ValidationError{Field: "code", Message: "The code must be 4 to 12 letters or digits."}
	}
	settings, err := s.LoadSettings(ctx, r.ProjectID)
	if err != nil {
		return dbq.Message{}, err
	}
	app := settings.EffectiveAppName(r.ProjectName)
	tpl := settings.EffectiveTemplate()
	body := Render(tpl, app, r.Code, settings.TTL, "", settings.WebOTPDomain)
	masked := Render(tpl, app, strings.Repeat("•", len(r.Code)), settings.TTL, "", settings.WebOTPDomain)
	msg, _, err := s.msgs.Send(ctx, messaging.SendRequest{
		ProjectID: r.ProjectID, Environment: r.Environment, To: r.To, Body: body,
		Purpose: messaging.PurposeOTP, DisplayBody: &masked, Metadata: r.Metadata,
		BodyVars: map[string]string{"code": r.Code, "app": app, "minutes": strconv.Itoa(minutesOf(settings.TTL))},
	})
	return msg, err
}

// ---- Verifying -------------------------------------------------------------

// VerifyRequest checks a code, by verification ID or by the number it was sent to.
type VerifyRequest struct {
	ProjectID   string
	Environment dbq.APIEnvironment
	// AppID, when set, limits the lookup to one app's verifications.
	AppID string
	ID    string
	To    string
	Code  string
}

// VerifyResult says whether the code was right, and the verification after the attempt.
type VerifyResult struct {
	Valid        bool
	Verification Verification
}

// Verify checks a code. A wrong code uses up an attempt; when none are left
// the verification fails and a new code is needed. Checking a verification
// that is no longer pending reports it without using an attempt.
func (s *Service) Verify(ctx context.Context, r VerifyRequest) (VerifyResult, error) {
	code := strings.TrimSpace(r.Code)
	if len(code) < 4 || len(code) > 10 || strings.Trim(code, "0123456789") != "" {
		return VerifyResult{}, &messaging.ValidationError{Field: "code", Message: "The code is 4 to 10 digits."}
	}
	if (r.ID == "") == (r.To == "") {
		return VerifyResult{}, &messaging.ValidationError{Field: "to", Message: "Pass the verification id, or the number the code was sent to, but not both."}
	}
	key, err := s.hmacKey(ctx)
	if err != nil {
		return VerifyResult{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VerifyResult{}, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	var row dbq.OtpVerification
	if r.ID != "" {
		row, err = q.GetOTPForUpdate(ctx, dbq.GetOTPForUpdateParams{ID: r.ID, ProjectID: r.ProjectID, Environment: r.Environment, AppID: nonEmpty(r.AppID)})
	} else {
		to, normErr := messaging.NormalizeE164(r.To)
		if normErr != nil {
			return VerifyResult{}, normErr
		}
		row, err = q.LatestPendingOTPForUpdate(ctx, dbq.LatestPendingOTPForUpdateParams{ProjectID: r.ProjectID, Environment: r.Environment, Recipient: to, AppID: nonEmpty(r.AppID)})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifyResult{}, ErrNotFound
	}
	if err != nil {
		return VerifyResult{}, err
	}

	valid := false
	before := row.Status
	switch {
	case row.Status != dbq.OtpStatusPending:
		// Finished already: report it as it is.
	case !s.now().Before(row.ExpiresAt):
		if row, err = q.ExpireOTP(ctx, row.ID); err != nil {
			return VerifyResult{}, err
		}
	default:
		valid = len(code) == int(row.CodeLength) && subtle.ConstantTimeCompare(hashCode(key, row.ID, code), row.CodeHash) == 1
		next := dbq.OtpStatusPending
		switch {
		case valid:
			next = dbq.OtpStatusVerified
		case row.Attempts+1 >= row.MaxAttempts:
			next = dbq.OtpStatusFailed
		}
		if row, err = q.FinishOTPAttempt(ctx, dbq.FinishOTPAttemptParams{ID: row.ID, Status: next}); err != nil {
			return VerifyResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifyResult{}, err
	}
	if row.Status != dbq.OtpStatusPending {
		// The code is spent; drop it from its messages if the phone is done with them.
		for _, m := range []*string{row.MessageID, row.FailoverMessageID} {
			if m == nil {
				continue
			}
			if err := s.q.RedactMessage(ctx, *m); err != nil {
				s.log.Warn("redact OTP message", "otp_id", row.ID, "message_id", *m, "error", err)
			}
		}
	}
	v, err := s.Get(ctx, r.ProjectID, &r.Environment, row.ID)
	if err != nil {
		return VerifyResult{}, err
	}
	if before == dbq.OtpStatusPending && row.Status != dbq.OtpStatusPending {
		s.announce(ctx, row.ProjectID, v)
	}
	return VerifyResult{Valid: valid, Verification: v}, nil
}

// announce emits the event for a verification that just finished. Canceled
// verifications are not announced: the newer code is what matters.
func (s *Service) announce(ctx context.Context, projectID string, v Verification) {
	if s.emitter == nil {
		return
	}
	var eventType string
	switch v.Status {
	case string(dbq.OtpStatusVerified):
		eventType = "otp.verified"
	case string(dbq.OtpStatusFailed):
		eventType = "otp.failed"
	case string(dbq.OtpStatusExpired):
		eventType = "otp.expired"
	default:
		return
	}
	v.Code = nil // events never carry the code, even in test mode
	if err := s.emitter.Emit(ctx, projectID, eventType, v); err != nil {
		s.log.Warn("could not announce verification", "otp_id", v.ID, "type", eventType, "error", err)
	}
}

// ---- Reading ---------------------------------------------------------------

// Verification is a one-time password as the API shows it.
type Verification struct {
	ID                string         `json:"id" example:"otp_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Status            string         `json:"status" enum:"pending,verified,expired,failed,canceled" doc:"canceled means a newer code was sent to the same number."`
	To                string         `json:"to" example:"+919876543210"`
	Environment       string         `json:"environment" enum:"live,test"`
	Attempts          int            `json:"attempts" doc:"Wrong codes entered so far, plus the right one if verified."`
	AttemptsRemaining int            `json:"attempts_remaining"`
	ExpiresAt         time.Time      `json:"expires_at"`
	ResendAvailableAt time.Time      `json:"resend_available_at" doc:"When a new code may be sent to this number."`
	VerifiedAt        *time.Time     `json:"verified_at" nullable:"true"`
	MessageID         *string        `json:"message_id" nullable:"true" doc:"The SMS that carries the code."`
	MessageStatus     *string        `json:"message_status" nullable:"true" enum:"created,queued,sending,sent,delivered,failed" doc:"Delivery status of that SMS."`
	AppID             *string        `json:"app_id" nullable:"true" example:"vap_01ja8z3k5wq2v7c9e4r2n0w6yb" doc:"The Verify app the code belongs to. Null if the app was deleted."`
	FailoverMessageID *string        `json:"failover_message_id" nullable:"true" doc:"The second SMS, sent through another route because the first was not sent in time."`
	FailoverStatus    *string        `json:"failover_message_status" nullable:"true" enum:"created,queued,sending,sent,delivered,failed" doc:"Delivery status of the failover SMS."`
	Metadata          map[string]any `json:"metadata"`
	Code              *string        `json:"code,omitempty" doc:"The code itself. Only returned for test keys, so tests can complete a verification without a phone."`
	CreatedAt         time.Time      `json:"created_at"`
}

// Get returns one verification. env limits it to an environment when set.
func (s *Service) Get(ctx context.Context, projectID string, env *dbq.APIEnvironment, otpID string) (Verification, error) {
	row, err := s.q.GetOTPView(ctx, dbq.GetOTPViewParams{ID: otpID, ProjectID: projectID, Environment: env})
	if errors.Is(err, pgx.ErrNoRows) {
		return Verification{}, ErrNotFound
	}
	if err != nil {
		return Verification{}, err
	}
	return s.viewRow(row), nil
}

// ListRequest filters a project's verifications, newest first.
type ListRequest struct {
	ProjectID     string
	AppID         string
	Environment   *dbq.APIEnvironment
	Status        string
	To            string
	StartingAfter string
	Limit         int
}

func (s *Service) List(ctx context.Context, r ListRequest) ([]Verification, bool, error) {
	params := dbq.ListOTPViewsParams{ProjectID: r.ProjectID, AppID: nonEmpty(r.AppID), Environment: r.Environment, RowLimit: int32(r.Limit + 1)}
	if r.Status != "" {
		status := dbq.OtpStatus(r.Status)
		params.Status = &status
	}
	if r.To != "" {
		to, err := messaging.NormalizeE164(r.To)
		if err != nil {
			return nil, false, err
		}
		params.Recipient = &to
	}
	if r.StartingAfter != "" {
		cursor, err := s.q.GetOTPView(ctx, dbq.GetOTPViewParams{ID: r.StartingAfter, ProjectID: r.ProjectID})
		if err != nil {
			return nil, false, &messaging.ValidationError{Field: "starting_after", Message: "Unknown verification ID."}
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListOTPViews(ctx, params)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > r.Limit
	if more {
		rows = rows[:r.Limit]
	}
	out := make([]Verification, len(rows))
	for i, row := range rows {
		out[i] = s.viewRow(dbq.GetOTPViewRow(row))
	}
	return out, more, nil
}

// VerificationStats summarizes verifications since a time.
type VerificationStats struct {
	Total                 int        `json:"total"`
	Verified              int        `json:"verified"`
	Pending               int        `json:"pending"`
	Expired               int        `json:"expired"`
	Failed                int        `json:"failed"`
	Canceled              int        `json:"canceled"`
	ConversionRate        *float64   `json:"conversion_rate" nullable:"true" doc:"Verified / finished verifications (excluding canceled ones), 0 to 1."`
	MedianSecondsToVerify float64    `json:"median_seconds_to_verify"`
	Failovers             int        `json:"failovers" doc:"Verifications whose code was resent through another route."`
	Blocked               BlockStats `json:"blocked" doc:"Send attempts refused by fraud protection, by reason."`
}

// Stats summarizes a project's verifications, or one app's when appID is set.
func (s *Service) Stats(ctx context.Context, projectID string, appID *string, env dbq.APIEnvironment, since time.Time) (VerificationStats, error) {
	r, err := s.q.OTPStats(ctx, dbq.OTPStatsParams{ProjectID: projectID, AppID: appID, Environment: env, Since: since})
	if err != nil {
		return VerificationStats{}, err
	}
	st := VerificationStats{Total: int(r.Total), Verified: int(r.Verified), Pending: int(r.Pending), Expired: int(r.Expired),
		Failed: int(r.Failed), Canceled: int(r.Canceled), MedianSecondsToVerify: r.MedianSecondsToVerify, Failovers: int(r.Failovers)}
	if finished := st.Verified + st.Expired + st.Failed; finished > 0 {
		rate := float64(st.Verified) / float64(finished)
		st.ConversionRate = &rate
	}
	if st.Blocked, err = s.blockStats(ctx, projectID, appID, env, since); err != nil {
		return VerificationStats{}, err
	}
	return st, nil
}

// MaintainResult counts what Maintain did.
type MaintainResult struct {
	Expired       int64
	Deleted       int64
	BlocksDeleted int64
}

// Maintain expires lapsed codes and deletes old verifications and blocks.
func (s *Service) Maintain(ctx context.Context) (MaintainResult, error) {
	rows, err := s.q.ExpireOTPs(ctx)
	if err != nil {
		return MaintainResult{}, err
	}
	for _, row := range rows {
		s.announce(ctx, row.ProjectID, s.view(row, nil))
	}
	res := MaintainResult{Expired: int64(len(rows))}
	if res.Deleted, err = s.q.DeleteOldOTPs(ctx, s.now().Add(-HistoryRetention)); err != nil {
		return res, err
	}
	res.BlocksDeleted, err = s.q.DeleteOldOTPBlocks(ctx, s.now().Add(-BlockRetention))
	return res, err
}

func (s *Service) viewRow(r dbq.GetOTPViewRow) Verification {
	v := s.view(dbq.OtpVerification{
		ID: r.ID, ProjectID: r.ProjectID, Environment: r.Environment, APIKeyID: r.APIKeyID, Recipient: r.Recipient,
		Status: r.Status, TestCode: r.TestCode, CodeLength: r.CodeLength, Attempts: r.Attempts, MaxAttempts: r.MaxAttempts,
		MessageID: r.MessageID, Metadata: r.Metadata, ExpiresAt: r.ExpiresAt, VerifiedAt: r.VerifiedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, AppID: r.AppID, FailoverMessageID: r.FailoverMessageID,
	}, r.MessageStatus)
	if r.FailoverMessageStatus != nil {
		fs := string(*r.FailoverMessageStatus)
		v.FailoverStatus = &fs
	}
	return v
}

func (s *Service) view(r dbq.OtpVerification, msgStatus *dbq.MessageStatus) Verification {
	status := string(r.Status)
	if r.Status == dbq.OtpStatusPending && !s.now().Before(r.ExpiresAt) {
		status = string(dbq.OtpStatusExpired) // the maintenance job records it shortly
	}
	v := Verification{
		ID: r.ID, Status: status, To: r.Recipient, Environment: string(r.Environment),
		Attempts: int(r.Attempts), AttemptsRemaining: max(0, int(r.MaxAttempts-r.Attempts)),
		ExpiresAt: r.ExpiresAt, ResendAvailableAt: r.CreatedAt.Add(ResendCooldown), VerifiedAt: r.VerifiedAt,
		MessageID: r.MessageID, Metadata: map[string]any{}, Code: r.TestCode, CreatedAt: r.CreatedAt,
		AppID: r.AppID, FailoverMessageID: r.FailoverMessageID,
	}
	if status != string(dbq.OtpStatusPending) {
		v.AttemptsRemaining = 0
	}
	if msgStatus != nil {
		ms := string(*msgStatus)
		v.MessageStatus = &ms
	}
	_ = json.Unmarshal(r.Metadata, &v.Metadata)
	return v
}

// ---- Codes and keys --------------------------------------------------------

func generateCode(n int) (string, error) {
	var b strings.Builder
	for range n {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String(), nil
}

// hashCode binds the code to its verification, so equal codes hash differently.
func hashCode(key []byte, otpID, code string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(otpID + ":" + code))
	return mac.Sum(nil)
}

// hmacKey loads the installation's OTP key, creating it on first use.
func (s *Service) hmacKey(ctx context.Context) ([]byte, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.key != nil {
		return s.key, nil
	}
	key, err := s.q.GetServerKey(ctx, hmacKeyName)
	if errors.Is(err, pgx.ErrNoRows) {
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		// Another instance may win the race; re-read whatever was stored.
		if err := s.q.InsertServerKey(ctx, dbq.InsertServerKeyParams{Name: hmacKeyName, PrivateKey: fresh}); err != nil {
			return nil, err
		}
		key, err = s.q.GetServerKey(ctx, hmacKeyName)
	}
	if err != nil {
		return nil, fmt.Errorf("load OTP key: %w", err)
	}
	s.key = key
	return key, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
