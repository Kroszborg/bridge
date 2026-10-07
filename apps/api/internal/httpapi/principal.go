package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/auth"
	"bridge/internal/db/dbq"
)

// SessionCookie is the dashboard session cookie name.
const SessionCookie = "bridge_session"

const (
	secSession = "session"
	secAPIKey  = "apiKey"
	secDevice  = "deviceCredential"
)

var (
	sessionAuth = []map[string][]string{{secSession: {}}}
	apiKeyAuth  = []map[string][]string{{secAPIKey: {}}}
	deviceAuth  = []map[string][]string{{secDevice: {}}}
)

const (
	sessionTouchInterval = 10 * time.Minute
	apiKeyTouchInterval  = time.Minute
	apiKeyRequestsPerMin = 300
	deviceRequestsPerMin = 120
)

// Principal is the authenticated caller: a dashboard session, an API key, or a paired device.
type Principal struct {
	User    *dbq.User
	Session *dbq.Session
	APIKey  *dbq.GetAPIKeyForAuthRow
	Device  *dbq.GetDeviceForAuthRow
}

func principalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey).(*Principal)
	return p
}

// authenticate enforces the security requirement declared on each operation.
// Session routes and API-key routes are disjoint: a route accepts one or the other.
func (s *Server) authenticate(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op == nil || len(op.Security) == 0 {
			next(ctx)
			return
		}
		var (
			p   *Principal
			err error
		)
		switch req := op.Security[0]; {
		case has(req, secSession):
			p, err = s.authSession(ctx)
		case has(req, secDevice):
			var d *dbq.GetDeviceForAuthRow
			if d, err = s.authDevice(ctx.Context(), ctx.Header("Authorization")); err == nil {
				p = &Principal{Device: d}
			}
		default:
			p, err = s.authAPIKey(ctx)
		}
		if err == nil {
			err = s.checkRole(ctx, p)
		}
		if err != nil {
			writeHumaErr(ctx, err)
			return
		}
		next(huma.WithValue(ctx, principalKey, p))
	}
}

func has(req map[string][]string, scheme string) bool {
	_, ok := req[scheme]
	return ok
}

