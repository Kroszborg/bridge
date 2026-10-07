// Package provider sends SMS through third-party providers (MSG91, Twilio,
// Vonage, Plivo) for projects without a phone, or when no phone can send.
//
// Each provider is a small HTTP client behind the Client interface. Clients
// never retry; the messaging pipeline decides what to do with an *Error.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Kind names a provider.
type Kind string

const (
	MSG91  Kind = "msg91"
	Twilio Kind = "twilio"
	Vonage Kind = "vonage"
	Plivo  Kind = "plivo"
)

// ProviderField is one setting a provider needs.
type ProviderField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
}

// ProviderSpec describes a provider for validation and for the dashboard's form.
type ProviderSpec struct {
	Kind        Kind            `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Credentials []ProviderField `json:"credentials"`
	Config      []ProviderField `json:"config"`
	// Callbacks: "per_message" when Bridge passes its callback URL with each
	// message; "account" when it must be set once in the provider's dashboard.
	Callbacks string `json:"callbacks" enum:"per_message,account"`
}

// Specs lists every supported provider.
var Specs = []ProviderSpec{msg91Spec, twilioSpec, vonageSpec, plivoSpec}

// SpecFor returns the spec of a kind.
func SpecFor(k Kind) (ProviderSpec, bool) {
	for _, s := range Specs {
		if s.Kind == k {
			return s, true
		}
	}
	return ProviderSpec{}, false
}

// Outgoing is one message to send.
type Outgoing struct {
	MessageID   string
	To          string // E.164 with a leading +
	Body        string
	Unicode     bool              // the body needs UCS-2
	Purpose     string            // "message" or "otp"
	Vars        map[string]string // template variables, e.g. "code" for one-time passwords
	CallbackURL string            // where delivery reports should go; empty when unknown
}

// Accepted is a provider's acknowledgement of a message.
type Accepted struct {
	ExternalID string
}

// Status is a delivery update from a provider's callback.
type Status string

const (
	StatusSent      Status = "sent"
	StatusDelivered Status = "delivered"
	StatusFailed    Status = "failed"
)

// Update is one delivery report.
type Update struct {
	ExternalID   string
	Status       Status
	ErrorCode    string
	ErrorMessage string
}

// Error is a provider's refusal. Retryable errors (timeouts, 429, 5xx) may
// succeed later or through another provider; the others will not.
type Error struct {
	Kind      Kind
	Retryable bool
	Code      string // e.g. "twilio_21211"
	Message   string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Kind, e.Message, e.Code) }

// Client sends through one provider account.
type Client interface {
	Send(ctx context.Context, m Outgoing) (Accepted, error)
	// Check verifies the credentials and returns a short description, such as
	// the account name or balance.
	Check(ctx context.Context) (string, error)
}

// Options configure clients. BaseURL replaces the provider's API host (tests).
type Options struct {
	HTTP    *http.Client
	BaseURL string
}

// New builds a client from decrypted credentials and the account's config.
func New(kind Kind, creds, config map[string]string, o Options) (Client, error) {
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if err := Validate(kind, creds, config); err != nil {
		return nil, err
	}
	b := base{kind: kind, http: o.HTTP}
	switch kind {
	case MSG91:
		b.url = or(o.BaseURL, "https://control.msg91.com")
		return &msg91{base: b, creds: creds, config: config}, nil
	case Twilio:
		b.url = or(o.BaseURL, "https://api.twilio.com")
		return &twilio{base: b, creds: creds, config: config}, nil
	case Vonage:
		b.url = or(o.BaseURL, "https://rest.nexmo.com")
		return &vonage{base: b, creds: creds, config: config}, nil
	case Plivo:
		b.url = or(o.BaseURL, "https://api.plivo.com")
		return &plivo{base: b, creds: creds, config: config}, nil
	}
	return nil, fmt.Errorf("unknown provider %q", kind)
}

// FieldError reports a missing or invalid setting.
type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Validate checks that required settings are present and that only known ones are set.
func Validate(kind Kind, creds, config map[string]string) error {
	spec, ok := SpecFor(kind)
	if !ok {
		return &FieldError{"kind", "Use msg91, twilio, vonage or plivo."}
	}
	check := func(prefix string, fields []ProviderField, values map[string]string) error {
		known := map[string]bool{}
		for _, f := range fields {
			known[f.Key] = true
			if f.Required && strings.TrimSpace(values[f.Key]) == "" {
				return &FieldError{prefix + "." + f.Key, f.Label + " is required."}
			}
		}
		for k := range values {
			if !known[k] {
				return &FieldError{prefix + "." + k, "Unknown setting for " + spec.Name + "."}
			}
		}
		return nil
	}
	if err := check("credentials", spec.Credentials, creds); err != nil {
		return err
	}
	if err := check("config", spec.Config, config); err != nil {
		return err
	}
	if kind == Twilio && config["from"] == "" && config["messaging_service_sid"] == "" {
		return &FieldError{"config.from", "Set a From number or sender ID, or a Messaging Service SID."}
	}
	if kind == MSG91 && config["otp_template_id"] == "" && config["message_template_id"] == "" {
		return &FieldError{"config.otp_template_id", "Set at least one DLT template: for one-time passwords, messages, or both."}
	}
	return nil
}

// Hint is what the dashboard may show of an account's credentials.
func Hint(kind Kind, creds map[string]string) string {
	switch kind {
	case Twilio:
		return creds["account_sid"]
	case Vonage:
		return creds["api_key"]
	case Plivo:
		return creds["auth_id"]
	case MSG91:
		if k := creds["auth_key"]; len(k) > 4 {
			return "…" + k[len(k)-4:]
		}
	}
	return ""
}

// ---- shared HTTP plumbing ----------------------------------------------------

type base struct {
	kind Kind
	url  string
	http *http.Client
}

// do sends a request and decodes a JSON response into out. HTTP and network
// failures become *Error with Retryable set for timeouts, 429 and 5xx.
func (b base) do(req *http.Request, out any) (int, error) {
	res, err := b.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0, err
		}
		return 0, &Error{Kind: b.kind, Retryable: true, Code: string(b.kind) + "_unreachable", Message: "Could not reach " + string(b.kind) + ": " + err.Error()}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	if res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests {
		return res.StatusCode, &Error{Kind: b.kind, Retryable: true, Code: fmt.Sprintf("%s_http_%d", b.kind, res.StatusCode),
			Message: fmt.Sprintf("%s answered %d %s", b.kind, res.StatusCode, http.StatusText(res.StatusCode))}
	}
	return res.StatusCode, nil
}

func digits(e164 string) string { return strings.TrimPrefix(e164, "+") }

func or(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
