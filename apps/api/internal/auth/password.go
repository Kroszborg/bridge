package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters follow the OWASP Password Storage Cheat Sheet
// (m=19 MiB, t=2, p=1).
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	PasswordMinLength = 10
	PasswordMaxLength = 128
)

// hashSlots caps concurrent password hashes so a burst of logins cannot
// exhaust memory (each hash allocates argonMemory KiB).
var hashSlots = make(chan struct{}, 4)

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", PasswordMinLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d characters", PasswordMaxLength)
	errMalformedHash    = errors.New("auth: malformed password hash")
)

// ValidatePassword applies Bridge's password policy (length only, per NIST
// SP 800-63B; composition rules are deliberately absent).
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < PasswordMinLength:
		return ErrPasswordTooShort
	case len(pw) > PasswordMaxLength*4 || n > PasswordMaxLength:
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns an encoded argon2id hash in PHC string format.
func HashPassword(ctx context.Context, pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := derive(ctx, pw, salt, argonMemory, argonTime, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether pw matches the encoded hash.
func VerifyPassword(ctx context.Context, pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedHash
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, errMalformedHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, errMalformedHash
	}
	got, err := derive(ctx, pw, salt, memory, iterations, threads, uint32(len(want)))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against when a login names an unknown email, so the
// response time does not reveal whether an account exists.
var dummyHash = func() string {
	h, err := HashPassword(context.Background(), "bridge-dummy-password-for-timing")
	if err != nil {
		panic(err)
	}
	return h
}()

// BurnPasswordCheck spends the same time as a real verification.
func BurnPasswordCheck(ctx context.Context, pw string) {
	_, _ = VerifyPassword(ctx, pw, dummyHash)
}

func derive(ctx context.Context, pw string, salt []byte, memory, iterations uint32, threads uint8, keyLen uint32) ([]byte, error) {
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-hashSlots }()
	return argon2.IDKey([]byte(pw), salt, iterations, memory, threads, keyLen), nil
}
