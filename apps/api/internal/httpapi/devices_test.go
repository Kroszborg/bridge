package httpapi_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"bridge/internal/auth"
	"bridge/internal/gateway"
	"bridge/internal/push"
)

type pairedDevice struct {
	ID         string
	Credential string
	Install    string
}

func newPairingToken(t *testing.T, c *client, projectID string) map[string]any {
	t.Helper()
	return c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/pairing-tokens", nil), http.StatusCreated).Body
}

func pair(t *testing.T, srv *httptest.Server, token, installation string) response {
	t.Helper()
	app := newClient(t, srv)
	app.origin = ""
	return app.do("POST", "/v1/device/pair", map[string]any{
		"token": token, "installation_id": installation, "device_model": "Google Pixel 7",
		"android_version": "15", "app_version": "0.1.0", "app_flavor": "foss",
	})
}

func pairNewDevice(t *testing.T, srv *httptest.Server, c *client, projectID string) pairedDevice {
	t.Helper()
	tok := newPairingToken(t, c, projectID)
	install := "install-" + strings.ToLower(auth.RandomString(12))
	r := pair(t, srv, tok["token"].(string), install)
	if r.Status != http.StatusOK {
		t.Fatalf("pair: %d %s", r.Status, r.Raw)
	}
	return pairedDevice{ID: r.Body["device_id"].(string), Credential: r.Body["credential"].(string), Install: install}
}

func deviceClient(t *testing.T, srv *httptest.Server, credential string) *client {
	dc := newClient(t, srv)
	dc.origin = ""
	dc.apiKey = credential // sent as "Authorization: Bearer <credential>"
	return dc
}

func TestPairingFlow(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)

	tok := newPairingToken(t, c, projectID)
	uri, _ := url.Parse(tok["pairing_uri"].(string))
	if uri.Scheme != "bridge" || uri.Host != "pair" || uri.Query().Get("token") != tok["token"] || uri.Query().Get("api") != "http://localhost:8080" {
		t.Fatalf("pairing_uri = %v", tok["pairing_uri"])
	}

	r := pair(t, srv, tok["token"].(string), "install-abcdefgh1")
	if r.Status != 200 {
		t.Fatalf("pair: %d %s", r.Status, r.Raw)
	}
	cred := r.Body["credential"].(string)
	if !strings.HasPrefix(cred, "bd_") || r.Body["project_id"] != projectID || r.Body["websocket_url"] != "ws://localhost:8080/v1/device/connect" {
		t.Fatalf("pair response: %s", r.Raw)
	}
	vapid := r.Body["push"].(map[string]any)["unifiedpush"].(map[string]any)["vapid_public_key"].(string)
	if len(vapid) < 80 || r.Body["push"].(map[string]any)["fcm"] != nil {
		t.Fatalf("push config: %v", r.Body["push"])
	}

	// A pairing token is single-use.
	if r := pair(t, srv, tok["token"].(string), "install-abcdefgh2"); r.Status != 401 || r.errCode() != "invalid_pairing_token" {
		t.Fatalf("reused token: %d %s", r.Status, r.Raw)
	}

	dc := deviceClient(t, srv, cred)
	self := dc.mustStatus(dc.do("GET", "/v1/device", nil), 200)
	if self.Body["name"] != "Google Pixel 7" || self.Body["status"] != "offline" || self.Body["project_name"] != "Default" {
		t.Fatalf("self: %s", self.Raw)
	}

	// Re-pairing the same installation keeps the device ID but rotates the credential.
	tok2 := newPairingToken(t, c, projectID)
	r2 := pair(t, srv, tok2["token"].(string), "install-abcdefgh1")
	if r2.Status != 200 || r2.Body["device_id"] != r.Body["device_id"] || r2.Body["credential"] == cred {
		t.Fatalf("re-pair: %s", r2.Raw)
	}
	if r := dc.do("GET", "/v1/device", nil); r.Status != 401 || r.errCode() != "invalid_device_credential" {
		t.Fatalf("old credential after re-pair: %d %s", r.Status, r.Raw)
	}

	// Expired tokens are rejected.
	tok3 := newPairingToken(t, c, projectID)
	if _, err := testDB.Pool.Exec(context.Background(), "UPDATE device_pairing_tokens SET expires_at = now() - interval '1 second' WHERE id = $1", tok3["id"]); err != nil {
		t.Fatal(err)
	}
	if r := pair(t, srv, tok3["token"].(string), "install-abcdefgh3"); r.Status != 401 {
		t.Fatalf("expired token: %d", r.Status)
	}

	list := c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/devices", nil), 200)
	if n := len(list.Body["data"].([]any)); n != 1 {
		t.Fatalf("expected 1 device, got %d", n)
	}

	// The developer API sees the same device through an API key.
	key := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/api-keys", map[string]any{"name": "k", "environment": "test"}), 201)
	dev := newClient(t, srv)
	dev.apiKey = key.Body["secret"].(string)
	got := dev.mustStatus(dev.do("GET", "/v1/devices/"+r.Body["device_id"].(string), nil), 200)
	if got.Body["device_model"] != "Google Pixel 7" {
		t.Fatalf("developer device: %s", got.Raw)
	}
	if strings.Contains(string(got.Raw), "installation") || strings.Contains(string(got.Raw), "credential") {
		t.Fatal("device response leaked installation ID or credential data")
	}
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/device/connect"
}

