package httpapi_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"bridge/internal/config"
	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
)

func verifyAppsPath(projectID string) string { return "/v1/projects/" + projectID + "/verify-apps" }

func createVerifyApp(t *testing.T, c *client, projectID string, body map[string]any) map[string]any {
	t.Helper()
	return c.mustStatus(c.do("POST", verifyAppsPath(projectID), body), 201).Body
}

func defaultApp(t *testing.T, c *client, projectID string) map[string]any {
	t.Helper()
	apps := c.mustStatus(c.do("GET", verifyAppsPath(projectID), nil), 200).Body["data"].([]any)
	app := apps[0].(map[string]any)
	if app["slug"] != "default" || app["is_default"] != true {
		t.Fatalf("first app = %v", app)
	}
	return app
}

func auditActions(t *testing.T, targetID string) []string {
	t.Helper()
	rows, err := testDB.Pool.Query(context.Background(), `SELECT action FROM audit_logs WHERE target_id = $1 ORDER BY created_at, id`, targetID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		_ = rows.Scan(&a)
		out = append(out, a)
	}
	return out
}

func TestVerifyAppsTemplatesAndDefaultAlias(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")

	// The default app exists from the start and backs the settings endpoints.
	def := defaultApp(t, c, projectID)
	if !strings.HasPrefix(def["publishable_key"].(string), "bpk_") || def["failover_after_seconds"] != float64(30) ||
		def["ip_hourly_limit"] != float64(10) || def["range_hourly_limit"] != float64(20) || def["country_hourly_limit"] != nil ||
		def["secret_set"] != true || def["widget_environment"] != "live" {
		t.Fatalf("default app = %v", def)
	}
	c.mustStatus(c.do("PUT", "/v1/projects/"+projectID+"/otp/settings", map[string]any{
		"app_name": "Acme", "code_length": 6, "ttl_seconds": 600, "max_attempts": 5,
	}), 200)
	def = defaultApp(t, c, projectID)
	if def["app_name"] != "Acme" || def["effective_app_name"] != "Acme" {
		t.Fatalf("alias did not update the default app: %v", def)
	}
	// Changing the default app updates what the alias reads.
	c.mustStatus(c.do("PATCH", verifyAppsPath(projectID)+"/"+def["id"].(string), map[string]any{"max_attempts": 3}), 200)
	if st := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/otp/settings", nil), 200).Body; st["max_attempts"] != float64(3) || st["app_name"] != "Acme" {
		t.Fatalf("alias settings = %v", st)
	}

	shop := createVerifyApp(t, c, projectID, map[string]any{"name": "Shop Front", "template": "Shop code: {code}", "code_length": 4})
	secret, _ := shop["secret"].(string)
	if shop["slug"] != "shop-front" || shop["is_default"] != false || !strings.HasPrefix(secret, "bvs_") ||
		shop["app_name"] != "Shop Front" || shop["preview"] != "Shop code: 4829" {
		t.Fatalf("created app = %v", shop)
	}
	got := c.mustStatus(c.do("GET", verifyAppsPath(projectID)+"/"+shop["id"].(string), nil), 200).Body
	if _, leaked := got["secret"]; leaked || got["preview"] != "Shop code: 4829" {
		t.Fatalf("get app = %v", got)
	}
	if r := c.do("POST", verifyAppsPath(projectID), map[string]any{"name": "Other", "slug": "shop-front"}); r.Status != 422 {
		t.Fatalf("duplicate slug: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", verifyAppsPath(projectID), map[string]any{"name": "Other", "slug": "default"}); r.Status != 422 {
		t.Fatalf("default slug: %d %s", r.Status, r.Raw)
	}

	// Each app sends its own message; the same number can have a code pending in both.
	to := uniqueNumber()
	bySlug := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to, "app": "shop-front"}), 201).Body
	byDefault := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to}), 201).Body
	if bySlug["app_id"] != shop["id"] || len(bySlug["code"].(string)) != 4 || byDefault["app_id"] != def["id"] || len(byDefault["code"].(string)) != 6 {
		t.Fatalf("sends = %v / %v", bySlug, byDefault)
	}
	shopMsg := dev.mustStatus(dev.do("GET", "/v1/messages/"+bySlug["message_id"].(string), nil), 200).Body
	defMsg := dev.mustStatus(dev.do("GET", "/v1/messages/"+byDefault["message_id"].(string), nil), 200).Body
	if shopMsg["body"] != "Shop code: ••••" || !strings.HasPrefix(defMsg["body"].(string), "•••••• is your Acme code.") {
		t.Fatalf("bodies = %q / %q", shopMsg["body"], defMsg["body"])
	}
	if st := dev.mustStatus(dev.do("GET", "/v1/otp/"+bySlug["id"].(string), nil), 200).Body; st["status"] != "pending" {
		t.Fatalf("a code of another app canceled this one: %v", st["status"])
	}
	byID := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber(), "app": shop["id"]}), 201).Body
	if byID["app_id"] != shop["id"] {
		t.Fatalf("send by app ID = %v", byID)
	}
	if r := dev.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber(), "app": "nope"}); r.Status != 404 || r.errCode() != "not_found" {
		t.Fatalf("unknown app: %d %s", r.Status, r.Raw)
	}

	// Verify by number within one app.
	res := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"to": to, "app": "shop-front", "code": bySlug["code"]}), 200).Body
	if res["valid"] != true || res["verification"].(map[string]any)["id"] != bySlug["id"] {
		t.Fatalf("verify in app = %v", res)
	}

	// Lists and stats filter by app.
	list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/otp?environment=test&app=shop-front", nil), 200).Body["data"].([]any)
	if len(list) != 2 {
		t.Fatalf("shop verifications = %d", len(list))
	}
	stats := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/otp/stats?environment=test&app="+def["id"].(string), nil), 200).Body
	if stats["total"] != float64(1) || stats["pending"] != float64(1) {
		t.Fatalf("default app stats = %v", stats)
	}
	appStats := c.mustStatus(c.do("GET", verifyAppsPath(projectID)+"/"+shop["id"].(string)+"/stats?environment=test", nil), 200).Body
	if appStats["total"] != float64(2) || appStats["verified"] != float64(1) || appStats["blocked"].(map[string]any)["total"] != float64(0) {
		t.Fatalf("shop stats = %v", appStats)
	}

	// Validation.
	path := verifyAppsPath(projectID) + "/" + shop["id"].(string)
	for _, bad := range []map[string]any{
		{"allowed_countries": []string{"XX"}},
		{"allowed_origins": []string{"http://shop.example"}},
		{"allowed_origins": []string{"https://shop.example/path"}},
		{"redirect_uris": []string{"https://shop.example/done#x"}},
		{"turnstile_site_key": "0x4AAAA"},
		{"template": "no code"},
	} {
		if r := c.do("PATCH", path, bad); r.Status != 422 {
			t.Errorf("accepted %v: %d %s", bad, r.Status, r.Raw)
		}
	}
	upd := c.mustStatus(c.do("PATCH", path, map[string]any{
		"allowed_countries": []string{"in", "US", "IN"}, "allowed_origins": []string{"https://Shop.Example/", "http://localhost:5173"},
		"country_hourly_limit": 100,
	}), 200).Body
	if strings.Join(toStrings(upd["allowed_countries"]), ",") != "IN,US" || strings.Join(toStrings(upd["allowed_origins"]), ",") != "https://shop.example,http://localhost:5173" ||
		upd["country_hourly_limit"] != float64(100) || upd["template"] != "Shop code: {code}" {
		t.Fatalf("update = %v", upd)
	}
	if upd = c.mustStatus(c.do("PATCH", path, map[string]any{"country_hourly_limit": 0, "allowed_countries": []string{}}), 200).Body; upd["country_hourly_limit"] != nil || len(toStrings(upd["allowed_countries"])) != 0 {
		t.Fatalf("clear = %v", upd)
	}

	// Secrets: reveal matches creation; rotation replaces it.
	if r := c.mustStatus(c.do("GET", path+"/secret", nil), 200).Body; r["secret"] != secret {
		t.Fatalf("revealed %v, created with %s", r["secret"], secret)
	}
	rotated := c.mustStatus(c.do("POST", path+"/secret", nil), 200).Body["secret"].(string)
	if rotated == secret || !strings.HasPrefix(rotated, "bvs_") {
		t.Fatalf("rotated = %s", rotated)
	}

	// The default app stays; others can be deleted.
	if r := c.do("DELETE", verifyAppsPath(projectID)+"/"+def["id"].(string), nil); r.Status != 409 {
		t.Fatalf("delete default: %d %s", r.Status, r.Raw)
	}
	c.mustStatus(c.do("DELETE", path, nil), 204)
	if r := c.do("GET", path, nil); r.Status != 404 {
		t.Fatalf("deleted app: %d", r.Status)
	}
	kept := dev.mustStatus(dev.do("GET", "/v1/otp/"+bySlug["id"].(string), nil), 200).Body
	if kept["app_id"] != nil {
		t.Fatalf("verification of a deleted app = %v", kept)
	}
	want := "verify_app.created,verify_app.updated,verify_app.updated,verify_app.secret_revealed,verify_app.secret_rotated,verify_app.deleted"
	if got := strings.Join(auditActions(t, shop["id"].(string)), ","); got != want {
		t.Fatalf("audit = %s", got)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestVerifyFraudBlocks(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	recv := newReceiver(t)
	createWebhook(t, c, projectID, map[string]any{"url": recv.URL, "events": []string{"otp.blocked"}})
	app := createVerifyApp(t, c, projectID, map[string]any{"name": "Guarded", "allowed_countries": []string{"US"}})
	path := verifyAppsPath(projectID) + "/" + app["id"].(string)
	awayFromWindowEdge(time.Hour)

	// Country not allowed.
	r := dev.do("POST", "/v1/otp", map[string]any{"to": "+919876500001", "app": "guarded", "client_ip": "198.51.100.7"})
	if r.Status != 403 || r.errCode() != "otp_blocked" || !strings.Contains(r.errMessage(), "IN numbers") {
		t.Fatalf("country: %d %s", r.Status, r.Raw)
	}
	d := recv.wait("otp.blocked")
	if d.Data["reason"] != "country_not_allowed" || d.Data["country"] != "IN" || d.Data["client_ip"] != "198.51.100.7" ||
		d.Data["app_id"] != app["id"] || d.Data["environment"] != "test" {
		t.Fatalf("otp.blocked = %v", d.Data)
	}
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": "+12015550123", "app": "guarded"}), 201)

	// Per-IP hourly limit.
	c.mustStatus(c.do("PATCH", path, map[string]any{"allowed_countries": []string{}, "ip_hourly_limit": 2}), 200)
	for _, to := range []string{"+919811100001", "+919822200002"} {
		dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to, "app": "guarded", "client_ip": "198.51.100.8"}), 201)
	}
	r = dev.do("POST", "/v1/otp", map[string]any{"to": "+919833300003", "app": "guarded", "client_ip": "198.51.100.8"})
	if r.Status != 429 || r.errCode() != "otp_blocked" || r.Header.Get("Retry-After") == "" || !strings.Contains(r.errMessage(), "IP address") {
		t.Fatalf("ip limit: %d %s", r.Status, r.Raw)
	}
	// Another IP is still fine.
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": "+919844400004", "app": "guarded", "client_ip": "198.51.100.9"}), 201)
	if r := dev.do("POST", "/v1/otp", map[string]any{"to": "+919855500005", "app": "guarded", "client_ip": "not-an-ip"}); r.Status != 422 {
		t.Fatalf("bad client_ip: %d %s", r.Status, r.Raw)
	}

	// Number-range bursts: the same number but its last three digits.
	c.mustStatus(c.do("PATCH", path, map[string]any{"range_hourly_limit": 2}), 200)
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": "+919876543001", "app": "guarded"}), 201)
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": "+919876543002", "app": "guarded"}), 201)
	r = dev.do("POST", "/v1/otp", map[string]any{"to": "+919876543003", "app": "guarded"})
	if r.Status != 429 || r.errCode() != "otp_blocked" || !strings.Contains(r.errMessage(), "range") {
		t.Fatalf("range burst: %d %s", r.Status, r.Raw)
	}
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": "+919876544003", "app": "guarded"}), 201)

	// Per-country cap.
	c.mustStatus(c.do("PATCH", path, map[string]any{"range_hourly_limit": 0, "country_hourly_limit": 1}), 200)
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": "+447400123456", "app": "guarded"}), 201)
	if r := dev.do("POST", "/v1/otp", map[string]any{"to": "+447400123789", "app": "guarded"}); r.Status != 429 || !strings.Contains(r.errMessage(), "GB numbers") {
		t.Fatalf("country cap: %d %s", r.Status, r.Raw)
	}

	// Blocks are stored, listed newest first and counted.
	var n int
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM otp_blocks WHERE app_id = $1`, app["id"]).Scan(&n); err != nil || n != 4 {
		t.Fatalf("otp_blocks rows = %d (%v)", n, err)
	}
	page := c.mustStatus(c.do("GET", path+"/blocks?limit=2", nil), 200).Body
	items := page["data"].([]any)
	if len(items) != 2 || page["has_more"] != true || items[0].(map[string]any)["reason"] != "country_limit" || items[1].(map[string]any)["reason"] != "range_burst" {
		t.Fatalf("blocks page 1 = %v", page)
	}
	next := c.mustStatus(c.do("GET", path+"/blocks?limit=2&starting_after="+items[1].(map[string]any)["id"].(string), nil), 200).Body
	if items := next["data"].([]any); len(items) != 2 || next["has_more"] != false || items[1].(map[string]any)["reason"] != "country_not_allowed" {
		t.Fatalf("blocks page 2 = %v", next)
	}
	if only := c.mustStatus(c.do("GET", path+"/blocks?reason=ip_limit", nil), 200).Body["data"].([]any); len(only) != 1 {
		t.Fatalf("ip_limit blocks = %v", only)
	}
	st := c.mustStatus(c.do("GET", path+"/stats?environment=test", nil), 200).Body["blocked"].(map[string]any)
	if st["total"] != float64(4) || st["country_not_allowed"] != float64(1) || st["ip_limit"] != float64(1) ||
		st["range_burst"] != float64(1) || st["country_limit"] != float64(1) || st["captcha_failed"] != float64(0) {
		t.Fatalf("block stats = %v", st)
	}
}

// fakeSiteverify stands in for Cloudflare Turnstile: "good" passes.
type fakeSiteverify struct {
	*httptest.Server
	mu    sync.Mutex
	forms []url.Values
}

func newFakeSiteverify(t *testing.T) *fakeSiteverify {
	f := &fakeSiteverify{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.forms = append(f.forms, r.PostForm)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("response") == "good" && r.PostForm.Get("secret") == "turnstile-secret" {
			_, _ = w.Write([]byte(`{"success":true,"hostname":"shop.example"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSiteverify) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forms[len(f.forms)-1]
}

