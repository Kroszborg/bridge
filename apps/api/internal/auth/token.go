// Package auth holds Bridge's credential primitives: API keys, session tokens,
// device credentials, password hashing, and the request principal.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"hash/crc32"
	"strings"
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Token prefixes. Prefixes make leaked secrets easy to recognise and let
// secret scanners match them.
const (
	APIKeyPrefix       = "bk_"
	SessionTokenPrefix = "bs_"
	DevicePrefix       = "bd_"
	PairingPrefix      = "bp_"
	InvitePrefix       = "bi_"
)

const (
	tokenRandomLen  = 43 // ~256 bits of entropy in base62
	apiKeyRandomLen = 40 // ~238 bits
	apiKeyCheckLen  = 6
	displayLen      = 6
)

// RandomString returns n cryptographically random base62 characters.
func RandomString(n int) string {
	out := make([]byte, 0, n)
	buf := make([]byte, n+n/2)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("auth: crypto/rand failed: " + err.Error())
		}
		for _, b := range buf {
			// Rejection sampling keeps the distribution uniform: 248 = 4*62.
			if b < 248 {
				out = append(out, base62[b%62])
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out)
}

// HashToken returns the SHA-256 digest stored in place of a secret. Secrets
// carry at least 238 bits of entropy, so a fast hash is appropriate; slow
// password hashes are reserved for human-chosen passwords.
func HashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// NewSessionToken returns a new opaque session token for the session cookie.
func NewSessionToken() string { return SessionTokenPrefix + RandomString(tokenRandomLen) }

// NewDeviceCredential returns a new long-lived device credential.
func NewDeviceCredential() string { return DevicePrefix + RandomString(tokenRandomLen) }

// NewPairingToken returns a new short-lived pairing token.
func NewPairingToken() string { return PairingPrefix + RandomString(tokenRandomLen) }

// Environment is the API key environment.
type Environment string

const (
	EnvLive Environment = "live"
	EnvTest Environment = "test"
)

// Valid reports whether e is a known environment.
func (e Environment) Valid() bool { return e == EnvLive || e == EnvTest }

// NewAPIKey returns a new secret API key and its non-secret display prefix.
//
// Format: bk_<env>_<40 random base62><6 base62 CRC32 checksum>.
// The checksum lets Bridge reject mistyped or fabricated keys without a
// database lookup.
func NewAPIKey(env Environment) (secret, display string) {
	head := APIKeyPrefix + string(env) + "_"
	random := RandomString(apiKeyRandomLen)
	secret = head + random + checksum(head+random)
	return secret, head + random[:displayLen]
}

// ParseAPIKey validates the structure and checksum of an API key and returns
// its environment. It does not check whether the key exists.
func ParseAPIKey(key string) (Environment, bool) {
	rest, ok := strings.CutPrefix(key, APIKeyPrefix)
	if !ok {
		return "", false
	}
	envPart, body, ok := strings.Cut(rest, "_")
	if !ok {
		return "", false
	}
	env := Environment(envPart)
	if !env.Valid() || len(body) != apiKeyRandomLen+apiKeyCheckLen || !isBase62(body) {
		return "", false
	}
	random, sum := body[:apiKeyRandomLen], body[apiKeyRandomLen:]
	if checksum(APIKeyPrefix+envPart+"_"+random) != sum {
		return "", false
	}
	return env, true
}

func checksum(s string) string {
	v := crc32.ChecksumIEEE([]byte(s))
	out := make([]byte, apiKeyCheckLen)
	for i := apiKeyCheckLen - 1; i >= 0; i-- {
		out[i] = base62[v%62]
		v /= 62
	}
	return string(out)
}

func isBase62(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}
