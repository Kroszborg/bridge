package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func listLogs(t *testing.T, c *client, projectID, query string) []map[string]any {
	t.Helper()
	r := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/request-logs?"+query, nil), 200)
	var rows []map[string]any
	for _, v := range r.Body["data"].([]any) {
		rows = append(rows, v.(map[string]any))
	}
	return rows
}

func TestRequestLog(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")

	sent := dev.mustStatus(dev.do("POST", "/v1/messages?to=+919876543210", map[string]any{"to": "+919876543210", "message": "secret text"}), 202)
	msgID := sent.Body["id"].(string)
	dev.mustStatus(dev.do("GET", "/v1/messages/"+msgID, nil), 200)
	bad := dev.do("POST", "/v1/messages", map[string]any{"to": "123", "message": "x"})
	if bad.Status != 422 {
		t.Fatalf("bad request: %d", bad.Status)
	}
	// Requests without a valid key cannot be attributed and are not logged.
	anon := newClient(t, srv)
	anon.origin, anon.apiKey = "", "bk_test_unknownunknownunknownunknown"
	anon.do("GET", "/v1/whoami", nil)
	// The dashboard's own (session) requests are not developer API requests.
	c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/usage", nil), 200)

	var rows []map[string]any
	waitFor(t, "three log entries", func() bool {
		rows = listLogs(t, c, projectID, "environment=test")
		return len(rows) == 3
	})
	errRow, getRow, postRow := rows[0], rows[1], rows[2]
	if postRow["method"] != "POST" || postRow["path"] != "/v1/messages" || postRow["status"] != float64(202) ||
		postRow["resource_id"] != msgID || postRow["environment"] != "test" || postRow["api_key_id"] == nil ||
		!strings.HasPrefix(postRow["request_id"].(string), "req_") || postRow["ip"] == nil {
		t.Fatalf("POST entry: %v", postRow)
	}
	if getRow["resource_id"] != msgID || getRow["status"] != float64(200) {
		t.Fatalf("GET entry: %v", getRow)
	}
	if errRow["status"] != float64(422) || errRow["error_code"] != "validation_failed" || errRow["resource_id"] != nil {
		t.Fatalf("error entry: %v", errRow)
	}
	if errRow["request_id"] != bad.Header.Get("X-Request-Id") {
		t.Fatal("request ID in the log does not match the response header")
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "secret text") || strings.Contains(string(raw), "9876543210") {
		t.Fatal("the request log stored a body or query string")
	}

	if got := listLogs(t, c, projectID, "environment=test&status=error"); len(got) != 1 {
		t.Fatalf("status=error: %d entries", len(got))
	}
	if got := listLogs(t, c, projectID, "environment=test&method=GET"); len(got) != 1 {
		t.Fatalf("method=GET: %d entries", len(got))
	}
	if got := listLogs(t, c, projectID, "environment=live"); len(got) != 0 {
		t.Fatalf("live: %d entries", len(got))
	}
	page := listLogs(t, c, projectID, "environment=test&limit=1&starting_after="+errRow["id"].(string))
	if len(page) != 1 || page[0]["id"] != getRow["id"] {
		t.Fatalf("pagination: %v", page)
	}

	// A revoked key's attempts are still attributed to the project.
	keys := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/api-keys", nil), 200).Body["data"].([]any)
	keyID := keys[0].(map[string]any)["id"].(string)
	c.mustStatus(c.do("DELETE", "/v1/projects/"+projectID+"/api-keys/"+keyID, nil), 204)
	dev.do("GET", "/v1/whoami", nil)
	waitFor(t, "revoked key entry", func() bool {
		rows = listLogs(t, c, projectID, "environment=test&status=4xx")
		return len(rows) == 2 && rows[0]["error_code"] == "invalid_api_key"
	})

	other, _, _ := signup(t, srv)
	if r := other.do("GET", "/v1/projects/"+projectID+"/request-logs", nil); r.Status != 404 {
		t.Fatalf("other tenant: %d", r.Status)
	}
}

