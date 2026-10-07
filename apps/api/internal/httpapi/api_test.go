package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bridge/internal/auth"
	"bridge/internal/config"
	"bridge/internal/db/dbq"
	"bridge/internal/events"
	"bridge/internal/gateway"
	"bridge/internal/httpapi"
	"bridge/internal/messaging"
	"bridge/internal/push"
	"bridge/internal/reqlog"
	"bridge/internal/status"
	"bridge/internal/testutil"
	"bridge/internal/webhook"
	"bridge/internal/worker"
)

// testAssignTimeout is short so reassignment tests run quickly.
const testAssignTimeout = 2 * time.Second

var testDB *testutil.Database

func TestMain(m *testing.M) {
	ctx := context.Background()
	d, err := testutil.NewDatabase(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration database:", err)
		os.Exit(1)
	}
	testDB = d
	code := m.Run()
	if d != nil {
		d.Close(ctx)
	}
	os.Exit(code)
}

const dashboardOrigin = "http://localhost:3000"

func testConfig() *config.Config {
	public, _ := url.Parse("http://localhost:8080")
	dash, _ := url.Parse(dashboardOrigin)
	return &config.Config{
		Env: "development", PublicURL: public, DashboardURL: dash,
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		SessionTTL:     24 * time.Hour, AllowSignup: true, LogFormat: "text",
		VAPIDSubject: "mailto:test@example.com", PushAllowPrivate: true,
	}
}

func newServer(t *testing.T, mutate ...func(*config.Config)) *httptest.Server {
	t.Helper()
	if testDB == nil {
		t.Skipf("set %s to run integration tests", testutil.EnvVar)
	}
	cfg := testConfig()
	for _, f := range mutate {
		f(cfg)
	}
	var logOut io.Writer = io.Discard
	if os.Getenv("BRIDGE_TEST_LOG") != "" {
		logOut = os.Stderr // set BRIDGE_TEST_LOG=1 to see server logs while debugging a test
	}
	logger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx, cancel := context.WithCancel(context.Background())
	pushService, err := push.New(ctx, dbq.New(testDB.Pool), cfg)
	if err != nil {
		t.Fatal(err)
	}
	hub := gateway.NewHub(ctx, testDB.Pool, logger)
	go func() { _ = hub.Run(ctx) }()
	hooks := webhook.New(webhook.Options{Pool: testDB.Pool, Logger: logger, AllowPrivate: true})
	msgs := messaging.New(messaging.Options{
		Pool: testDB.Pool, Logger: logger, Publisher: hub.Send, Waker: pushService, Emitter: hooks,
		Config: messaging.Config{AssignTimeout: testAssignTimeout},
	})
	hub.SetHandler(msgs)
	// A real River worker processes dispatch and simulation jobs.
	health := status.New(testDB.Pool, logger, "test")
	go health.Heartbeat(ctx, "api")
	go health.Heartbeat(ctx, "worker")
	jobs, err := worker.NewClient(testDB.Pool, logger, msgs, hooks, health, worker.Retention{})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Start(ctx); err != nil {
		t.Fatal(err)
	}
	requests := reqlog.New(testDB.Pool, logger, 50*time.Millisecond)
	go func() { _ = requests.Run(ctx) }()
	broker := events.NewBroker(testDB.Pool, logger)
	go func() { _ = broker.Run(ctx) }()
	srv := httptest.NewServer(httpapi.New(httpapi.Options{
		Config: cfg, Pool: testDB.Pool, Logger: logger, Version: "test", Hub: hub, Push: pushService, Messaging: msgs, Webhooks: hooks,
		RequestLog: requests, Events: broker, Status: health,
	}).Handler())
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = jobs.Stop(stopCtx)
		cancel()
		srv.Close()
	})
	return srv
}

var ipCounter atomic.Uint32

// client is a browser-like test client with its own cookie jar and client IP.
type client struct {
	t      *testing.T
	base   string
	http   *http.Client
	ip     string
	apiKey string
	origin string
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	n := ipCounter.Add(1)
	return &client{
		t: t, base: srv.URL, http: &http.Client{Jar: jar},
		ip:     fmt.Sprintf("203.0.%d.%d", n/250, n%250+1),
		origin: dashboardOrigin,
	}
}

