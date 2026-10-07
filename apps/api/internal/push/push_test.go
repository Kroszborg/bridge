package push

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func receiver(t *testing.T) (*ecdh.PrivateKey, []byte) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return priv, auth
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	priv, auth := receiver(t)
	msg := []byte(`{"t":"wake","r":"job"}`)
	body, err := Encrypt(msg, priv.PublicKey().Bytes(), auth)
	if err != nil {
		t.Fatal(err)
	}
	if string(body[16:20]) != "\x00\x00\x10\x00" || body[20] != 65 {
		t.Fatalf("aes128gcm header malformed: rs=%x idlen=%d", body[16:20], body[20])
	}
	got, err := Decrypt(body, priv, auth)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(msg) {
		t.Fatalf("decrypted %q, want %q", got, msg)
	}

	// The wrong auth secret must not decrypt.
	wrong := make([]byte, 16)
	if _, err := Decrypt(body, priv, wrong); err == nil {
		t.Fatal("decrypted with the wrong auth secret")
	}
	// Each message uses a fresh key and salt.
	body2, _ := Encrypt(msg, priv.PublicKey().Bytes(), auth)
	if string(body2[:16]) == string(body[:16]) {
		t.Fatal("salt reused")
	}
}

func TestVAPIDJWTVerifies(t *testing.T) {
	key, der, err := GenerateVAPIDKey()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseVAPIDKey(der)
	if err != nil || !parsed.Equal(key) {
		t.Fatalf("PKCS#8 round trip failed: %v", err)
	}
	w := NewWebPush(key, "mailto:ops@example.com", nil)
	jwt, err := w.vapidJWT("https://push.example.com")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT %q", jwt)
	}
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c map[string]any
	_ = json.Unmarshal(claims, &c)
	if c["aud"] != "https://push.example.com" || c["sub"] != "mailto:ops@example.com" {
		t.Fatalf("claims = %v", c)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Fatal("VAPID signature does not verify")
	}
}

func TestWebPushSend(t *testing.T) {
	priv, auth := receiver(t)
	var gotBody []byte
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	key, _, _ := GenerateVAPIDKey()
	w := NewWebPush(key, "mailto:ops@example.com", srv.Client())
	sub := Subscription{
		Endpoint: srv.URL + "/up/abc",
		P256dh:   base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
	if err := w.Send(context.Background(), sub, []byte("hello"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if gotHeaders.Get("Content-Encoding") != "aes128gcm" || gotHeaders.Get("Urgency") != "high" || gotHeaders.Get("TTL") != "3600" {
		t.Fatalf("headers = %v", gotHeaders)
	}
	if !strings.HasPrefix(gotHeaders.Get("Authorization"), "vapid t=") || !strings.Contains(gotHeaders.Get("Authorization"), ", k="+w.PublicKey()) {
		t.Fatalf("Authorization = %q", gotHeaders.Get("Authorization"))
	}
	plain, err := Decrypt(gotBody, priv, auth)
	if err != nil || string(plain) != "hello" {
		t.Fatalf("push service received undecryptable body: %v %q", err, plain)
	}

	sub.Endpoint = srv.URL + "/gone"
	if err := w.Send(context.Background(), sub, []byte("x"), time.Hour); !errors.Is(err, ErrGone) {
		t.Fatalf("410 should map to ErrGone, got %v", err)
	}
	sub.Auth = "short"
	if err := w.Send(context.Background(), sub, []byte("x"), time.Hour); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("bad auth secret should be ErrInvalidTarget, got %v", err)
	}
}

func TestFCMSend(t *testing.T) {
	var got map[string]map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["message"]["token"] == "stale" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
			return
		}
		if r.URL.Path != "/v1/projects/demo/messages:send" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"projects/demo/messages/1"}`))
	}))
	defer srv.Close()

	f := &FCM{
		projectID: "demo",
		tokens:    oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "ya29.test"}),
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	if err := f.Send(context.Background(), "device-token", map[string]string{"t": "wake"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer ya29.test" {
		t.Fatalf("Authorization = %q", auth)
	}
	android := got["message"]["android"].(map[string]any)
	if android["priority"] != "HIGH" || android["ttl"] != "3600s" {
		t.Fatalf("android config = %v", android)
	}
	if err := f.Send(context.Background(), "stale", nil, time.Hour); !errors.Is(err, ErrGone) {
		t.Fatalf("UNREGISTERED should map to ErrGone, got %v", err)
	}
}

func TestSafeClientBlocksPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))
	defer srv.Close()
	resp, err := SafeClient(false).Get(srv.URL) // 127.0.0.1
	if err == nil {
		resp.Body.Close()
		t.Fatal("SafeClient connected to a loopback address")
	}
	if !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("expected ErrInvalidTarget, got %v", err)
	}
	resp, err = SafeClient(true).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate should permit loopback: %v", err)
	}
	resp.Body.Close()
	for _, ip := range []string{"10.1.2.3", "192.168.0.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1"} {
		if IsPublic(netip.MustParseAddr(ip)) {
			t.Errorf("%s treated as public", ip)
		}
	}
	if !IsPublic(netip.MustParseAddr("8.8.8.8")) {
		t.Error("8.8.8.8 treated as private")
	}
}
