package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bridge/internal/config"
)

const (
	testSiteKey   = "0x4AAAAAAAtestsitekey"
	testSecretKey = "0x4AAAAAAAtestsecretkey"
)

// fakeTurnstile stands in for siteverify on the account forms. A token
// "pass-<action>" passes for that action from the dashboard's host;
// "pass-elsewhere" passes from another host; anything else fails. While down
// it answers like an outage.
type fakeTurnstile struct {
	*httptest.Server
	down  atomic.Bool
	mu    sync.Mutex
	forms []url.Values
}

func newFakeTurnstile(t *testing.T) *fakeTurnstile {
	f := &fakeTurnstile{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.down.Load() {
			http.Error(w, "<html>bad gateway</html>", http.StatusBadGateway)
			return
		}
		_ = r.ParseForm()
		f.mu.Lock()
		f.forms = append(f.forms, r.PostForm)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		token := r.PostForm.Get("response")
		switch {
		case r.PostForm.Get("secret") != testSecretKey:
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-secret"]}`))
		case token == "pass-elsewhere":
			_, _ = w.Write([]byte(`{"success":true,"hostname":"evil.example","action":"signup"}`))
		case strings.HasPrefix(token, "pass-"):
			_, _ = w.Write([]byte(`{"success":true,"hostname":"localhost","action":"` + strings.TrimPrefix(token, "pass-") + `"}`))
		default:
			_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeTurnstile) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.forms)
}

func (f *fakeTurnstile) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forms[len(f.forms)-1]
}

func withTurnstile(f *fakeTurnstile) func(*config.Config) {
	return func(c *config.Config) {
		c.TurnstileSiteKey, c.TurnstileSecretKey, c.TurnstileVerifyURL = testSiteKey, testSecretKey, f.URL
	}
}