// authDevice resolves a device credential (Authorization: Bearer bd_…). It is
// shared by the REST endpoints and the WebSocket upgrade.
func (s *Server) authDevice(ctx context.Context, header string) (*dbq.GetDeviceForAuthRow, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	token = strings.TrimSpace(token)
	if header == "" || !ok || !strings.HasPrefix(token, auth.DevicePrefix) || len(token) > 128 {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidDeviceCredential,
			"Missing or malformed device credential. Pair the device again from the dashboard.")
	}
	row, err := s.q.GetDeviceForAuth(ctx, auth.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidDeviceCredential,
			"This device credential is not recognised. Pair the device again from the dashboard.")
	}
	if err != nil {
		return nil, err
	}
	if row.Device.RevokedAt != nil {
		return nil, Errorf(http.StatusUnauthorized, CodeDeviceRevoked,
			"This device was removed from the project on "+row.Device.RevokedAt.UTC().Format(time.RFC3339)+". Pair it again to reconnect.")
	}
	if err := s.limit(ctx, "device:"+row.Device.ID, deviceRequestsPerMin, time.Minute); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Server) authSession(ctx huma.Context) (*Principal, error) {
	c, err := huma.ReadCookie(ctx, SessionCookie)
	if err != nil || c.Value == "" {
		return nil, Errorf(http.StatusUnauthorized, CodeUnauthenticated, "Sign in to continue.")
	}
	row, err := s.q.GetSessionByTokenHash(ctx.Context(), auth.HashToken(c.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		cleared := s.clearSessionCookie()
		return nil, huma.ErrorWithHeaders(
			Errorf(http.StatusUnauthorized, CodeUnauthenticated, "Your session has expired. Sign in again."),
			http.Header{"Set-Cookie": {cleared.String()}})
	}
	if err != nil {
		return nil, err
	}
	if time.Since(row.Session.LastSeenAt) > sessionTouchInterval {
		expires := time.Now().Add(s.cfg.SessionTTL)
		if err := s.q.TouchSession(ctx.Context(), dbq.TouchSessionParams{ID: row.Session.ID, ExpiresAt: expires}); err != nil {
			s.log.Warn("could not extend session", "request_id", RequestIDFrom(ctx.Context()), "error", err)
		} else {
			cookie := s.sessionCookie(c.Value, expires)
			ctx.AppendHeader("Set-Cookie", cookie.String())
		}
	}
	return &Principal{User: &row.User, Session: &row.Session}, nil
}

func (s *Server) authAPIKey(ctx huma.Context) (*Principal, error) {
	key, err := s.apiKeyFromHeader(ctx.Context(), ctx.Header("Authorization"))
	if err != nil {
		return nil, err
	}
	return &Principal{APIKey: key}, nil
}

// apiKeyFromHeader resolves `Authorization: Bearer bk_…`, enforces the key's
// rate limit and records its use. Shared by Huma routes and the event stream.
func (s *Server) apiKeyFromHeader(ctx context.Context, header string) (*dbq.GetAPIKeyForAuthRow, error) {
	if header == "" {
		return nil, Errorf(http.StatusUnauthorized, CodeUnauthenticated,
			"Missing API key. Send it in the Authorization header: `Authorization: Bearer bk_live_…`.")
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidAPIKey,
			"The Authorization header must use the Bearer scheme: `Authorization: Bearer bk_live_…`.")
	}
	if _, ok := auth.ParseAPIKey(strings.TrimSpace(token)); !ok {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidAPIKey,
			"The API key is malformed. Copy it again from the dashboard; keys start with bk_live_ or bk_test_.")
	}
	key, err := s.q.GetAPIKeyForAuth(ctx, auth.HashToken(strings.TrimSpace(token)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidAPIKey, "This API key does not exist. It may belong to another Bridge instance.")
	}
	if err != nil {
		return nil, err
	}
	// From here the request belongs to a project, so it appears in its request
	// log, including attempts with a revoked or expired key.
	if m := metaFrom(ctx); m != nil {
		m.apiKey = &key
	}
	now := time.Now()
	if key.RevokedAt != nil {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidAPIKey,
			"This API key was revoked on "+key.RevokedAt.UTC().Format(time.RFC3339)+". Create a new key in the dashboard.")
	}
	if key.ExpiresAt != nil && now.After(*key.ExpiresAt) {
		return nil, Errorf(http.StatusUnauthorized, CodeInvalidAPIKey,
			"This API key expired on "+key.ExpiresAt.UTC().Format(time.RFC3339)+". Create a new key in the dashboard.")
	}
	if err := s.limit(ctx, "api_key:"+key.ID, apiKeyRequestsPerMin, time.Minute); err != nil {
		return nil, err
	}
	if key.LastUsedAt == nil || now.Sub(*key.LastUsedAt) > apiKeyTouchInterval {
		if err := s.q.TouchAPIKey(ctx, key.ID); err != nil {
			s.log.Warn("could not record API key use", "request_id", RequestIDFrom(ctx), "api_key_id", key.ID, "error", err)
		}
	}
	return &key, nil
}

func (s *Server) sessionCookie(token string, expires time.Time) http.Cookie {
	return http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) clearSessionCookie() http.Cookie {
	return http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// limit applies a rate limit and fails open if the limiter is unavailable.
func (s *Server) limit(ctx context.Context, key string, n int, window time.Duration) error {
	res, err := s.limiter.Hit(ctx, key, n, window)
	if err != nil {
		s.log.Warn("rate limiter unavailable; allowing request", "request_id", RequestIDFrom(ctx), "error", err)
	}
	if !res.Allowed {
		return rateLimited(res.RetryAfter)
	}
	return nil
}

// writeHumaErr renders an error from inside a Huma middleware.
func writeHumaErr(ctx huma.Context, err error) {
	var he huma.HeadersError
	if errors.As(err, &he) {
		for k, values := range he.GetHeaders() {
			for _, v := range values {
				ctx.AppendHeader(k, v)
			}
		}
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		se := huma.NewErrorWithContext(ctx, http.StatusInternalServerError, "unexpected error", err)
		ae, _ = se.(*APIError)
	}
	if ae.Body.RequestID == "" {
		ae.Body.RequestID = RequestIDFrom(ctx.Context())
	}
	ctx.SetHeader("Content-Type", "application/json")
	ctx.SetStatus(ae.GetStatus())
	_ = json.NewEncoder(ctx.BodyWriter()).Encode(ae)
}