func dial(t *testing.T, srv *httptest.Server, credential string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL(srv), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + credential}},
	})
}

func readFrame(t *testing.T, ws *websocket.Conn) gateway.Outbound {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	var m gateway.Outbound
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("frame %s: %v", data, err)
	}
	return m
}

func writeFrame(t *testing.T, ws *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func deviceStatus(t *testing.T, c *client, projectID, deviceID string) map[string]any {
	t.Helper()
	return c.mustStatus(c.do("GET", "/v1/projects/"+projectID+"/devices/"+deviceID, nil), 200).Body
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func closeStatus(t *testing.T, ws *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func TestDeviceWebSocketLifecycle(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	d := pairNewDevice(t, srv, c, projectID)

	ws, _, err := dial(t, srv, d.Credential)
	if err != nil {
		t.Fatal(err)
	}
	welcome := readFrame(t, ws)
	if welcome.Type != "welcome" || welcome.DeviceID != d.ID || welcome.ProtocolVersion != 1 || welcome.MaxHeartbeatSeconds != 600 {
		t.Fatalf("welcome = %+v", welcome)
	}
	waitFor(t, "device online", func() bool { return deviceStatus(t, c, projectID, d.ID)["status"] == "online" })

	battery, charging, network := int16(77), true, "wifi"
	writeFrame(t, ws, gateway.Inbound{Type: "heartbeat", Seq: 7, NextIn: 30, Status: &gateway.Status{
		BatteryLevel: &battery, IsCharging: &charging, NetworkType: &network,
	}})
	if ack := readFrame(t, ws); ack.Type != "heartbeat_ack" || ack.Seq != 7 {
		t.Fatalf("ack = %+v", ack)
	}
	st := deviceStatus(t, c, projectID, d.ID)
	if st["battery_level"] != float64(77) || st["is_charging"] != true || st["network_type"] != "wifi" || st["heartbeat_interval_seconds"] != float64(30) {
		t.Fatalf("status after heartbeat: %v", st)
	}

	// Waking a connected device goes over its socket.
	wake := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/devices/"+d.ID+"/wake", nil), 200)
	if wake.Body["via"] != "websocket" {
		t.Fatalf("wake = %s", wake.Raw)
	}
	if f := readFrame(t, ws); f.Type != "sync" || f.Reason != "manual" {
		t.Fatalf("expected sync frame, got %+v", f)
	}

	// A second connection replaces the first.
	ws2, _, err := dial(t, srv, d.Credential)
	if err != nil {
		t.Fatal(err)
	}
	if code := closeStatus(t, ws); code != gateway.CloseReplaced {
		t.Fatalf("old connection closed with %d, want %d", code, gateway.CloseReplaced)
	}
	if f := readFrame(t, ws2); f.Type != "welcome" {
		t.Fatalf("second connection got %+v", f)
	}
	// The replaced connection's cleanup must not mark the device offline.
	time.Sleep(200 * time.Millisecond)
	if s := deviceStatus(t, c, projectID, d.ID)["status"]; s != "online" {
		t.Fatalf("status after replacement = %v", s)
	}

	// Removing the device tells it to unpair and closes the socket.
	c.mustStatus(c.do("DELETE", "/v1/projects/"+projectID+"/devices/"+d.ID, nil), 204)
	if f := readFrame(t, ws2); f.Type != "unpaired" {
		t.Fatalf("expected unpaired frame, got %+v", f)
	}
	if code := closeStatus(t, ws2); code != gateway.CloseUnpaired {
		t.Fatalf("close code %d, want %d", code, gateway.CloseUnpaired)
	}
	if s := deviceStatus(t, c, projectID, d.ID)["status"]; s != "disabled" {
		t.Fatalf("status after removal = %v", s)
	}
	_, resp, err := dial(t, srv, d.Credential)
	if err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("revoked device reconnected: %v %v", err, resp)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "device_revoked") {
		t.Fatalf("expected device_revoked, got %s", body)
	}
}

func TestDeviceDisconnectMarksOffline(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	d := pairNewDevice(t, srv, c, projectID)

	ws, _, err := dial(t, srv, d.Credential)
	if err != nil {
		t.Fatal(err)
	}
	readFrame(t, ws)
	waitFor(t, "online", func() bool { return deviceStatus(t, c, projectID, d.ID)["status"] == "online" })
	_ = ws.Close(websocket.StatusNormalClosure, "bye")
	waitFor(t, "offline", func() bool { return deviceStatus(t, c, projectID, d.ID)["status"] == "offline" })

	if _, _, err := dial(t, srv, "bd_not-a-real-credential"); err == nil {
		t.Fatal("bogus credential connected")
	}
}

func TestDeviceHTTPHeartbeatAndPushWake(t *testing.T) {
	srv := newServer(t)
	c, _, projectID := signup(t, srv)
	d := pairNewDevice(t, srv, c, projectID)
	dc := deviceClient(t, srv, d.Credential)

	dc.mustStatus(dc.do("POST", "/v1/device/heartbeat", map[string]any{"next_in": 900, "status": map[string]any{"battery_level": 15, "is_charging": false}}), 200)
	st := deviceStatus(t, c, projectID, d.ID)
	if st["battery_level"] != float64(15) || st["heartbeat_interval_seconds"] != float64(600) {
		t.Fatalf("HTTP heartbeat should store status and clamp the interval: %v", st)
	}

	// Offline without push: nothing to wake through.
	if w := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/devices/"+d.ID+"/wake", nil), 200); w.Body["via"] != "none" {
		t.Fatalf("wake without push = %s", w.Raw)
	}

	// A UnifiedPush distributor stand-in records what it receives.
	uaPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	authSecret := make([]byte, 16)
	_, _ = rand.Read(authSecret)
	var mu sync.Mutex
	var received [][]byte
	distributor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, b)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer distributor.Close()

	if r := dc.do("PUT", "/v1/device/push", map[string]any{"provider": "unifiedpush", "endpoint": distributor.URL + "/up/xyz"}); r.Status != 422 {
		t.Fatalf("UnifiedPush without keys should be rejected: %d", r.Status)
	}
	dc.mustStatus(dc.do("PUT", "/v1/device/push", map[string]any{
		"provider": "unifiedpush", "endpoint": distributor.URL + "/up/xyz",
		"p256dh": base64.RawURLEncoding.EncodeToString(uaPriv.PublicKey().Bytes()),
		"auth":   base64.RawURLEncoding.EncodeToString(authSecret),
	}), 204)
	if p := deviceStatus(t, c, projectID, d.ID)["push_provider"]; p != "unifiedpush" {
		t.Fatalf("push_provider = %v", p)
	}

	w := c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/devices/"+d.ID+"/wake", nil), 200)
	if w.Body["via"] != "push" {
		t.Fatalf("wake = %s", w.Raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("distributor received %d pushes", len(received))
	}
	plain, err := push.Decrypt(received[0], uaPriv, authSecret)
	if err != nil {
		t.Fatalf("device could not decrypt the wake-up: %v", err)
	}
	var payload map[string]string
	_ = json.Unmarshal(plain, &payload)
	if payload["t"] != "wake" || payload["r"] != "manual" {
		t.Fatalf("payload = %s", plain)
	}

	// The app can unpair itself.
	dc.mustStatus(dc.do("DELETE", "/v1/device", nil), 204)
	if r := dc.do("GET", "/v1/device", nil); r.Status != 401 || r.errCode() != "device_revoked" {
		t.Fatalf("after self-unpair: %d %s", r.Status, r.Raw)
	}
}

func TestDeviceTenantIsolation(t *testing.T) {
	srv := newServer(t)
	alice, _, aliceProject := signup(t, srv)
	bob, _, bobProject := signup(t, srv)
	d := pairNewDevice(t, srv, alice, aliceProject)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/v1/projects/" + aliceProject + "/devices"},
		{"GET", "/v1/projects/" + aliceProject + "/devices/" + d.ID},
		{"DELETE", "/v1/projects/" + aliceProject + "/devices/" + d.ID},
		{"POST", "/v1/projects/" + aliceProject + "/devices/" + d.ID + "/wake"},
		{"POST", "/v1/projects/" + aliceProject + "/pairing-tokens"},
		// Bob's own project path with Alice's device ID.
		{"GET", "/v1/projects/" + bobProject + "/devices/" + d.ID},
	} {
		if r := bob.do(tc.method, tc.path, nil); r.Status != 404 {
			t.Errorf("bob %s %s: %d", tc.method, tc.path, r.Status)
		}
	}
	key := bob.mustStatus(bob.do("POST", "/v1/projects/"+bobProject+"/api-keys", map[string]any{"name": "k", "environment": "live"}), 201)
	dev := newClient(t, srv)
	dev.apiKey = key.Body["secret"].(string)
	if r := dev.do("GET", "/v1/devices/"+d.ID, nil); r.Status != 404 {
		t.Fatalf("bob's API key read alice's device: %d", r.Status)
	}
	if r := dev.mustStatus(dev.do("GET", "/v1/devices", nil), 200); len(r.Body["data"].([]any)) != 0 {
		t.Fatal("bob's device list includes alice's device")
	}
	// A device credential never works as an API key, and vice versa.
	dev.apiKey = d.Credential
	if r := dev.do("GET", "/v1/devices", nil); r.Status != 401 {
		t.Fatalf("device credential accepted as API key: %d", r.Status)
	}
	dc := deviceClient(t, srv, key.Body["secret"].(string))
	if r := dc.do("GET", "/v1/device", nil); r.Status != 401 {
		t.Fatalf("API key accepted as device credential: %d", r.Status)
	}
}
