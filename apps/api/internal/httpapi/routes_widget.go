package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/db/dbq"
	"bridge/internal/otp"
)

// The drop-in widget and the hosted verification page call these endpoints
// from the browser. They need no credentials: the app's publishable key in
// the path identifies it, CORS admits only the app's allowed origins (and the
// dashboard, which serves the hosted page), and per-IP rate limits, the app's
// fraud protection and optionally Cloudflare Turnstile guard sends.

// Per-IP rate limits on the public widget endpoints, per minute.
const (
	widgetConfigPerMin   = 120
	widgetSendPerMin     = 10
	widgetVerifyPerMin   = 30
	widgetRedirectPerMin = 60
)

type widgetAppKeyType struct{}

var widgetAppKey widgetAppKeyType

type WidgetPath struct {
	PublishableKey string `path:"publishableKey" pattern:"^bpk_[0-9A-Za-z]{32}$" example:"bpk_9fK2mQ7xR4tV8wY1zA3bC5dE6gH0jL2n"`
}

// WidgetConfig is what the widget needs to render.
type WidgetConfig struct {
	AppName          string  `json:"app_name" example:"Acme" doc:"The name to show, as in the SMS."`
	CodeLength       int     `json:"code_length" example:"6"`
	TTLSeconds       int     `json:"ttl_seconds" example:"600"`
	TurnstileSiteKey *string `json:"turnstile_site_key" nullable:"true" doc:"When set, render Cloudflare Turnstile and send its token with each send."`
	Environment      string  `json:"environment" enum:"live,test" doc:"test sends nothing and returns the code."`
}

