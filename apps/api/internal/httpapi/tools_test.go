package httpapi_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"bridge/internal/config"
	"bridge/internal/gateway"
	"bridge/internal/schedule"
	"bridge/internal/webhook"
)

// waitUntil polls cond for up to 20 seconds.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func counts(b map[string]any) map[string]any { return b["counts"].(map[string]any) }

func num(v any) int {
	f, _ := v.(float64)
	return int(f)
}

// messagesWith lists the project's messages whose metadata has key = value.
func messagesWith(t *testing.T, c *client, path, key, value string) []map[string]any {
	t.Helper()
	r := c.mustStatus(c.do("GET", path, nil), 200)
	var out []map[string]any
	for _, m := range r.Body["data"].([]any) {
		msg := m.(map[string]any)
		if meta, _ := msg["metadata"].(map[string]any); meta[key] == value {
			out = append(out, msg)
		}
	}
	return out
}

func TestBroadcasts(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	recv := newReceiver(t)
	ep := createWebhook(t, c, projectID, map[string]any{"url": recv.URL, "events": []string{"broadcast.completed"}})

	dev.mustStatus(dev.do("POST", "/v1/opt-outs", map[string]any{"number": "+91 98000 00003"}), 201)
	dev.mustStatus(dev.do("POST", "/v1/opt-outs", map[string]any{"number": "+919800000003"}), 200) // already there

	body := map[string]any{
		"name":     "Shipping",
		"template": "Hi {name}, order {order} has shipped.",
		"recipients": []map[string]any{
			{"to": "+91 98000 00001", "vars": map[string]string{"name": "Ada", "order": "#1"}},
			{"to": "+919800000002", "vars": map[string]string{"name": "Grace", "order": "#2"}},
			{"to": "+919800000001", "vars": map[string]string{"name": "Ada again", "order": "#3"}}, // duplicate
			{"to": "+919800000003", "vars": map[string]string{"name": "Opted", "order": "#4"}},     // opted out
		},
		"dry_run": true,
	}
	preview := dev.mustStatus(dev.do("POST", "/v1/broadcasts", body), 200).Body
	if preview["dry_run"] != true || num(preview["recipients"]) != 2 || num(preview["duplicates"]) != 1 ||
		num(preview["skipped_opted_out"]) != 1 || num(preview["total_segments"]) != 2 {
		t.Fatalf("preview: %v", preview)
	}
	samples := preview["samples"].([]any)
	if len(samples) != 2 || samples[0].(map[string]any)["text"] != "Hi Ada, order #1 has shipped." || samples[0].(map[string]any)["to"] != "+919800000001" {
		t.Fatalf("samples: %v", samples)
	}
	if list := dev.mustStatus(dev.do("GET", "/v1/broadcasts", nil), 200).Body["data"].([]any); len(list) != 0 {
		t.Fatalf("a dry run created a broadcast: %v", list)
	}

	// Every placeholder must be filled for every row; the error names the first bad row.
	bad := map[string]any{"template": "Hi {name}", "recipients": []map[string]any{
		{"to": "+919800000001", "vars": map[string]string{"name": "Ada"}},
		{"to": "+919800000002", "vars": map[string]string{"nickname": "G"}},
	}}
	r := dev.do("POST", "/v1/broadcasts", bad)
	if r.Status != 422 || !strings.Contains(string(r.Raw), "Row 2") || !strings.Contains(string(r.Raw), "body.recipients[1].vars") {
		t.Fatalf("missing var: %d %s", r.Status, r.Raw)
	}
	r = dev.do("POST", "/v1/broadcasts", map[string]any{"template": "Hi", "recipients": []map[string]any{{"to": "12345"}}})
	if r.Status != 422 || !strings.Contains(string(r.Raw), "body.recipients[0].to") {
		t.Fatalf("bad number: %d %s", r.Status, r.Raw)
	}

	delete(body, "dry_run")
	created := dev.mustStatus(dev.do("POST", "/v1/broadcasts", body), 201).Body
	id := created["id"].(string)
	if created["status"] != "sending" || num(counts(created)["recipients"]) != 2 || num(counts(created)["skipped"]) != 1 ||
		num(counts(created)["duplicates"]) != 1 || created["environment"] != "test" {
		t.Fatalf("created: %v", created)
	}

	var final map[string]any
	waitUntil(t, "the broadcast to complete", func() bool {
		final = dev.mustStatus(dev.do("GET", "/v1/broadcasts/"+id, nil), 200).Body
		return final["status"] == "completed"
	})
	waitUntil(t, "delivery reports", func() bool {
		final = dev.mustStatus(dev.do("GET", "/v1/broadcasts/"+id, nil), 200).Body
		return num(counts(final)["delivered"]) == 2
	})
	if num(counts(final)["queued"]) != 0 || num(counts(final)["failed"]) != 0 || final["completed_at"] == nil {
		t.Fatalf("final: %v", final)
	}
	msgs := messagesWith(t, dev, "/v1/messages", "broadcast_id", id)
	if len(msgs) != 2 {
		t.Fatalf("broadcast messages: %v", msgs)
	}
	for _, m := range msgs {
		if m["to"] == "+919800000001" && m["body"] != "Hi Ada, order #1 has shipped." {
			t.Fatalf("rendered body: %v", m)
		}
	}
	hook := recv.wait("broadcast.completed")
	verify(t, ep["secret"].(string), hook)
	if hook.Data["id"] != id || hook.Data["status"] != "completed" {
		t.Fatalf("broadcast.completed: %v", hook.Data)
	}

	// A scheduled broadcast waits, and can be canceled before it starts.
	later := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	scheduled := dev.mustStatus(dev.do("POST", "/v1/broadcasts", map[string]any{
		"template": "Later", "recipients": []map[string]any{{"to": "+919800000011"}, {"to": "+919800000012"}}, "scheduled_at": later,
	}), 201).Body
	if scheduled["status"] != "scheduled" || scheduled["scheduled_at"] == nil {
		t.Fatalf("scheduled: %v", scheduled)
	}
	canceled := dev.mustStatus(dev.do("POST", "/v1/broadcasts/"+scheduled["id"].(string)+"/cancel", nil), 200).Body
	if canceled["status"] != "canceled" || num(counts(canceled)["canceled"]) != 2 || num(counts(canceled)["queued"]) != 0 {
		t.Fatalf("canceled: %v", canceled)
	}
	if r := dev.do("POST", "/v1/broadcasts/"+scheduled["id"].(string)+"/cancel", nil); r.Status != 409 {
		t.Fatalf("second cancel: %d %s", r.Status, r.Raw)
	}
	if r := dev.do("GET", "/v1/broadcasts/brd_01ja8z3k5wq2v7c9e4r2n0w6yb", nil); r.Status != 404 {
		t.Fatalf("unknown broadcast: %d", r.Status)
	}
	if list := dev.mustStatus(dev.do("GET", "/v1/broadcasts?status=canceled", nil), 200).Body["data"].([]any); len(list) != 1 {
		t.Fatalf("filtered list: %v", list)
	}
}

