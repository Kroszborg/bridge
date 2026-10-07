package verifytoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// independentSign builds an HS256 JWT by hand, the way the RFC describes it,
// without any code from this package.
func independentSign(t *testing.T, secret, header, payload string) string {
	t.Helper()
	enc := base64.RawURLEncoding
	input := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString([]byte(payload))
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(input))
	return input + "." + enc.EncodeToString(m.Sum(nil))
}

func TestVerifyAcceptsIndependentlySignedToken(t *testing.T) {
	secret := "bvs_example_secret"
	now := time.Unix(1_800_000_000, 0)
	token := independentSign(t, secret, `{"typ":"JWT","alg":"HS256"}`,
		`{"iss":"https://bridge.example.com","aud":"vap_01","sub":"+919876543210","vid":"otp_01","env":"live","iat":1800000000,"exp":1800000600,"jti":"abc"}`)

	c, err := Verify([]byte(secret), token, "https://bridge.example.com", "vap_01", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "+919876543210" || c.VerificationID != "otp_01" || c.Environment != "live" || c.ExpiresAt != 1800000600 || c.ID != "abc" {
		t.Fatalf("claims = %+v", c)
	}

	if _, err := Verify([]byte("another secret"), token, "", "", now); !errors.Is(err, ErrSignature) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := Verify([]byte(secret), token, "https://other.example.com", "", now); !errors.Is(err, ErrIssuer) {
		t.Fatalf("wrong issuer: %v", err)
	}
	if _, err := Verify([]byte(secret), token, "", "vap_02", now); !errors.Is(err, ErrAudience) {
		t.Fatalf("wrong audience: %v", err)
	}
	if _, err := Verify([]byte(secret), token, "", "", now.Add(11*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
	// Tampering with the payload breaks the signature.
	parts := strings.Split(token, ".")
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"+15550000001","exp":1900000000}`)) + "." + parts[2]
	if _, err := Verify([]byte(secret), forged, "", "", now); !errors.Is(err, ErrSignature) {
		t.Fatalf("forged payload: %v", err)
	}
	// alg=none and other algorithms are refused outright.
	none := independentSign(t, secret, `{"alg":"none"}`, `{"exp":1900000000}`)
	if _, err := Verify([]byte(secret), none, "", "", now); !errors.Is(err, ErrMalformed) {
		t.Fatalf("alg none: %v", err)
	}
	for _, bad := range []string{"", "a.b", "a.b.c.d", "!!.??.**"} {
		if _, err := Verify([]byte(secret), bad, "", "", now); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// The example token from jwt.io, signed with "your-256-bit-secret".
func TestKnownVector(t *testing.T) {
	const token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	// The signature is right; the token has no exp, so it is refused as expired.
	if _, err := Verify([]byte("your-256-bit-secret"), token, "", "", time.Unix(1516239022, 0)); !errors.Is(err, ErrExpired) {
		t.Fatalf("jwt.io vector: %v", err)
	}
	if _, err := Verify([]byte("not-the-secret"), token, "", "", time.Unix(1516239022, 0)); !errors.Is(err, ErrSignature) {
		t.Fatalf("jwt.io vector, wrong key: %v", err)
	}
}

func TestSignProducesStandardToken(t *testing.T) {
	secret := []byte("bvs_0123456789")
	now := time.Now()
	c := New("http://localhost:8080", "vap_x", "+15550000001", "otp_y", "test", now)
	token, err := Sign(secret, c)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token = %q", token)
	}
	// Recompute the signature by hand.
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(parts[0] + "." + parts[1]))
	if want := base64.RawURLEncoding.EncodeToString(m.Sum(nil)); parts[2] != want {
		t.Fatalf("signature %s, want %s", parts[2], want)
	}
	var header map[string]string
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if err := json.Unmarshal(raw, &header); err != nil || header["alg"] != "HS256" || header["typ"] != "JWT" {
		t.Fatalf("header = %s", raw)
	}
	var claims map[string]any
	raw, _ = base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"iss", "aud", "sub", "vid", "env", "iat", "exp", "jti"} {
		if _, ok := claims[k]; !ok {
			t.Errorf("claim %s missing: %s", k, raw)
		}
	}
	if claims["exp"].(float64)-claims["iat"].(float64) != 600 {
		t.Fatalf("lifetime = %v", claims["exp"].(float64)-claims["iat"].(float64))
	}
	got, err := Verify(secret, token, "http://localhost:8080", "vap_x", now)
	if err != nil || got != c {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	parsed, err := Parse(token)
	if err != nil || parsed.Audience != "vap_x" {
		t.Fatalf("parse: %+v %v", parsed, err)
	}
}
