// Package verifytoken issues and checks the tokens Bridge hands out when the
// Verify widget or hosted page confirms a phone number.
//
// A token is a compact JWT (RFC 7519) signed with HS256 under the Verify
// app's secret. The HMAC key is the secret string itself, as UTF-8 bytes, so
// any JWT library can check a token:
//
//	jwt.verify(token, appSecret, { algorithms: ["HS256"], audience: appId, issuer: bridgeURL })
//
// Claims: iss (the Bridge API URL), aud (the app ID), sub (the phone number in
// E.164), vid (the verification ID), env (live or test), iat, exp (10 minutes
// later) and jti (random).
package verifytoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Lifetime is how long a token is valid.
const Lifetime = 10 * time.Minute

// leeway tolerates small clock differences when checking iat and exp.
const leeway = 30 * time.Second

// Claims are the token's contents.
type Claims struct {
	Issuer         string `json:"iss"`
	Audience       string `json:"aud"`
	Subject        string `json:"sub"`
	VerificationID string `json:"vid"`
	Environment    string `json:"env,omitempty"`
	IssuedAt       int64  `json:"iat"`
	ExpiresAt      int64  `json:"exp"`
	ID             string `json:"jti"`
}

// Errors returned by Verify. ErrMalformed and ErrSignature are permanent;
// ErrExpired means the token was once valid.
var (
	ErrMalformed = errors.New("the token is not a well-formed HS256 JWT")
	ErrSignature = errors.New("the token's signature does not match")
	ErrExpired   = errors.New("the token has expired")
	ErrIssuer    = errors.New("the token was issued by another server")
	ErrAudience  = errors.New("the token was issued for another app")
)

// headerSegment is the fixed, encoded header {"alg":"HS256","typ":"JWT"}.
var headerSegment = b64([]byte(`{"alg":"HS256","typ":"JWT"}`))

// New fills in the time and ID claims of a token issued now.
func New(issuer, audience, subject, verificationID, environment string, now time.Time) Claims {
	jti := make([]byte, 16)
	_, _ = rand.Read(jti)
	return Claims{
		Issuer: issuer, Audience: audience, Subject: subject, VerificationID: verificationID, Environment: environment,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(Lifetime).Unix(), ID: hex.EncodeToString(jti),
	}
}

// Sign encodes and signs claims with key.
func Sign(key []byte, c Claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signingInput := headerSegment + "." + b64(payload)
	return signingInput + "." + b64(mac(key, signingInput)), nil
}

// Parse decodes a token's claims without checking its signature. Use it only
// to find which key to check the token with.
func Parse(token string) (Claims, error) {
	_, payload, _, err := split(token)
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, ErrMalformed
	}
	return c, nil
}

// Verify checks the signature, then the issuer and audience when given, then
// the expiry, and returns the claims.
func Verify(key []byte, token, issuer, audience string, now time.Time) (Claims, error) {
	token = strings.TrimSpace(token)
	header, payload, sig, err := split(token)
	if err != nil {
		return Claims{}, err
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "HS256" {
		return Claims{}, ErrMalformed
	}
	signingInput := token[:strings.LastIndexByte(token, '.')]
	if !hmac.Equal(sig, mac(key, signingInput)) {
		return Claims{}, ErrSignature
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, ErrMalformed
	}
	if issuer != "" && c.Issuer != issuer {
		return c, ErrIssuer
	}
	if audience != "" && c.Audience != audience {
		return c, ErrAudience
	}
	if c.ExpiresAt == 0 || now.Add(-leeway).Unix() >= c.ExpiresAt || time.Unix(c.IssuedAt, 0).After(now.Add(leeway)) {
		return c, ErrExpired
	}
	return c, nil
}

func split(token string) (header, payload, sig []byte, err error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 4096 {
		return nil, nil, nil, ErrMalformed
	}
	var errs [3]error
	header, errs[0] = base64.RawURLEncoding.DecodeString(parts[0])
	payload, errs[1] = base64.RawURLEncoding.DecodeString(parts[1])
	sig, errs[2] = base64.RawURLEncoding.DecodeString(parts[2])
	if errors.Join(errs[:]...) != nil {
		return nil, nil, nil, ErrMalformed
	}
	return header, payload, sig, nil
}

func mac(key []byte, signingInput string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(signingInput))
	return m.Sum(nil)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