func TestBroadcastCancelStopsWaitingMessages(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	// A paired phone that is offline: live messages wait in the queue for it.
	pairNewDevice(t, srv, c, projectID)
	base := "/v1/projects/" + projectID + "/broadcasts"

	b := c.mustStatus(c.do("POST", base+"?environment=live", map[string]any{
		"template":   "Sale starts now",
		"recipients": []map[string]any{{"to": "+919800000021"}, {"to": "+919800000022"}, {"to": "+919800000023"}},
	}), 201).Body
	id := b["id"].(string)
	waitUntil(t, "the broadcast's messages", func() bool {
		return len(messagesWith(t, c, "/v1/projects/"+projectID+"/messages?environment=live", "broadcast_id", id)) == 3
	})
	got := c.mustStatus(c.do("POST", base+"/"+id+"/cancel", nil), 200).Body
	if got["status"] != "canceled" || num(counts(got)["canceled"]) != 3 || num(counts(got)["queued"]) != 0 {
		t.Fatalf("after cancel: %v", got)
	}
	for _, m := range messagesWith(t, c, "/v1/projects/"+projectID+"/messages?environment=live", "broadcast_id", id) {
		if m["status"] != "failed" || m["error_code"] != "canceled" {
			t.Fatalf("message after cancel: %v", m)
		}
	}
	audit := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/broadcasts?environment=live", nil), 200).Body["data"].([]any)
	if len(audit) != 1 {
		t.Fatalf("list: %v", audit)
	}
}