func signupBody(email string, extra map[string]any) map[string]any {
	b := map[string]any{"email": email, "password": "correct horse battery"}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func TestAuthConfigTurnstileSiteKey(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	if cfg := c.mustStatus(c.do("GET", "/v1/auth/config", nil), http.StatusOK).Body; cfg["turnstile_site_key"] != nil {
		t.Fatalf("no Turnstile configured, got site key %v", cfg["turnstile_site_key"])
	}
	srv = newServer(t, withTurnstile(newFakeTurnstile(t)))
	c = newClient(t, srv)
	cfg := c.mustStatus(c.do("GET", "/v1/auth/config", nil), http.StatusOK)
	if cfg.Body["turnstile_site_key"] != testSiteKey {
		t.Fatalf("turnstile_site_key = %v", cfg.Body["turnstile_site_key"])
	}
	if strings.Contains(string(cfg.Raw), testSecretKey) {
		t.Fatal("the secret key must never be exposed")
	}
}

func TestSignupTurnstile(t *testing.T) {
	ts := newFakeTurnstile(t)
	srv := newServer(t, withTurnstile(ts))

	for _, tc := range []struct {
		token, code string
		status      int
	}{
		{"", "captcha_required", http.StatusBadRequest},
		{"nope", "captcha_failed", http.StatusBadRequest},
		{"pass-login", "captcha_failed", http.StatusBadRequest}, // a token made for another form
		{"pass-elsewhere", "captcha_failed", http.StatusBadRequest},
	} {
		c := newClient(t, srv)
		r := c.do("POST", "/v1/auth/signup", signupBody(uniqueEmail(), map[string]any{"turnstile_token": tc.token}))
		if r.Status != tc.status || r.errCode() != tc.code {
			t.Fatalf("token %q: %d %s", tc.token, r.Status, r.Raw)
		}
	}

	c := newClient(t, srv)
	c.mustStatus(c.do("POST", "/v1/auth/signup", signupBody(uniqueEmail(), map[string]any{"turnstile_token": "pass-signup"})), http.StatusCreated)
	if f := ts.last(); f.Get("remoteip") != c.ip || f.Get("secret") != testSecretKey || f.Get("response") != "pass-signup" {
		t.Fatalf("siteverify form = %v", f)
	}

	// An outage fails closed: nobody signs up unchecked.
	ts.down.Store(true)
	c = newClient(t, srv)
	if r := c.do("POST", "/v1/auth/signup", signupBody(uniqueEmail(), map[string]any{"turnstile_token": "pass-signup"})); r.Status != http.StatusServiceUnavailable || r.errCode() != "captcha_unavailable" {
		t.Fatalf("Turnstile down: %d %s", r.Status, r.Raw)
	}
	ts.down.Store(false)

	// A wrong secret is the operator's problem, not the visitor's.
	srv = newServer(t, withTurnstile(ts), func(cfg *config.Config) { cfg.TurnstileSecretKey = "0x4AAAAAAAwrongsecret" })
	c = newClient(t, srv)
	if r := c.do("POST", "/v1/auth/signup", signupBody(uniqueEmail(), map[string]any{"turnstile_token": "pass-signup"})); r.Status != http.StatusServiceUnavailable || r.errCode() != "captcha_unavailable" {
		t.Fatalf("wrong secret: %d %s", r.Status, r.Raw)
	}
}

func TestLoginCaptchaAfterFailures(t *testing.T) {
	ts := newFakeTurnstile(t)
	srv := newServer(t, withTurnstile(ts))
	email := uniqueEmail()
	owner := newClient(t, srv)
	owner.mustStatus(owner.do("POST", "/v1/auth/signup", signupBody(email, map[string]any{"turnstile_token": "pass-signup"})), http.StatusCreated)

	c := newClient(t, srv)
	good := map[string]any{"email": email, "password": "correct horse battery"}
	calls := ts.calls()
	c.mustStatus(c.do("POST", "/v1/auth/login", good), http.StatusOK)
	if ts.calls() != calls {
		t.Fatal("a sign-in without failures must not call Turnstile")
	}
	for range 3 {
		c.mustStatus(c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "wrong password!"}), http.StatusUnauthorized)
	}
	// Failures are counted per address, so another client is asked too.
	other := newClient(t, srv)
	if r := other.do("POST", "/v1/auth/login", good); r.Status != http.StatusBadRequest || r.errCode() != "captcha_required" {
		t.Fatalf("after 3 failures: %d %s", r.Status, r.Raw)
	}
	withToken := func(token string) map[string]any {
		return map[string]any{"email": email, "password": "correct horse battery", "turnstile_token": token}
	}
	if r := other.do("POST", "/v1/auth/login", withToken("nope")); r.Status != http.StatusBadRequest || r.errCode() != "captcha_failed" {
		t.Fatalf("rejected token: %d %s", r.Status, r.Raw)
	}
	if r := other.do("POST", "/v1/auth/login", withToken("pass-signup")); r.errCode() != "captcha_failed" {
		t.Fatalf("a sign-up token must not sign in: %d %s", r.Status, r.Raw)
	}
	other.mustStatus(other.do("POST", "/v1/auth/login", withToken("pass-login")), http.StatusOK)
	if f := ts.last(); f.Get("remoteip") != other.ip {
		t.Fatalf("remoteip = %q, want %q", f.Get("remoteip"), other.ip)
	}
	// A wrong password with a valid token is still just a wrong password.
	if r := other.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "wrong password!", "turnstile_token": "pass-login"}); r.Status != http.StatusUnauthorized {
		t.Fatalf("wrong password with a token: %d %s", r.Status, r.Raw)
	}

	// While Turnstile is down, sign-in falls back to the rate limits.
	ts.down.Store(true)
	other.mustStatus(other.do("POST", "/v1/auth/login", withToken("pass-login")), http.StatusOK)
	ts.down.Store(false)

	// Failures from one client across addresses count too (5 per IP).
	bot := newClient(t, srv)
	for range 5 {
		bot.mustStatus(bot.do("POST", "/v1/auth/login", map[string]any{"email": uniqueEmail(), "password": "guess guess"}), http.StatusUnauthorized)
	}
	if r := bot.do("POST", "/v1/auth/login", map[string]any{"email": uniqueEmail(), "password": "guess guess"}); r.errCode() != "captcha_required" {
		t.Fatalf("after 5 failures from one IP: %d %s", r.Status, r.Raw)
	}
}

func TestLoginWithoutTurnstileNeverAsks(t *testing.T) {
	srv := newServer(t)
	c, _, _ := signup(t, srv)
	me := c.mustStatus(c.do("GET", "/v1/me", nil), http.StatusOK).Body["user"].(map[string]any)
	email := me["email"].(string)
	for range 5 {
		c.mustStatus(c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "wrong password!"}), http.StatusUnauthorized)
	}
	c.mustStatus(c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "correct horse battery"}), http.StatusOK)
}

