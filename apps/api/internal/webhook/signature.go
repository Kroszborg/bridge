// Package webhook delivers Bridge events to developer endpoints, signed with
// the Standard Webhooks scheme (https://www.standardwebhooks.com), retried
// with backoff, and logged attempt by attempt.
package webhook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SecretPrefix starts every signing secret, as Standard Webhooks libraries expect.
const SecretPrefix = "whsec_"

// Header names defined by Standard Webhooks.
const (
	HeaderID        = "webhook-id"
	HeaderTimestamp = "webhook-timestamp"
	HeaderSignature = "webhook-signature"
)

// DefaultTolerance is how far a timestamp may be from the receiver's clock.
const DefaultTolerance = 5 * time.Minute

var (
	ErrInvalidSecret    = errors.New("webhook secret must be whsec_ followed by base64")
	ErrMissingHeaders   = errors.New("missing webhook-id, webhook-timestamp or webhook-signature")
	ErrTimestamp        = errors.New("webhook timestamp is outside the tolerance")
	ErrInvalidSignature = errors.New("no webhook signature matches")
)

// NewSecret returns a new signing secret with 32 random bytes.
func NewSecret() string {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return SecretPrefix + base64.StdEncoding.EncodeToString(key)
}

func secretKey(secret string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, SecretPrefix))
	if err != nil || len(key) == 0 {
		return nil, ErrInvalidSecret
	}
	return key, nil
}

// Sign returns the webhook-signature header value for a payload:
// "v1," + base64(HMAC-SHA256(key, id + "." + unix timestamp + "." + body)).
func Sign(secret, msgID string, ts time.Time, body []byte) (string, error) {
	key, err := secretKey(secret)
	if err != nil {
		return "", err
	}
	return "v1," + base64.StdEncoding.EncodeToString(mac(key, msgID, ts.Unix(), body)), nil
}

func mac(key []byte, msgID string, ts int64, body []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msgID + "." + strconv.FormatInt(ts, 10) + "."))
	h.Write(body)
	return h.Sum(nil)
}

// Verify checks a received webhook the way a receiver should: the timestamp
// must be within tolerance and one of the space-separated v1 signatures must
// match. Bridge uses it in tests; the docs show the same steps for receivers.
func Verify(secret string, h http.Header, body []byte, tolerance time.Duration, now time.Time) error {
	msgID, tsRaw, sigs := h.Get(HeaderID), h.Get(HeaderTimestamp), h.Get(HeaderSignature)
	if msgID == "" || tsRaw == "" || sigs == "" {
		return ErrMissingHeaders
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		return ErrTimestamp
	}
	if d := now.Sub(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return ErrTimestamp
	}
	key, err := secretKey(secret)
	if err != nil {
		return err
	}
	want := mac(key, msgID, ts, body)
	for _, sig := range strings.Fields(sigs) {
		version, value, ok := strings.Cut(sig, ",")
		if !ok || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(value)
		if err == nil && hmac.Equal(got, want) {
			return nil
		}
	}
	return ErrInvalidSignature
}