func TestScheduledMessages(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	ctx := context.Background()

	// Validation.
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(time.DateOnly)
	for _, tc := range []struct {
		schedule map[string]any
		field    string
	}{
		{map[string]any{"kind": "once", "at": "09:00", "date": yesterday, "time_zone": "UTC"}, "body.schedule.date"},
		{map[string]any{"kind": "weekly", "at": "09:00", "time_zone": "UTC"}, "body.schedule.days"},
		{map[string]any{"kind": "daily", "at": "09:00", "time_zone": "Mars/Base"}, "body.schedule.time_zone"},
		{map[string]any{"kind": "monthly", "at": "09:00", "time_zone": "UTC"}, "body.schedule.day_of_month"},
	} {
		r := dev.do("POST", "/v1/schedules", map[string]any{"to": "+919800000031", "message": "x", "schedule": tc.schedule})
		if r.Status != 422 || !strings.Contains(string(r.Raw), tc.field) {
			t.Errorf("%v: %d %s", tc.schedule, r.Status, r.Raw)
		}
	}

	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly)
	once := dev.mustStatus(dev.do("POST", "/v1/schedules", map[string]any{
		"name": "Reminder", "to": "+919800000031", "message": "Your appointment is tomorrow.",
		"schedule": map[string]any{"kind": "once", "at": "09:00", "date": tomorrow, "time_zone": "UTC"},
	}), 201).Body
	if once["status"] != "active" || once["next_run_at"] != tomorrow+"T09:00:00Z" || once["description"] != "Once on "+tomorrow+" at 09:00 (UTC)" {
		t.Fatalf("once: %v", once)
	}

	// A daily schedule two hours from now in Kolkata, so the next run is unambiguous.
	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	at := time.Now().In(kolkata).Add(2 * time.Hour).Format("15:04")
	daily := dev.mustStatus(dev.do("POST", "/v1/schedules", map[string]any{
		"to": "+919800000032", "message": "Daily digest",
		"schedule": map[string]any{"kind": "daily", "at": at, "time_zone": "Asia/Kolkata"},
	}), 201).Body
	spec := schedule.Spec{Kind: "daily", At: at, TimeZone: "Asia/Kolkata"}
	loc, _ := spec.Normalize()

	// Make both due now instead of waiting; the worker claims them within a tick.
	if _, err := testDB.Pool.Exec(ctx, `UPDATE scheduled_messages SET next_run_at = now() - interval '1 second' WHERE id = ANY($1)`,
		[]string{once["id"].(string), daily["id"].(string)}); err != nil {
		t.Fatal(err)
	}
	var gotOnce, gotDaily map[string]any
	waitUntil(t, "both schedules to run", func() bool {
		gotOnce = dev.mustStatus(dev.do("GET", "/v1/schedules/"+once["id"].(string), nil), 200).Body
		gotDaily = dev.mustStatus(dev.do("GET", "/v1/schedules/"+daily["id"].(string), nil), 200).Body
		return gotOnce["last_message_id"] != nil && gotDaily["last_message_id"] != nil
	})
	if gotOnce["status"] != "completed" || gotOnce["next_run_at"] != nil || num(gotOnce["run_count"]) != 1 {
		t.Fatalf("once after running: %v", gotOnce)
	}
	wantNext, _ := spec.Next(time.Now(), loc)
	if gotDaily["status"] != "active" || gotDaily["next_run_at"] != wantNext.UTC().Format(time.RFC3339) || num(gotDaily["run_count"]) != 1 {
		t.Fatalf("daily after running: %v (want next %s)", gotDaily, wantNext.UTC())
	}
	m := waitStatus(t, dev, gotDaily["last_message_id"].(string), "delivered")
	if m["body"] != "Daily digest" || m["metadata"].(map[string]any)["schedule_id"] != daily["id"] {
		t.Fatalf("scheduled message: %v", m)
	}

	// Run now sends at once and keeps the timetable.
	dailyID := daily["id"].(string)
	ran := dev.mustStatus(dev.do("POST", "/v1/schedules/"+dailyID+"/run", nil), 200).Body
	if ran["next_run_at"] != gotDaily["next_run_at"] || num(ran["run_count"]) != 2 {
		t.Fatalf("run now: %v", ran)
	}
	waitUntil(t, "the manual run's message", func() bool {
		cur := dev.mustStatus(dev.do("GET", "/v1/schedules/"+dailyID, nil), 200).Body
		return cur["last_message_id"] != gotDaily["last_message_id"]
	})

	// Pause, edit, resume.
	paused := dev.mustStatus(dev.do("POST", "/v1/schedules/"+dailyID+"/pause", nil), 200).Body
	if paused["status"] != "paused" {
		t.Fatalf("paused: %v", paused)
	}
	weekly := dev.mustStatus(dev.do("PATCH", "/v1/schedules/"+dailyID, map[string]any{
		"message":  "Weekly digest",
		"schedule": map[string]any{"kind": "weekly", "at": "07:15", "days": []string{"sat", "mon"}, "time_zone": "Europe/London"},
	}), 200).Body
	if weekly["message"] != "Weekly digest" || weekly["status"] != "paused" || weekly["description"] != "Every Mon, Sat at 07:15 (Europe/London)" {
		t.Fatalf("patched: %v", weekly)
	}
	resumed := dev.mustStatus(dev.do("POST", "/v1/schedules/"+dailyID+"/resume", nil), 200).Body
	wspec := schedule.Spec{Kind: "weekly", At: "07:15", Days: []string{"mon", "sat"}, TimeZone: "Europe/London"}
	wloc, _ := wspec.Normalize()
	wantWeekly, _ := wspec.Next(time.Now(), wloc)
	if resumed["status"] != "active" || resumed["next_run_at"] != wantWeekly.UTC().Format(time.RFC3339) {
		t.Fatalf("resumed: %v (want %s)", resumed, wantWeekly.UTC())
	}

	// A run to an opted-out number sends nothing and says why.
	dev.mustStatus(dev.do("POST", "/v1/opt-outs", map[string]any{"number": "+919800000033"}), 201)
	blocked := dev.mustStatus(dev.do("POST", "/v1/schedules", map[string]any{
		"to": "+919800000033", "message": "Hello", "schedule": map[string]any{"kind": "daily", "at": "10:00", "time_zone": "UTC"},
	}), 201).Body
	dev.mustStatus(dev.do("POST", "/v1/schedules/"+blocked["id"].(string)+"/run", nil), 200)
	waitUntil(t, "the refusal to be recorded", func() bool {
		cur := dev.mustStatus(dev.do("GET", "/v1/schedules/"+blocked["id"].(string), nil), 200).Body
		e, _ := cur["last_error"].(string)
		return strings.Contains(e, "opted out")
	})

	// The dashboard sees the test schedules and can delete them.
	list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/schedules?environment=test", nil), 200).Body["data"].([]any)
	if len(list) != 3 {
		t.Fatalf("dashboard list: %d", len(list))
	}
	c.mustStatus(c.do("DELETE", "/v1/projects/"+projectID+"/schedules/"+blocked["id"].(string), nil), 204)
	if r := dev.do("GET", "/v1/schedules/"+blocked["id"].(string), nil); r.Status != 404 {
		t.Fatalf("deleted schedule: %d", r.Status)
	}
	// A live key does not see test schedules.
	live := apiKeyClient(t, srv, c, projectID, "live")
	if r := live.do("GET", "/v1/schedules/"+dailyID, nil); r.Status != 404 {
		t.Fatalf("live key read a test schedule: %d", r.Status)
	}
}

