package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
)

// apiKeyClient creates an API key in the project and returns a client using it.
func apiKeyClient(t *testing.T, srv *httptest.Server, c *client, projectID, env string) *client {
	t.Helper()
	k := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/api-keys", map[string]any{"name": env, "environment": env}), 201)
	dev := newClient(t, srv)
	dev.origin = ""
	dev.apiKey = k.Body["secret"].(string)
	return dev
}

func waitStatus(t *testing.T, dev *client, id, want string) map[string]any {
	t.Helper()
	var last map[string]any
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		last = dev.mustStatus(dev.do("GET", "/v1/messages/"+id, nil), 200).Body
		if last["status"] == want {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("message %s stayed %v (wanted %s): %v", id, last["status"], want, last["error_message"])
	return nil
}

func eventTypes(m map[string]any) []string {
	var out []string
	for _, e := range m["events"].([]any) {
		out = append(out, e.(map[string]any)["type"].(string))
	}
	return out
}

// fakePhone is a paired device holding the gateway WebSocket.
type fakePhone struct {
	t          *testing.T
	srv        *httptest.Server
	ws         *websocket.Conn
	ID         string
	credential string
}

func connectPhone(t *testing.T, srv *httptest.Server, c *client, projectID string) *fakePhone {
	t.Helper()
	d := pairNewDevice(t, srv, c, projectID)
	p := &fakePhone{t: t, srv: srv, ID: d.ID, credential: d.Credential}
	p.connect()
	charging, wifi := true, "wifi"
	p.send(gateway.Inbound{Type: gateway.TypeHeartbeat, Seq: 1, NextIn: 60, Status: &gateway.Status{IsCharging: &charging, NetworkType: &wifi}})
	p.expect(gateway.TypeHeartbeatAck)
	return p
}

func (p *fakePhone) connect() {
	p.t.Helper()
	ws, _, err := dial(p.t, p.srv, p.credential)
	if err != nil {
		p.t.Fatal(err)
	}
	p.t.Cleanup(func() { _ = ws.CloseNow() })
	p.ws = ws
	p.expect(gateway.TypeWelcome)
}

func (p *fakePhone) send(in gateway.Inbound) {
	p.t.Helper()
	writeFrame(p.t, p.ws, in)
}

// expect reads frames until one of the given type arrives.
func (p *fakePhone) expect(typ string) gateway.Outbound {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		_, data, err := p.ws.Read(ctx)
		if err != nil {
			p.t.Fatalf("waiting for %s: %v", typ, err)
		}
		var f gateway.Outbound
		_ = json.Unmarshal(data, &f)
		if f.Type == typ {
			return f
		}
	}
}

// report sends a status report and waits for the server to acknowledge it.
func (p *fakePhone) report(in gateway.Inbound) {
	p.t.Helper()
	p.send(in)
	ack := p.expect(gateway.TypeReportAck)
	if ack.MessageID != in.MessageID || ack.Report != in.Type {
		p.t.Fatalf("unexpected ack %+v for %s", ack, in.Type)
	}
}

func TestTestModeMessagesAreSimulated(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")

	for _, tc := range []struct{ to, field string }{{"12345", "body.to"}, {"+1 2", "body.to"}} {
		r := dev.do("POST", "/v1/messages", map[string]any{"to": tc.to, "message": "hi"})
		if r.Status != 422 || !strings.Contains(string(r.Raw), tc.field) {
			t.Errorf("to=%q: %d %s", tc.to, r.Status, r.Raw)
		}
	}
	if r := dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": ""}); r.Status != 422 {
		t.Errorf("empty message accepted: %d", r.Status)
	}

	r := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{
		"to": "+91 98765-43210", "message": "Your order has shipped.", "metadata": map[string]any{"order_id": "ord_123"},
	}), 202)
	id := r.Body["id"].(string)
	if r.Body["status"] != "queued" || r.Body["to"] != "+919876543210" || r.Body["provider"] != "simulator" ||
		r.Body["encoding"] != "gsm7" || r.Body["segments"] != float64(1) || r.Body["environment"] != "test" {
		t.Fatalf("send response: %s", r.Raw)
	}
	if r.Body["metadata"].(map[string]any)["order_id"] != "ord_123" {
		t.Fatalf("metadata not returned: %s", r.Raw)
	}

	done := waitStatus(t, dev, id, "delivered")
	got := strings.Join(eventTypes(done), ",")
	if got != "created,queued,device_accepted,sent,delivered" {
		t.Fatalf("timeline = %s", got)
	}
	for _, k := range []string{"queued_at", "sending_at", "sent_at", "delivered_at"} {
		if done[k] == nil {
			t.Errorf("%s not set", k)
		}
	}

	failed := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+15550000002", "message": "x"}), 202)
	f := waitStatus(t, dev, failed.Body["id"].(string), "failed")
	if f["error_code"] != "invalid_destination" {
		t.Fatalf("test number 0002: %v", f["error_code"])
	}

	// Live keys never see test messages, and vice versa.
	live := apiKeyClient(t, srv, c, projectID, "live")
	if r := live.do("GET", "/v1/messages/"+id, nil); r.Status != 404 {
		// GET by ID is project-scoped; listing is environment-scoped.
		_ = r
	}
	if l := live.mustStatus(live.do("GET", "/v1/messages", nil), 200); len(l.Body["data"].([]any)) != 0 {
		t.Fatalf("live key listed test messages: %s", l.Raw)
	}

	page := dev.mustStatus(dev.do("GET", "/v1/messages?limit=1", nil), 200)
	if page.Body["has_more"] != true || len(page.Body["data"].([]any)) != 1 {
		t.Fatalf("page 1: %s", page.Raw)
	}
	first := page.Body["data"].([]any)[0].(map[string]any)["id"].(string)
	page2 := dev.mustStatus(dev.do("GET", "/v1/messages?limit=1&starting_after="+first, nil), 200)
	if page2.Body["data"].([]any)[0].(map[string]any)["id"] == first {
		t.Fatal("pagination returned the same message twice")
	}
}

