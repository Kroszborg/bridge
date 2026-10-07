package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bridge/internal/webhook"
)

func TestParseAllowsFlagsAnywhere(t *testing.T) {
	var wait bool
	var sim int
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.BoolVar(&wait, "wait", false, "")
	fs.IntVar(&sim, "sim", 0, "")
	pos := parse(fs, []string{"+919876543210", "--sim", "2", "Your", "order", "--wait", "shipped"})
	if strings.Join(pos, "|") != "+919876543210|Your|order|shipped" || !wait || sim != 2 {
		t.Fatalf("pos=%v wait=%v sim=%d", pos, wait, sim)
	}
}

func TestConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BRIDGE_CONFIG", filepath.Join(dir, "bridge", "config.json"))
	t.Setenv("BRIDGE_URL", "")
	t.Setenv("BRIDGE_API_KEY", "")

	r, err := resolve("", "")
	if err != nil || r.URL != "http://localhost:8080" || r.APIKey != "" {
		t.Fatalf("defaults: %+v %v", r, err)
	}
	path, err := saveFile(fileConfig{URL: "https://saved.example/", APIKey: "bk_test_saved"})
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("config readable by others: %v", fi.Mode())
	}
	r, _ = resolve("", "")
	if r.URL != "https://saved.example" || r.APIKey != "bk_test_saved" || r.Source != "config file" {
		t.Fatalf("file: %+v", r)
	}
	t.Setenv("BRIDGE_API_KEY", "bk_test_env")
	r, _ = resolve("", "")
	if r.APIKey != "bk_test_env" {
		t.Fatalf("env: %+v", r)
	}
	r, _ = resolve("http://flag:1", "bk_live_flag")
	if r.URL != "http://flag:1" || r.APIKey != "bk_live_flag" || r.Source != "--api-key" {
		t.Fatalf("flags: %+v", r)
	}
	if _, err := removeFile(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamParsesEventsAndAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bk_test_good" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key","message":"nope","request_id":"req_1"}}`)
			return
		}
		if r.URL.Query().Get("types") != "message.delivered,message.received" {
			t.Errorf("types = %q", r.URL.Query().Get("types"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "retry: 3000\n: connected\n\n: ping\n\n")
		_, _ = io.WriteString(w, `id: evt_1`+"\nevent: message.delivered\n"+`data: {"type":"message.delivered","timestamp":"2026-10-07T09:00:00Z","data":{"id":"msg_1","to":"+919876543210","direction":"outbound"}}`+"\n\n")
	}))
	defer srv.Close()

	c := &client{baseURL: srv.URL, apiKey: "bk_test_good", http: http.DefaultClient}
	var got []sseEvent
	connected := false
	err := c.stream(context.Background(), []string{"message.delivered", "message.received"}, func() { connected = true }, func(ev sseEvent) {
		got = append(got, ev)
	})
	if !connected || len(got) != 1 || got[0].ID != "evt_1" || got[0].Type != "message.delivered" {
		t.Fatalf("connected=%v events=%+v err=%v", connected, got, err)
	}
	if !strings.Contains(describeEvent(got[0]), "msg_1 → +919876543210") {
		t.Fatalf("describe: %q", describeEvent(got[0]))
	}

	bad := &client{baseURL: srv.URL, apiKey: "bk_test_bad", http: http.DefaultClient}
	err = bad.stream(context.Background(), nil, func() {}, func(sseEvent) {})
	var perm *permanentError
	if err == nil || !strings.Contains(err.Error(), "invalid_api_key") || !asPermanent(err, &perm) {
		t.Fatalf("bad key: %v", err)
	}
}

func asPermanent(err error, target **permanentError) bool {
	p, ok := err.(*permanentError)
	*target = p
	return ok
}

func TestListenForwardsSignedEvents(t *testing.T) {
	secret := webhook.NewSecret()
	body := []byte(`{"type":"message.received","timestamp":"2026-10-07T09:00:00Z","data":{"from":"AX-HDFCBK"}}`)
	var verifyErr error
	got := 0
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		verifyErr = webhook.Verify(secret, r.Header, raw, webhook.DefaultTolerance, time.Now())
		got++
		w.WriteHeader(204)
	}))
	defer local.Close()

	status, _, err := deliver(context.Background(), http.DefaultClient, local.URL, secret, sseEvent{ID: "evt_9", Type: "message.received", Data: body})
	if err != nil || status != 204 || got != 1 {
		t.Fatalf("status=%d err=%v got=%d", status, err, got)
	}
	if verifyErr != nil {
		t.Fatalf("forwarded request does not verify: %v", verifyErr)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	e := &apiError{Code: "validation_failed", Message: "validation failed", RequestID: "req_1"}
	e.Details = append(e.Details, struct {
		Location string `json:"location"`
		Message  string `json:"message"`
	}{"body.to", "Use E.164."})
	want := "validation_failed: validation failed\n  body.to: Use E.164.\n  (request req_1)"
	if fmt.Sprint(e) != want {
		t.Fatalf("got %q", e.Error())
	}
}
