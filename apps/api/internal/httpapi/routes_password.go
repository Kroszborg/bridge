package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/auth"
	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/mail"
)

const passwordResetTTL = time.Hour

type AuthConfig struct {
	SignupOpen    bool `json:"signup_open" doc:"Whether anyone can create an account. Invites always work."`
	PasswordReset bool `json:"password_reset" doc:"Whether forgotten passwords can be reset by email."`
	Hosted        bool `json:"hosted" doc:"Whether this is hosted Bridge (plans and billing apply)."`
	// Legal pages on the public website, when BRIDGE_SITE_URL is set.
	TermsURL   *string `json:"terms_url" nullable:"true"`
	PrivacyURL *string `json:"privacy_url" nullable:"true"`
}

func (s *Server) registerPassword(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getAuthConfig", Method: http.MethodGet, Path: "/v1/auth/config", Tags: []string{"Auth"},
		Summary:     "Get sign-in options",
		Description: "Public, no authentication. What the sign-in and sign-up pages should offer on this server.",
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body AuthConfig }, error) {
		open := s.cfg.AllowSignup
		if !open {
			n, err := s.q.CountUsers(ctx)
			if err != nil {
				return nil, err
			}
			open = n == 0 // the first account can always be created
		}
		out := AuthConfig{SignupOpen: open, PasswordReset: s.mail != nil, Hosted: s.billing.Enabled()}
		if site := s.cfg.SiteURL; site != nil {
			terms, privacy := site.String()+"/terms", site.String()+"/privacy"
			out.TermsURL, out.PrivacyURL = &terms, &privacy
		}
		return &struct{ Body AuthConfig }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "requestPasswordReset", Method: http.MethodPost, Path: "/v1/auth/password-reset", Tags: []string{"Auth"},
		Summary: "Email a password reset link",
		Description: "Always answers 202, whether or not an account uses the address, so the endpoint cannot be used " +
			"to find accounts. The link works once and expires after an hour.",
		DefaultStatus: http.StatusAccepted, Errors: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Email string `json:"email" format:"email" maxLength:"254" example:"ada@example.com"`
		}
	}) (*struct{}, error) {
		if s.mail == nil {
			return nil, Errorf(http.StatusServiceUnavailable, CodeUnavailable,
				"Password reset by email is not set up on this server. Ask its administrator to reset your password.")
		}
		email := strings.ToLower(strings.TrimSpace(in.Body.Email))
		if err := s.limit(ctx, "reset:ip:"+ClientIPFrom(ctx).String(), 10, time.Hour); err != nil {
			return nil, err
		}
		if err := s.limit(ctx, "reset:email:"+email, 3, time.Hour); err != nil {
			return nil, err
		}
		user, err := s.q.GetUserByEmail(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		token := auth.ResetPrefix + auth.RandomString(32)
		if _, err := s.q.CreatePasswordReset(ctx, dbq.CreatePasswordResetParams{
			ID: id.New(id.PasswordReset), UserID: user.ID, TokenHash: auth.HashToken(token), ExpiresAt: time.Now().Add(passwordResetTTL),
		}); err != nil {
			return nil, err
		}
		// Sent in the background so the response takes as long for unknown addresses.
		link := strings.TrimRight(s.cfg.DashboardURL.String(), "/") + "/reset-password?token=" + token
		go s.sendResetEmail(context.WithoutCancel(ctx), user.Email, link)
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "confirmPasswordReset", Method: http.MethodPost, Path: "/v1/auth/password-reset/confirm", Tags: []string{"Auth"},
		Summary:     "Set a new password with a reset link",
		Description: "Sets the password, signs out every other session and starts a new one.",
		Errors:      []int{http.StatusGone, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Token    string `json:"token" minLength:"1" maxLength:"128"`
			Password string `json:"password" minLength:"10" maxLength:"128" doc:"At least 10 characters."`
		}
	}) (*sessionOutput, error) {
		if err := s.limit(ctx, "reset-confirm:ip:"+ClientIPFrom(ctx).String(), 20, time.Hour); err != nil {
			return nil, err
		}
		if err := auth.ValidatePassword(in.Body.Password); err != nil {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.password", Message: err.Error()})
		}
		gone := Errorf(http.StatusGone, "reset_link_unavailable", "This reset link was already used or has expired. Request a new one.")
		if !strings.HasPrefix(in.Body.Token, auth.ResetPrefix) {
			return nil, gone
		}
		hash, err := auth.HashPassword(ctx, in.Body.Password)
		if err != nil {
			return nil, err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		reset, err := q.ClaimPasswordReset(ctx, auth.HashToken(in.Body.Token))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, gone
		}
		if err != nil {
			return nil, err
		}
		if err := q.UpdateUserPassword(ctx, dbq.UpdateUserPasswordParams{ID: reset.UserID, PasswordHash: hash}); err != nil {
			return nil, err
		}
		if err := q.ExpirePasswordResets(ctx, reset.UserID); err != nil {
			return nil, err
		}
		// Whoever knew the old password is signed out everywhere.
		if err := q.DeleteUserSessions(ctx, reset.UserID); err != nil {
			return nil, err
		}
		cookie, err := s.startSession(ctx, q, reset.UserID)
		if err != nil {
			return nil, err
		}
		user, err := q.GetUserByID(ctx, reset.UserID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		s.log.Info("password reset", "request_id", RequestIDFrom(ctx), "user_id", user.ID)
		me, err := s.meResponse(ctx, &user)
		if err != nil {
			return nil, err
		}
		return &sessionOutput{SetCookie: cookie, Body: me}, nil
	})
}

func (s *Server) sendResetEmail(ctx context.Context, to, link string) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	err := s.mail.Send(ctx, mail.Message{
		To:      to,
		Subject: "Reset your Bridge password",
		Text: "Someone asked to reset the password of your Bridge account, " + to + ".\n\n" +
			"Choose a new password here:\n" + link + "\n\n" +
			"The link works once and expires in an hour. If you did not ask for this, ignore this email; " +
			"your password stays the same.\n",
	})
	if err != nil {
		s.log.Error("password reset email failed", "error", err)
	}
}