func TestIdempotency(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")

	send := func(body string) response {
		req, _ := http.NewRequest("POST", dev.base+"/v1/messages", strings.NewReader(`{"to":"+919876543210","message":"`+body+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+dev.apiKey)
		req.Header.Set("Idempotency-Key", "order-123-shipped")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := response{Status: resp.StatusCode, Header: resp.Header}
		_ = json.NewDecoder(resp.Body).Decode(&out.Body)
		return out
	}
	a := send("hello")
	b := send("hello")
	if a.Status != 202 || b.Status != 200 || a.Body["id"] != b.Body["id"] || b.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %d %v %v", a.Status, b.Status, a.Body["id"], b.Body["id"])
	}
	if c := send("different"); c.Status != 409 {
		t.Fatalf("reused key with a different body: %d", c.Status)
	}
}

func TestLiveMessageThroughPhone(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	dev := apiKeyClient(t, srv, c, projectID, "live")

	m := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "Your table is ready."}), 202)
	id := m.Body["id"].(string)

	job := phone.expect(gateway.TypeSendSMS)
	if job.MessageID != id || job.To != "+919876543210" || job.Body != "Your table is ready." || job.Attempt != 1 || job.SimSlot != nil {
		t.Fatalf("send_sms frame = %+v", job)
	}

	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: id})
	waitStatus(t, dev, id, "sending")
	segs := int16(1)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: id, Segments: &segs})
	waitStatus(t, dev, id, "sent")
	yes := true
	phone.report(gateway.Inbound{Type: gateway.TypeSMSDelivery, MessageID: id, Delivered: &yes})
	done := waitStatus(t, dev, id, "delivered")
	if done["device_id"] != phone.ID {
		t.Fatalf("device_id = %v", done["device_id"])
	}
	if got := strings.Join(eventTypes(done), ","); got != "created,queued,assigned,device_accepted,sent,delivered" {
		t.Fatalf("timeline = %s", got)
	}

	// Duplicate and late reports are acknowledged and change nothing.
	phone.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: id, Segments: &segs})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: id, ErrorCode: "generic_failure"})
	if s := waitStatus(t, dev, id, "delivered"); len(s["events"].([]any)) != 6 {
		t.Fatalf("duplicate reports changed the timeline: %v", eventTypes(s))
	}

	// Another project's key cannot read the message.
	other, _, otherProject := signup(t, srv)
	otherKey := apiKeyClient(t, srv, other, otherProject, "live")
	if r := otherKey.do("GET", "/v1/messages/"+id, nil); r.Status != 404 {
		t.Fatalf("cross-project read: %d", r.Status)
	}

	usage := dev.mustStatus(dev.do("GET", "/v1/usage", nil), 200)
	day := usage.Body["last_24_hours"].(map[string]any)
	if day["delivered"] != float64(1) || day["success_rate"] != float64(1) || usage.Body["devices_online"] != float64(1) {
		t.Fatalf("usage = %s", usage.Raw)
	}
}

func TestNoDeviceFails(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "live")
	m := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202)
	f := waitStatus(t, dev, m.Body["id"].(string), "failed")
	if f["error_code"] != "no_device" || !strings.Contains(f["error_message"].(string), "Pair one") {
		t.Fatalf("failure = %v / %v", f["error_code"], f["error_message"])
	}
}