// take expects the next send_sms frame and reports it sent.
func (p *fakePhone) take() gateway.Outbound {
	p.t.Helper()
	f := p.expect(gateway.TypeSendSMS)
	p.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: f.MessageID, Attempt: f.Attempt})
	p.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: f.MessageID, Attempt: f.Attempt})
	return f
}

// receive delivers an incoming SMS from the phone and returns its message ID.
func (p *fakePhone) receive(t *testing.T, live *client, inboundID, from, body string) string {
	t.Helper()
	p.send(gateway.Inbound{Type: gateway.TypeSMSReceived, InboundID: inboundID, From: from, Body: body, ReceivedAt: time.Now().UnixMilli()})
	if ack := p.expect(gateway.TypeReportAck); ack.MessageID != inboundID {
		t.Fatalf("ack %+v", ack)
	}
	var id string
	waitUntil(t, "the incoming SMS", func() bool {
		r := live.mustStatus(live.do("GET", "/v1/messages?direction=inbound&from="+url.QueryEscape(from), nil), 200)
		for _, m := range r.Body["data"].([]any) {
			if m.(map[string]any)["body"] == body {
				id = m.(map[string]any)["id"].(string)
				return true
			}
		}
		return false
	})
	return id
}

func enableForwarding(t *testing.T, c *client, projectID string, phone *fakePhone) {
	t.Helper()
	c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID+"/devices/"+phone.ID, map[string]any{"forward_inbound": true}), 200)
	phone.expect(gateway.TypeConfig)
}

