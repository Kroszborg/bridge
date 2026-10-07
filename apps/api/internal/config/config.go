// Package config loads and validates Bridge's runtime configuration from
// environment variables. Every variable is documented in .env.example.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env            string   // "development" or "production"
	HTTPAddr       string   // listen address for `bridge serve`
	DatabaseURL    string   // PostgreSQL connection string
	PublicURL      *url.URL // externally reachable API base URL
	DashboardURL   *url.URL // dashboard origin, the only origin allowed to use session cookies
	LogLevel       slog.Level
	LogFormat      string         // "json" or "text"
	TrustedProxies []netip.Prefix // peers whose X-Forwarded-For header is trusted
	SessionTTL     time.Duration
	CookieSecure   bool
	AllowSignup    bool
	DBMaxConns     int32
	VAPIDSubject   string // contact URI sent to WebPush services (mailto: or https:)
	// Allow push endpoints on private networks (e.g. a self-hosted ntfy on the LAN).
	// Off by default so a paired device cannot make the server call internal hosts.
	PushAllowPrivate bool
	// WebhookAllowPrivate lets webhook endpoints resolve to private addresses
	// (for apps on the same host or Docker network).
	WebhookAllowPrivate bool
	// MessageRetention is how long message bodies are kept before redaction.
	MessageRetention time.Duration
	// RequestLogRetention is how long developer API request logs are kept.
	RequestLogRetention time.Duration
	// OperatorEmails may see System health. Empty: the first account.
	OperatorEmails []string
	// SecretKey encrypts provider credentials and integration secrets
	// (BRIDGE_SECRET_KEY, 32 bytes as base64 or hex). Nil: they cannot be saved.
	SecretKey []byte
	FCM       *FCMConfig
}

// FCMConfig enables Firebase Cloud Messaging wake-ups for the gateway app's
// "gms" flavor. It is optional; the FOSS flavor uses UnifiedPush instead.
type FCMConfig struct {
	CredentialsFile string // service account JSON used to send messages
	ProjectID       string
	// Public client configuration handed to paired devices so the app can
	// initialise Firebase at runtime (no google-services.json in the build).
	AppID    string
	APIKey   string
	SenderID string
}

// Production reports whether Bridge runs in production mode.
func (c *Config) Production() bool { return c.Env == "production" }

// Load reads configuration from the environment.
func Load() (*Config, error) { return load(os.Getenv) }