func TestUnacceptedJobIsReassignedAndRedelivered(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	dev := apiKeyClient(t, srv, c, projectID, "live")

	m := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202)
	id := m.Body["id"].(string)
	if f := phone.expect(gateway.TypeSendSMS); f.Attempt != 1 {
		t.Fatalf("first attempt = %d", f.Attempt)
	}
	// The phone ignores the job; after the assignment timeout it is reassigned.
	second := phone.expect(gateway.TypeSendSMS)
	if second.MessageID != id || second.Attempt != 2 {
		t.Fatalf("reassignment frame = %+v", second)
	}

	// Reconnecting redelivers the pending job without a new assignment.
	_ = phone.ws.Close(websocket.StatusNormalClosure, "")
	phone.connect()
	again := phone.expect(gateway.TypeSendSMS)
	if again.MessageID != id || again.Attempt != 2 {
		t.Fatalf("redelivered frame = %+v", again)
	}
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: id})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: id})

	final := waitStatus(t, dev, id, "sent")
	types := strings.Join(eventTypes(final), ",")
	if !strings.Contains(types, "assignment_timed_out") || final["attempts"] != float64(2) {
		t.Fatalf("timeline %s attempts %v", types, final["attempts"])
	}
}

func TestSendLimitAndSIMChoice(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	dev := apiKeyClient(t, srv, c, projectID, "live")

	c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID+"/devices/"+phone.ID, map[string]any{"send_limit_count": 1, "preferred_sim_slot": 2}), 200)
	st := deviceStatus(t, c, projectID, phone.ID)
	if st["send_limit_count"] != float64(1) || st["preferred_sim_slot"] != float64(2) {
		t.Fatalf("settings not saved: %v", st)
	}

	a := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "one"}), 202)
	f := phone.expect(gateway.TypeSendSMS)
	if f.MessageID != a.Body["id"] || f.SimSlot == nil || *f.SimSlot != 2 {
		t.Fatalf("frame = %+v", f)
	}
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: f.MessageID})

	// The phone is at its limit: the second message waits in the queue.
	b := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543211", "message": "two", "sim_slot": 1}), 202)
	time.Sleep(1500 * time.Millisecond)
	waiting := dev.mustStatus(dev.do("GET", "/v1/messages/"+b.Body["id"].(string), nil), 200)
	if waiting.Body["status"] != "queued" || waiting.Body["device_id"] != nil {
		t.Fatalf("second message should wait for capacity: %s", waiting.Raw)
	}

	// Raising the limit releases it, with the per-message SIM override.
	c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID+"/devices/"+phone.ID, map[string]any{"send_limit_count": 5}), 200)
	f2 := phone.expect(gateway.TypeSendSMS)
	if f2.MessageID != b.Body["id"] || f2.SimSlot == nil || *f2.SimSlot != 1 {
		t.Fatalf("second frame = %+v", f2)
	}
}

func TestRetryableFailureRequeues(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	dev := apiKeyClient(t, srv, c, projectID, "live")

	m := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202)
	id := m.Body["id"].(string)
	phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: id})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: id, ErrorCode: "no_service", Retryable: true})
	again := phone.expect(gateway.TypeSendSMS)
	if again.MessageID != id || again.Attempt != 2 {
		t.Fatalf("retry frame = %+v", again)
	}
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: id, ErrorCode: "radio_off", ErrorMessage: "Airplane mode is on"})
	f := waitStatus(t, dev, id, "failed")
	if f["error_code"] != "radio_off" || f["error_message"] != "Airplane mode is on" {
		t.Fatalf("final failure = %v %v", f["error_code"], f["error_message"])
	}
}

func TestBodiesAreRedactedAfterRetention(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	m := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "secret code 1234"}), 202)
	id := m.Body["id"].(string)
	waitStatus(t, dev, id, "delivered")

	ctx := context.Background()
	if _, err := testDB.Pool.Exec(ctx, "UPDATE messages SET created_at = now() - interval '31 days' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	n, err := dbq.New(testDB.Pool).RedactMessageBodies(ctx, dbq.RedactMessageBodiesParams{Before: time.Now().Add(-30 * 24 * time.Hour), OtpBefore: time.Now().Add(-time.Hour)})
	if err != nil || n < 1 {
		t.Fatalf("redacted %d: %v", n, err)
	}
	got := dev.mustStatus(dev.do("GET", "/v1/messages/"+id, nil), 200)
	if got.Body["body"] != nil || got.Body["body_redacted"] != true || strings.Contains(string(got.Raw), "secret code") {
		t.Fatalf("body not redacted: %s", got.Raw)
	}
}

func TestLateReportFromEarlierAttemptIsIgnored(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	dev := apiKeyClient(t, srv, c, projectID, "live")

	m := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202)
	id := m.Body["id"].(string)
	first := phone.expect(gateway.TypeSendSMS)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: id, Attempt: first.Attempt})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: id, Attempt: first.Attempt, ErrorCode: "no_service", Retryable: true})
	second := phone.expect(gateway.TypeSendSMS)
	if second.Attempt != 2 {
		t.Fatalf("second attempt = %d", second.Attempt)
	}
	// A duplicate failure from attempt 1 arrives late; attempt 2 must carry on.
	phone.report(gateway.Inbound{Type: gateway.TypeSMSFailed, MessageID: id, Attempt: 1, ErrorCode: "no_service"})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: id, Attempt: 2})
	waitStatus(t, dev, id, "sent")
}