func widgetClient(t *testing.T, srv *httptest.Server, origin string) *client {
	w := newClient(t, srv)
	w.origin = origin
	return w
}

// claimsOf decodes a JWT's payload and checks its HS256 signature by hand.
func claimsOf(t *testing.T, token, secret string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token = %q", token)
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(parts[0] + "." + parts[1]))
	if base64.RawURLEncoding.EncodeToString(m.Sum(nil)) != parts[2] {
		t.Fatal("token signature does not match the app secret")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestVerifyWidgetFlow(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	app := createVerifyApp(t, c, projectID, map[string]any{
		"name": "Shop", "widget_environment": "test", "allowed_origins": []string{"https://shop.example"},
		"redirect_uris": []string{"https://shop.example/done"},
	})
	key := app["publishable_key"].(string)
	base := "/v1/widget/" + key
	w := widgetClient(t, srv, "https://shop.example")

	cfg := w.mustStatus(w.do("GET", base, nil), 200)
	if cfg.Header.Get("Access-Control-Allow-Origin") != "https://shop.example" || cfg.Body["app_name"] != "Shop" ||
		cfg.Body["environment"] != "test" || cfg.Body["code_length"] != float64(6) || cfg.Body["turnstile_site_key"] != nil {
		t.Fatalf("config = %v (ACAO %q)", cfg.Body, cfg.Header.Get("Access-Control-Allow-Origin"))
	}

	// CORS: preflight from an allowed origin, refusal elsewhere; the dashboard (hosted page) is always allowed.
	pre := w.doWithHeaders("OPTIONS", base+"/send", nil, map[string]string{"Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "content-type"})
	if pre.Status != 204 || pre.Header.Get("Access-Control-Allow-Origin") != "https://shop.example" ||
		!strings.Contains(pre.Header.Get("Access-Control-Allow-Methods"), "POST") || !strings.Contains(strings.ToLower(pre.Header.Get("Access-Control-Allow-Headers")), "content-type") {
		t.Fatalf("preflight: %d %v", pre.Status, pre.Header)
	}
	evil := widgetClient(t, srv, "https://evil.example")
	if r := evil.doWithHeaders("OPTIONS", base+"/send", nil, map[string]string{"Access-Control-Request-Method": "POST"}); r.Status != 403 || r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("evil preflight: %d %v", r.Status, r.Header)
	}
	if r := evil.do("POST", base+"/send", map[string]any{"to": uniqueNumber()}); r.Status != 403 || r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("evil send: %d %s", r.Status, r.Raw)
	}
	if r := widgetClient(t, srv, dashboardOrigin).do("GET", base, nil); r.Status != 200 || r.Header.Get("Access-Control-Allow-Origin") != dashboardOrigin {
		t.Fatalf("hosted page origin: %d %v", r.Status, r.Header)
	}
	if r := w.do("GET", "/v1/widget/bpk_00000000000000000000000000000000", nil); r.Status != 404 {
		t.Fatalf("unknown key: %d", r.Status)
	}

	// Redirect URIs must match exactly.
	w.mustStatus(w.do("GET", base+"/redirect-check?redirect_uri="+url.QueryEscape("https://shop.example/done"), nil), 200)
	if r := w.do("GET", base+"/redirect-check?redirect_uri="+url.QueryEscape("https://shop.example/done/"), nil); r.Status != 400 || r.errCode() != "invalid_request" {
		t.Fatalf("unknown redirect: %d %s", r.Status, r.Raw)
	}

	// Send, a wrong code, then the right one, which returns a token.
	to := uniqueNumber()
	sent := w.mustStatus(w.do("POST", base+"/send", map[string]any{"to": to}), 201).Body
	code, _ := sent["code"].(string)
	if !sixDigits.MatchString(code) || !strings.HasPrefix(sent["verification_id"].(string), "otp_") || sent["expires_at"] == nil {
		t.Fatalf("send = %v", sent)
	}
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	bad := w.mustStatus(w.do("POST", base+"/verify", map[string]any{"verification_id": sent["verification_id"], "code": wrong}), 200).Body
	if bad["valid"] != false || bad["attempts_remaining"] != float64(4) || bad["token"] != nil {
		t.Fatalf("wrong code = %v", bad)
	}
	ok := w.mustStatus(w.do("POST", base+"/verify", map[string]any{"verification_id": sent["verification_id"], "code": code}), 200).Body
	token, _ := ok["token"].(string)
	if ok["valid"] != true || ok["status"] != "verified" || token == "" || ok["token_expires_at"] == nil {
		t.Fatalf("right code = %v", ok)
	}

	// The token is an HS256 JWT signed with the app's secret.
	secret := c.mustStatus(c.do("GET", verifyAppsPath(projectID)+"/"+app["id"].(string)+"/secret", nil), 200).Body["secret"].(string)
	if secret != app["secret"] {
		t.Fatalf("secret changed: %s vs %v", secret, app["secret"])
	}
	claims := claimsOf(t, token, secret)
	if claims["iss"] != "http://localhost:8080" || claims["aud"] != app["id"] || claims["sub"] != to || claims["vid"] != sent["verification_id"] ||
		claims["env"] != "test" || claims["exp"].(float64)-claims["iat"].(float64) != 600 || claims["jti"] == "" {
		t.Fatalf("claims = %v", claims)
	}

	// Bridge checks tokens too, for the matching environment only.
	testKey := apiKeyClient(t, srv, c, projectID, "test")
	res := testKey.mustStatus(testKey.do("POST", "/v1/otp/tokens/verify", map[string]any{"token": token}), 200).Body
	if res["valid"] != true || res["phone"] != to || res["verification_id"] != sent["verification_id"] || res["app_id"] != app["id"] ||
		res["environment"] != "test" || res["expires_at"] == nil || res["reason"] != nil {
		t.Fatalf("token check = %v", res)
	}
	liveKey := apiKeyClient(t, srv, c, projectID, "live")
	if res := liveKey.mustStatus(liveKey.do("POST", "/v1/otp/tokens/verify", map[string]any{"token": token}), 200).Body; res["valid"] != false || res["reason"] != "environment_mismatch" {
		t.Fatalf("live key, test token = %v", res)
	}
	parts := strings.Split(token, ".")
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"`+app["id"].(string)+`","sub":"+15550000009","env":"test","exp":4102444800}`)) + "." + parts[2]
	if res := testKey.mustStatus(testKey.do("POST", "/v1/otp/tokens/verify", map[string]any{"token": forged}), 200).Body; res["valid"] != false || res["reason"] != "bad_signature" {
		t.Fatalf("forged token = %v", res)
	}
	if res := testKey.mustStatus(testKey.do("POST", "/v1/otp/tokens/verify", map[string]any{"token": "garbage"}), 200).Body; res["reason"] != "malformed" {
		t.Fatalf("garbage token = %v", res)
	}
	// Another project's key cannot use the token.
	other, _, otherProject := signup(t, srv)
	otherKey := apiKeyClient(t, srv, other, otherProject, "test")
	if res := otherKey.mustStatus(otherKey.do("POST", "/v1/otp/tokens/verify", map[string]any{"token": token}), 200).Body; res["reason"] != "unknown_app" {
		t.Fatalf("other project = %v", res)
	}
	// Rotating the secret invalidates issued tokens.
	c.mustStatus(c.do("POST", verifyAppsPath(projectID)+"/"+app["id"].(string)+"/secret", nil), 200)
	if res := testKey.mustStatus(testKey.do("POST", "/v1/otp/tokens/verify", map[string]any{"token": token}), 200).Body; res["reason"] != "bad_signature" {
		t.Fatalf("after rotation = %v", res)
	}
	// Widgets only see their own app's verifications.
	devSent := testKey.mustStatus(testKey.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber()}), 201).Body
	if r := w.do("POST", base+"/verify", map[string]any{"verification_id": devSent["id"], "code": devSent["code"]}); r.Status != 404 {
		t.Fatalf("widget checked another app's code: %d %s", r.Status, r.Raw)
	}
}

func TestVerifyWidgetTurnstile(t *testing.T) {
	fake := newFakeSiteverify(t)
	srv := newServer(t, func(c *config.Config) { c.TurnstileVerifyURL = fake.URL })
	c, _, projectID := signup(t, srv)
	app := createVerifyApp(t, c, projectID, map[string]any{
		"name": "Captcha", "widget_environment": "test", "allowed_origins": []string{"https://shop.example"},
		"turnstile_site_key": "1x00000000000000000000AA", "turnstile_secret": "turnstile-secret",
	})
	raw, _ := json.Marshal(app)
	if app["turnstile_secret_set"] != true || strings.Contains(string(raw), "turnstile-secret") {
		t.Fatalf("app = %s", raw)
	}
	base := "/v1/widget/" + app["publishable_key"].(string)
	w := widgetClient(t, srv, "https://shop.example")
	if cfg := w.mustStatus(w.do("GET", base, nil), 200).Body; cfg["turnstile_site_key"] != "1x00000000000000000000AA" {
		t.Fatalf("config = %v", cfg)
	}

	for _, body := range []map[string]any{{"to": uniqueNumber()}, {"to": uniqueNumber(), "turnstile_token": "bad"}} {
		if r := w.do("POST", base+"/send", body); r.Status != 403 || r.errCode() != "otp_blocked" || !strings.Contains(r.errMessage(), "CAPTCHA") {
			t.Fatalf("send %v: %d %s", body, r.Status, r.Raw)
		}
	}
	w.mustStatus(w.do("POST", base+"/send", map[string]any{"to": uniqueNumber(), "turnstile_token": "good"}), 201)
	if f := fake.last(); f.Get("secret") != "turnstile-secret" || f.Get("response") != "good" || f.Get("remoteip") != w.ip {
		t.Fatalf("siteverify form = %v (client ip %s)", f, w.ip)
	}
	blocks := c.mustStatus(c.do("GET", verifyAppsPath(projectID)+"/"+app["id"].(string)+"/blocks", nil), 200).Body["data"].([]any)
	if len(blocks) != 2 || blocks[0].(map[string]any)["reason"] != "captcha_failed" || blocks[0].(map[string]any)["client_ip"] != w.ip {
		t.Fatalf("blocks = %v", blocks)
	}
	// The developer API, called from your server, needs no CAPTCHA.
	dev := apiKeyClient(t, srv, c, projectID, "test")
	dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber(), "app": app["id"]}), 201)

	// Removing Turnstile needs both keys cleared.
	path := verifyAppsPath(projectID) + "/" + app["id"].(string)
	if r := c.do("PATCH", path, map[string]any{"turnstile_secret": ""}); r.Status != 422 {
		t.Fatalf("half removal: %d %s", r.Status, r.Raw)
	}
	upd := c.mustStatus(c.do("PATCH", path, map[string]any{"turnstile_site_key": "", "turnstile_secret": ""}), 200).Body
	if upd["turnstile_site_key"] != nil || upd["turnstile_secret_set"] != false {
		t.Fatalf("removed = %v", upd)
	}
	w.mustStatus(w.do("POST", base+"/send", map[string]any{"to": uniqueNumber()}), 201)
}

func failoverJobsDone(t *testing.T, otpID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var done bool
		if err := testDB.Pool.QueryRow(context.Background(),
			`SELECT count(*) > 0 AND bool_and(state = 'completed') FROM river_job WHERE kind = 'otp.failover' AND args->>'verification_id' = $1`,
			otpID).Scan(&done); err != nil {
			t.Fatal(err)
		}
		if done {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("failover check for %s did not run", otpID)
}

var liveCodePattern = regexp.MustCompile(`^(\d{6}) is your`)

func TestVerifyFailoverWhenPhoneNeverAccepts(t *testing.T) {
	fake := newFakeTwilio(t)
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	live := apiKeyClient(t, srv, c, projectID, "live")
	addTwilio(t, c, projectID) // routing stays "phones": only failover uses Twilio
	def := defaultApp(t, c, projectID)
	c.mustStatus(c.do("PATCH", verifyAppsPath(projectID)+"/"+def["id"].(string), map[string]any{"failover_after_seconds": 1}), 200)

	to := uniqueNumber()
	sent := live.mustStatus(live.do("POST", "/v1/otp", map[string]any{"to": to}), 201).Body
	job := phone.expect(gateway.TypeSendSMS) // the phone gets the job but never accepts it
	m := liveCodePattern.FindStringSubmatch(job.Body)
	if m == nil {
		t.Fatalf("SMS body = %q", job.Body)
	}
	code := m[1]

	original := waitMessage(t, c, projectID, job.MessageID, "failed")
	if original["error_code"] != "superseded_by_failover" {
		t.Fatalf("original = %v %v", original["error_code"], original["error_message"])
	}
	v := live.mustStatus(live.do("GET", "/v1/otp/"+sent["id"].(string), nil), 200).Body
	fid, _ := v["failover_message_id"].(string)
	if fid == "" || v["message_id"] != job.MessageID || v["status"] != "pending" {
		t.Fatalf("verification = %v", v)
	}
	resent := waitMessage(t, c, projectID, fid, "sent")
	if resent["provider"] != "twilio" || resent["purpose"] != "otp" || strings.Contains(resent["body"].(string), code) {
		t.Fatalf("failover message = %v", resent)
	}
	var failoverEvent map[string]any
	for _, e := range resent["events"].([]any) {
		if ev := e.(map[string]any); ev["type"] == "failover" {
			failoverEvent = ev
		}
	}
	if failoverEvent == nil || failoverEvent["detail"].(map[string]any)["original_message_id"] != job.MessageID {
		t.Fatalf("failover event = %v (events %v)", failoverEvent, resent["events"])
	}
	forms := fake.sent()
	if len(forms) != 1 || forms[0].Get("To") != to || forms[0].Get("Body") != job.Body {
		t.Fatalf("twilio got %v, phone had %q", forms, job.Body)
	}

	// The same code verifies, and both messages lose their text.
	res := live.mustStatus(live.do("POST", "/v1/otp/verify", map[string]any{"id": sent["id"], "code": code}), 200).Body
	if res["valid"] != true || res["verification"].(map[string]any)["failover_message_status"] != "sent" {
		t.Fatalf("verify = %v", res)
	}
	var bodies int
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE id IN ($1, $2) AND body <> ''`, job.MessageID, fid).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatalf("%d message bodies kept (err %v)", bodies, err)
	}
	stats := c.mustStatus(c.do("GET", verifyAppsPath(projectID)+"/"+def["id"].(string)+"/stats?environment=live", nil), 200).Body
	if stats["failovers"] != float64(1) {
		t.Fatalf("stats = %v", stats)
	}
	failoverJobsDone(t, sent["id"].(string))
	if n := len(fake.sent()); n != 1 {
		t.Fatalf("code sent %d times through Twilio", n)
	}
}

