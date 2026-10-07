package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bridge/internal/gateway"
)

type sseEvent struct {
	ID, Type string
	Data     map[string]any
}

// openStream connects to the event stream and returns a channel of events.
func openStream(t *testing.T, srv *httptest.Server, key, query string) <-chan sseEvent {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/v1/events/stream"+query, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	out := make(chan sseEvent, 64)
	connected := make(chan struct{})
	go func() {
		defer resp.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		var ev sseEvent
		once := false
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ": connected"):
				if !once {
					once = true
					close(connected)
				}
			case strings.HasPrefix(line, "id: "):
				ev.ID = line[4:]
			case strings.HasPrefix(line, "event: "):
				ev.Type = line[7:]
			case strings.HasPrefix(line, "data: "):
				var env struct {
					Type string         `json:"type"`
					Data map[string]any `json:"data"`
				}
				_ = json.Unmarshal([]byte(line[6:]), &env)
				ev.Data = env.Data
			case line == "" && ev.Type != "":
				out <- ev
				ev = sseEvent{}
			}
		}
	}()
	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not connect")
	}
	return out
}

func nextEvent(t *testing.T, c <-chan sseEvent, typ string) sseEvent {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-c:
			if !ok {
				t.Fatalf("stream closed waiting for %s", typ)
			}
			if ev.Type == typ {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %s event on the stream", typ)
		}
	}
}

func inboundFrame(id, from, body string) gateway.Inbound {
	return gateway.Inbound{Type: gateway.TypeSMSReceived, InboundID: id, From: from, Body: body, ReceivedAt: time.Now().UnixMilli()}
}

func TestEventStream(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	testKey := apiKeyClient(t, srv, c, projectID, "test")
	liveKey := apiKeyClient(t, srv, c, projectID, "live")

	all := openStream(t, srv, testKey.apiKey, "")
	onlyDelivered := openStream(t, srv, testKey.apiKey, "?types=message.delivered")
	live := openStream(t, srv, liveKey.apiKey, "")

	msg := testKey.mustStatus(testKey.do("POST", "/v1/messages", map[string]any{"to": "+15550000001", "message": "stream me"}), 202)
	id := msg.Body["id"].(string)

	sent := nextEvent(t, all, "message.sent")
	if sent.Data["id"] != id || !strings.HasPrefix(sent.ID, "evt_") {
		t.Fatalf("sent event: %+v", sent)
	}
	delivered := nextEvent(t, all, "message.delivered")
	if delivered.Data["status"] != "delivered" {
		t.Fatalf("delivered event: %+v", delivered)
	}
	if ev := nextEvent(t, onlyDelivered, "message.delivered"); ev.ID != delivered.ID {
		t.Fatal("filtered stream got a different event")
	}

	// A long incoming SMS is too big for NOTIFY; it is stored and still streamed.
	phone := connectPhone(t, srv, c, projectID)
	c.mustStatus(c.do("PATCH", "/v1/projects/"+projectID+"/devices/"+phone.ID, map[string]any{"forward_inbound": true}), 200)
	phone.expect("config")
	long := strings.Repeat("मेरा संदेश ", 450) // ~4,900 characters, ~13 KB of UTF-8
	phone.send(inboundFrame("in-long", "+919800000001", long))
	phone.expect("report_ack")
	got := nextEvent(t, live, "message.received")
	if body, _ := got.Data["body"].(string); body != long {
		t.Fatalf("long inbound body changed: %d runes", len([]rune(body)))
	}

	// The live stream never sees test messages.
	select {
	case ev := <-live:
		if strings.HasPrefix(ev.Type, "message.") && ev.Data["environment"] == "test" {
			t.Fatalf("live stream received a test event: %+v", ev)
		}
	case <-time.After(300 * time.Millisecond):
	}

	// Bad input.
	req, _ := http.NewRequest("GET", srv.URL+"/v1/events/stream?types=message.exploded", nil)
	req.Header.Set("Authorization", "Bearer "+testKey.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 422 {
		t.Fatalf("bad types: %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/v1/events/stream")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("no key: %d", resp.StatusCode)
	}
}
