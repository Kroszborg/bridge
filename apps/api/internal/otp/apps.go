package otp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nyaruka/phonenumbers"

	"bridge/internal/auth"
	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
	"bridge/internal/secretbox"
)

// Verify apps. A project can have several, each with its own name, message
// template, code settings, limits and widget. The app with slug "default"
// holds what used to be the project's settings; it is created the first time
// it is needed and cannot be deleted.
const (
	DefaultAppSlug = "default"

	PublishableKeyPrefix = "bpk_"
	AppSecretPrefix      = "bvs_"

	DefaultFailoverAfter    = 30 // seconds
	DefaultIPHourlyLimit    = 10
	DefaultRangeHourlyLimit = 20

	maxAppsPerProject = 50
	maxListEntries    = 20
)

var (
	// ErrAppNotFound means no app in the project has the given ID or slug.
	ErrAppNotFound = errors.New("verify app not found")
	// ErrDefaultApp means the default app cannot be deleted.
	ErrDefaultApp = errors.New("the default app cannot be deleted")
	// ErrTooManyApps means the project reached maxAppsPerProject.
	ErrTooManyApps = errors.New("too many verify apps")
	// ErrNoSecretKey means BRIDGE_SECRET_KEY is not set, so secrets cannot be stored.
	ErrNoSecretKey = secretbox.ErrNoKey

	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

// AppConfig is everything about an app that the dashboard edits. Secrets are
// passed separately.
type AppConfig struct {
	Name     string
	Settings Settings
	// FailoverAfter is in seconds; 0 turns failover off.
	FailoverAfter      int
	AllowedCountries   []string
	IPHourlyLimit      int // 0: off
	RangeHourlyLimit   int // 0: off
	CountryHourlyLimit int // 0: no cap
	AllowedOrigins     []string
	RedirectURIs       []string
	WidgetEnvironment  dbq.APIEnvironment
	TurnstileSiteKey   string
}

// DefaultAppConfig is a new app's configuration.
func DefaultAppConfig(name string) AppConfig {
	return AppConfig{
		Name: name, Settings: Defaults(), FailoverAfter: DefaultFailoverAfter,
		IPHourlyLimit: DefaultIPHourlyLimit, RangeHourlyLimit: DefaultRangeHourlyLimit,
		AllowedCountries: []string{}, AllowedOrigins: []string{}, RedirectURIs: []string{},
		WidgetEnvironment: dbq.ApiEnvironmentLive,
	}
}

// ConfigOf reads an app row's configuration.
func ConfigOf(a dbq.VerifyApp) AppConfig {
	c := AppConfig{
		Name: a.Name, Settings: SettingsOf(a), FailoverAfter: int(a.FailoverAfterSeconds),
		AllowedCountries: nonNil(a.AllowedCountries), IPHourlyLimit: int(a.IpHourlyLimit), RangeHourlyLimit: int(a.RangeHourlyLimit),
		AllowedOrigins: nonNil(a.AllowedOrigins), RedirectURIs: nonNil(a.RedirectUris), WidgetEnvironment: a.WidgetEnvironment,
		TurnstileSiteKey: deref(a.TurnstileSiteKey),
	}
	if a.CountryHourlyLimit != nil {
		c.CountryHourlyLimit = int(*a.CountryHourlyLimit)
	}
	return c
}

// SettingsOf reads an app's code settings.
func SettingsOf(a dbq.VerifyApp) Settings {
	return Settings{
		AppName: deref(a.AppName), Template: deref(a.Template), CodeLength: int(a.CodeLength),
		TTL: time.Duration(a.TtlSeconds) * time.Second, MaxAttempts: int(a.MaxAttempts),
		WebOTPDomain: deref(a.WebOtpDomain), Custom: true,
	}
}

// normalize trims and canonicalizes the configuration, then validates it.
func (c *AppConfig) normalize() error {
	c.Name = strings.TrimSpace(c.Name)
	c.Settings.AppName = strings.TrimSpace(c.Settings.AppName)
	c.Settings.Template = strings.TrimSpace(c.Settings.Template)
	c.Settings.WebOTPDomain = strings.ToLower(strings.TrimSpace(c.Settings.WebOTPDomain))
	c.TurnstileSiteKey = strings.TrimSpace(c.TurnstileSiteKey)
	if c.Name == "" || len([]rune(c.Name)) > 60 {
		return &messaging.ValidationError{Field: "name", Message: "Use a name of 1 to 60 characters."}
	}
	if err := c.Settings.Validate(); err != nil {
		return err
	}
	if c.FailoverAfter < 0 || c.FailoverAfter > 600 {
		return &messaging.ValidationError{Field: "failover_after_seconds", Message: "Use 0 (off) to 600 seconds."}
	}
	if c.IPHourlyLimit < 0 || c.IPHourlyLimit > 100000 {
		return &messaging.ValidationError{Field: "ip_hourly_limit", Message: "Use 0 (off) to 100000."}
	}
	if c.RangeHourlyLimit < 0 || c.RangeHourlyLimit > 100000 {
		return &messaging.ValidationError{Field: "range_hourly_limit", Message: "Use 0 (off) to 100000."}
	}
	if c.CountryHourlyLimit < 0 || c.CountryHourlyLimit > 1000000 {
		return &messaging.ValidationError{Field: "country_hourly_limit", Message: "Use 1 to 1000000, or null for no cap."}
	}
	if c.WidgetEnvironment != dbq.ApiEnvironmentLive && c.WidgetEnvironment != dbq.ApiEnvironmentTest {
		return &messaging.ValidationError{Field: "widget_environment", Message: "Use live or test."}
	}
	if len(c.TurnstileSiteKey) > 100 {
		return &messaging.ValidationError{Field: "turnstile_site_key", Message: "Use the site key from the Cloudflare dashboard."}
	}

	countries := make([]string, 0, len(c.AllowedCountries))
	supported := phonenumbers.GetSupportedRegions()
	for _, raw := range c.AllowedCountries {
		cc := strings.ToUpper(strings.TrimSpace(raw))
		if !supported[cc] {
			return &messaging.ValidationError{Field: "allowed_countries", Message: fmt.Sprintf("%q is not an ISO 3166-1 alpha-2 country code with phone numbers, such as IN or US.", raw)}
		}
		if !slices.Contains(countries, cc) {
			countries = append(countries, cc)
		}
	}
	slices.Sort(countries)
	c.AllowedCountries = countries

	origins := make([]string, 0, len(c.AllowedOrigins))
	for _, raw := range c.AllowedOrigins {
		o, ok := NormalizeOrigin(raw)
		if !ok {
			return &messaging.ValidationError{Field: "allowed_origins", Message: fmt.Sprintf("%q is not an origin. Use https://example.com, without a path (http:// only for localhost).", raw)}
		}
		if !slices.Contains(origins, o) {
			origins = append(origins, o)
		}
	}
	if len(origins) > maxListEntries {
		return &messaging.ValidationError{Field: "allowed_origins", Message: fmt.Sprintf("Use at most %d origins.", maxListEntries)}
	}
	c.AllowedOrigins = origins

	uris := make([]string, 0, len(c.RedirectURIs))
	for _, raw := range c.RedirectURIs {
		u := strings.TrimSpace(raw)
		if !validRedirectURI(u) {
			return &messaging.ValidationError{Field: "redirect_uris", Message: fmt.Sprintf("%q is not a valid redirect URI. Use an absolute https:// URL without a #fragment (http:// only for localhost).", raw)}
		}
		if !slices.Contains(uris, u) {
			uris = append(uris, u)
		}
	}
	if len(uris) > maxListEntries {
		return &messaging.ValidationError{Field: "redirect_uris", Message: fmt.Sprintf("Use at most %d redirect URIs.", maxListEntries)}
	}
	c.RedirectURIs = uris
	return nil
}

// NormalizeOrigin returns scheme://host[:port] for an https origin, or an
// http origin on localhost.
func NormalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && (scheme != "http" || !isLocalHost(u.Hostname())) {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

func validRedirectURI(raw string) bool {
	if raw == "" || len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && isLocalHost(u.Hostname()))
}

func isLocalHost(h string) bool {
	h = strings.ToLower(h)
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---- Resolving -------------------------------------------------------------

// ResolveApp finds an app by ID (vap_…) or slug. An empty ref is the default
// app, which is created if the project has none yet.
func (s *Service) ResolveApp(ctx context.Context, projectID, ref string) (dbq.VerifyApp, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == DefaultAppSlug {
		return s.DefaultApp(ctx, projectID)
	}
	var (
		app dbq.VerifyApp
		err error
	)
	if strings.HasPrefix(ref, id.VerifyApp+"_") {
		app, err = s.q.GetVerifyApp(ctx, dbq.GetVerifyAppParams{ID: ref, ProjectID: projectID})
	} else {
		app, err = s.q.GetVerifyAppBySlug(ctx, dbq.GetVerifyAppBySlugParams{ProjectID: projectID, Slug: ref})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return app, ErrAppNotFound
	}
	return app, err
}

// GetApp returns one of the project's apps by ID.
func (s *Service) GetApp(ctx context.Context, projectID, appID string) (dbq.VerifyApp, error) {
	app, err := s.q.GetVerifyApp(ctx, dbq.GetVerifyAppParams{ID: appID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return app, ErrAppNotFound
	}
	return app, err
}

// DefaultApp returns the project's default app, creating it on first use.
func (s *Service) DefaultApp(ctx context.Context, projectID string) (dbq.VerifyApp, error) {
	app, err := s.q.GetVerifyAppBySlug(ctx, dbq.GetVerifyAppBySlugParams{ProjectID: projectID, Slug: DefaultAppSlug})
	if !errors.Is(err, pgx.ErrNoRows) {
		return app, err
	}
	appID := id.New(id.VerifyApp)
	params := dbq.InsertDefaultVerifyAppParams{ID: appID, ProjectID: projectID, PublishableKey: newPublishableKey()}
	if s.box.Ready() {
		if params.Secret, err = s.box.Seal(appID, []byte(newAppSecret())); err != nil {
			return app, err
		}
	}
	// A concurrent request may create it first; either way, read what is stored.
	if err := s.q.InsertDefaultVerifyApp(ctx, params); err != nil {
		return app, err
	}
	return s.q.GetVerifyAppBySlug(ctx, dbq.GetVerifyAppBySlugParams{ProjectID: projectID, Slug: DefaultAppSlug})
}

// AppByPublishableKey finds the app a widget belongs to.
func (s *Service) AppByPublishableKey(ctx context.Context, key string) (dbq.VerifyApp, error) {
	if !strings.HasPrefix(key, PublishableKeyPrefix) || len(key) > 64 {
		return dbq.VerifyApp{}, ErrAppNotFound
	}
	app, err := s.q.GetVerifyAppByPublishableKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return app, ErrAppNotFound
	}
	return app, err
}

// ListApps returns the project's apps, the default app first (created if missing).
func (s *Service) ListApps(ctx context.Context, projectID string) ([]dbq.VerifyApp, error) {
	if _, err := s.DefaultApp(ctx, projectID); err != nil {
		return nil, err
	}
	return s.q.ListVerifyApps(ctx, projectID)
}

// ---- Changing --------------------------------------------------------------

// CreateApp validates and stores a new app. slug is derived from the name
// when empty. It returns the app and its signing secret, shown once.
func (s *Service) CreateApp(ctx context.Context, projectID, slug string, c AppConfig, turnstileSecret string) (dbq.VerifyApp, string, error) {
	if err := c.normalize(); err != nil {
		return dbq.VerifyApp{}, "", err
	}
	if _, err := s.DefaultApp(ctx, projectID); err != nil {
		return dbq.VerifyApp{}, "", err
	}
	existing, err := s.q.ListVerifyApps(ctx, projectID)
	if err != nil {
		return dbq.VerifyApp{}, "", err
	}
	if len(existing) >= maxAppsPerProject {
		return dbq.VerifyApp{}, "", ErrTooManyApps
	}
	taken := func(candidate string) bool {
		return candidate == DefaultAppSlug || slices.ContainsFunc(existing, func(a dbq.VerifyApp) bool { return a.Slug == candidate })
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug != "" {
		if !slugPattern.MatchString(slug) {
			return dbq.VerifyApp{}, "", &messaging.ValidationError{Field: "slug", Message: "Use 1 to 40 lowercase letters, digits and dashes, starting with a letter or digit."}
		}
		if taken(slug) {
			return dbq.VerifyApp{}, "", &messaging.ValidationError{Field: "slug", Message: "Another app in this project uses this slug."}
		}
	} else {
		slug = slugFromName(c.Name)
		for base := slug; taken(slug); {
			slug = strings.Trim(base[:min(len(base), 34)], "-") + "-" + strings.ToLower(auth.RandomString(5))
		}
	}

	appID := id.New(id.VerifyApp)
	params := dbq.InsertVerifyAppParams{ID: appID, ProjectID: projectID, Slug: slug, Name: c.Name, PublishableKey: newPublishableKey()}
	// Without BRIDGE_SECRET_KEY the app gets its secret once the key is set.
	secret := ""
	if s.box.Ready() {
		secret = newAppSecret()
		if params.Secret, err = s.box.Seal(appID, []byte(secret)); err != nil {
			return dbq.VerifyApp{}, "", err
		}
	}
	if err := s.applyConfig(appID, c, turnstileSecret, nil, func(u dbq.UpdateVerifyAppParams) {
		params.AppName, params.Template, params.CodeLength, params.TtlSeconds = u.AppName, u.Template, u.CodeLength, u.TtlSeconds
		params.MaxAttempts, params.WebOtpDomain, params.FailoverAfterSeconds = u.MaxAttempts, u.WebOtpDomain, u.FailoverAfterSeconds
		params.AllowedCountries, params.IpHourlyLimit, params.RangeHourlyLimit = u.AllowedCountries, u.IpHourlyLimit, u.RangeHourlyLimit
		params.CountryHourlyLimit, params.AllowedOrigins, params.RedirectUris = u.CountryHourlyLimit, u.AllowedOrigins, u.RedirectUris
		params.WidgetEnvironment, params.TurnstileSiteKey, params.TurnstileSecret = u.WidgetEnvironment, u.TurnstileSiteKey, u.TurnstileSecret
	}); err != nil {
		return dbq.VerifyApp{}, "", err
	}
	app, err := s.q.InsertVerifyApp(ctx, params)
	if db.IsUniqueViolation(err, "") {
		return app, "", &messaging.ValidationError{Field: "slug", Message: "Another app in this project uses this slug."}
	}
	return app, secret, err
}

// UpdateApp validates and stores an app's configuration. turnstileSecret nil
// keeps the stored Turnstile secret; "" removes it.
func (s *Service) UpdateApp(ctx context.Context, app dbq.VerifyApp, c AppConfig, turnstileSecret *string) (dbq.VerifyApp, error) {
	if err := c.normalize(); err != nil {
		return app, err
	}
	params := dbq.UpdateVerifyAppParams{ID: app.ID, ProjectID: app.ProjectID}
	secret := ""
	if turnstileSecret != nil {
		secret = *turnstileSecret
	}
	keep := app.TurnstileSecret
	if turnstileSecret != nil {
		keep = nil
	}
	if err := s.applyConfig(app.ID, c, secret, keep, func(u dbq.UpdateVerifyAppParams) {
		u.ID, u.ProjectID = app.ID, app.ProjectID
		params = u
	}); err != nil {
		return app, err
	}
	updated, err := s.q.UpdateVerifyApp(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return app, ErrAppNotFound
	}
	return updated, err
}

// applyConfig turns a validated configuration into column values. A new
// Turnstile secret is sealed; keep is the already sealed one to keep.
func (s *Service) applyConfig(appID string, c AppConfig, turnstileSecret string, keep []byte, set func(dbq.UpdateVerifyAppParams)) error {
	u := dbq.UpdateVerifyAppParams{
		Name: c.Name, AppName: nonEmpty(c.Settings.AppName), Template: nonEmpty(c.Settings.Template),
		CodeLength: int16(c.Settings.CodeLength), TtlSeconds: int32(c.Settings.TTL / time.Second), MaxAttempts: int16(c.Settings.MaxAttempts),
		WebOtpDomain: nonEmpty(c.Settings.WebOTPDomain), FailoverAfterSeconds: int32(c.FailoverAfter),
		AllowedCountries: c.AllowedCountries, IpHourlyLimit: int32(c.IPHourlyLimit), RangeHourlyLimit: int32(c.RangeHourlyLimit),
		AllowedOrigins: c.AllowedOrigins, RedirectUris: c.RedirectURIs, WidgetEnvironment: c.WidgetEnvironment,
		TurnstileSiteKey: nonEmpty(c.TurnstileSiteKey), TurnstileSecret: keep,
	}
	if c.CountryHourlyLimit > 0 {
		n := int32(c.CountryHourlyLimit)
		u.CountryHourlyLimit = &n
	}
	if secret := strings.TrimSpace(turnstileSecret); secret != "" {
		if len(secret) > 200 {
			return &messaging.ValidationError{Field: "turnstile_secret", Message: "Use the secret key from the Cloudflare dashboard."}
		}
		sealed, err := s.box.Seal(turnstileAD(appID), []byte(secret))
		if err != nil {
			return err
		}
		u.TurnstileSecret = sealed
	}
	if (u.TurnstileSiteKey == nil) != (u.TurnstileSecret == nil) {
		return &messaging.ValidationError{Field: "turnstile_secret", Message: "Set both the Turnstile site key and secret key, or neither."}
	}
	set(u)
	return nil
}

// DeleteApp removes an app other than the default one.
func (s *Service) DeleteApp(ctx context.Context, app dbq.VerifyApp) error {
	if app.Slug == DefaultAppSlug {
		return ErrDefaultApp
	}
	n, err := s.q.DeleteVerifyApp(ctx, dbq.DeleteVerifyAppParams{ID: app.ID, ProjectID: app.ProjectID})
	if err == nil && n == 0 {
		return ErrAppNotFound
	}
	return err
}

// RotateSecret replaces an app's signing secret and returns the new one.
// Tokens signed with the old secret stop verifying.
func (s *Service) RotateSecret(ctx context.Context, app dbq.VerifyApp) (dbq.VerifyApp, string, error) {
	secret := newAppSecret()
	sealed, err := s.box.Seal(app.ID, []byte(secret))
	if err != nil {
		return app, "", err
	}
	app, err = s.q.SetVerifyAppSecret(ctx, dbq.SetVerifyAppSecretParams{ID: app.ID, ProjectID: app.ProjectID, Secret: sealed})
	return app, secret, err
}

// AppSecret returns an app's signing secret, creating it if the app has none
// yet (apps created before BRIDGE_SECRET_KEY was set).
func (s *Service) AppSecret(ctx context.Context, app dbq.VerifyApp) (string, error) {
	if app.Secret == nil {
		sealed, err := s.box.Seal(app.ID, []byte(newAppSecret()))
		if err != nil {
			return "", err
		}
		if _, err := s.q.SetVerifyAppSecretIfMissing(ctx, dbq.SetVerifyAppSecretIfMissingParams{ID: app.ID, Secret: sealed}); err != nil {
			return "", err
		}
		if app, err = s.q.GetVerifyAppByID(ctx, app.ID); err != nil {
			return "", err
		}
	}
	plain, err := s.box.Open(app.ID, app.Secret)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// turnstileSecret opens an app's Turnstile secret.
func (s *Service) turnstileSecret(app dbq.VerifyApp) (string, error) {
	plain, err := s.box.Open(turnstileAD(app.ID), app.TurnstileSecret)
	return string(plain), err
}

func turnstileAD(appID string) string { return appID + ":turnstile" }

func newPublishableKey() string { return PublishableKeyPrefix + auth.RandomString(32) }

func newAppSecret() string { return AppSecretPrefix + auth.RandomString(48) }

// slugFromName turns an app name into a slug.
func slugFromName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "app"
	}
	return slug
}