func TestUsageHistory(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	dev := apiKeyClient(t, srv, c, projectID, "test")
	ok := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+15550000001", "message": "hi"}), 202)
	fail := dev.mustStatus(dev.do("POST", "/v1/messages", map[string]any{"to": "+15550000002", "message": "hi"}), 202)
	waitStatus(t, dev, ok.Body["id"].(string), "delivered")
	waitStatus(t, dev, fail.Body["id"].(string), "failed")

	var today map[string]any
	waitFor(t, "usage with requests", func() bool {
		r := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/usage/history?environment=test&days=7&tz=Asia/Kolkata", nil), 200)
		days := r.Body["days"].([]any)
		if len(days) != 7 || r.Body["time_zone"] != "Asia/Kolkata" {
			t.Fatalf("history: %s", r.Raw)
		}
		today = days[6].(map[string]any)
		return today["requests"].(float64) >= 4
	})
	wantDate := time.Now().In(mustLoad(t, "Asia/Kolkata")).Format(time.DateOnly)
	if today["date"] != wantDate || today["outbound"] != float64(2) || today["delivered"] != float64(1) ||
		today["failed"] != float64(1) || today["segments"] != float64(2) {
		t.Fatalf("today: %v", today)
	}

	// The developer endpoint shows the key's environment.
	h := dev.mustStatus(dev.do("GET", "/v1/usage/history?days=1", nil), 200)
	if h.Body["environment"] != "test" || len(h.Body["days"].([]any)) != 1 {
		t.Fatalf("developer history: %s", h.Raw)
	}
	if r := c.do("GET", "/v1/projects/"+projectID+"/usage/history?tz=Mars/Olympus", nil); r.Status != 422 {
		t.Fatalf("bad tz: %d", r.Status)
	}
}

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestPlaygroundSend(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	base := "/v1/projects/" + projectID + "/messages"

	// Defaults to test mode: simulated, no phone needed.
	r := c.mustStatus(c.do("POST", base, map[string]any{"to": "+15550000001", "message": "from the playground"}), 202)
	if r.Body["environment"] != "test" || r.Body["provider"] != "simulator" {
		t.Fatalf("playground send: %s", r.Raw)
	}
	waitFor(t, "simulated delivery", func() bool {
		m := c.mustStatus(c.do("GET", base+"/"+r.Body["id"].(string), nil), 200)
		return m.Body["status"] == "delivered"
	})

	// Live without a phone fails cleanly rather than hanging.
	live := c.mustStatus(c.do("POST", base+"?environment=live", map[string]any{"to": "+919876543210", "message": "live"}), 202)
	if live.Body["environment"] != "live" {
		t.Fatalf("live: %s", live.Raw)
	}
	waitFor(t, "no-device failure", func() bool {
		m := c.mustStatus(c.do("GET", base+"/"+live.Body["id"].(string), nil), 200)
		return m.Body["status"] == "failed" && m.Body["error_code"] == "no_device"
	})

	// Idempotency works the same as the developer API.
	first := c.doWithHeaders("POST", base, map[string]any{"to": "+15550000001", "message": "once"}, map[string]string{"Idempotency-Key": "pg-1"})
	second := c.doWithHeaders("POST", base, map[string]any{"to": "+15550000001", "message": "once"}, map[string]string{"Idempotency-Key": "pg-1"})
	if first.Status != 202 || second.Status != 200 || first.Body["id"] != second.Body["id"] {
		t.Fatalf("idempotency: %d %d", first.Status, second.Status)
	}

	// Session sends are not developer API requests, so they are not in the request log.
	time.Sleep(200 * time.Millisecond)
	if got := listLogs(t, c, projectID, "environment=test"); len(got) != 0 {
		t.Fatalf("playground sends logged as API requests: %v", got)
	}
}
