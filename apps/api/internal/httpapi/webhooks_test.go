package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bridge/internal/gateway"
	"bridge/internal/webhook"
	"bridge/internal/worker"
)

type delivery struct {
	Header   http.Header
	Body     []byte
	Envelope webhook.Envelope
	Data     map[string]any
}

// receiver is a webhook endpoint that records every request.
type receiver struct {
	*httptest.Server
	t      *testing.T
	got    chan delivery
	status atomic.Int32
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{t: t, got: make(chan delivery, 100)}
	r.status.Store(200)
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		d := delivery{Header: req.Header.Clone(), Body: body}
		_ = json.Unmarshal(body, &d.Envelope)
		_ = json.Unmarshal(d.Envelope.Data, &d.Data)
		r.got <- d
		w.WriteHeader(int(r.status.Load()))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(r.Close)
	return r
}

// wait returns the next delivery of the given event type.
func (r *receiver) wait(typ string) delivery {
	r.t.Helper()
	timeout := time.After(25 * time.Second)
	for {
		select {
		case d := <-r.got:
			if d.Envelope.Type == typ {
				return d
			}
		case <-timeout:
			r.t.Fatalf("no %s webhook arrived", typ)
		}
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func verify(t *testing.T, secret string, d delivery) {
	t.Helper()
	if err := webhook.Verify(secret, d.Header, d.Body, webhook.DefaultTolerance, time.Now()); err != nil {
		t.Fatalf("signature does not verify: %v (headers %v)", err, d.Header)
	}
}

func createWebhook(t *testing.T, c *client, projectID string, body map[string]any) map[string]any {
	t.Helper()
	return c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/webhooks", body), 201).Body
}

func TestWebhookEndpointsAndSignedDelivery(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	recv := newReceiver(t)
	base := "/v1/projects/" + projectID + "/webhooks"

	// Validation.
	if r := c.do("POST", base, map[string]any{"url": "ftp://example.com/x"}); r.Status != 422 {
		t.Fatalf("ftp URL: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", base, map[string]any{"url": recv.URL, "events": []string{"message.exploded"}}); r.Status != 422 {
		t.Fatalf("unknown event: %d %s", r.Status, r.Raw)
	}

	ep := createWebhook(t, c, projectID, map[string]any{
		"url": recv.URL, "description": "Orders service", "events": []string{"message.delivered", "message.sent", "message.sent"},
	})
	secret := ep["secret"].(string)
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret %q", secret)
	}
	if got := ep["events"].([]any); len(got) != 2 {
		t.Fatalf("events not de-duplicated: %v", got)
	}
	epID := ep["id"].(string)
	list := c.mustStatus(c.do("GET", base, nil), 200)
	if strings.Contains(string(list.Raw), secret) {
		t.Fatal("list leaks the signing secret")
	}

	// A simulated message produces signed message.sent and message.delivered webhooks.
	dev := apiKeyClient(t, srv, c, projectID, "test")
	msg := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+15550000001", "message": "hello"}), 202).Body
	sent := recv.wait("message.sent")
	verify(t, secret, sent)
	delivered := recv.wait("message.delivered")
	verify(t, secret, delivered)
	if delivered.Data["id"] != msg["id"] || delivered.Data["status"] != "delivered" || delivered.Data["environment"] != "test" {
		t.Fatalf("unexpected payload: %s", delivered.Body)
	}
	if delivered.Header.Get("webhook-id") == sent.Header.Get("webhook-id") {
		t.Fatal("events share a webhook-id")
	}

	waitFor(t, "delivery log", func() bool {
		r := c.mustStatus(c.do("GET", base+"/"+epID+"/deliveries", nil), 200)
		var rows []map[string]any
		_ = json.Unmarshal(r.Raw, &rows)
		return len(rows) == 2 && rows[0]["succeeded"] == true && rows[0]["response_status"] == float64(200)
	})

	// Secret reveal and rotation.
	revealed := c.mustStatus(c.do("GET", base+"/"+epID+"/secret", nil), 200).Body["secret"]
	if revealed != secret {
		t.Fatal("revealed secret differs")
	}
	rotated := c.mustStatus(c.do("POST", base+"/"+epID+"/rotate-secret", nil), 200).Body["secret"].(string)
	if rotated == secret {
		t.Fatal("rotation kept the secret")
	}

	// Test events go to this endpoint whatever it subscribes to, signed with the new secret.
	c.mustStatus(c.do("POST", base+"/"+epID+"/test", nil), 202)
	verify(t, rotated, recv.wait(webhook.EventTest))

	// Another user cannot see or change it.
	other, _, _ := signup(t, srv)
	if r := other.do("GET", base+"/"+epID, nil); r.Status != 404 {
		t.Fatalf("other tenant: %d", r.Status)
	}

	updated := c.mustStatus(c.do("PATCH", base+"/"+epID, map[string]any{"events": []string{}, "description": "All events"}), 200).Body
	if len(updated["events"].([]any)) != 0 || updated["description"] != "All events" {
		t.Fatalf("update: %v", updated)
	}
	c.mustStatus(c.do("DELETE", base+"/"+epID, nil), 204)
	c.mustStatus(c.do("GET", base+"/"+epID, nil), 404)
}

func TestWebhookFailuresAreRetriedThenDisabled(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	recv := newReceiver(t)
	recv.status.Store(500)
	base := "/v1/projects/" + projectID + "/webhooks"
	ep := createWebhook(t, c, projectID, map[string]any{"url": recv.URL})
	epID := ep["id"].(string)

	c.mustStatus(c.do("POST", base+"/"+epID+"/test", nil), 202)
	first := recv.wait(webhook.EventTest)
	recv.status.Store(204)
	// The retry comes about 5 seconds later with the same webhook-id.
	retry := recv.wait(webhook.EventTest)
	if retry.Header.Get("webhook-id") != first.Header.Get("webhook-id") {
		t.Fatal("retry has a different webhook-id")
	}
	waitFor(t, "two logged attempts", func() bool {
		var rows []map[string]any
		_ = json.Unmarshal(c.mustStatus(c.do("GET", base+"/"+epID+"/deliveries", nil), 200).Raw, &rows)
		return len(rows) == 2 && rows[1]["response_status"] == float64(500) && rows[0]["succeeded"] == true && rows[0]["attempt"] == float64(2)
	})
	got := c.mustStatus(c.do("GET", base+"/"+epID, nil), 200).Body
	if got["failing_since"] != nil || got["last_failure_at"] == nil || got["last_success_at"] == nil {
		t.Fatalf("health after recovery: %v", got)
	}

	// An endpoint that has been failing for 5 days is disabled on its next failure.
	recv.status.Store(503)
	if _, err := testDB.Pool.Exec(context.Background(),
		`UPDATE webhook_endpoints SET failing_since = now() - interval '6 days' WHERE id = $1`, epID); err != nil {
		t.Fatal(err)
	}
	c.mustStatus(c.do("POST", base+"/"+epID+"/test", nil), 202)
	recv.wait(webhook.EventTest)
	waitFor(t, "endpoint disabled", func() bool {
		b := c.mustStatus(c.do("GET", base+"/"+epID, nil), 200).Body
		return b["enabled"] == false && strings.Contains(b["disabled_reason"].(string), "5 days")
	})
	if r := c.do("POST", base+"/"+epID+"/test", nil); r.Status != 409 {
		t.Fatalf("test on disabled endpoint: %d", r.Status)
	}
	enabled := c.mustStatus(c.do("PATCH", base+"/"+epID, map[string]any{"enabled": true}), 200).Body
	if enabled["enabled"] != true || enabled["disabled_reason"] != nil || enabled["failing_since"] != nil {
		t.Fatalf("re-enable: %v", enabled)
	}
}

func TestInboundSMSForwarding(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	recv := newReceiver(t)
	ep := createWebhook(t, c, projectID, map[string]any{"url": recv.URL, "events": []string{"message.received"}})
	phone := connectPhone(t, srv, c, projectID)
	live := apiKeyClient(t, srv, c, projectID, "live")
	inbound := func(id, from, body string) gateway.Inbound {
		slot := int16(2)
		return gateway.Inbound{Type: gateway.TypeSMSReceived, InboundID: id, From: from, Body: body, SimSlot: &slot, ReceivedAt: time.Now().UnixMilli()}
	}
	ackFor := func(in gateway.Inbound) {
		t.Helper()
		phone.send(in)
		ack := phone.expect(gateway.TypeReportAck)
		if ack.MessageID != in.InboundID || ack.Report != gateway.TypeSMSReceived {
			t.Fatalf("ack %+v", ack)
		}
	}
	countInbound := func() int {
		r := live.mustStatus(live.do("GET", "/v1/messages?direction=inbound", nil), 200)
		return len(r.Body["data"].([]any))
	}

	// Forwarding is off by default: the SMS is acknowledged and dropped.
	ackFor(inbound("in-off", "+919800000001", "dropped"))
	if n := countInbound(); n != 0 {
		t.Fatalf("stored %d messages with forwarding off", n)
	}

	// Turning it on tells the connected phone at once.
	d := c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID+"/devices/"+phone.ID, map[string]any{"forward_inbound": true}), 200).Body
	if d["forward_inbound"] != true {
		t.Fatalf("device: %v", d)
	}
	if cfg := phone.expect(gateway.TypeConfig); cfg.ForwardInbound == nil || !*cfg.ForwardInbound {
		t.Fatalf("config frame %+v", cfg)
	}

	in := inbound("in-1", "AX-HDFCBK", "Your OTP is 482913. Do not share it.")
	ackFor(in)
	ackFor(in) // a resend after a lost ack is not stored twice
	hook := recv.wait(webhook.EventMessageReceived)
	verify(t, ep["secret"].(string), hook)
	if hook.Data["from"] != "AX-HDFCBK" || hook.Data["direction"] != "inbound" || hook.Data["status"] != "received" ||
		hook.Data["body"] != in.Body || hook.Data["sim_slot"] != float64(2) || hook.Data["device_id"] != phone.ID {
		t.Fatalf("payload: %s", hook.Body)
	}
	if n := countInbound(); n != 1 {
		t.Fatalf("%d inbound messages, want 1", n)
	}
	msg := live.mustStatus(live.do("GET", "/v1/messages/"+hook.Data["id"].(string), nil), 200).Body
	if got := eventTypes(msg); len(got) != 1 || got[0] != "received" {
		t.Fatalf("timeline %v", got)
	}
	if r := live.mustStatus(live.do("GET", "/v1/messages?direction=inbound&from=AX-HDFCBK", nil), 200); len(r.Body["data"].([]any)) != 1 {
		t.Fatal("from filter")
	}
	if r := live.mustStatus(live.do("GET", "/v1/messages?direction=outbound", nil), 200); len(r.Body["data"].([]any)) != 0 {
		t.Fatal("direction filter")
	}

	// The setting survives a reconnect.
	_ = phone.ws.CloseNow()
	ws, _, err := dial(t, srv, phone.credential)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	welcome := readFrame(t, ws)
	if welcome.Type != gateway.TypeWelcome || welcome.ForwardInbound == nil || !*welcome.ForwardInbound {
		t.Fatalf("welcome %+v", welcome)
	}

	// The HTTP check-in reports it too, for the background sync.
	hb := deviceClient(t, srv, phone.credential).mustStatus(deviceClient(t, srv, phone.credential).do("POST", "/v1/device/heartbeat", map[string]any{"next_in": 60}), 200)
	if hb.Body["forward_inbound"] != true {
		t.Fatalf("heartbeat %v", hb.Body)
	}
}

