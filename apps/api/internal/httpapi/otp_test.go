package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bridge/internal/gateway"
)

var sixDigits = regexp.MustCompile(`^\d{6}$`)

var numberCounter atomic.Uint32

// uniqueNumber returns a fresh Indian mobile number, so per-number limits
// never carry over between tests or repeated runs.
func uniqueNumber() string {
	return fmt.Sprintf("+9198%04d%04d", time.Now().UnixNano()%10000, numberCounter.Add(1)%10000)
}

// backdateOTPs moves a number's codes into the past so the resend cooldown
// does not block the next send.
func backdateOTPs(t *testing.T, to string) {
	t.Helper()
	if _, err := testDB.Pool.Exec(context.Background(),
		`UPDATE otp_verifications SET created_at = created_at - interval '1 minute' WHERE recipient = $1`, to); err != nil {
		t.Fatal(err)
	}
}

func TestOTPTestModeFlow(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	to := uniqueNumber()

	sent := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to[:3] + " " + to[3:8] + "-" + to[8:], "metadata": map[string]any{"user": "u_1"}}), 201).Body
	code, _ := sent["code"].(string)
	if !sixDigits.MatchString(code) || sent["status"] != "pending" || sent["to"] != to || sent["environment"] != "test" ||
		sent["attempts_remaining"] != float64(5) || sent["message_id"] == nil {
		t.Fatalf("send = %v", sent)
	}
	if sent["metadata"].(map[string]any)["user"] != "u_1" {
		t.Fatalf("metadata = %v", sent["metadata"])
	}

	// The SMS shows the code masked, never the code itself.
	msg := dev.mustStatus(dev.do("GET", "/v1/messages/"+sent["message_id"].(string), nil), 200).Body
	body, _ := msg["body"].(string)
	if msg["purpose"] != "otp" || strings.Contains(body, code) || !strings.Contains(body, "••••••") || !strings.Contains(body, "Default") {
		t.Fatalf("message body = %q purpose = %v", body, msg["purpose"])
	}

	// A wrong code uses an attempt; the right one verifies.
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	r := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"to": to, "code": wrong}), 200).Body
	if r["valid"] != false || r["verification"].(map[string]any)["attempts_remaining"] != float64(4) {
		t.Fatalf("wrong code = %v", r)
	}
	r = dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"to": to, "code": code}), 200).Body
	v := r["verification"].(map[string]any)
	if r["valid"] != true || v["status"] != "verified" || v["verified_at"] == nil || v["attempts"] != float64(2) {
		t.Fatalf("right code = %v", r)
	}

	// The hash is erased once finished, and a used code cannot be reused.
	var hashGone bool
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT code_hash IS NULL FROM otp_verifications WHERE id = $1`, sent["id"]).Scan(&hashGone); err != nil || !hashGone {
		t.Fatalf("code hash kept after verification (err %v)", err)
	}
	if r := dev.do("POST", "/v1/otp/verify", map[string]any{"to": to, "code": code}); r.Status != 404 {
		t.Fatalf("reuse by number: %d %s", r.Status, r.Raw)
	}
	again := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"id": sent["id"], "code": code}), 200).Body
	if again["valid"] != false || again["verification"].(map[string]any)["status"] != "verified" {
		t.Fatalf("reuse by id = %v", again)
	}

	got := dev.mustStatus(dev.do("GET", "/v1/otp/"+sent["id"].(string), nil), 200).Body
	if got["status"] != "verified" {
		t.Fatalf("get = %v", got)
	}

	// Input validation.
	for _, body := range []map[string]any{
		{"to": to, "code": "12ab56"},
		{"code": "123456"},
		{"to": to, "id": sent["id"], "code": "123456"},
	} {
		if r := dev.do("POST", "/v1/otp/verify", body); r.Status != 422 {
			t.Errorf("verify %v: %d %s", body, r.Status, r.Raw)
		}
	}
	if r := dev.do("POST", "/v1/otp", map[string]any{"to": to, "android_app_hash": "short"}); r.Status != 422 {
		t.Errorf("bad app hash: %d", r.Status)
	}
}

func TestOTPAttemptsExpiryAndResend(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	to := uniqueNumber()

	first := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to}), 201).Body

	// Resending at once is refused with Retry-After.
	r := dev.do("POST", "/v1/otp", map[string]any{"to": to})
	if r.Status != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" || r.errCode() != "rate_limited" {
		t.Fatalf("immediate resend: %d %s", r.Status, r.Raw)
	}

	// A new code cancels the old one.
	backdateOTPs(t, to)
	second := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to}), 201).Body
	old := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"id": first["id"], "code": first["code"]}), 200).Body
	if old["valid"] != false || old["verification"].(map[string]any)["status"] != "canceled" {
		t.Fatalf("old code after resend = %v", old)
	}

	// Five wrong codes fail the verification; then even the right code is refused.
	wrong := "000000"
	if wrong == second["code"] {
		wrong = "111111"
	}
	for i := range 5 {
		res := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"id": second["id"], "code": wrong}), 200).Body
		want := "pending"
		if i == 4 {
			want = "failed"
		}
		if st := res["verification"].(map[string]any)["status"]; st != want {
			t.Fatalf("attempt %d: status %v, want %s", i+1, st, want)
		}
	}
	res := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"id": second["id"], "code": second["code"]}), 200).Body
	if res["valid"] != false || res["verification"].(map[string]any)["status"] != "failed" {
		t.Fatalf("right code after failure = %v", res)
	}

	// Expired codes are refused.
	backdateOTPs(t, to)
	third := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to}), 201).Body
	if _, err := testDB.Pool.Exec(context.Background(), `UPDATE otp_verifications SET expires_at = now() - interval '1 second' WHERE id = $1`, third["id"]); err != nil {
		t.Fatal(err)
	}
	if got := dev.mustStatus(dev.do("GET", "/v1/otp/"+third["id"].(string), nil), 200).Body; got["status"] != "expired" {
		t.Fatalf("lapsed code shows %v", got["status"])
	}
	res = dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"to": to, "code": third["code"]}), 200).Body
	if res["valid"] != false || res["verification"].(map[string]any)["status"] != "expired" {
		t.Fatalf("expired code = %v", res)
	}

	// At most five codes per number per hour (three sent above).
	for range 2 {
		backdateOTPs(t, to)
		dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to}), 201)
	}
	backdateOTPs(t, to)
	if r := dev.do("POST", "/v1/otp", map[string]any{"to": to}); r.Status != http.StatusTooManyRequests {
		t.Fatalf("sixth code in an hour: %d %s", r.Status, r.Raw)
	}
}

func TestOTPLiveThroughPhone(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	dev := apiKeyClient(t, srv, c, projectID, "live")
	testKey := apiKeyClient(t, srv, c, projectID, "test")

	c.mustStatus(c.do("PUT", "/v1/projects/"+projectID+"/otp/settings", map[string]any{
		"app_name": "Acme", "template": "Your {app} code is {code}. Valid {minutes} min.",
		"code_length": 8, "ttl_seconds": 300, "max_attempts": 3,
	}), 200)

	to := uniqueNumber()
	sent := dev.mustStatus(dev.do("POST", "/v1/otp", map[string]any{"to": to, "android_app_hash": "FA+9qCX9VSu"}), 201).Body
	if _, ok := sent["code"]; ok {
		t.Fatal("live keys must never receive the code")
	}
	if sent["attempts_remaining"] != float64(3) {
		t.Fatalf("send = %v", sent)
	}

	job := phone.expect(gateway.TypeSendSMS)
	m := regexp.MustCompile(`^Your Acme code is (\d{8})\. Valid 5 min\.\nFA\+9qCX9VSu$`).FindStringSubmatch(job.Body)
	if m == nil {
		t.Fatalf("SMS body = %q", job.Body)
	}
	code := m[1]

	// Test keys cannot see or check live codes.
	if r := testKey.do("GET", "/v1/otp/"+sent["id"].(string), nil); r.Status != 404 {
		t.Fatalf("test key read live verification: %d", r.Status)
	}
	if r := testKey.do("POST", "/v1/otp/verify", map[string]any{"id": sent["id"], "code": code}); r.Status != 404 {
		t.Fatalf("test key verified live code: %d", r.Status)
	}

	segs := int16(1)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: job.MessageID})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: job.MessageID, Segments: &segs})
	waitStatus(t, dev, job.MessageID, "sent")

	res := dev.mustStatus(dev.do("POST", "/v1/otp/verify", map[string]any{"to": to, "code": code}), 200).Body
	if res["valid"] != true || res["verification"].(map[string]any)["message_status"] != "sent" {
		t.Fatalf("verify = %v", res)
	}

	// Once used and sent, the code is gone from the stored message too.
	var stored string
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT body FROM messages WHERE id = $1`, job.MessageID).Scan(&stored); err != nil || stored != "" {
		t.Fatalf("stored body after verification = %q (err %v)", stored, err)
	}
	msg := dev.mustStatus(dev.do("GET", "/v1/messages/"+job.MessageID, nil), 200).Body
	if b, _ := msg["body"].(string); !strings.HasPrefix(b, "Your Acme code is ••••••••.") {
		t.Fatalf("API body = %q", b)
	}

	// Dashboard list and stats.
	list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/otp?environment=live", nil), 200).Body
	if items := list["data"].([]any); len(items) != 1 || items[0].(map[string]any)["status"] != "verified" {
		t.Fatalf("list = %v", list)
	}
	stats := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/otp/stats?environment=live", nil), 200).Body
	if stats["verified"] != float64(1) || stats["conversion_rate"] != float64(1) {
		t.Fatalf("stats = %v", stats)
	}
}