type response struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

func (r response) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (r response) errMessage() string {
	e, _ := r.Body["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

func (c *client) do(method, path string, body any) response {
	c.t.Helper()
	return c.doWithHeaders(method, path, body, nil)
}

func (c *client) doWithHeaders(method, path string, body any, headers map[string]string) response {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Forwarded-For", c.ip)
	if c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: raw}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (c *client) mustStatus(r response, want int) response {
	c.t.Helper()
	if r.Status != want {
		c.t.Fatalf("status %d, want %d: %s", r.Status, want, r.Raw)
	}
	return r
}

var emailCounter atomic.Uint32

func uniqueEmail() string {
	return fmt.Sprintf("user%d-%s@example.com", emailCounter.Add(1), strings.ToLower(auth.RandomString(6)))
}

// signup creates a user and returns the client, their organization ID and default project ID.
func signup(t *testing.T, srv *httptest.Server) (*client, string, string) {
	t.Helper()
	c := newClient(t, srv)
	r := c.mustStatus(c.do("POST", "/v1/auth/signup", map[string]any{
		"email": uniqueEmail(), "password": "correct horse battery", "name": "Ada Lovelace",
	}), http.StatusCreated)
	orgs := r.Body["organizations"].([]any)
	orgID := orgs[0].(map[string]any)["id"].(string)
	pr := c.mustStatus(c.do("GET", "/v1/organizations/"+orgID+"/projects", nil), http.StatusOK)
	projects := pr.Body["data"].([]any)
	return c, orgID, projects[0].(map[string]any)["id"].(string)
}

func TestSignupLoginLogout(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	email := uniqueEmail()

	r := c.mustStatus(c.do("POST", "/v1/auth/signup", map[string]any{"email": email, "password": "correct horse battery", "name": "Ada Lovelace"}), 201)
	if got := r.Body["user"].(map[string]any)["email"]; got != email {
		t.Fatalf("user email = %v", got)
	}
	if ts := r.Body["user"].(map[string]any)["created_at"].(string); !strings.HasSuffix(ts, "Z") {
		t.Fatalf("timestamps must be UTC, got %q", ts)
	}
	orgs := r.Body["organizations"].([]any)
	if len(orgs) != 1 || orgs[0].(map[string]any)["name"] != "Ada's workspace" || orgs[0].(map[string]any)["role"] != "owner" {
		t.Fatalf("unexpected organizations %v", orgs)
	}
	setCookie := r.Header.Get("Set-Cookie")
	for _, attr := range []string{"bridge_session=bs_", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(setCookie, attr) {
			t.Errorf("Set-Cookie %q missing %q", setCookie, attr)
		}
	}

	c.mustStatus(c.do("GET", "/v1/me", nil), 200)
	c.mustStatus(c.do("POST", "/v1/auth/logout", nil), 204)
	if r := c.do("GET", "/v1/me", nil); r.Status != 401 || r.errCode() != "unauthenticated" {
		t.Fatalf("after logout: %d %s", r.Status, r.Raw)
	}

	if r := c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "wrong password!"}); r.Status != 401 || r.errMessage() != "Email or password is incorrect." {
		t.Fatalf("wrong password: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/auth/login", map[string]any{"email": "nobody-" + email, "password": "whatever123"}); r.Status != 401 || r.errMessage() != "Email or password is incorrect." {
		t.Fatalf("unknown email must look identical to a wrong password: %d %s", r.Status, r.Raw)
	}
	// Email matching is case-insensitive.
	c.mustStatus(c.do("POST", "/v1/auth/login", map[string]any{"email": strings.ToUpper(email), "password": "correct horse battery"}), 200)
	c.mustStatus(c.do("GET", "/v1/me", nil), 200)

	if r := newClient(t, srv).do("POST", "/v1/auth/signup", map[string]any{"email": strings.ToUpper(email), "password": "another password"}); r.Status != 409 || r.errCode() != "conflict" {
		t.Fatalf("duplicate signup: %d %s", r.Status, r.Raw)
	}
}

func TestSignupValidation(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	r := c.do("POST", "/v1/auth/signup", map[string]any{"email": "not-an-email", "password": "short"})
	if r.Status != 422 || r.errCode() != "validation_failed" {
		t.Fatalf("got %d %s", r.Status, r.Raw)
	}
	details := r.Body["error"].(map[string]any)["details"].([]any)
	if len(details) < 2 {
		t.Fatalf("expected per-field details, got %s", r.Raw)
	}
}

func TestSignupDisabled(t *testing.T) {
	signup(t, newServer(t)) // ensure at least one user exists
	srv := newServer(t, func(c *config.Config) { c.AllowSignup = false })
	r := newClient(t, srv).do("POST", "/v1/auth/signup", map[string]any{"email": uniqueEmail(), "password": "correct horse battery"})
	if r.Status != 403 || r.errCode() != "forbidden" {
		t.Fatalf("got %d %s", r.Status, r.Raw)
	}
}

func TestProjectsAndOrganizations(t *testing.T) {
	srv := newServer(t)
	c, orgID, projectID := signup(t, srv)

	r := c.mustStatus(c.do("POST", "/v1/organizations/"+orgID+"/projects", map[string]any{"name": "Checkout Service"}), 201)
	if r.Body["slug"] != "checkout-service" {
		t.Fatalf("slug = %v", r.Body["slug"])
	}
	// Duplicate names get a unique slug.
	r2 := c.mustStatus(c.do("POST", "/v1/organizations/"+orgID+"/projects", map[string]any{"name": "Checkout Service"}), 201)
	if r2.Body["slug"] == "checkout-service" || !strings.HasPrefix(r2.Body["slug"].(string), "checkout-service-") {
		t.Fatalf("duplicate slug = %v", r2.Body["slug"])
	}
	list := c.mustStatus(c.do("GET", "/v1/organizations/"+orgID+"/projects", nil), 200)
	if n := len(list.Body["data"].([]any)); n != 3 {
		t.Fatalf("expected 3 projects, got %d", n)
	}
	r = c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID, map[string]any{"name": "Renamed"}), 200)
	if r.Body["name"] != "Renamed" {
		t.Fatalf("rename failed: %s", r.Raw)
	}
	if r := c.do("PATCH", "/v1/projects/"+projectID, map[string]any{"name": "   "}); r.Status != 422 {
		t.Fatalf("blank name accepted: %d", r.Status)
	}
	c.mustStatus(c.do("POST", "/v1/organizations", map[string]any{"name": "Second Org"}), 201)
	orgs := c.mustStatus(c.do("GET", "/v1/organizations", nil), 200)
	if n := len(orgs.Body["data"].([]any)); n != 2 {
		t.Fatalf("expected 2 organizations, got %d", n)
	}
}

