package auth

import (
	"context"
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	ctx := context.Background()
	h, err := HashPassword(ctx, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format %q", h)
	}
	ok, err := VerifyPassword(ctx, "correct horse battery staple", h)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(correct) = %v, %v", ok, err)
	}
	ok, err = VerifyPassword(ctx, "correct horse battery stapler", h)
	if err != nil || ok {
		t.Fatalf("VerifyPassword(wrong) = %v, %v", ok, err)
	}
	h2, _ := HashPassword(ctx, "correct horse battery staple")
	if h == h2 {
		t.Fatal("two hashes of the same password are identical; salt is not random")
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$AA$AA", "$argon2id$v=19$m=x$AA$AA"} {
		if _, err := VerifyPassword(context.Background(), "pw", h); err == nil {
			t.Errorf("VerifyPassword accepted malformed hash %q", h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("short") == nil {
		t.Error("accepted a 5-character password")
	}
	if ValidatePassword(strings.Repeat("a", PasswordMaxLength+1)) == nil {
		t.Error("accepted an over-long password")
	}
	if err := ValidatePassword("ten chars!"); err != nil {
		t.Errorf("rejected a valid password: %v", err)
	}
}