// autoReplyEvent waits for the auto_reply timeline event of an incoming SMS.
func autoReplyEvent(t *testing.T, live *client, messageID string) map[string]any {
	t.Helper()
	var detail map[string]any
	waitUntil(t, "the auto_reply event", func() bool {
		r := live.mustStatus(live.do("GET", "/v1/messages/"+messageID, nil), 200)
		for _, e := range r.Body["events"].([]any) {
			if ev := e.(map[string]any); ev["type"] == "auto_reply" {
				detail = ev["detail"].(map[string]any)
				return true
			}
		}
		return false
	})
	return detail
}

func TestOptOutKeywordsAndAutoReplies(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	enableForwarding(t, c, projectID, phone)
	live := apiKeyClient(t, srv, c, projectID, "live")
	const person = "+919811111111"

	stop := phone.receive(t, live, "in-stop-1", person, " Stop ")
	reply := phone.take()
	if reply.To != person || reply.Body != "You are unsubscribed. Reply START to subscribe again." {
		t.Fatalf("STOP reply frame: %+v", reply)
	}
	ev := autoReplyEvent(t, live, stop)
	if ev["reply_message_id"] != reply.MessageID || ev["action"] != "opt_out" || ev["rule_id"] == nil {
		t.Fatalf("auto_reply event: %v", ev)
	}
	o := live.mustStatus(live.do("GET", "/v1/opt-outs/"+url.PathEscape(person), nil), 200).Body
	if o["source"] != "keyword" || o["keyword"] != "STOP" || o["number"] != person {
		t.Fatalf("opt-out: %v", o)
	}
	// "+" in a path also works unescaped.
	live.mustStatus(live.do("GET", "/v1/opt-outs/"+person, nil), 200)

	// Ordinary messages are refused; one-time passwords still go.
	r := live.do("POST", "/v1/messages", map[string]any{"to": person, "message": "Big sale!"})
	if r.Status != 409 || r.errCode() != "opted_out" {
		t.Fatalf("send to opted-out number: %d %s", r.Status, r.Raw)
	}
	live.mustStatus(live.do("POST", "/v1/otp", map[string]any{"to": person}), 201)
	phone.take()

	// Loop protection: the same rule answers a number once per 10 minutes.
	again := phone.receive(t, live, "in-stop-2", person, "STOP")
	if ev := autoReplyEvent(t, live, again); ev["reply_skipped"] != "loop_protection" || ev["reply_message_id"] != nil {
		t.Fatalf("second STOP: %v", ev)
	}

	// START opts back in, with its own reply.
	start := phone.receive(t, live, "in-start", person, "start")
	if f := phone.take(); f.Body != "You are subscribed again." || f.To != person {
		t.Fatalf("START reply: %+v", f)
	}
	if ev := autoReplyEvent(t, live, start); ev["action"] != "opt_in" {
		t.Fatalf("START event: %v", ev)
	}
	if r := live.do("GET", "/v1/opt-outs/"+url.PathEscape(person), nil); r.Status != 404 {
		t.Fatalf("still opted out: %d", r.Status)
	}
	live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": person, "message": "Welcome back"}), 202)
	phone.take()

	// Sender IDs and short codes are never answered or opted out.
	for i, sender := range []string{"AX-SHOPCO", "57575"} {
		id := phone.receive(t, live, fmt.Sprintf("in-alpha-%d", i), sender, "STOP")
		if ev := autoReplyEvent(t, live, id); ev["skipped"] != "sender_not_a_phone_number" {
			t.Fatalf("%s: %v", sender, ev)
		}
	}
	optOuts := live.mustStatus(live.do("GET", "/v1/opt-outs", nil), 200).Body["data"].([]any)
	if len(optOuts) != 0 {
		t.Fatalf("opt-outs: %v", optOuts)
	}

	// The rules: three defaults, editable; a custom rule answers keywords.
	base := "/v1/projects/" + projectID + "/auto-replies"
	rules := c.mustStatus(c.do("GET", base, nil), 200).Body["data"].([]any)
	if len(rules) != 3 || rules[0].(map[string]any)["name"] != "Unsubscribe" || rules[2].(map[string]any)["keywords"].([]any)[0] != "HELP" {
		t.Fatalf("default rules: %v", rules)
	}
	if r := c.do("POST", base, map[string]any{"name": "Nothing", "keywords": []string{"x"}}); r.Status != 422 {
		t.Fatalf("rule without reply or action: %d %s", r.Status, r.Raw)
	}
	hours := c.mustStatus(c.do("POST", base, map[string]any{
		"name": "Hours", "match": "contains", "keywords": []string{"open"}, "reply": "We are open 9 to 5.", "priority": 50,
	}), 201).Body
	phone.receive(t, live, "in-hours", "+919811111112", "When are you OPEN today?")
	if f := phone.take(); f.Body != "We are open 9 to 5." {
		t.Fatalf("custom reply: %+v", f)
	}
	c.mustStatus(c.do("PATCH", base+"/"+hours["id"].(string), map[string]any{"enabled": false}), 200)
	c.mustStatus(c.do("DELETE", base+"/"+hours["id"].(string), nil), 204)
	if n := len(c.mustStatus(c.do("GET", base, nil), 200).Body["data"].([]any)); n != 3 {
		t.Fatalf("rules after delete: %d", n)
	}

	// Dashboard opt-outs are audited and can be exported.
	opts := "/v1/projects/" + projectID + "/opt-outs"
	c.mustStatus(c.do("POST", opts, map[string]any{"number": "+919811111113"}), 201)
	exp := c.mustStatus(c.do("GET", opts+"/export", nil), 200)
	if !strings.Contains(string(exp.Raw), "+919811111113,manual,,") || !strings.HasPrefix(exp.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("export: %s", exp.Raw)
	}
	c.mustStatus(c.do("DELETE", opts+"/"+url.PathEscape("+919811111113"), nil), 204)
	audit := c.mustStatus(c.do("GET", "/v1/organizations/"+orgOf(t, c, projectID)+"/audit-logs", nil), 200).Raw
	for _, action := range []string{"opt_out.added", "opt_out.removed", "auto_reply.created", "auto_reply.deleted"} {
		if !strings.Contains(string(audit), action) {
			t.Errorf("audit log lacks %s", action)
		}
	}
}