func TestTenantIsolation(t *testing.T) {
	srv := newServer(t)
	alice, aliceOrg, aliceProject := signup(t, srv)
	bob, _, _ := signup(t, srv)

	key := alice.mustStatus(alice.do("POST", "/v1/projects/"+aliceProject+"/api-keys", map[string]any{"name": "a", "environment": "live"}), 201)
	keyID := key.Body["id"].(string)

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/v1/organizations/" + aliceOrg, nil},
		{"GET", "/v1/organizations/" + aliceOrg + "/projects", nil},
		{"POST", "/v1/organizations/" + aliceOrg + "/projects", map[string]any{"name": "x"}},
		{"GET", "/v1/projects/" + aliceProject, nil},
		{"PATCH", "/v1/projects/" + aliceProject, map[string]any{"name": "pwned"}},
		{"GET", "/v1/projects/" + aliceProject + "/api-keys", nil},
		{"POST", "/v1/projects/" + aliceProject + "/api-keys", map[string]any{"name": "x", "environment": "live"}},
		{"DELETE", "/v1/projects/" + aliceProject + "/api-keys/" + keyID, nil},
	} {
		if r := bob.do(tc.method, tc.path, tc.body); r.Status != 404 || r.errCode() != "not_found" {
			t.Errorf("bob %s %s: got %d %s, want 404", tc.method, tc.path, r.Status, r.Raw)
		}
	}
	// Alice's key still works: Bob's revoke attempt changed nothing.
	k := newClient(t, srv)
	k.apiKey = key.Body["secret"].(string)
	k.mustStatus(k.do("GET", "/v1/whoami", nil), 200)
}