func TestPasswordResetTurnstile(t *testing.T) {
	ts := newFakeTurnstile(t)
	srv := newServer(t, withTurnstile(ts))
	email := uniqueEmail()
	owner := newClient(t, srv)
	owner.mustStatus(owner.do("POST", "/v1/auth/signup", signupBody(email, map[string]any{"turnstile_token": "pass-signup"})), http.StatusCreated)

	c := newClient(t, srv)
	if r := c.do("POST", "/v1/auth/password-reset", map[string]any{"email": email}); r.Status != http.StatusBadRequest || r.errCode() != "captcha_required" {
		t.Fatalf("no token: %d %s", r.Status, r.Raw)
	}
	ts.down.Store(true)
	if r := c.do("POST", "/v1/auth/password-reset", map[string]any{"email": email, "turnstile_token": "pass-reset"}); r.Status != http.StatusServiceUnavailable || r.errCode() != "captcha_unavailable" {
		t.Fatalf("Turnstile down: %d %s", r.Status, r.Raw)
	}
	ts.down.Store(false)
	c.mustStatus(c.do("POST", "/v1/auth/password-reset", map[string]any{"email": email, "turnstile_token": "pass-reset"}), http.StatusAccepted)
	if m := sentMail.waitFor(t, email); !strings.Contains(m.Text, "/reset-password?token=") {
		t.Fatalf("reset email: %+v", m)
	}
}

func TestHoneypot(t *testing.T) {
	srv := newServer(t)
	email := uniqueEmail()
	c := newClient(t, srv)
	r := c.do("POST", "/v1/auth/signup", signupBody(email, map[string]any{"website": "https://spam.example"}))
	if r.Status != http.StatusBadRequest || r.errCode() != "invalid_request" {
		t.Fatalf("honeypot sign-up: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "correct horse battery"}); r.Status != http.StatusUnauthorized {
		t.Fatalf("the honeypot sign-up created an account: %d %s", r.Status, r.Raw)
	}
	// An empty honeypot is what people send.
	c.mustStatus(c.do("POST", "/v1/auth/signup", signupBody(email, map[string]any{"website": ""})), http.StatusCreated)

	// A reset request looks accepted but sends nothing.
	sentMail.waitFor(t, email) // the sign-up's verification code
	sentBefore := mailCount(email)
	other := newClient(t, srv)
	other.mustStatus(other.do("POST", "/v1/auth/password-reset", map[string]any{"email": email, "website": "x"}), http.StatusAccepted)
	time.Sleep(200 * time.Millisecond)
	if mailCount(email) != sentBefore {
		t.Fatal("a honeypot reset request sent an email")
	}
}

func mailCount(to string) int {
	sentMail.mu.Lock()
	defer sentMail.mu.Unlock()
	n := 0
	for _, m := range sentMail.sent {
		if m.To == to {
			n++
		}
	}
	return n
}

func TestDisposableEmail(t *testing.T) {
	srv := newServer(t, func(c *config.Config) { c.BlockDisposableEmail = true })
	c := newClient(t, srv)
	r := c.do("POST", "/v1/auth/signup", signupBody("bot"+strings.ToLower(uniqueEmail()[:8])+"@Mailinator.com", nil))
	if r.Status != http.StatusUnprocessableEntity || r.errCode() != "email_not_allowed" || !strings.Contains(r.errMessage(), "disposable") {
		t.Fatalf("disposable sign-up: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/auth/signup", signupBody("x@sub.yopmail.com", nil)); r.errCode() != "email_not_allowed" {
		t.Fatalf("disposable subdomain: %d %s", r.Status, r.Raw)
	}
	user := newClient(t, srv)
	user.mustStatus(user.do("POST", "/v1/auth/signup", signupBody(uniqueEmail(), nil)), http.StatusCreated)
	// Nor can an account move to one later.
	if r := user.do("POST", "/v1/me/email/change", map[string]any{"email": "later@10minutemail.com", "password": "correct horse battery"}); r.Status != http.StatusUnprocessableEntity || r.errCode() != "email_not_allowed" {
		t.Fatalf("email change to a disposable address: %d %s", r.Status, r.Raw)
	}

	// Self-hosted servers allow them unless they opt in.
	srv = newServer(t)
	c = newClient(t, srv)
	c.mustStatus(c.do("POST", "/v1/auth/signup", signupBody("self"+strings.ToLower(uniqueEmail()[:8])+"@mailinator.com", nil)), http.StatusCreated)
}

func TestAPIResponsesAreNoindex(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	for _, r := range []response{
		c.do("GET", "/healthz", nil),
		c.do("GET", "/v1/auth/config", nil),
		c.do("GET", "/v1/me", nil),
		c.do("GET", "/v1/no-such-route", nil),
	} {
		if got := r.Header.Get("X-Robots-Tag"); got != "noindex" {
			t.Fatalf("X-Robots-Tag = %q (status %d)", got, r.Status)
		}
	}
}