type WidgetSendResult struct {
	VerificationID    string    `json:"verification_id" example:"otp_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	ExpiresAt         time.Time `json:"expires_at"`
	ResendAvailableAt time.Time `json:"resend_available_at"`
	Code              *string   `json:"code,omitempty" doc:"Only for test-environment widgets."`
}

type WidgetVerifyResult struct {
	Valid             bool       `json:"valid"`
	Status            string     `json:"status" enum:"pending,verified,expired,failed,canceled"`
	AttemptsRemaining int        `json:"attempts_remaining"`
	Token             *string    `json:"token,omitempty" doc:"Only when valid: a signed JWT (HS256, the app's secret) proving the number was verified. Send it to your server and check it there, or with POST /v1/otp/tokens/verify."`
	TokenExpiresAt    *time.Time `json:"token_expires_at,omitempty"`
}

type WidgetRedirectCheck struct {
	OK bool `json:"ok"`
}

// widgetCORS answers preflight requests to /v1/widget/{publishableKey}/… and
// admits only the app's allowed origins. It also loads the app for the handler.
func (s *Server) widgetCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/v1/widget/")
		if !ok || s.otp == nil {
			next.ServeHTTP(w, r)
			return
		}
		key, _, _ := strings.Cut(rest, "/")
		w.Header().Add("Vary", "Origin")
		app, err := s.otp.AppByPublishableKey(r.Context(), key)
		if err != nil {
			if !errors.Is(err, otp.ErrAppNotFound) {
				s.log.Error("load widget app", "request_id", RequestIDFrom(r.Context()), "error", err)
				writeRawError(w, r, http.StatusInternalServerError, CodeInternal, "Bridge hit an unexpected error. It has been logged.")
				return
			}
			writeRawError(w, r, http.StatusNotFound, CodeNotFound, "No Verify app has this publishable key. Copy it again from the Bridge dashboard.")
			return
		}
		origin := r.Header.Get("Origin")
		allowed := origin != "" && s.widgetOriginAllowed(app, origin)
		if allowed {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Expose-Headers", "Retry-After, X-Request-Id")
			h.Set("Access-Control-Max-Age", "600")
		}
		if origin != "" && !allowed {
			writeRawError(w, r, http.StatusForbidden, CodeForbidden,
				"The origin "+clipString(origin, 100)+" may not use this Verify app. Add it to the app's allowed origins in the Bridge dashboard.")
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), widgetAppKey, app)))
	})
}

func (s *Server) widgetOriginAllowed(app dbq.VerifyApp, origin string) bool {
	if origin == s.cfg.DashboardOrigin() {
		return true // the hosted page
	}
	o, ok := otp.NormalizeOrigin(origin)
	return ok && o == origin && slices.Contains(app.AllowedOrigins, o)
}

// widgetApp returns the app widgetCORS loaded, or loads it.
func (s *Server) widgetApp(ctx context.Context, key string) (dbq.VerifyApp, error) {
	if app, ok := ctx.Value(widgetAppKey).(dbq.VerifyApp); ok && app.PublishableKey == key {
		return app, nil
	}
	app, err := s.otp.AppByPublishableKey(ctx, key)
	if errors.Is(err, otp.ErrAppNotFound) {
		return app, Errorf(http.StatusNotFound, CodeNotFound, "No Verify app has this publishable key. Copy it again from the Bridge dashboard.")
	}
	return app, err
}

func (s *Server) widgetLimit(ctx context.Context, op string, perMin int) error {
	return s.limit(ctx, "widget:"+op+":"+ClientIPFrom(ctx).String(), perMin, time.Minute)
}

func (s *Server) registerWidget(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getWidgetConfig", Method: http.MethodGet, Path: "/v1/widget/{publishableKey}", Tags: []string{"Widget"},
		Summary: "Get the widget's settings", Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *WidgetPath) (*struct{ Body WidgetConfig }, error) {
		app, err := s.widgetApp(ctx, in.PublishableKey)
		if err != nil {
			return nil, err
		}
		if err := s.widgetLimit(ctx, "config", widgetConfigPerMin); err != nil {
			return nil, err
		}
		project, err := s.q.GetProjectByID(ctx, app.ProjectID)
		if err != nil {
			return nil, err
		}
		st := otp.SettingsOf(app)
		return &struct{ Body WidgetConfig }{Body: WidgetConfig{
			AppName: st.EffectiveAppName(project.Name), CodeLength: st.CodeLength, TTLSeconds: int(st.TTL / time.Second),
			TurnstileSiteKey: app.TurnstileSiteKey, Environment: string(app.WidgetEnvironment),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "sendWidgetCode", Method: http.MethodPost, Path: "/v1/widget/{publishableKey}/send", Tags: []string{"Widget"},
		Summary: "Send a code from the widget",
		Description: "Applies the app's fraud protection with the caller's IP address, and its Turnstile check when configured. " +
			"Refusals use the `otp_blocked` error code (403 or 429).",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct {
		WidgetPath
		Body struct {
			To             string `json:"to" minLength:"3" maxLength:"32" example:"+919876543210" doc:"The number to verify, in E.164 format."`
			TurnstileToken string `json:"turnstile_token,omitempty" maxLength:"2048" doc:"The Cloudflare Turnstile response, when the app uses Turnstile."`
		}
	}) (*struct{ Body WidgetSendResult }, error) {
		app, err := s.widgetApp(ctx, in.PublishableKey)
		if err != nil {
			return nil, err
		}
		if err := s.widgetLimit(ctx, "send", widgetSendPerMin); err != nil {
			return nil, err
		}
		project, err := s.q.GetProjectByID(ctx, app.ProjectID)
		if err != nil {
			return nil, err
		}
		v, err := s.otp.Send(ctx, otp.SendRequest{
			ProjectID: app.ProjectID, ProjectName: project.Name, Environment: app.WidgetEnvironment, App: app.ID,
			To: in.Body.To, Metadata: map[string]any{"source": "widget"}, ClientIP: ClientIPFrom(ctx),
			Widget: true, TurnstileToken: in.Body.TurnstileToken,
		})
		if err != nil {
			return nil, otpError(err)
		}
		return &struct{ Body WidgetSendResult }{Body: WidgetSendResult{
			VerificationID: v.ID, ExpiresAt: v.ExpiresAt, ResendAvailableAt: v.ResendAvailableAt, Code: v.Code,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "checkWidgetCode", Method: http.MethodPost, Path: "/v1/widget/{publishableKey}/verify", Tags: []string{"Widget"},
		Summary: "Check a code from the widget",
		Description: "A wrong code uses one attempt. A right one returns a signed token (valid 10 minutes) that proves the number " +
			"was verified; pass it to your server.",
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		WidgetPath
		Body struct {
			VerificationID string `json:"verification_id" pattern:"^otp_[0-9a-z]{26}$"`
			Code           string `json:"code" minLength:"4" maxLength:"10" example:"482913"`
		}
	}) (*struct{ Body WidgetVerifyResult }, error) {
		app, err := s.widgetApp(ctx, in.PublishableKey)
		if err != nil {
			return nil, err
		}
		if err := s.widgetLimit(ctx, "verify", widgetVerifyPerMin); err != nil {
			return nil, err
		}
		// Make sure a token can be signed before an attempt is used up.
		if _, err := s.otp.AppSecret(ctx, app); err != nil {
			return nil, otpError(err)
		}
		res, err := s.otp.Verify(ctx, otp.VerifyRequest{
			ProjectID: app.ProjectID, Environment: app.WidgetEnvironment, AppID: app.ID, ID: in.Body.VerificationID, Code: in.Body.Code,
		})
		if err != nil {
			return nil, otpError(err)
		}
		v := res.Verification
		out := WidgetVerifyResult{Valid: res.Valid, Status: v.Status, AttemptsRemaining: v.AttemptsRemaining}
		if res.Valid {
			token, exp, err := s.otp.IssueToken(ctx, app, v)
			if err != nil {
				return nil, otpError(err)
			}
			out.Token, out.TokenExpiresAt = &token, &exp
		}
		return &struct{ Body WidgetVerifyResult }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "checkWidgetRedirect", Method: http.MethodGet, Path: "/v1/widget/{publishableKey}/redirect-check", Tags: []string{"Widget"},
		Summary:     "Check a hosted page redirect URI",
		Description: "200 when redirect_uri exactly matches one of the app's redirect URIs; 400 otherwise. The hosted page checks this before starting.",
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		WidgetPath
		RedirectURI string `query:"redirect_uri" maxLength:"2048" doc:"The URL the hosted page would send the user back to."`
	}) (*struct{ Body WidgetRedirectCheck }, error) {
		app, err := s.widgetApp(ctx, in.PublishableKey)
		if err != nil {
			return nil, err
		}
		if err := s.widgetLimit(ctx, "redirect", widgetRedirectPerMin); err != nil {
			return nil, err
		}
		if in.RedirectURI == "" || !slices.Contains(app.RedirectUris, in.RedirectURI) {
			return nil, Errorf(http.StatusBadRequest, CodeInvalidRequest,
				"This redirect URI is not registered for the app. Add it under the app's redirect URIs in the Bridge dashboard.")
		}
		return &struct{ Body WidgetRedirectCheck }{Body: WidgetRedirectCheck{OK: true}}, nil
	})
}
