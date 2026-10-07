package auth

import (
	"strings"
	"testing"
)

func TestAPIKeyRoundTrip(t *testing.T) {
	for _, env := range []Environment{EnvLive, EnvTest} {
		secret, display := NewAPIKey(env)
		if !strings.HasPrefix(secret, "bk_"+string(env)+"_") {
			t.Fatalf("secret %q missing prefix", secret)
		}
		if !strings.HasPrefix(secret, display) {
			t.Fatalf("display %q is not a prefix of the secret", display)
		}
		if len(display) >= len(secret)/2 {
			t.Fatalf("display %q reveals too much of the key", display)
		}
		got, ok := ParseAPIKey(secret)
		if !ok || got != env {
			t.Fatalf("ParseAPIKey(%q) = %q, %v; want %q, true", secret, got, ok, env)
		}
	}
}

func TestParseAPIKeyRejectsTampering(t *testing.T) {
	secret, _ := NewAPIKey(EnvLive)
	cases := map[string]string{
		"empty":            "",
		"wrong prefix":     "sk" + secret[2:],
		"unknown env":      strings.Replace(secret, "_live_", "_prod_", 1),
		"env swapped":      strings.Replace(secret, "_live_", "_test_", 1),
		"truncated":        secret[:len(secret)-1],
		"extra char":       secret + "A",
		"bad character":    secret[:10] + "-" + secret[11:],
		"flipped char":     flip(secret, 15),
		"flipped checksum": flip(secret, len(secret)-1),
	}
	for name, key := range cases {
		if _, ok := ParseAPIKey(key); ok {
			t.Errorf("%s: ParseAPIKey(%q) accepted a tampered key", name, key)
		}
	}
}

func TestRandomStringAlphabetAndLength(t *testing.T) {
	s := RandomString(500)
	if len(s) != 500 || !isBase62(s) {
		t.Fatalf("RandomString produced %q", s)
	}
}

func TestHashTokenStable(t *testing.T) {
	a, b := HashToken("bs_abc"), HashToken("bs_abc")
	if string(a) != string(b) || len(a) != 32 {
		t.Fatal("HashToken is not a stable 32-byte digest")
	}
	if string(HashToken("bs_abd")) == string(a) {
		t.Fatal("different inputs hashed equally")
	}
}

func flip(s string, i int) string {
	b := []byte(s)
	if b[i] == 'a' {
		b[i] = 'b'
	} else {
		b[i] = 'a'
	}
	return string(b)
}