func load(get func(string) string) (*Config, error) {
	var errs []error
	str := func(key, def string) string {
		if v := strings.TrimSpace(get(key)); v != "" {
			return v
		}
		return def
	}
	parseURL := func(key, def string) *url.URL {
		raw := str(key, def)
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be an absolute http(s) URL, got %q", key, raw))
			return &url.URL{}
		}
		u.Path = strings.TrimSuffix(u.Path, "/")
		return u
	}
	parseBool := func(key string, def bool) bool {
		raw := str(key, "")
		if raw == "" {
			return def
		}
		v, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be true or false, got %q", key, raw))
		}
		return v
	}

	c := &Config{
		Env:          str("BRIDGE_ENV", "development"),
		HTTPAddr:     str("BRIDGE_HTTP_ADDR", ":8080"),
		DatabaseURL:  str("BRIDGE_DATABASE_URL", ""),
		PublicURL:    parseURL("BRIDGE_PUBLIC_URL", "http://localhost:8080"),
		DashboardURL: parseURL("BRIDGE_DASHBOARD_URL", "http://localhost:3000"),
		LogFormat:    str("BRIDGE_LOG_FORMAT", ""),
		AllowSignup:  parseBool("BRIDGE_ALLOW_SIGNUP", true),
	}

	if c.Env != "development" && c.Env != "production" {
		errs = append(errs, fmt.Errorf("BRIDGE_ENV must be development or production, got %q", c.Env))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("BRIDGE_DATABASE_URL is required"))
	}
	if c.LogFormat == "" {
		c.LogFormat = "text"
		if c.Production() {
			c.LogFormat = "json"
		}
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("BRIDGE_LOG_FORMAT must be json or text, got %q", c.LogFormat))
	}
	if err := c.LogLevel.UnmarshalText([]byte(str("BRIDGE_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("BRIDGE_LOG_LEVEL: %w", err))
	}

	ttl, err := time.ParseDuration(str("BRIDGE_SESSION_TTL", "720h"))
	if err != nil || ttl < time.Hour {
		errs = append(errs, errors.New("BRIDGE_SESSION_TTL must be a duration of at least 1h, e.g. 720h"))
	}
	c.SessionTTL = ttl

	maxConns, err := strconv.ParseInt(str("BRIDGE_DB_MAX_CONNS", "20"), 10, 32)
	if err != nil || maxConns < 2 {
		errs = append(errs, errors.New("BRIDGE_DB_MAX_CONNS must be an integer >= 2"))
	}
	c.DBMaxConns = int32(maxConns)

	for raw := range strings.SplitSeq(str("BRIDGE_TRUSTED_PROXIES", "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("BRIDGE_TRUSTED_PROXIES: invalid CIDR %q", raw))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, p.Masked())
	}

	c.VAPIDSubject = str("BRIDGE_VAPID_SUBJECT", "")
	if c.VAPIDSubject == "" {
		if c.PublicURL.Scheme == "https" {
			c.VAPIDSubject = c.PublicURL.Scheme + "://" + c.PublicURL.Host
		} else {
			c.VAPIDSubject = "mailto:bridge@localhost"
		}
	}
	if !strings.HasPrefix(c.VAPIDSubject, "mailto:") && !strings.HasPrefix(c.VAPIDSubject, "https://") {
		errs = append(errs, errors.New("BRIDGE_VAPID_SUBJECT must start with mailto: or https://"))
	}

	c.PushAllowPrivate = parseBool("BRIDGE_PUSH_ALLOW_PRIVATE_ENDPOINTS", false)
	c.WebhookAllowPrivate = parseBool("BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS", false)

	retention, err := time.ParseDuration(str("BRIDGE_MESSAGE_RETENTION", "720h"))
	if err != nil || retention < time.Hour {
		errs = append(errs, errors.New("BRIDGE_MESSAGE_RETENTION must be a duration of at least 1h, e.g. 720h for 30 days"))
	}
	c.MessageRetention = retention

	logRetention, err := time.ParseDuration(str("BRIDGE_REQUEST_LOG_RETENTION", "336h"))
	if err != nil || logRetention < time.Hour {
		errs = append(errs, errors.New("BRIDGE_REQUEST_LOG_RETENTION must be a duration of at least 1h, e.g. 336h for 14 days"))
	}
	c.RequestLogRetention = logRetention

	for _, e := range strings.Split(str("BRIDGE_OPERATOR_EMAILS", ""), ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			c.OperatorEmails = append(c.OperatorEmails, e)
		}
	}

	if raw := str("BRIDGE_SECRET_KEY", ""); raw != "" {
		key, err := parseSecretKey(raw)
		if err != nil {
			errs = append(errs, err)
		}
		c.SecretKey = key
	}

	fcm := FCMConfig{
		CredentialsFile: str("BRIDGE_FCM_CREDENTIALS_FILE", ""),
		ProjectID:       str("BRIDGE_FCM_PROJECT_ID", ""),
		AppID:           str("BRIDGE_FCM_APP_ID", ""),
		APIKey:          str("BRIDGE_FCM_API_KEY", ""),
		SenderID:        str("BRIDGE_FCM_SENDER_ID", ""),
	}
	if fcm != (FCMConfig{}) {
		for key, v := range map[string]string{
			"BRIDGE_FCM_CREDENTIALS_FILE": fcm.CredentialsFile, "BRIDGE_FCM_PROJECT_ID": fcm.ProjectID,
			"BRIDGE_FCM_APP_ID": fcm.AppID, "BRIDGE_FCM_API_KEY": fcm.APIKey, "BRIDGE_FCM_SENDER_ID": fcm.SenderID,
		} {
			if v == "" {
				errs = append(errs, fmt.Errorf("%s is required when Firebase Cloud Messaging is configured", key))
			}
		}
		c.FCM = &fcm
	}

	// Secure cookies are required whenever the dashboard is served over HTTPS.
	c.CookieSecure = parseBool("BRIDGE_COOKIE_SECURE", c.DashboardURL.Scheme == "https")
	if c.Production() && !c.CookieSecure && c.DashboardURL.Hostname() != "localhost" {
		slog.Warn("session cookies are not marked Secure; serve the dashboard over HTTPS in production")
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// DashboardOrigin returns the scheme://host[:port] origin of the dashboard.
func (c *Config) DashboardOrigin() string {
	return c.DashboardURL.Scheme + "://" + c.DashboardURL.Host
}

// parseSecretKey accepts 32 bytes as base64 (standard or URL-safe) or hex,
// e.g. the output of `openssl rand -base64 32`.
func parseSecretKey(raw string) ([]byte, error) {
	for _, dec := range []func(string) ([]byte, error){
		hex.DecodeString, base64.StdEncoding.DecodeString, base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString, base64.RawURLEncoding.DecodeString,
	} {
		if key, err := dec(raw); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("BRIDGE_SECRET_KEY must be 32 random bytes as base64 or hex; generate one with: openssl rand -base64 32")
}
