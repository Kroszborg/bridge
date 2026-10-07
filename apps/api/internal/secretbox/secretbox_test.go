package secretbox

import (
	"bytes"
	"errors"
	"testing"
)

func TestSealOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal("prv_1", []byte(`{"auth_token":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("secret")) {
		t.Fatal("plaintext visible in sealed value")
	}
	plain, err := b.Open("prv_1", sealed)
	if err != nil || string(plain) != `{"auth_token":"secret"}` {
		t.Fatalf("open = %q, %v", plain, err)
	}
	// Bound to the row: another ID cannot open it.
	if _, err := b.Open("prv_2", sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("other id: %v", err)
	}
	// A different key cannot open it.
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open("prv_1", sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("other key: %v", err)
	}
	// Two seals of the same value differ (random nonce).
	again, _ := b.Seal("prv_1", []byte(`{"auth_token":"secret"}`))
	if bytes.Equal(sealed, again) {
		t.Fatal("nonce reused")
	}
}

func TestNoKey(t *testing.T) {
	b, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Ready() {
		t.Fatal("ready without a key")
	}
	if _, err := b.Seal("x", []byte("y")); !errors.Is(err, ErrNoKey) {
		t.Fatalf("seal: %v", err)
	}
}
