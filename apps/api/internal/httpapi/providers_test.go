package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"bridge/internal/config"
	"bridge/internal/gateway"
	"bridge/internal/provider"
	"bridge/internal/webhook"
)

// fakeTwilio answers the Messages API with status and records each request's form.
type fakeTwilio struct {
	*httptest.Server
	mu     sync.Mutex
	forms  []url.Values
	status int
	body   string
}

func newFakeTwilio(t *testing.T) *fakeTwilio {
	f := &fakeTwilio{status: 201, body: `{"sid":"SM00000000000000000000000000000001","status":"queued"}`}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		f.mu.Lock()
		f.forms = append(f.forms, form)
		status, body := f.status, f.body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	fakeProviders = map[provider.Kind]string{provider.Twilio: f.URL}
	t.Cleanup(func() { fakeProviders = nil })
	return f
}

func (f *fakeTwilio) sent() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.forms...)
}

func addTwilio(t *testing.T, c *client, projectID string) map[string]any {
	t.Helper()
	return c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/providers", map[string]any{
		"kind":        "twilio",
		"credentials": map[string]string{"account_sid": "AC123", "auth_token": "secret-token"},
		"config":      map[string]string{"from": "+15005550006"},
	}), 201).Body
}

func waitMessage(t *testing.T, c *client, projectID, id string, want ...string) map[string]any {
	t.Helper()
	var last map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		last = c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/messages/"+id, nil), 200).Body
		for _, w := range want {
			if last["status"] == w {
				return last
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("message %s is %v, wanted %v: %v", id, last["status"], want, last["error_message"])
	return nil
}

func TestProviderAccounts(t *testing.T) {
	// Without BRIDGE_SECRET_KEY credentials cannot be stored.
	srv := newServer(t, func(c *config.Config) { c.SecretKey = nil })
	c, _, projectID := signup(t, srv)
	r := c.do("POST", "/v1/projects/"+projectID+"/providers", map[string]any{
		"kind": "twilio", "credentials": map[string]string{"account_sid": "AC1", "auth_token": "t"}, "config": map[string]string{"from": "+1"},
	})
	if r.Status != 409 || !strings.Contains(r.errMessage(), "BRIDGE_SECRET_KEY") {
		t.Fatalf("no key: %d %s", r.Status, r.Raw)
	}

	fake := newFakeTwilio(t)
	fake.body = `{"friendly_name":"Acme","status":"active"}`
	fake.status = 200
	srv = newServer(t)
	c, _, projectID = signup(t, srv)
	base := "/v1/projects/" + projectID + "/providers"
	if r := c.do("POST", base, map[string]any{"kind": "twilio", "credentials": map[string]string{"account_sid": "AC1"}, "config": map[string]string{"from": "+1"}}); r.Status != 422 || !strings.Contains(string(r.Raw), "credentials.auth_token") {
		t.Fatalf("missing token: %d %s", r.Status, r.Raw)
	}
	acct := addTwilio(t, c, projectID)
	raw, _ := json.Marshal(acct)
	if strings.Contains(string(raw), "secret-token") || acct["credential_hint"] != "AC123" || acct["callbacks"] != "per_message" ||
		!strings.Contains(acct["callback_url"].(string), "/v1/provider-callbacks/"+acct["id"].(string)+"/") {
		t.Fatalf("account = %s", raw)
	}
	if r := c.do("POST", base, map[string]any{"kind": "twilio", "credentials": map[string]string{"account_sid": "AC2", "auth_token": "t"}, "config": map[string]string{"from": "+1"}}); r.Status != 409 {
		t.Fatalf("second twilio: %d", r.Status)
	}
	check := c.mustStatus(c.do("POST", base+"/"+acct["id"].(string)+"/check", nil), 200).Body
	if check["ok"] != true || check["detail"] != `Account "Acme" is active.` {
		t.Fatalf("check = %v", check)
	}
	upd := c.mustStatus(c.do("PATCH", base+"/"+acct["id"].(string), map[string]any{"enabled": false, "config": map[string]string{"messaging_service_sid": "MG1"}}), 200).Body
	if upd["enabled"] != false || upd["config"].(map[string]any)["messaging_service_sid"] != "MG1" {
		t.Fatalf("update = %v", upd)
	}
	c.mustStatus(c.do("DELETE", base+"/"+acct["id"].(string), nil), 204)
	if list := c.mustStatus(c.do("GET", base, nil), 200).Body; list != nil {
		t.Fatalf("list after delete = %v", list)
	}
}

func TestProviderFallbackAndDeliveryReports(t *testing.T) {
	fake := newFakeTwilio(t)
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	live := apiKeyClient(t, srv, c, projectID, "live")
	acct := addTwilio(t, c, projectID)

	// Default routing (phones only): no phone, so the message fails as before.
	m := live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "phones only"}), 202).Body
	if got := waitMessage(t, c, projectID, m["id"].(string), "failed"); got["error_code"] != "no_device" {
		t.Fatalf("phones only: %v", got["error_code"])
	}
	if len(fake.sent()) != 0 {
		t.Fatal("phones-only routing used a provider")
	}

	c.mustStatus(c.do("PUT", "/v1/projects/"+projectID+"/routing", map[string]any{"mode": "phones_then_providers", "fallback_after_seconds": 0}), 200)
	m = live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "Your order has shipped."}), 202).Body
	got := waitMessage(t, c, projectID, m["id"].(string), "sent", "failed")
	if got["status"] != "sent" || got["provider"] != "twilio" {
		t.Fatalf("fallback: %v %v %v", got["status"], got["provider"], got["error_message"])
	}
	forms := fake.sent()
	if len(forms) != 1 || forms[0].Get("To") != "+919876543210" || forms[0].Get("Body") != "Your order has shipped." ||
		!strings.HasSuffix(forms[0].Get("StatusCallback"), strings.TrimPrefix(acct["callback_url"].(string), "http://localhost:8080")) {
		t.Fatalf("twilio request = %v", forms)
	}
	var types []string
	for _, e := range got["events"].([]any) {
		types = append(types, e.(map[string]any)["type"].(string))
	}
	if strings.Join(types, ",") != "created,queued,provider_fallback,provider_accepted,sent" {
		t.Fatalf("timeline = %v", types)
	}

	// Delivery report through the account's callback URL; a wrong token is refused.
	callback := srv.URL + strings.TrimPrefix(acct["callback_url"].(string), "http://localhost:8080")
	bad, _ := http.Post(srv.URL+"/v1/provider-callbacks/"+acct["id"].(string)+"/wrong", "application/x-www-form-urlencoded", strings.NewReader("MessageSid=SM00000000000000000000000000000001&MessageStatus=delivered"))
	if bad.StatusCode != 404 {
		t.Fatalf("wrong token: %d", bad.StatusCode)
	}
	res, err := http.Post(callback, "application/x-www-form-urlencoded", strings.NewReader("MessageSid=SM00000000000000000000000000000001&MessageStatus=delivered"))
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("callback: %v %v", res, err)
	}
	waitMessage(t, c, projectID, m["id"].(string), "delivered")

	// Test keys never reach a provider.
	testKey := apiKeyClient(t, srv, c, projectID, "test")
	tm := testKey.mustStatus(testKey.do("POST", "/v1/messages", map[string]any{"to": "+15550000001", "message": "simulated"}), 202).Body
	if got := waitMessage(t, c, projectID, tm["id"].(string), "delivered"); got["provider"] != "simulator" {
		t.Fatalf("test message provider = %v", got["provider"])
	}
	if len(fake.sent()) != 1 {
		t.Fatalf("test message reached twilio: %d requests", len(fake.sent()))
	}
}