func orgOf(t *testing.T, c *client, projectID string) string {
	t.Helper()
	return c.mustStatus(c.do("GET", "/v1/projects/"+projectID, nil), 200).Body["organization_id"].(string)
}

// fakeSMTP is a minimal SMTP server that records messages.
type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	auth []string
	mail []string
	got  chan string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, got: make(chan string, 10)}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	say("220 fake.smtp ESMTP")
	var envelope []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		switch upper := strings.ToUpper(cmd); {
		case strings.HasPrefix(upper, "EHLO"):
			say("250-fake.smtp")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd[len("AUTH PLAIN"):]))
			s.mu.Lock()
			s.auth = append(s.auth, string(raw))
			s.mu.Unlock()
			say("235 ok")
		case strings.HasPrefix(upper, "MAIL FROM:"), strings.HasPrefix(upper, "RCPT TO:"):
			envelope = append(envelope, cmd)
			say("250 ok")
		case upper == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.mail = append(s.mail, strings.Join(envelope, "\n"))
			s.mu.Unlock()
			s.got <- b.String()
			say("250 queued")
		case upper == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func TestForwardingRules(t *testing.T) {
	type tgCall struct {
		Path string
		Body map[string]any
	}
	tgCalls := make(chan tgCall, 10)
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tgCalls <- tgCall{Path: r.URL.Path, Body: body}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer tg.Close()
	smtpSrv := newFakeSMTP(t)
	recv := newReceiver(t)

	srv := newServer(t, func(cfg *config.Config) {
		cfg.TelegramAPIURL = tg.URL
		cfg.WebhookAllowPrivate = true // the receiver listens on localhost
		cfg.SMTP = &config.SMTPConfig{Host: "127.0.0.1", Port: smtpSrv.port(), Username: "bridge", Password: "s3cret",
			From: "Bridge <sms@example.com>", TLS: config.SMTPNone}
	})
	c, _, projectID := signup(t, srv)
	phone := connectPhone(t, srv, c, projectID)
	enableForwarding(t, c, projectID, phone)
	live := apiKeyClient(t, srv, c, projectID, "live")
	base := "/v1/projects/" + projectID + "/forwarding-rules"
	const token = "123456789:AAFakeTokenForTestsOnly_abcdefghijk"

	if r := c.do("POST", base, map[string]any{"name": "x", "destinations": []map[string]any{{"type": "telegram", "chat_id": "-100123"}}}); r.Status != 422 {
		t.Fatalf("telegram without token: %d %s", r.Status, r.Raw)
	}
	rule := c.mustStatus(c.do("POST", base, map[string]any{
		"name": "Everything",
		"destinations": []map[string]any{
			{"type": "phone", "to": "+919822222222"},
			{"type": "telegram", "chat_id": "-1001234567890", "bot_token": token},
			{"type": "webhook", "url": recv.URL + "/slack", "format": "slack"},
			{"type": "email", "to": "Alerts <alerts@example.com>"},
		},
	}), 201).Body
	secret := rule["signing_secret"].(string)
	if !strings.HasPrefix(secret, "whsec_") || strings.Contains(string(c.do("GET", base, nil).Raw), token) {
		t.Fatalf("created rule: %v", rule)
	}
	dests := rule["destinations"].([]any)
	if len(dests) != 4 || dests[1].(map[string]any)["bot_token_set"] != true || dests[3].(map[string]any)["to"] != "alerts@example.com" {
		t.Fatalf("destinations: %v", dests)
	}
	// A rule that does not match this sender.
	c.mustStatus(c.do("POST", base, map[string]any{
		"name": "Banks only", "match": map[string]any{"senders": []string{"AX-*"}},
		"destinations": []map[string]any{{"type": "webhook", "url": recv.URL + "/banks"}},
	}), 201)
	list := c.mustStatus(c.do("GET", base, nil), 200).Body
	if list["email_available"] != true || list["telegram_available"] != true || len(list["data"].([]any)) != 2 {
		t.Fatalf("list: %v", list)
	}

	const sender, text = "+919833333333", "Your parcel arrived"
	inbound := phone.receive(t, live, "in-fwd-1", sender, text)

	fwd := phone.take()
	if fwd.To != "+919822222222" || fwd.Body != "From "+sender+": "+text {
		t.Fatalf("forwarded SMS: %+v", fwd)
	}
	select {
	case call := <-tgCalls:
		if call.Path != "/bot"+token+"/sendMessage" || call.Body["chat_id"] != "-1001234567890" ||
			!strings.Contains(call.Body["text"].(string), "SMS from "+sender) || !strings.Contains(call.Body["text"].(string), text) {
			t.Fatalf("telegram call: %+v", call)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("telegram was not called")
	}
	select {
	case d := <-recv.got:
		verify(t, secret, d)
		var slack map[string]string
		_ = json.Unmarshal(d.Body, &slack)
		if !strings.Contains(slack["text"], text) || d.Header.Get(webhook.HeaderID) == "" {
			t.Fatalf("slack webhook: %s", d.Body)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the webhook was not called")
	}
	select {
	case mail := <-smtpSrv.got:
		if !strings.Contains(mail, "Subject: SMS from "+sender) || !strings.Contains(mail, text) || !strings.Contains(mail, "To: alerts@example.com") {
			t.Fatalf("email: %s", mail)
		}
		smtpSrv.mu.Lock()
		auth, env := smtpSrv.auth, smtpSrv.mail
		smtpSrv.mu.Unlock()
		if len(auth) != 1 || auth[0] != "\x00bridge\x00s3cret" || !strings.Contains(env[0], "<alerts@example.com>") {
			t.Fatalf("smtp auth %q envelope %q", auth, env)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no email arrived")
	}

	ruleID := rule["id"].(string)
	var deliveries []any
	waitUntil(t, "every delivery to succeed", func() bool {
		deliveries = c.mustStatus(c.do("GET", base+"/"+ruleID+"/deliveries", nil), 200).Body["data"].([]any)
		if len(deliveries) != 4 {
			return false
		}
		for _, d := range deliveries {
			if d.(map[string]any)["status"] != "succeeded" {
				return false
			}
		}
		return true
	})
	for _, d := range deliveries {
		dd := d.(map[string]any)
		if dd["message_id"] != inbound || (dd["destination_type"] == "phone" && dd["forwarded_message_id"] != fwd.MessageID) {
			t.Fatalf("delivery: %v", dd)
		}
	}
	fm := live.mustStatus(live.do("GET", "/v1/messages/"+fwd.MessageID, nil), 200).Body
	if fm["metadata"].(map[string]any)["forwarded_from"] != inbound {
		t.Fatalf("forwarded message metadata: %v", fm["metadata"])
	}

	// The forward arriving back at one of the project's phones is not forwarded again.
	echo := phone.receive(t, live, "in-fwd-echo", "+919822222222", "From "+sender+": "+text)
	waitUntil(t, "the echo to be recognised", func() bool {
		r := live.mustStatus(live.do("GET", "/v1/messages/"+echo, nil), 200)
		return strings.Contains(string(r.Raw), "forwarded_by_bridge")
	})
	if n := len(c.mustStatus(c.do("GET", base+"/"+ruleID+"/deliveries", nil), 200).Body["data"].([]any)); n != 4 {
		t.Fatalf("the echo was forwarded: %d deliveries", n)
	}

	// Updating keeps a destination's stored token when its ID is given without one.
	tgID := dests[1].(map[string]any)["id"].(string)
	updated := c.mustStatus(c.do("PATCH", base+"/"+ruleID, map[string]any{
		"destinations": []map[string]any{{"id": tgID, "type": "telegram", "chat_id": "@bridgealerts"}},
	}), 200).Body
	ud := updated["destinations"].([]any)
	if len(ud) != 1 || ud[0].(map[string]any)["id"] != tgID || ud[0].(map[string]any)["bot_token_set"] != true || ud[0].(map[string]any)["chat_id"] != "@bridgealerts" {
		t.Fatalf("updated destinations: %v", ud)
	}
	c.mustStatus(c.do("GET", base+"/"+ruleID+"/secret", nil), 200)
	c.mustStatus(c.do("DELETE", base+"/"+ruleID, nil), 204)
}

func TestForwardingEmailNeedsSMTP(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	r := c.do("POST", "/v1/projects/"+projectID+"/forwarding-rules", map[string]any{
		"name": "Mail", "destinations": []map[string]any{{"type": "email", "to": "ops@example.com"}},
	})
	if r.Status != 409 || !strings.Contains(r.errMessage(), "BRIDGE_SMTP_HOST") {
		t.Fatalf("email without SMTP: %d %s", r.Status, r.Raw)
	}
	if list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/forwarding-rules", nil), 200).Body; list["email_available"] != false {
		t.Fatalf("list: %v", list)
	}
}
