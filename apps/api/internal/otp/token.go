package otp

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/verifytoken"
)

// IssueToken signs a token proving that v's number was verified, for app.
func (s *Service) IssueToken(ctx context.Context, app dbq.VerifyApp, v Verification) (string, time.Time, error) {
	secret, err := s.AppSecret(ctx, app)
	if err != nil {
		return "", time.Time{}, err
	}
	c := verifytoken.New(s.issuer, app.ID, v.To, v.ID, v.Environment, s.now())
	token, err := verifytoken.Sign([]byte(secret), c)
	return token, time.Unix(c.ExpiresAt, 0).UTC(), err
}

// Reasons a token is not valid.
const (
	TokenMalformed       = "malformed"
	TokenUnknownApp      = "unknown_app"
	TokenBadSignature    = "bad_signature"
	TokenWrongIssuer     = "wrong_issuer"
	TokenExpired         = "expired"
	TokenWrongEnv        = "environment_mismatch"
	TokenNotVerification = "verification_mismatch"
)

// TokenCheck is the outcome of checking a token.
type TokenCheck struct {
	Valid  bool
	Reason string // why not, when not valid
	Claims verifytoken.Claims
}

// CheckToken verifies a token for a project and environment: the signature
// under the app's secret, issuer, expiry, environment, and that the
// verification it names was verified for that number.
func (s *Service) CheckToken(ctx context.Context, projectID string, env dbq.APIEnvironment, token string) (TokenCheck, error) {
	unverified, err := verifytoken.Parse(token)
	if err != nil {
		return TokenCheck{Reason: TokenMalformed}, nil
	}
	app, err := s.q.GetVerifyApp(ctx, dbq.GetVerifyAppParams{ID: unverified.Audience, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && app.Secret == nil) {
		return TokenCheck{Reason: TokenUnknownApp}, nil
	}
	if err != nil {
		return TokenCheck{}, err
	}
	secret, err := s.AppSecret(ctx, app)
	if err != nil {
		return TokenCheck{}, err
	}
	c, err := verifytoken.Verify([]byte(secret), token, s.issuer, app.ID, s.now())
	switch {
	case errors.Is(err, verifytoken.ErrSignature):
		return TokenCheck{Reason: TokenBadSignature}, nil
	case errors.Is(err, verifytoken.ErrIssuer):
		return TokenCheck{Reason: TokenWrongIssuer, Claims: c}, nil
	case errors.Is(err, verifytoken.ErrExpired):
		return TokenCheck{Reason: TokenExpired, Claims: c}, nil
	case err != nil:
		return TokenCheck{Reason: TokenMalformed}, nil
	}
	if c.Environment != string(env) {
		return TokenCheck{Reason: TokenWrongEnv, Claims: c}, nil
	}
	v, err := s.q.GetOTPByID(ctx, c.VerificationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (v.ProjectID != projectID || v.AppID == nil || *v.AppID != app.ID || v.Status != dbq.OtpStatusVerified || v.Recipient != c.Subject)) {
		return TokenCheck{Reason: TokenNotVerification, Claims: c}, nil
	}
	if err != nil {
		return TokenCheck{}, err
	}
	return TokenCheck{Valid: true, Claims: c}, nil
}
