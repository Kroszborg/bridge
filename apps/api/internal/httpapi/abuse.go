package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"bridge/internal/disposable"
)

// Abuse controls for the public account forms: Cloudflare Turnstile on
// sign-up, password-reset requests and repeated failed sign-ins; a honeypot
// field; and refusing disposable email addresses. All of it is optional:
// without BRIDGE_TURNSTILE_* there is no CAPTCHA, and disposable addresses are
// only refused on hosted Bridge or with BRIDGE_BLOCK_DISPOSABLE_EMAIL.

// captchaTimeout bounds a siteverify call, so an outage cannot hang the forms.
const captchaTimeout = 5 * time.Second

// Failed sign-ins in loginFailWindow after which the next attempt needs a
// Turnstile token, per email address and per client IP. The IP threshold is
// higher because offices and mobile carriers share addresses.
const (
	loginFailWindow      = 15 * time.Minute
	loginFailsPerEmail   = 3
	loginFailsPerAddress = 5
)

// Turnstile actions, set by the dashboard when it renders the widget and
// checked against siteverify's answer.
const (
	captchaSignup = "signup"
	captchaLogin  = "login"
	captchaReset  = "reset"
)

// errCaptchaUnavailable means siteverify could not give an answer. Callers
// decide whether to fail closed (sign-up, reset) or fall back to rate limits
// (sign-in).
var errCaptchaUnavailable = errors.New("captcha unavailable")

// Cloudflare's dummy secret keys (always pass, always fail, token spent)
// answer with a fixed hostname and no action, so those are not compared.
var turnstileTestSecret = regexp.MustCompile(`^[123]x0+AA$`)

func captchaRequired() error {
	return Errorf(http.StatusBadRequest, CodeCaptchaRequired, "Complete the security check, then try again.")
}

// verifyCaptcha checks a Turnstile token for action with siteverify. It
// returns nil when the token passes, an *APIError when it is missing or
// rejected, and errCaptchaUnavailable when siteverify cannot answer.
func (s *Server) verifyCaptcha(ctx context.Context, token, action string) error {
	if strings.TrimSpace(token) == "" {
		return captchaRequired()
	}
	ip := ""
	if a := ClientIPFrom(ctx); a.IsValid() {
		ip = a.String()
	}
	vctx, cancel := context.WithTimeout(ctx, captchaTimeout)
	defer cancel()
	res, err := s.captcha.Verify(vctx, s.cfg.TurnstileSecretKey, token, ip)
	if err != nil {
		s.log.Warn("turnstile siteverify is unreachable", "request_id", RequestIDFrom(ctx), "action", action, "error", err)
		return errCaptchaUnavailable
	}
	if slices.ContainsFunc(res.ErrorCodes, func(c string) bool { return strings.HasSuffix(c, "-secret") }) {
		s.log.Error("turnstile rejected BRIDGE_TURNSTILE_SECRET_KEY; check it matches the site key", "request_id", RequestIDFrom(ctx), "error_codes", res.ErrorCodes)
		return errCaptchaUnavailable
	}
	if slices.Contains(res.ErrorCodes, "internal-error") {
		s.log.Warn("turnstile siteverify had an internal error", "request_id", RequestIDFrom(ctx), "action", action)
		return errCaptchaUnavailable
	}
	ok := res.Success
	if ok && !turnstileTestSecret.MatchString(s.cfg.TurnstileSecretKey) {
		// The token must come from the dashboard's own pages and this form.
		if res.Hostname != "" && !strings.EqualFold(res.Hostname, s.cfg.DashboardURL.Hostname()) {
			ok = false
		}
		if res.Action != "" && res.Action != action {
			ok = false
		}
	}
	if !ok {
		s.log.Info("turnstile token rejected", "request_id", RequestIDFrom(ctx), "action", action,
			"error_codes", res.ErrorCodes, "hostname", res.Hostname, "token_action", res.Action)
		return Errorf(http.StatusBadRequest, CodeCaptchaFailed, "The security check failed or expired. Complete it again, then retry.")
	}
	return nil
}

// requireCaptcha is verifyCaptcha for forms that fail closed: while siteverify
// is down, they answer 503 instead of letting unchecked requests through.
func (s *Server) requireCaptcha(ctx context.Context, token, action string) error {
	if s.captcha == nil {
		return nil
	}
	err := s.verifyCaptcha(ctx, token, action)
	if errors.Is(err, errCaptchaUnavailable) {
		return Errorf(http.StatusServiceUnavailable, CodeCaptchaUnavailable,
			"The security check cannot be verified right now. Try again in a few minutes.")
	}
	return err
}

func loginFailKeys(ctx context.Context, email string) (byEmail, byIP string) {
	return "login-fail:email:" + email, "login-fail:ip:" + ClientIPFrom(ctx).String()
}

// loginNeedsCaptcha reports whether recent failed sign-ins for this address or
// from this client mean the next attempt must pass Turnstile.
func (s *Server) loginNeedsCaptcha(ctx context.Context, email string) bool {
	if s.captcha == nil {
		return false
	}
	byEmail, byIP := loginFailKeys(ctx, email)
	for _, c := range []struct {
		key string
		max int
	}{{byEmail, loginFailsPerEmail}, {byIP, loginFailsPerAddress}} {
		n, err := s.limiter.Count(ctx, c.key, loginFailWindow)
		if err != nil {
			s.log.Warn("failed sign-in counter unavailable", "request_id", RequestIDFrom(ctx), "error", err)
			return false
		}
		if n >= c.max {
			return true
		}
	}
	return false
}

// loginFailed counts a failed sign-in towards loginNeedsCaptcha.
func (s *Server) loginFailed(ctx context.Context, email string) {
	if s.captcha == nil {
		return
	}
	byEmail, byIP := loginFailKeys(ctx, email)
	for _, key := range []string{byEmail, byIP} {
		if _, err := s.limiter.Hit(ctx, key, 1<<20, loginFailWindow); err != nil {
			s.log.Warn("failed sign-in counter unavailable", "request_id", RequestIDFrom(ctx), "error", err)
		}
	}
}

// honeypotFilled reports whether a bot filled the hidden field that people
// never see. It is logged, and the caller answers without acting.
func (s *Server) honeypotFilled(ctx context.Context, form, value string) bool {
	if value == "" {
		return false
	}
	s.log.Info("honeypot field filled; request ignored", "request_id", RequestIDFrom(ctx), "form", form,
		"ip", ClientIPFrom(ctx).String())
	return true
}

// checkEmailAllowed refuses disposable email addresses when this server blocks them.
func (s *Server) checkEmailAllowed(email string) error {
	if s.cfg.BlockDisposableEmail && disposable.Email(email) {
		return Errorf(http.StatusUnprocessableEntity, CodeEmailNotAllowed,
			"Temporary or disposable email addresses cannot be used. Use an address you will keep, such as your work email.")
	}
	return nil
}