func TestProviderRefusalFailsMessage(t *testing.T) {
	fake := newFakeTwilio(t)
	fake.status, fake.body = 400, `{"code":21211,"message":"The 'To' number is not a valid phone number."}`
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	live := apiKeyClient(t, srv, c, projectID, "live")
	acct := addTwilio(t, c, projectID)
	c.mustStatus(c.do("PUT", "/v1/projects/"+projectID+"/routing", map[string]any{"mode": "providers", "fallback_after_seconds": 60}), 200)

	m := live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202).Body
	got := waitMessage(t, c, projectID, m["id"].(string), "failed")
	if got["error_code"] != "twilio_21211" || !strings.Contains(got["error_message"].(string), "not a valid phone number") {
		t.Fatalf("failure = %v %v", got["error_code"], got["error_message"])
	}
	accounts := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/providers", nil), 200).Raw
	if !bytes.Contains(accounts, []byte("not a valid phone number")) {
		t.Fatalf("last_error not recorded: %s (account %v)", accounts, acct["id"])
	}
}

func TestSupabaseSendSMSHook(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	in := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/integrations", map[string]any{"kind": "supabase_send_sms", "environment": "test"}), 201).Body
	hook := srv.URL + strings.TrimPrefix(in["hook_url"].(string), "http://localhost:8080")
	if !strings.HasSuffix(hook, "/v1/hooks/supabase/"+in["id"].(string)) {
		t.Fatalf("hook url = %v", in["hook_url"])
	}
	payload := []byte(`{"user":{"id":"6481a5c1","phone":"+15550000001"},"sms":{"otp":"561166"}}`)
	post := func(secret string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, hook, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if secret != "" {
			now := time.Now()
			sig, err := webhook.Sign(secret, "msg_test", now, payload)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("webhook-id", "msg_test")
			req.Header.Set("webhook-timestamp", strconv.FormatInt(now.Unix(), 10))
			req.Header.Set("webhook-signature", sig)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Before the secret is pasted, Supabase gets a clear error.
	if res := post(""); res.StatusCode != 503 {
		t.Fatalf("no secret: %d", res.StatusCode)
	}
	secret := "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if r := c.do("PATCH", "/v1/projects/"+projectID+"/integrations/"+in["id"].(string), map[string]any{"secret": "not-a-secret"}); r.Status != 422 {
		t.Fatalf("bad secret: %d", r.Status)
	}
	upd := c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID+"/integrations/"+in["id"].(string), map[string]any{"secret": "v1," + secret}), 200).Body
	if upd["secret_set"] != true {
		t.Fatalf("update = %v", upd)
	}

	other := "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	if res := post(other); res.StatusCode != 401 {
		t.Fatalf("wrong signature: %d", res.StatusCode)
	}
	res := post(secret)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("hook: %d %s", res.StatusCode, body)
	}

	// The SMS uses the project's template, masked like Bridge's own codes.
	list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/messages?environment=test", nil), 200).Body["data"].([]any)
	m := list[0].(map[string]any)
	if m["purpose"] != "otp" || m["to"] != "+15550000001" || strings.Contains(m["body"].(string), "561166") ||
		!strings.HasPrefix(m["body"].(string), "•••••• is your Default code.") || m["metadata"].(map[string]any)["source"] != "supabase" {
		t.Fatalf("message = %v", m)
	}
	ints := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/integrations", nil), 200).Raw
	if !bytes.Contains(ints, []byte(`"last_used_at":"`)) {
		t.Fatalf("last_used_at not recorded: %s", ints)
	}
}