func TestVerifyNoFailoverOncePhoneAccepted(t *testing.T) {
	fake := newFakeTwilio(t)
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	live := apiKeyClient(t, srv, c, projectID, "live")
	addTwilio(t, c, projectID)
	def := defaultApp(t, c, projectID)
	c.mustStatus(c.do("PATCH", verifyAppsPath(projectID)+"/"+def["id"].(string), map[string]any{"failover_after_seconds": 1}), 200)

	sent := live.mustStatus(live.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber()}), 201).Body
	job := phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: job.MessageID})
	failoverJobsDone(t, sent["id"].(string))

	if got := waitMessage(t, c, projectID, job.MessageID, "sending"); got["error_code"] != nil {
		t.Fatalf("original = %v", got)
	}
	if v := live.mustStatus(live.do("GET", "/v1/otp/"+sent["id"].(string), nil), 200).Body; v["failover_message_id"] != nil {
		t.Fatalf("failed over a message the phone accepted: %v", v)
	}
	if n := len(fake.sent()); n != 0 {
		t.Fatalf("Twilio was used %d times", n)
	}
}

func TestVerifyFailoverAtOnceWhenSendFails(t *testing.T) {
	fake := newFakeTwilio(t)
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	live := apiKeyClient(t, srv, c, projectID, "live")
	addTwilio(t, c, projectID)
	def := defaultApp(t, c, projectID)
	c.mustStatus(c.do("PATCH", verifyAppsPath(projectID)+"/"+def["id"].(string), map[string]any{"failover_after_seconds": 600}), 200)

	sent := live.mustStatus(live.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber()}), 201).Body
	job := phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: job.MessageID})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: job.MessageID, ErrorCode: "radio_off", ErrorMessage: "Airplane mode"})

	deadline := time.Now().Add(10 * time.Second)
	var v map[string]any
	for time.Now().Before(deadline) {
		if v = live.mustStatus(live.do("GET", "/v1/otp/"+sent["id"].(string), nil), 200).Body; v["failover_message_id"] != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	fid, _ := v["failover_message_id"].(string)
	if fid == "" {
		t.Fatalf("no failover after the send failed: %v", v)
	}
	if got := waitMessage(t, c, projectID, fid, "sent"); got["provider"] != "twilio" {
		t.Fatalf("failover message = %v", got)
	}
	if got := waitMessage(t, c, projectID, job.MessageID, "failed"); got["error_code"] != "radio_off" {
		t.Fatalf("original kept its own failure: %v", got["error_code"])
	}

	// An ambiguous failure (the SMS may be out already) never fails over: no duplicate code.
	before := len(fake.sent())
	sent = live.mustStatus(live.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber()}), 201).Body
	job = phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: job.MessageID})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: job.MessageID, ErrorCode: "generic_failure", ErrorMessage: "maybe sent"})
	waitMessage(t, c, projectID, job.MessageID, "failed")
	time.Sleep(time.Second)
	if v := live.mustStatus(live.do("GET", "/v1/otp/"+sent["id"].(string), nil), 200).Body; v["failover_message_id"] != nil || len(fake.sent()) != before {
		t.Fatalf("failed over an ambiguous failure: %v", v)
	}
}

func TestOTPBodyKeptUntilVerificationEnds(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	sent := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": uniqueNumber()}), 201).Body
	id := sent["message_id"].(string)
	waitStatus(t, dev, id, "delivered")

	ctx := context.Background()
	redact := func() {
		now := time.Now()
		if _, err := dbq.New(testDB.Pool).RedactMessageBodies(ctx, dbq.RedactMessageBodiesParams{Before: now.Add(-30 * 24 * time.Hour), OtpBefore: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() string {
		var body string
		if err := testDB.Pool.QueryRow(ctx, `SELECT body FROM messages WHERE id = $1`, id).Scan(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	redact()
	if !strings.Contains(stored(), sent["code"].(string)) {
		t.Fatal("a pending code's message was redacted as soon as it was sent")
	}
	if _, err := testDB.Pool.Exec(ctx, `UPDATE messages SET created_at = now() - interval '61 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	redact()
	if stored() != "" {
		t.Fatal("a code's message was kept past the longest code lifetime")
	}
}
