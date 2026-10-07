// Package httpapi serves Bridge's REST API.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bridge/internal/config"
	"bridge/internal/db/dbq"
	"bridge/internal/events"
	"bridge/internal/gateway"
	"bridge/internal/messaging"
	"bridge/internal/otp"
	"bridge/internal/provider"
	"bridge/internal/push"
	"bridge/internal/ratelimit"
	"bridge/internal/reqlog"
	"bridge/internal/secretbox"
	"bridge/internal/status"
	"bridge/internal/webhook"
)

type Server struct {
	cfg     *config.Config
	pool    *pgxpool.Pool
	q       *dbq.Queries
	log     *slog.Logger
	limiter ratelimit.Limiter
	hub     *gateway.Hub
	push    *push.Service
	msgs    *messaging.Service
	otp     *otp.Service
	// providers manages SMS provider accounts; always set (it refuses to store
	// credentials without BRIDGE_SECRET_KEY).
	providers *provider.Router
	hooks     *webhook.Service
	reqlog    *reqlog.Recorder
	events    *events.Broker
	status    *status.Service
	version   string
}

type Options struct {
	Config    *config.Config
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Version   string
	Hub       *gateway.Hub
	Push      *push.Service
	Messaging *messaging.Service
	Webhooks  *webhook.Service
	// RequestLog records developer API requests; nil disables the request log.
	RequestLog *reqlog.Recorder
	// Events serves the live event stream; nil disables it.
	Events *events.Broker
	// Providers manages SMS provider accounts and integrations' secrets.
	Providers *provider.Router
	// Status serves /v1/status and /v1/system; nil disables them.
	Status  *status.Service
	Limiter ratelimit.Limiter // defaults to the Postgres limiter
}

func New(o Options) *Server {
	q := dbq.New(o.Pool)
	limiter := o.Limiter
	if limiter == nil {
		limiter = ratelimit.NewPostgres(q)
	}
	s := &Server{cfg: o.Config, pool: o.Pool, q: q, log: o.Logger, limiter: limiter, hub: o.Hub, push: o.Push, msgs: o.Messaging, hooks: o.Webhooks, reqlog: o.RequestLog, events: o.Events, status: o.Status, version: o.Version}
	s.providers = o.Providers
	if s.providers == nil {
		box, _ := secretbox.New(o.Config.SecretKey)
		s.providers = provider.NewRouter(q, box, provider.RouterOptions{PublicURL: o.Config.PublicURL.String()})
	}
	if o.Messaging != nil {
		var emitter otp.Emitter
		if o.Webhooks != nil { // a nil *webhook.Service must not become a non-nil interface
			emitter = o.Webhooks
		}
		s.otp = otp.New(o.Pool, o.Messaging, limiter, emitter, o.Logger)
	}
	return s
}

// Handler returns the complete HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(
		requestID,
		clientIP(s.cfg.TrustedProxies),
		accessLog(s.log),
		recoverer(s.log),
		securityHeaders(s.cfg.PublicURL.Scheme == "https"),
		originCheck(s.cfg.DashboardOrigin()),
		requestLog(s.reqlog),
	)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
	})
	r.Get("/readyz", s.ready)
	r.Get("/v1/device/connect", s.deviceConnect)
	r.Get("/v1/events/stream", s.eventStream)
	// Provider delivery reports: unauthenticated, the URL carries the account's secret.
	r.Post("/v1/provider-callbacks/{providerId}/{token}", s.providerCallback)
	r.Get("/v1/provider-callbacks/{providerId}/{token}", s.providerCallback)
	// Supabase Auth's Send SMS hook, signed with the integration's secret.
	r.Post("/v1/hooks/supabase/{integrationId}", s.supabaseSendSMS)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeRawError(w, r, http.StatusNotFound, CodeNotFound, "No route matches "+r.Method+" "+r.URL.Path+". See /docs for the API reference.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeRawError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, r.Method+" is not supported on "+r.URL.Path+".")
	})

	api := newAPI(r, s.version, s.cfg.PublicURL.String(), s.log)
	api.UseMiddleware(s.authenticate(api))
	s.register(api)
	return r
}