func TestPhoneFailureFallbackAvoidsDuplicates(t *testing.T) {
	fake := newFakeTwilio(t)
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	live := apiKeyClient(t, srv, c, projectID, "live")
	addTwilio(t, c, projectID)
	c.mustStatus(c.do("PUT", "/v1/projects/"+projectID+"/routing", map[string]any{"mode": "phones_then_providers", "fallback_after_seconds": 60}), 200)

	// An ambiguous failure may already have reached the network: no provider send.
	m := live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "maybe sent"}), 202).Body
	job := phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: job.MessageID, ErrorCode: "generic_failure", ErrorMessage: "generic"})
	if got := waitMessage(t, c, projectID, m["id"].(string), "failed"); got["error_code"] != "generic_failure" {
		t.Fatalf("ambiguous failure: %v", got["error_code"])
	}
	if n := len(fake.sent()); n != 0 {
		t.Fatalf("provider used after an ambiguous phone failure (%d requests)", n)
	}

	// A SIM problem says nothing about the destination: the provider takes over.
	m = live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "use a provider"}), 202).Body
	job = phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: job.MessageID, ErrorCode: "sim_absent", ErrorMessage: "no SIM"})
	if got := waitMessage(t, c, projectID, m["id"].(string), "sent", "failed"); got["provider"] != "twilio" {
		t.Fatalf("sim failure: %v via %v", got["status"], got["provider"])
	}
}