func TestAPIKeyLifecycle(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)

	created := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/api-keys", map[string]any{"name": "Prod", "environment": "live"}), 201)
	secret := created.Body["secret"].(string)
	if !strings.HasPrefix(secret, "bk_live_") || !strings.HasPrefix(secret, created.Body["prefix"].(string)) {
		t.Fatalf("unexpected key %q / prefix %v", secret, created.Body["prefix"])
	}
	keyID := created.Body["id"].(string)

	list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/api-keys", nil), 200)
	if strings.Contains(string(list.Raw), secret) || strings.Contains(string(list.Raw), `"secret"`) {
		t.Fatal("list endpoint leaked a secret")
	}

	dev := newClient(t, srv)
	dev.origin = ""
	dev.apiKey = secret
	who := dev.mustStatus(dev.do("GET", "/v1/whoami", nil), 200)
	if who.Body["project_id"] != projectID || who.Body["environment"] != "live" {
		t.Fatalf("whoami = %s", who.Raw)
	}

	testKey := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/api-keys", map[string]any{"name": "CI", "environment": "test"}), 201)
	dev.apiKey = testKey.Body["secret"].(string)
	if r := dev.mustStatus(dev.do("GET", "/v1/whoami", nil), 200); r.Body["environment"] != "test" {
		t.Fatalf("test key environment = %v", r.Body["environment"])
	}

	c.mustStatus(c.do("DELETE", "/v1/projects/"+projectID+"/api-keys/"+keyID, nil), 204)
	c.mustStatus(c.do("DELETE", "/v1/projects/"+projectID+"/api-keys/"+keyID, nil), 204) // idempotent
	dev.apiKey = secret
	if r := dev.do("GET", "/v1/whoami", nil); r.Status != 401 || r.errCode() != "invalid_api_key" || !strings.Contains(r.errMessage(), "revoked") {
		t.Fatalf("revoked key: %d %s", r.Status, r.Raw)
	}

	// Session cookies never authenticate developer endpoints.
	if r := c.do("GET", "/v1/whoami", nil); r.Status != 401 {
		t.Fatalf("session reached an API-key route: %d", r.Status)
	}

	for name, tc := range map[string]struct{ key, code string }{
		"missing":                 {"", "unauthenticated"},
		"malformed":               {"bk_live_nope", "invalid_api_key"},
		"well-formed but unknown": {func() string { s, _ := auth.NewAPIKey(auth.EnvLive); return s }(), "invalid_api_key"},
	} {
		dev.apiKey = tc.key
		if r := dev.do("GET", "/v1/whoami", nil); r.Status != 401 || r.errCode() != tc.code {
			t.Errorf("%s key: %d %s", name, r.Status, r.Raw)
		}
	}
}

func TestExpiredAPIKey(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	created := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/api-keys", map[string]any{"name": "Short", "environment": "live", "expires_in_days": 1}), 201)
	if _, err := testDB.Pool.Exec(context.Background(), "UPDATE api_keys SET expires_at = now() - interval '1 minute' WHERE id = $1", created.Body["id"]); err != nil {
		t.Fatal(err)
	}
	dev := newClient(t, srv)
	dev.apiKey = created.Body["secret"].(string)
	if r := dev.do("GET", "/v1/whoami", nil); r.Status != 401 || !strings.Contains(r.errMessage(), "expired") {
		t.Fatalf("expired key: %d %s", r.Status, r.Raw)
	}
}

