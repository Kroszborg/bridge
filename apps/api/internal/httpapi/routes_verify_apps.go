package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/db/dbq"
	"bridge/internal/otp"
)

// VerifyApp is one product or customer's verification setup: its message,
// code settings, fraud protection, delivery failover and widget.
type VerifyApp struct {
	ID        string `json:"id" example:"vap_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Slug      string `json:"slug" example:"default" doc:"Pass it (or the ID) as app when sending a code."`
	Name      string `json:"name" example:"Default"`
	IsDefault bool   `json:"is_default" doc:"Used when a request names no app. It cannot be deleted."`

	AppName          *string `json:"app_name" nullable:"true" doc:"Replaces {app} in the message. Null uses the project name."`
	Template         *string `json:"template" nullable:"true" doc:"The SMS text with {code}, and optionally {app} and {minutes}. Null uses the default."`
	CodeLength       int     `json:"code_length"`
	TTLSeconds       int     `json:"ttl_seconds"`
	MaxAttempts      int     `json:"max_attempts"`
	WebOTPDomain     *string `json:"web_otp_domain" nullable:"true"`
	DefaultTemplate  string  `json:"default_template"`
	EffectiveAppName string  `json:"effective_app_name"`
	Preview          string  `json:"preview" doc:"The SMS these settings produce, with an example code."`

	FailoverAfterSeconds int `json:"failover_after_seconds" doc:"Live codes whose SMS no phone accepted within this many seconds (or that failed) are resent once through another route: the project's SMS providers, or another online phone. 0 is off."`

	AllowedCountries   []string `json:"allowed_countries" doc:"ISO 3166-1 alpha-2 codes. Empty allows every country."`
	IPHourlyLimit      int      `json:"ip_hourly_limit" doc:"Codes per end-user IP address per hour (from client_ip, or the widget's caller). 0 is off."`
	RangeHourlyLimit   int      `json:"range_hourly_limit" doc:"Codes per hour to numbers that differ only in their last 3 digits. Catches sequential-number pumping. 0 is off."`
	CountryHourlyLimit *int     `json:"country_hourly_limit" nullable:"true" doc:"Codes per hour to any one country. Null is no cap."`

	PublishableKey     string   `json:"publishable_key" example:"bpk_9fK2…" doc:"Identifies the app to the drop-in widget and hosted page. Safe to expose."`
	AllowedOrigins     []string `json:"allowed_origins" doc:"Origins whose pages may embed the widget. The dashboard origin (hosted page) is always allowed."`
	RedirectURIs       []string `json:"redirect_uris" doc:"Exact URLs the hosted page may send the user back to."`
	WidgetEnvironment  string   `json:"widget_environment" enum:"live,test" doc:"test widgets send nothing and show the code, for development."`
	TurnstileSiteKey   *string  `json:"turnstile_site_key" nullable:"true" doc:"Cloudflare Turnstile site key. When set, widget sends need a valid Turnstile token."`
	TurnstileSecretSet bool     `json:"turnstile_secret_set"`
	SecretSet          bool     `json:"secret_set" doc:"Whether the app has a token signing secret. It is created when needed once BRIDGE_SECRET_KEY is set."`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreatedVerifyApp is a new app with its signing secret, shown this once (it can be revealed again later).
type CreatedVerifyApp struct {
	VerifyApp
	Secret *string `json:"secret" nullable:"true" doc:"Signs verification tokens (HS256; the secret string is the key). Null without BRIDGE_SECRET_KEY."`
}

type VerifyAppSecret struct {
	Secret string `json:"secret" example:"bvs_…" doc:"Verify tokens with HS256 using this string, as UTF-8 bytes, as the key."`
}

type VerifyAppList struct {
	Data []VerifyApp `json:"data"`
}

// Block is a send attempt refused by fraud protection.
type Block = otp.Block

type BlockList struct {
	Data    []Block `json:"data"`
	HasMore bool    `json:"has_more" doc:"Pass the last block's ID as starting_after to fetch the next page."`
}

// VerifyAppFields are the optional settings of an app; omitted fields stay
// unchanged (or take their defaults on creation).
type VerifyAppFields struct {
	AppName      *string `json:"app_name,omitempty" maxLength:"40" doc:"Replaces {app}. An empty string uses the project name."`
	Template     *string `json:"template,omitempty" maxLength:"300" doc:"The SMS text with {code}. An empty string uses the default template."`
	CodeLength   *int    `json:"code_length,omitempty" minimum:"4" maximum:"10"`
	TTLSeconds   *int    `json:"ttl_seconds,omitempty" minimum:"60" maximum:"3600"`
	MaxAttempts  *int    `json:"max_attempts,omitempty" minimum:"1" maximum:"10"`
	WebOTPDomain *string `json:"web_otp_domain,omitempty" maxLength:"253" doc:"An empty string removes it."`

	FailoverAfterSeconds *int `json:"failover_after_seconds,omitempty" minimum:"0" maximum:"600" doc:"0 turns failover off. Default 30."`

	AllowedCountries   []string `json:"allowed_countries,omitempty" maxItems:"250" doc:"ISO 3166-1 alpha-2 codes. An empty list allows every country."`
	IPHourlyLimit      *int     `json:"ip_hourly_limit,omitempty" minimum:"0" maximum:"100000" doc:"Default 10. 0 is off."`
	RangeHourlyLimit   *int     `json:"range_hourly_limit,omitempty" minimum:"0" maximum:"100000" doc:"Default 20. 0 is off."`
	CountryHourlyLimit *int     `json:"country_hourly_limit,omitempty" minimum:"0" maximum:"1000000" doc:"0 removes the cap."`

	AllowedOrigins    []string `json:"allowed_origins,omitempty" maxItems:"20" doc:"https origins, such as https://shop.example.com (http only for localhost)."`
	RedirectURIs      []string `json:"redirect_uris,omitempty" maxItems:"20" doc:"Absolute https URLs, matched exactly."`
	WidgetEnvironment *string  `json:"widget_environment,omitempty" enum:"live,test"`
	TurnstileSiteKey  *string  `json:"turnstile_site_key,omitempty" maxLength:"100" doc:"An empty string removes Turnstile (send an empty turnstile_secret too)."`
	TurnstileSecret   *string  `json:"turnstile_secret,omitempty" maxLength:"200" writeOnly:"true" doc:"Turnstile secret key. Write-only; stored encrypted."`
}

type VerifyAppCreateInput struct {
	Name string `json:"name" minLength:"1" maxLength:"60" example:"Acme Shop"`
	Slug string `json:"slug,omitempty" pattern:"^[a-z0-9][a-z0-9-]{0,39}$" doc:"Derived from the name when left out. Cannot be changed later."`
	VerifyAppFields
}

type VerifyAppUpdateInput struct {
	Name *string `json:"name,omitempty" minLength:"1" maxLength:"60"`
	VerifyAppFields
}

type VerifyAppPath struct {
	ProjectPath
	AppID string `path:"appId" pattern:"^vap_[0-9a-z]{26}$" example:"vap_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

// TokenVerification is the result of checking a widget's verification token.
type TokenVerification struct {
	Valid          bool       `json:"valid"`
	Reason         *string    `json:"reason" nullable:"true" enum:"malformed,unknown_app,bad_signature,wrong_issuer,expired,environment_mismatch,verification_mismatch" doc:"Why the token is not valid."`
	Phone          *string    `json:"phone" nullable:"true" example:"+919876543210" doc:"The verified number (E.164)."`
	VerificationID *string    `json:"verification_id" nullable:"true"`
	AppID          *string    `json:"app_id" nullable:"true"`
	Environment    *string    `json:"environment" nullable:"true" enum:"live,test"`
	ExpiresAt      *time.Time `json:"expires_at" nullable:"true"`
}

func (s *Server) toVerifyApp(a dbq.VerifyApp, projectName string) VerifyApp {
	st := otp.SettingsOf(a)
	out := VerifyApp{
		ID: a.ID, Slug: a.Slug, Name: a.Name, IsDefault: a.Slug == otp.DefaultAppSlug,
		AppName: a.AppName, Template: a.Template, CodeLength: int(a.CodeLength), TTLSeconds: int(a.TtlSeconds),
		MaxAttempts: int(a.MaxAttempts), WebOTPDomain: a.WebOtpDomain, DefaultTemplate: otp.DefaultTemplate,
		EffectiveAppName: st.EffectiveAppName(projectName), FailoverAfterSeconds: int(a.FailoverAfterSeconds),
		AllowedCountries: nonNilStrings(a.AllowedCountries), IPHourlyLimit: int(a.IpHourlyLimit), RangeHourlyLimit: int(a.RangeHourlyLimit),
		PublishableKey: a.PublishableKey, AllowedOrigins: nonNilStrings(a.AllowedOrigins), RedirectURIs: nonNilStrings(a.RedirectUris),
		WidgetEnvironment: string(a.WidgetEnvironment), TurnstileSiteKey: a.TurnstileSiteKey, TurnstileSecretSet: a.TurnstileSecret != nil,
		SecretSet: a.Secret != nil, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if a.CountryHourlyLimit != nil {
		n := int(*a.CountryHourlyLimit)
		out.CountryHourlyLimit = &n
	}
	out.Preview = toOTPSettings(st, projectName).Preview
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// apply copies the fields present in f onto c and lists their names, for the audit log.
func (f VerifyAppFields) apply(c *otp.AppConfig) []string {
	var changed []string
	setStr := func(name string, src *string, dst *string) {
		if src != nil {
			*dst, changed = *src, append(changed, name)
		}
	}
	setInt := func(name string, src *int, dst *int) {
		if src != nil {
			*dst, changed = *src, append(changed, name)
		}
	}
	setList := func(name string, src []string, dst *[]string) {
		if src != nil {
			*dst, changed = src, append(changed, name)
		}
	}
	setStr("app_name", f.AppName, &c.Settings.AppName)
	setStr("template", f.Template, &c.Settings.Template)
	setInt("code_length", f.CodeLength, &c.Settings.CodeLength)
	if f.TTLSeconds != nil {
		c.Settings.TTL, changed = time.Duration(*f.TTLSeconds)*time.Second, append(changed, "ttl_seconds")
	}
	setInt("max_attempts", f.MaxAttempts, &c.Settings.MaxAttempts)
	setStr("web_otp_domain", f.WebOTPDomain, &c.Settings.WebOTPDomain)
	setInt("failover_after_seconds", f.FailoverAfterSeconds, &c.FailoverAfter)
	setList("allowed_countries", f.AllowedCountries, &c.AllowedCountries)
	setInt("ip_hourly_limit", f.IPHourlyLimit, &c.IPHourlyLimit)
	setInt("range_hourly_limit", f.RangeHourlyLimit, &c.RangeHourlyLimit)
	setInt("country_hourly_limit", f.CountryHourlyLimit, &c.CountryHourlyLimit)
	setList("allowed_origins", f.AllowedOrigins, &c.AllowedOrigins)
	setList("redirect_uris", f.RedirectURIs, &c.RedirectURIs)
	if f.WidgetEnvironment != nil {
		c.WidgetEnvironment, changed = dbq.APIEnvironment(*f.WidgetEnvironment), append(changed, "widget_environment")
	}
	setStr("turnstile_site_key", f.TurnstileSiteKey, &c.TurnstileSiteKey)
	if f.TurnstileSecret != nil {
		changed = append(changed, "turnstile_secret")
	}
	return changed
}

func (s *Server) verifyAppForUser(ctx context.Context, in *VerifyAppPath) (dbq.GetProjectForUserRow, dbq.VerifyApp, error) {
	p, err := s.projectForUser(ctx, in.ProjectID)
	if err != nil {
		return p, dbq.VerifyApp{}, err
	}
	app, err := s.otp.GetApp(ctx, p.ID, in.AppID)
	if err != nil {
		return p, app, verifyAppError(err, in.AppID)
	}
	return p, app, nil
}

func verifyAppError(err error, appID string) error {
	if errors.Is(err, otp.ErrAppNotFound) {
		return notFound("Verify app " + appID)
	}
	return otpError(err)
}

func (s *Server) registerVerifyApps(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listVerifyApps", Method: http.MethodGet, Path: "/v1/projects/{projectId}/verify-apps", Tags: []string{"Verify"},
		Summary: "List Verify apps", Description: "The default app comes first.", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body VerifyAppList }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		rows, err := s.otp.ListApps(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		out := make([]VerifyApp, 0, len(rows))
		for _, a := range rows {
			out = append(out, s.toVerifyApp(a, p.Name))
		}
		return &struct{ Body VerifyAppList }{Body: VerifyAppList{Data: out}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createVerifyApp", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/verify-apps", Tags: []string{"Verify"},
		Summary:     "Create a Verify app",
		Description: "Returns the app's token signing secret once; reveal it again with the secret endpoint. Omitted settings take their defaults; app_name defaults to the app's name.",
		Security:    sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body VerifyAppCreateInput
	}) (*struct{ Body CreatedVerifyApp }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		c := otp.DefaultAppConfig(in.Body.Name)
		if name := strings.TrimSpace(in.Body.Name); utf8.RuneCountInString(name) <= 40 {
			c.Settings.AppName = name // {app} is the app's name unless app_name says otherwise
		}
		in.Body.apply(&c)
		app, secret, err := s.otp.CreateApp(ctx, p.ID, in.Body.Slug, c, deref(in.Body.TurnstileSecret))
		if err != nil {
			return nil, otpError(err)
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "verify_app.created", TargetType: "verify_app", TargetID: app.ID,
			Metadata: map[string]any{"name": app.Name, "slug": app.Slug},
		}); err != nil {
			return nil, err
		}
		out := CreatedVerifyApp{VerifyApp: s.toVerifyApp(app, p.Name)}
		if secret != "" {
			out.Secret = &secret
		}
		return &struct{ Body CreatedVerifyApp }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getVerifyApp", Method: http.MethodGet, Path: "/v1/projects/{projectId}/verify-apps/{appId}", Tags: []string{"Verify"},
		Summary: "Get a Verify app", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *VerifyAppPath) (*struct{ Body VerifyApp }, error) {
		p, app, err := s.verifyAppForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		return &struct{ Body VerifyApp }{Body: s.toVerifyApp(app, p.Name)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateVerifyApp", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/verify-apps/{appId}", Tags: []string{"Verify"},
		Summary: "Update a Verify app", Description: "Omitted fields stay unchanged. Lists replace the stored list.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		VerifyAppPath
		Body VerifyAppUpdateInput
	}) (*struct{ Body VerifyApp }, error) {
		p, app, err := s.verifyAppForUser(ctx, &in.VerifyAppPath)
		if err != nil {
			return nil, err
		}
		c := otp.ConfigOf(app)
		changed := in.Body.apply(&c)
		if in.Body.Name != nil {
			c.Name, changed = *in.Body.Name, append(changed, "name")
		}
		if app, err = s.otp.UpdateApp(ctx, app, c, in.Body.TurnstileSecret); err != nil {
			return nil, verifyAppError(err, in.AppID)
		}
		if changed == nil {
			changed = []string{}
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "verify_app.updated", TargetType: "verify_app", TargetID: app.ID,
			Metadata: map[string]any{"changed": changed},
		}); err != nil {
			return nil, err
		}
		return &struct{ Body VerifyApp }{Body: s.toVerifyApp(app, p.Name)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteVerifyApp", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/verify-apps/{appId}", Tags: []string{"Verify"},
		Summary:     "Delete a Verify app",
		Description: "Its widget stops working and its tokens stop verifying. Past verifications are kept without an app. The default app cannot be deleted.",
		Security:    sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *VerifyAppPath) (*struct{}, error) {
		p, app, err := s.verifyAppForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if err := s.otp.DeleteApp(ctx, app); err != nil {
			return nil, verifyAppError(err, in.AppID)
		}
		return nil, s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "verify_app.deleted", TargetType: "verify_app", TargetID: app.ID,
			Metadata: map[string]any{"name": app.Name, "slug": app.Slug},
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "getVerifyAppSecret", Metadata: adminOnly, Method: http.MethodGet, Path: "/v1/projects/{projectId}/verify-apps/{appId}/secret", Tags: []string{"Verify"},
		Summary: "Reveal the token signing secret", Description: "Every reveal is recorded in the audit log.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *VerifyAppPath) (*struct{ Body VerifyAppSecret }, error) {
		p, app, err := s.verifyAppForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		secret, err := s.otp.AppSecret(ctx, app)
		if err != nil {
			return nil, otpError(err)
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "verify_app.secret_revealed", TargetType: "verify_app", TargetID: app.ID,
		}); err != nil {
			return nil, err
		}
		return &struct{ Body VerifyAppSecret }{Body: VerifyAppSecret{Secret: secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rotateVerifyAppSecret", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/verify-apps/{appId}/secret", Tags: []string{"Verify"},
		Summary: "Rotate the token signing secret", Description: "Returns the new secret. Tokens signed with the old one stop verifying at once.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *VerifyAppPath) (*struct{ Body VerifyAppSecret }, error) {
		p, app, err := s.verifyAppForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		app, secret, err := s.otp.RotateSecret(ctx, app)
		if err != nil {
			return nil, otpError(err)
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "verify_app.secret_rotated", TargetType: "verify_app", TargetID: app.ID,
		}); err != nil {
			return nil, err
		}
		return &struct{ Body VerifyAppSecret }{Body: VerifyAppSecret{Secret: secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listVerifyAppBlocks", Method: http.MethodGet, Path: "/v1/projects/{projectId}/verify-apps/{appId}/blocks", Tags: []string{"Verify"},
		Summary: "List blocked send attempts", Description: "Newest first. Kept for 30 days.", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		VerifyAppPath
		Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
		StartingAfter string `query:"starting_after" doc:"A block ID; returns blocks created before it."`
		Reason        string `query:"reason" enum:"country_not_allowed,ip_limit,range_burst,country_limit,captcha_failed"`
		Environment   string `query:"environment" enum:"live,test"`
	}) (*struct{ Body BlockList }, error) {
		_, app, err := s.verifyAppForUser(ctx, &in.VerifyAppPath)
		if err != nil {
			return nil, err
		}
		var env *dbq.APIEnvironment
		if in.Environment != "" {
			e := dbq.APIEnvironment(in.Environment)
			env = &e
		}
		limit := in.Limit
		if limit == 0 {
			limit = 25
		}
		items, more, err := s.otp.ListBlocks(ctx, otp.ListBlocksRequest{
			AppID: app.ID, Environment: env, Reason: in.Reason, StartingAfter: in.StartingAfter, Limit: limit,
		})
		if err != nil {
			return nil, otpError(err)
		}
		return &struct{ Body BlockList }{Body: BlockList{Data: items, HasMore: more}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getVerifyAppStats", Method: http.MethodGet, Path: "/v1/projects/{projectId}/verify-apps/{appId}/stats", Tags: []string{"Verify"},
		Summary: "Verify app statistics for the last 30 days", Description: "Verifications by status, failovers, and blocked attempts by reason.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		VerifyAppPath
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body otp.VerificationStats }, error) {
		p, app, err := s.verifyAppForUser(ctx, &in.VerifyAppPath)
		if err != nil {
			return nil, err
		}
		st, err := s.otp.Stats(ctx, p.ID, &app.ID, dbq.APIEnvironment(in.Environment), time.Now().Add(-30*24*time.Hour))
		if err != nil {
			return nil, err
		}
		return &struct{ Body otp.VerificationStats }{Body: st}, nil
	})

	// ---- Developer API -------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "verifyVerificationToken", Method: http.MethodPost, Path: "/v1/otp/tokens/verify", Tags: []string{"Developer API"},
		Summary: "Check a verification token",
		Description: "Checks a token issued by the Verify widget or hosted page: its signature under the app's secret, issuer, expiry, " +
			"environment (it must match the API key's) and the verification it names. You can also verify tokens yourself: " +
			"they are HS256 JWTs signed with the app's secret, with `aud` = app ID, `sub` = phone number and `vid` = verification ID.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Token string `json:"token" minLength:"1" maxLength:"4096"`
		}
	}) (*struct{ Body TokenVerification }, error) {
		k := principalFrom(ctx).APIKey
		res, err := s.otp.CheckToken(ctx, k.ProjectID, k.Environment, in.Body.Token)
		if err != nil {
			return nil, err
		}
		out := TokenVerification{Valid: res.Valid}
		if !res.Valid {
			out.Reason = &res.Reason
		} else {
			c := res.Claims
			exp := time.Unix(c.ExpiresAt, 0).UTC()
			out.Phone, out.VerificationID, out.AppID, out.Environment, out.ExpiresAt = &c.Subject, &c.VerificationID, &c.Audience, &c.Environment, &exp
			setResource(ctx, c.VerificationID)
		}
		return &struct{ Body TokenVerification }{Body: out}, nil
	})
}