func TestOTPSettings(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	path := "/v1/projects/" + projectID + "/otp/settings"

	def := c.mustStatus(c.do("GET", path, nil), 200).Body
	if def["template"] != nil || def["code_length"] != float64(6) || def["effective_app_name"] != "Default" ||
		!strings.HasPrefix(def["preview"].(string), "482913 is your Default code.") {
		t.Fatalf("defaults = %v", def)
	}

	for _, bad := range []map[string]any{
		{"template": "No code here", "code_length": 6, "ttl_seconds": 600, "max_attempts": 5},
		{"template": "{code} and {code}", "code_length": 6, "ttl_seconds": 600, "max_attempts": 5},
		{"template": "{code} for {user}", "code_length": 6, "ttl_seconds": 600, "max_attempts": 5},
		{"web_otp_domain": "https://example.com/", "code_length": 6, "ttl_seconds": 600, "max_attempts": 5},
		{"code_length": 3, "ttl_seconds": 600, "max_attempts": 5},
	} {
		if r := c.do("PUT", path, bad); r.Status != 422 {
			t.Errorf("accepted %v: %d", bad, r.Status)
		}
	}

	saved := c.mustStatus(c.do("PUT", path, map[string]any{
		"app_name": "Acme", "code_length": 6, "ttl_seconds": 600, "max_attempts": 5, "web_otp_domain": "Acme.example.com",
	}), 200).Body
	if saved["web_otp_domain"] != "acme.example.com" ||
		saved["preview"] != "482913 is your Acme code. It expires in 10 minutes. Do not share it.\n\n@acme.example.com #482913" {
		t.Fatalf("saved = %v", saved)
	}

	// The dashboard can send and check test codes.
	sent := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/otp", map[string]any{"to": uniqueNumber()}), 201).Body
	res := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/otp/verify", map[string]any{"id": sent["id"], "code": sent["code"]}), 200).Body
	if res["valid"] != true {
		t.Fatalf("dashboard verify = %v", res)
	}
}