// awayFromWindowEdge waits if a fixed rate-limit window of this size ends
// within 5 seconds, so a burst of test requests all counts against one window.
func awayFromWindowEdge(window time.Duration) {
	now := time.Now().UTC()
	if left := now.Truncate(window).Add(window).Sub(now); left < 5*time.Second {
		time.Sleep(left + 100*time.Millisecond)
	}
}

func TestLoginRateLimits(t *testing.T) {
	srv := newServer(t)

	t.Run("per email", func(t *testing.T) {
		awayFromWindowEdge(15 * time.Minute)
		email := uniqueEmail()
		for i := range 10 {
			c := newClient(t, srv) // fresh IP each time
			if r := c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "wrong-password"}); r.Status != 401 {
				t.Fatalf("attempt %d: %d", i+1, r.Status)
			}
		}
		r := newClient(t, srv).do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "wrong-password"})
		if r.Status != 429 || r.errCode() != "rate_limited" || r.Header.Get("Retry-After") == "" {
			t.Fatalf("11th attempt: %d %s (Retry-After %q)", r.Status, r.Raw, r.Header.Get("Retry-After"))
		}
	})

	t.Run("per IP", func(t *testing.T) {
		awayFromWindowEdge(time.Minute)
		c := newClient(t, srv)
		for i := range 20 {
			if r := c.do("POST", "/v1/auth/login", map[string]any{"email": uniqueEmail(), "password": "wrong-password"}); r.Status != 401 {
				t.Fatalf("attempt %d: %d", i+1, r.Status)
			}
		}
		if r := c.do("POST", "/v1/auth/login", map[string]any{"email": uniqueEmail(), "password": "wrong-password"}); r.Status != 429 {
			t.Fatalf("21st attempt from one IP: %d", r.Status)
		}
	})
}

func TestCrossSiteRequestsBlocked(t *testing.T) {
	srv := newServer(t)
	c, _, _ := signup(t, srv)

	c.origin = "https://evil.example"
	if r := c.do("POST", "/v1/organizations", map[string]any{"name": "x"}); r.Status != 403 || r.errCode() != "forbidden" {
		t.Fatalf("cross-origin POST: %d %s", r.Status, r.Raw)
	}
	// Reads stay allowed; SameSite=Lax already withholds the cookie on cross-site subresource requests.
	c.mustStatus(c.do("GET", "/v1/me", nil), 200)

	c.origin = ""
	req, _ := http.NewRequest("POST", c.base+"/v1/organizations", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross-site POST without Origin: %d", resp.StatusCode)
	}

	c.origin = dashboardOrigin
	c.mustStatus(c.do("POST", "/v1/organizations", map[string]any{"name": "Legit"}), 201)
}

func TestErrorEnvelopeAndRequestID(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)

	r := c.do("GET", "/v1/does-not-exist", nil)
	rid := r.Header.Get("X-Request-Id")
	if r.Status != 404 || r.errCode() != "not_found" || !strings.HasPrefix(rid, "req_") {
		t.Fatalf("got %d %s (request id %q)", r.Status, r.Raw, rid)
	}
	if body := r.Body["error"].(map[string]any)["request_id"]; body != rid {
		t.Fatalf("body request_id %v != header %q", body, rid)
	}

	r = c.do("GET", "/v1/me", nil)
	if r.Body["error"].(map[string]any)["request_id"] != r.Header.Get("X-Request-Id") {
		t.Fatalf("handler error missing request id: %s", r.Raw)
	}

	req, _ := http.NewRequest("POST", c.base+"/v1/auth/login", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode/100 != 4 || body["error"]["code"] == "" {
		t.Fatalf("malformed JSON: %d %v", resp.StatusCode, body)
	}

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if r.Header.Get(h) == "" {
			t.Errorf("missing security header %s", h)
		}
	}
}

func TestHealth(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	c.mustStatus(c.do("GET", "/healthz", nil), 200)
	c.mustStatus(c.do("GET", "/readyz", nil), 200)
	if r := c.do("GET", "/openapi.json", nil); r.Status != 200 || r.Body["openapi"] != "3.1.0" {
		t.Fatalf("openapi: %d", r.Status)
	}
}