func (s *Server) register(api huma.API) {
	s.registerAuth(api)
	s.registerOrganizations(api)
	s.registerProjects(api)
	s.registerAPIKeys(api)
	s.registerDeveloper(api)
	s.registerDevices(api)
	s.registerDeviceSelf(api)
	s.registerMessages(api)
	s.registerWebhooks(api)
	s.registerLogs(api)
	s.registerUsage(api)
	s.registerTeams(api)
	s.registerAccount(api)
	s.registerStatus(api)
	s.registerOTP(api)
	s.registerProviders(api)
	s.registerIntegrations(api)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		s.log.Error("readiness check failed", "error", err)
		writeRawError(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Database is unreachable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

var errorModelOnce sync.Once

func newAPI(r chi.Router, version, serverURL string, logger *slog.Logger) huma.API {
	errorModelOnce.Do(func() {
		installErrorModel(logger)
		// Handlers always return non-nil slices, so arrays are never null in the contract.
		huma.DefaultArrayNullable = false
	})

	cfg := huma.DefaultConfig("Bridge API", version)
	cfg.CreateHooks = nil // no $schema links in response bodies
	cfg.DocsRenderer = huma.DocsRendererScalar
	cfg.Info.Description = "Open-source infrastructure for SMS and phone verification. " +
		"Developer endpoints authenticate with an API key (`Authorization: Bearer bk_live_…`). " +
		"Dashboard endpoints use a session cookie."
	cfg.Info.License = &huma.License{Name: "AGPL-3.0-only", Identifier: "AGPL-3.0-only"}
	cfg.Servers = []*huma.Server{{URL: serverURL}}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		secAPIKey: {
			Type: "http", Scheme: "bearer", BearerFormat: "bk_live_… or bk_test_…",
			Description: "Project API key. Create one in the dashboard under API keys. Test keys never send real SMS.",
		},
		secSession: {
			Type: "apiKey", In: "cookie", Name: SessionCookie,
			Description: "Dashboard session cookie, set by `POST /v1/auth/login`.",
		},
		secDevice: {
			Type: "http", Scheme: "bearer", BearerFormat: "bd_…",
			Description: "Device credential issued to the Android gateway when it pairs.",
		},
	}
	cfg.Tags = []*huma.Tag{
		{Name: "Developer API", Description: "Endpoints your application calls with an API key."},
		{Name: "Auth", Description: "Dashboard sign-up, sign-in and sessions."},
		{Name: "Organizations"},
		{Name: "Projects"},
		{Name: "API keys"},
		{Name: "Devices", Description: "Paired Android gateways."},
		{Name: "Gateway", Description: "Endpoints used by the Bridge Android app itself."},
		{Name: "Messages", Description: "Message history for the dashboard."},
		{Name: "Verify", Description: "One-time passwords for the dashboard."},
		{Name: "Integrations", Description: "Connections to other services, such as Supabase Auth's Send SMS hook."},
		{Name: "Providers", Description: "SMS providers (MSG91, Twilio, Vonage, Plivo) and routing between them and your phones."},
		{Name: "Status", Description: "Service health for the public status page and operators."},
		{Name: "Logs", Description: "Developer API request logs for the dashboard."},
		{Name: "Webhooks", Description: "Endpoints that receive signed event notifications (Standard Webhooks)."},
	}
	cfg.Transformers = append(cfg.Transformers, requestIDTransformer)
	return humachi.New(r, cfg)
}

// OpenAPI returns the OpenAPI 3.1 document without connecting to a database.
func OpenAPI(version string) ([]byte, error) {
	s := &Server{log: slog.Default(), version: version}
	api := newAPI(chi.NewRouter(), version, "http://localhost:8080", s.log)
	s.register(api)
	return json.MarshalIndent(api.OpenAPI(), "", "  ")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
