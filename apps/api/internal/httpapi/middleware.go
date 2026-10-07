package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/reqlog"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientIPKey
	principalKey
	requestMetaKey
	userAgentKey
)

func userAgentFrom(ctx context.Context) string {
	v, _ := ctx.Value(userAgentKey).(string)
	return v
}

// requestMeta is filled in while a request is handled (by authentication and
// handlers) and read by the request log once the response is written.
type requestMeta struct {
	apiKey     *dbq.GetAPIKeyForAuthRow
	resourceID string
}

func metaFrom(ctx context.Context) *requestMeta {
	m, _ := ctx.Value(requestMetaKey).(*requestMeta)
	return m
}

// setResource records the resource a request created or read, for the request log.
func setResource(ctx context.Context, id string) {
	if m := metaFrom(ctx); m != nil {
		m.resourceID = id
	}
}

// requestLog records every API-key request (metadata only) for the dashboard.
// Requests without a recognised key cannot be attributed to a project and are
// only in the access log.
func requestLog(rec *reqlog.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if rec == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			meta := &requestMeta{}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			body := &cappedBuffer{max: 2048}
			ww.Tee(body)
			next.ServeHTTP(ww, r.WithContext(context.WithValue(r.Context(), requestMetaKey, meta)))
			if meta.apiKey == nil {
				return
			}
			e := reqlog.Entry{
				ID: id.New(id.RequestLog), RequestID: RequestIDFrom(r.Context()),
				ProjectID: meta.apiKey.ProjectID, APIKeyID: meta.apiKey.ID, Environment: string(meta.apiKey.Environment),
				Method: r.Method, Path: r.URL.Path, Status: ww.Status(), Duration: time.Since(start),
				IP: ClientIPFrom(r.Context()), UserAgent: clipString(r.UserAgent(), 200), ResourceID: meta.resourceID,
				At: start.UTC(),
			}
			if e.Status >= 400 {
				e.ResourceID = "" // the resource was not created or found
				var env struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if json.Unmarshal(body.Bytes(), &env) == nil {
					e.ErrorCode = clipString(env.Error.Code, 64)
				}
			}
			rec.Record(e)
		})
	}
}

// cappedBuffer keeps the first max bytes written to it.
type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func clipString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// RequestIDFrom returns the request ID stored in ctx.
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// ClientIPFrom returns the resolved client IP stored in ctx.
func ClientIPFrom(ctx context.Context) netip.Addr {
	v, _ := ctx.Value(clientIPKey).(netip.Addr)
	return v
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_\-]{8,64}$`)

// requestID accepts a well-formed incoming X-Request-Id (so a proxy's ID
// carries through) or generates one, and echoes it on the response.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if !validRequestID.MatchString(rid) {
			rid = id.New(id.Request)
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := context.WithValue(r.Context(), requestIDKey, rid)
		ctx = context.WithValue(ctx, userAgentKey, clipString(r.UserAgent(), 300))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientIP resolves the client address. X-Forwarded-For is honoured only when
// the direct peer is a trusted proxy, and is walked right to left so a client
// cannot spoof its address by sending its own header.
func clientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip, _ := netip.ParseAddr(host)
			ip = ip.Unmap()
			if ip.IsValid() && isTrusted(ip) {
				hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break
					}
					ip = hop.Unmap()
					if !isTrusted(ip) {
						break
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}

// accessLog writes one structured line per request. It never logs headers,
// query strings or bodies, which may carry secrets or message content.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := r.URL.Path
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			if route == "/healthz" || route == "/readyz" {
				return
			}
			level := slog.LevelInfo
			if ww.Status() >= 500 {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("ip", ClientIPFrom(r.Context()).String()),
			)
		})
	}
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.Error("panic serving request", "request_id", RequestIDFrom(r.Context()), "panic", rec, "stack", string(debug.Stack()))
					writeRawError(w, r, http.StatusInternalServerError, CodeInternal, "Bridge hit an unexpected error. It has been logged.")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originCheck blocks cross-site requests that ride on the session cookie
// (CSRF). It applies only to state-changing methods that carry the cookie;
// API-key requests and non-browser clients are unaffected.
func originCheck(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if _, err := r.Cookie(SessionCookie); err != nil {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			switch {
			case origin != "" && origin == allowedOrigin:
			case origin == "" && r.Header.Get("Sec-Fetch-Site") != "cross-site":
			default:
				writeRawError(w, r, http.StatusForbidden, CodeForbidden,
					"Cross-site request blocked. Session-authenticated requests must come from the Bridge dashboard.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