func TestDevicePresenceWebhooks(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	recv := newReceiver(t)
	createWebhook(t, c, projectID, map[string]any{"url": recv.URL, "events": []string{"device.online", "device.offline"}})
	phone := connectPhone(t, srv, c, projectID)

	// Drive the periodic presence job directly instead of waiting for it.
	ctx := context.Background()
	jobs, err := worker.NewInsertOnlyClient(testDB.Pool, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	hooks := webhook.New(webhook.Options{Pool: testDB.Pool, Logger: quietLogger(), AllowPrivate: true})
	hooks.SetJobInserter(jobs)

	if err := hooks.EmitPresence(ctx); err != nil {
		t.Fatal(err)
	}
	if on := recv.wait(webhook.EventDeviceOnline); on.Data["id"] != phone.ID || on.Data["status"] != "online" {
		t.Fatalf("online payload %s", on.Body)
	}
	if err := hooks.EmitPresence(ctx); err != nil { // no change, no event
		t.Fatal(err)
	}

	_ = phone.ws.CloseNow()
	waitFor(t, "device offline", func() bool { return deviceStatus(t, c, projectID, phone.ID)["status"] == "offline" })
	if err := hooks.EmitPresence(ctx); err != nil {
		t.Fatal(err)
	}
	// Within the grace period a disconnect is not reported.
	select {
	case d := <-recv.got:
		t.Fatalf("%s sent during the offline grace period", d.Envelope.Type)
	case <-time.After(time.Second):
	}
	if _, err := testDB.Pool.Exec(ctx, `UPDATE devices SET last_seen_at = now() - interval '3 minutes' WHERE id = $1`, phone.ID); err != nil {
		t.Fatal(err)
	}
	if err := hooks.EmitPresence(ctx); err != nil {
		t.Fatal(err)
	}
	if off := recv.wait(webhook.EventDeviceOffline); off.Data["id"] != phone.ID {
		t.Fatalf("offline payload %s", off.Body)
	}
	select {
	case d := <-recv.got:
		t.Fatalf("unexpected extra event %s", d.Envelope.Type)
	case <-time.After(500 * time.Millisecond):
	}
}
