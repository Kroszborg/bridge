package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/auth"
	"bridge/internal/billing"
	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/mail"
	"bridge/internal/messaging"
	"bridge/internal/otp"
)

// Account verification: users prove their email address with a code Bridge
// emails them, and a phone number with a code Bridge sends through its own
// Verify, using the operator's project (BRIDGE_ACCOUNT_VERIFY_API_KEY).

const (
	emailCodeLength      = 6
	emailCodeTTL         = 15 * time.Minute
	emailCodeCooldown    = time.Minute
	emailCodesPerHour    = 5
	emailCodeMaxAttempts = 5
	// phoneCodesPerHour caps how many SMS one account can make the operator's
	// project send, across numbers; Verify adds its own per-number limits.
	phoneCodesPerHour = 5
	// codeChecksPerHour caps confirmation requests per account.
	codeChecksPerHour = 30

	// accountPhonePurpose marks the codes in the operator's Verify project.
	accountPhonePurpose = "account_phone"
)

// EmailVerificationSent describes the code just emailed.
type EmailVerificationSent struct {
	Email             string    `json:"email" example:"ada@example.com" doc:"The address the code was sent to."`
	ExpiresAt         time.Time `json:"expires_at" doc:"The code stops working at this time, 15 minutes after it was sent."`
	ResendAvailableAt time.Time `json:"resend_available_at" doc:"When another code may be requested."`
}

// PhoneVerificationSent describes the code just sent by SMS.
type PhoneVerificationSent struct {
	ID                string    `json:"id" example:"otp_01ja8z3k5wq2v7c9e4r2n0w6yb" doc:"The verification in this server's Verify project."`
	To                string    `json:"to" example:"+919876543210" doc:"The number, in E.164 format."`
	Environment       string    `json:"environment" enum:"live,test" doc:"live sends a real SMS; test (the server verifies phones with a test key) sends nothing."`
	ExpiresAt         time.Time `json:"expires_at"`
	ResendAvailableAt time.Time `json:"resend_available_at" doc:"When another code may be sent to this number."`
	TestCode          *string   `json:"test_code,omitempty" example:"482913" doc:"The code itself. Only returned when the server verifies phones with a test key, which sends no SMS."`
}

type EmailVerificationConfirmInput struct {
	Code string `json:"code" minLength:"1" maxLength:"12" example:"482913" doc:"The code you received."`
}

type EmailChangeInput struct {
	Email    string `json:"email" format:"email" maxLength:"254" example:"ada@example.org" doc:"The new address. The code goes there."`
	Password string `json:"password" maxLength:"128" doc:"Your current password."`
}

type PhoneVerificationInput struct {
	Phone string `json:"phone" minLength:"3" maxLength:"32" example:"+919876543210" doc:"The number in international E.164 format, with the country code."`
}

type PhoneVerificationConfirmInput struct {
	Phone string `json:"phone" minLength:"3" maxLength:"32" example:"+919876543210" doc:"The number the code was sent to."`
	Code  string `json:"code" minLength:"1" maxLength:"12" example:"482913" doc:"The code from the SMS."`
}

func (s *Server) registerVerification(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "sendEmailVerification", Method: http.MethodPost, Path: "/v1/me/email/verification", Tags: []string{"Auth"},
		Summary: "Email a verification code",
		Description: "Emails a 6-digit code to your address. The code expires after 15 minutes or 5 wrong attempts, and " +
			"replaces any code sent before. One code a minute and at most 5 an hour. Sign-up sends the first code by itself. " +
			"Needs an SMTP server (`email_verification` in `GET /v1/auth/config`).",
		Security: sessionAuth, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusConflict, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body EmailVerificationSent }, error) {
		if s.mail == nil {
			return nil, Errorf(http.StatusServiceUnavailable, CodeEmailVerificationUnavailable,
				"Email verification is not set up on this server. Its operator can turn it on by configuring SMTP (BRIDGE_SMTP_HOST).")
		}
		u := principalFrom(ctx).User
		if u.EmailVerifiedAt != nil {
			return nil, Errorf(http.StatusConflict, CodeAlreadyVerified, "Your email address is already verified.")
		}
		sent, err := s.issueEmailCode(ctx, u, emailPurposeVerify, u.Email)
		if err != nil {
			return nil, err
		}
		return &struct{ Body EmailVerificationSent }{Body: sent}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "confirmEmailVerification", Method: http.MethodPost, Path: "/v1/me/email/verification/confirm", Tags: []string{"Auth"},
		Summary: "Verify your email address",
		Description: "Checks the code from the verification email and marks your address verified. A wrong code uses one of " +
			"5 attempts; after the last one, or 15 minutes, ask for a new code. Errors: `invalid_code` (wrong code) and " +
			"`code_expired` (no usable code: expired, used up or replaced).",
		Security: sessionAuth,
		Errors:   []int{http.StatusBadRequest, http.StatusConflict, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct{ Body EmailVerificationConfirmInput }) (*meOutput, error) {
		return s.confirmEmailCode(ctx, strings.TrimSpace(in.Body.Code))
	})

	huma.Register(api, huma.Operation{
		OperationID: "requestEmailChange", Method: http.MethodPost, Path: "/v1/me/email/change", Tags: []string{"Auth"},
		Summary: "Change your email address",
		Description: "Needs your current password. Emails a 6-digit code to the new address and a notice to the current one; " +
			"the address changes once the code is confirmed with `POST /v1/me/email/change/confirm`. The code expires " +
			"after 15 minutes or 5 wrong attempts. One code a minute and at most 5 an hour. `email_in_use` when another " +
			"account uses the address.",
		Security: sessionAuth, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct{ Body EmailChangeInput }) (*struct{ Body EmailVerificationSent }, error) {
		sent, err := s.requestEmailChange(ctx, &in.Body)
		if err != nil {
			return nil, err
		}
		return &struct{ Body EmailVerificationSent }{Body: sent}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "confirmEmailChange", Method: http.MethodPost, Path: "/v1/me/email/change/confirm", Tags: []string{"Auth"},
		Summary: "Confirm the new email address",
		Description: "Checks the code sent to the new address and switches your account to it; the new address counts as " +
			"verified. You stay signed in, and password reset links sent to the old address stop working. Errors: " +
			"`invalid_code`, `code_expired` and `email_in_use` (another account took the address meanwhile).",
		Security: sessionAuth,
		Errors:   []int{http.StatusBadRequest, http.StatusConflict, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct{ Body EmailVerificationConfirmInput }) (*meOutput, error) {
		return s.confirmEmailChange(ctx, strings.TrimSpace(in.Body.Code))
	})

	huma.Register(api, huma.Operation{
		OperationID: "sendPhoneVerification", Method: http.MethodPost, Path: "/v1/me/phone/verification", Tags: []string{"Auth"},
		Summary: "Text a verification code to a phone number",
		Description: "Sends a code by SMS through this server's own Verify, from the project of `BRIDGE_ACCOUNT_VERIFY_API_KEY`. " +
			"With a live key the SMS goes out through that project's paired phones or providers; with a test key nothing is " +
			"sent and the code is returned in `test_code`. A number can be verified on one account only (`phone_in_use`). " +
			"One code per number every 30 seconds, at most 5 per number and 5 per account an hour. " +
			"Needs `phone_verification` in `GET /v1/auth/config`.",
		Security: sessionAuth, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct{ Body PhoneVerificationInput }) (*struct{ Body PhoneVerificationSent }, error) {
		sent, err := s.sendPhoneCode(ctx, in.Body.Phone)
		if err != nil {
			return nil, err
		}
		return &struct{ Body PhoneVerificationSent }{Body: sent}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "confirmPhoneVerification", Method: http.MethodPost, Path: "/v1/me/phone/verification/confirm", Tags: []string{"Auth"},
		Summary: "Verify a phone number",
		Description: "Checks the code sent to the number and saves it as your verified phone number, replacing any number " +
			"verified before. Errors: `invalid_code` (wrong code; the code allows 5 attempts) and `code_expired` " +
			"(no usable code for the number: expired, used up or replaced).",
		Security: sessionAuth,
		Errors:   []int{http.StatusBadRequest, http.StatusConflict, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct{ Body PhoneVerificationConfirmInput }) (*meOutput, error) {
		return s.confirmPhoneCode(ctx, in.Body.Phone, strings.TrimSpace(in.Body.Code))
	})

	huma.Register(api, huma.Operation{
		OperationID: "removePhone", Method: http.MethodDelete, Path: "/v1/me/phone", Tags: []string{"Auth"},
		Summary: "Remove your phone number", Description: "Forgets your verified phone number. You can verify a number again later.",
		Security: sessionAuth,
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		had := principalFrom(ctx).User.PhoneVerifiedAt != nil
		user, err := q.ClearUserPhone(ctx, principalFrom(ctx).User.ID)
		if err != nil {
			return nil, err
		}
		if had {
			if err := s.auditUser(ctx, q, "user.phone_removed"); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.meResult(ctx, &user)
	})
}

// ---- Email ----------------------------------------------------------------

// Purposes of emailed codes.
const (
	emailPurposeVerify = "verify" // proves the account's current address
	emailPurposeChange = "change" // proves a new address before switching to it
)

// issueEmailCode replaces the user's pending code of the purpose with a new
// one for the address, and emails it in the background.
func (s *Server) issueEmailCode(ctx context.Context, u *dbq.User, purpose, to string) (EmailVerificationSent, error) {
	latest, err := s.q.LatestEmailVerification(ctx, dbq.LatestEmailVerificationParams{UserID: u.ID, Purpose: purpose})
	switch {
	case err == nil:
		if wait := time.Until(latest.CreatedAt.Add(emailCodeCooldown)); wait > 0 {
			return EmailVerificationSent{}, retryAfter("A code was emailed moments ago. Wait before asking for another.", wait)
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return EmailVerificationSent{}, err
	}
	if err := s.limit(ctx, "email-code:"+purpose+":"+u.ID, emailCodesPerHour, time.Hour); err != nil {
		return EmailVerificationSent{}, err
	}
	code, err := randomDigits(emailCodeLength)
	if err != nil {
		return EmailVerificationSent{}, err
	}
	codeID := id.New(id.EmailCode)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EmailVerificationSent{}, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	if err := q.ExpireEmailVerifications(ctx, dbq.ExpireEmailVerificationsParams{UserID: u.ID, Purpose: purpose}); err != nil {
		return EmailVerificationSent{}, err
	}
	row, err := q.CreateEmailVerification(ctx, dbq.CreateEmailVerificationParams{
		ID: codeID, UserID: u.ID, Purpose: purpose, Email: to, CodeHash: emailCodeHash(codeID, code), ExpiresAt: time.Now().Add(emailCodeTTL),
	})
	if err != nil {
		return EmailVerificationSent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EmailVerificationSent{}, err
	}
	go s.sendEmailCode(context.WithoutCancel(ctx), purpose, to, code)
	return EmailVerificationSent{Email: to, ExpiresAt: row.ExpiresAt, ResendAvailableAt: row.CreatedAt.Add(emailCodeCooldown)}, nil
}

// sendFirstEmailCode emails a new account its first code without holding up
// sign-up; a failure only means the user asks for a code later.
func (s *Server) sendFirstEmailCode(ctx context.Context, u dbq.User) {
	if s.mail == nil {
		return
	}
	go func() {
		if _, err := s.issueEmailCode(context.WithoutCancel(ctx), &u, emailPurposeVerify, u.Email); err != nil {
			s.log.Warn("could not send the first email verification code", "user_id", u.ID, "error", err)
		}
	}()
}

func (s *Server) sendEmailCode(ctx context.Context, purpose, to, code string) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	what := "verify " + to
	ignore := "If you did not create a Bridge account or ask for this code, ignore this email."
	if purpose == emailPurposeChange {
		what = "make " + to + " the email address of your Bridge account"
		ignore = "If you did not ask to change the email address of a Bridge account, ignore this email; nothing changes."
	}
	err := s.mail.Send(ctx, mail.Message{
		To:      to,
		Subject: "Your Bridge verification code is " + code,
		Text: "Your Bridge verification code is " + code + ".\n\n" +
			"Enter it in the Bridge dashboard to " + what + ". It expires in 15 minutes.\n\n" + ignore + "\n",
	})
	if err != nil {
		s.log.Error("verification email failed", "purpose", purpose, "error", err)
	}
}

// sendEmailChangeNotice warns the current address that someone signed in as
// the user is moving the account to another address.
func (s *Server) sendEmailChangeNotice(ctx context.Context, from, to string) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	reset := strings.TrimRight(s.cfg.DashboardURL.String(), "/") + "/forgot-password"
	err := s.mail.Send(ctx, mail.Message{
		To:      from,
		Subject: "Your Bridge email address is being changed",
		Text: "Someone signed in to your Bridge account, " + from + ", asked to change its email address to " + to + ".\n\n" +
			"The change happens once the code sent to " + to + " is entered. After that, you sign in with the new address.\n\n" +
			"If this wasn't you, reset your password now, which signs out every session:\n" + reset + "\n",
	})
	if err != nil {
		s.log.Error("email change notice failed", "error", err)
	}
}

// checkEmailCode checks the user's pending code of the purpose and, when it
// is right, uses it up and runs apply in the same transaction. Wrong and
// expired codes are recorded before the error is returned.
func (s *Server) checkEmailCode(ctx context.Context, purpose, code string,
	apply func(q *dbq.Queries, ev dbq.EmailVerification) (dbq.User, error),
) (dbq.User, error) {
	u := principalFrom(ctx).User
	if err := s.limit(ctx, "email-code-check:"+u.ID, codeChecksPerHour, time.Hour); err != nil {
		return dbq.User{}, err
	}
	if !isDigits(code, emailCodeLength) {
		return dbq.User{}, Errorf(http.StatusBadRequest, CodeInvalidCode, "The code is the 6 digits from the email.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbq.User{}, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	fail := func(apiErr error) (dbq.User, error) {
		if err := tx.Commit(ctx); err != nil {
			return dbq.User{}, err
		}
		return dbq.User{}, apiErr
	}
	ev, err := q.PendingEmailVerificationForUpdate(ctx, dbq.PendingEmailVerificationForUpdateParams{UserID: u.ID, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.User{}, Errorf(http.StatusBadRequest, CodeCodeExpired, "There is no code to check. Ask for a new one.")
	}
	if err != nil {
		return dbq.User{}, err
	}
	if !time.Now().Before(ev.ExpiresAt) || ev.Attempts >= emailCodeMaxAttempts {
		if err := q.ConsumeEmailVerification(ctx, ev.ID); err != nil {
			return dbq.User{}, err
		}
		return fail(Errorf(http.StatusBadRequest, CodeCodeExpired, "This code has expired. Ask for a new one."))
	}
	if subtle.ConstantTimeCompare(emailCodeHash(ev.ID, code), ev.CodeHash) != 1 {
		ev, err := q.RecordEmailVerificationAttempt(ctx, dbq.RecordEmailVerificationAttemptParams{MaxAttempts: emailCodeMaxAttempts, ID: ev.ID})
		if err != nil {
			return dbq.User{}, err
		}
		return fail(wrongCode(emailCodeMaxAttempts - int(ev.Attempts)))
	}
	if err := q.ConsumeEmailVerification(ctx, ev.ID); err != nil {
		return dbq.User{}, err
	}
	user, err := apply(q, ev)
	if err != nil {
		return dbq.User{}, err
	}
	return user, tx.Commit(ctx)
}

func (s *Server) confirmEmailCode(ctx context.Context, code string) (*meOutput, error) {
	u := principalFrom(ctx).User
	if u.EmailVerifiedAt != nil {
		return nil, Errorf(http.StatusConflict, CodeAlreadyVerified, "Your email address is already verified.")
	}
	user, err := s.checkEmailCode(ctx, emailPurposeVerify, code, func(q *dbq.Queries, ev dbq.EmailVerification) (dbq.User, error) {
		user, err := q.MarkUserEmailVerified(ctx, dbq.MarkUserEmailVerifiedParams{ID: u.ID, Email: ev.Email})
		if errors.Is(err, pgx.ErrNoRows) {
			// The address changed after the code was sent; it proves nothing now.
			return user, Errorf(http.StatusBadRequest, CodeCodeExpired, "Your email address changed after this code was sent. Ask for a new one.")
		}
		if err != nil {
			return user, err
		}
		return user, s.auditUser(ctx, q, "user.email_verified")
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("email verified", "request_id", RequestIDFrom(ctx), "user_id", u.ID)
	return s.meResult(ctx, &user)
}

func (s *Server) requestEmailChange(ctx context.Context, in *EmailChangeInput) (EmailVerificationSent, error) {
	if s.mail == nil {
		return EmailVerificationSent{}, Errorf(http.StatusServiceUnavailable, CodeEmailVerificationUnavailable,
			"Changing your email address needs email, which is not set up on this server. Ask its operator to configure SMTP (BRIDGE_SMTP_HOST).")
	}
	u := principalFrom(ctx).User
	email := strings.TrimSpace(in.Email)
	if strings.EqualFold(email, u.Email) {
		return EmailVerificationSent{}, huma.Error422UnprocessableEntity("validation failed",
			&huma.ErrorDetail{Location: "body.email", Message: "This is already your email address."})
	}
	// The password first, so the endpoint cannot be used to find accounts.
	if err := s.checkPassword(ctx, u, in.Password, "body.password"); err != nil {
		return EmailVerificationSent{}, err
	}
	if err := s.checkEmailAllowed(email); err != nil {
		return EmailVerificationSent{}, err
	}
	if _, err := s.q.GetUserByEmail(ctx, email); err == nil {
		return EmailVerificationSent{}, emailInUse()
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return EmailVerificationSent{}, err
	}
	sent, err := s.issueEmailCode(ctx, u, emailPurposeChange, email)
	if err != nil {
		return EmailVerificationSent{}, err
	}
	go s.sendEmailChangeNotice(context.WithoutCancel(ctx), u.Email, email)
	return sent, nil
}

func (s *Server) confirmEmailChange(ctx context.Context, code string) (*meOutput, error) {
	u := principalFrom(ctx).User
	user, err := s.checkEmailCode(ctx, emailPurposeChange, code, func(q *dbq.Queries, ev dbq.EmailVerification) (dbq.User, error) {
		user, err := q.ChangeUserEmail(ctx, dbq.ChangeUserEmailParams{ID: u.ID, Email: ev.Email})
		if db.IsUniqueViolation(err, "users_email_key") {
			return user, emailInUse()
		}
		if err != nil {
			return user, err
		}
		// Codes and reset links sent to the old address stop working.
		if err := q.ExpireAllEmailVerifications(ctx, u.ID); err != nil {
			return user, err
		}
		if err := q.ExpirePasswordResets(ctx, u.ID); err != nil {
			return user, err
		}
		return user, s.auditUserWith(ctx, q, "user.email_changed", map[string]any{"from": u.Email, "to": user.Email})
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("email changed", "request_id", RequestIDFrom(ctx), "user_id", u.ID)
	return s.meResult(ctx, &user)
}

func emailInUse() error {
	return Errorf(http.StatusConflict, CodeEmailInUse, "Another Bridge account uses this email address.")
}

// emailCodeHash binds a code to its row, so equal codes hash differently.
func emailCodeHash(codeID, code string) []byte {
	sum := sha256.Sum256([]byte(codeID + ":" + code))
	return sum[:]
}

// ---- Phone ----------------------------------------------------------------

func (s *Server) phoneVerificationEnabled() bool {
	return s.cfg != nil && s.cfg.AccountVerifyAPIKey != "" && s.otp != nil
}

// accountVerifyKey resolves BRIDGE_ACCOUNT_VERIFY_API_KEY on every use, so a
// revoked or replaced key takes effect at once.
func (s *Server) accountVerifyKey(ctx context.Context) (dbq.GetAPIKeyForAuthRow, error) {
	if !s.phoneVerificationEnabled() {
		return dbq.GetAPIKeyForAuthRow{}, Errorf(http.StatusServiceUnavailable, CodePhoneVerificationUnavailable,
			"Phone verification is not set up on this server. Its operator can turn it on with BRIDGE_ACCOUNT_VERIFY_API_KEY.")
	}
	unavailable := Errorf(http.StatusServiceUnavailable, CodePhoneVerificationUnavailable,
		"Phone verification is unavailable on this server right now. Try again later.")
	key, err := s.q.GetAPIKeyForAuth(ctx, auth.HashToken(s.cfg.AccountVerifyAPIKey))
	if errors.Is(err, pgx.ErrNoRows) {
		s.log.Error("BRIDGE_ACCOUNT_VERIFY_API_KEY is not a key of any project on this server; phone verification is unavailable",
			"request_id", RequestIDFrom(ctx))
		return key, unavailable
	}
	if err != nil {
		return key, err
	}
	switch {
	case key.RevokedAt != nil:
		s.log.Error("BRIDGE_ACCOUNT_VERIFY_API_KEY was revoked; phone verification is unavailable until it is replaced",
			"request_id", RequestIDFrom(ctx), "api_key_id", key.ID, "project_id", key.ProjectID)
		return key, unavailable
	case key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt):
		s.log.Error("BRIDGE_ACCOUNT_VERIFY_API_KEY expired; phone verification is unavailable until it is replaced",
			"request_id", RequestIDFrom(ctx), "api_key_id", key.ID, "project_id", key.ProjectID)
		return key, unavailable
	}
	return key, nil
}

func (s *Server) sendPhoneCode(ctx context.Context, phone string) (PhoneVerificationSent, error) {
	u := principalFrom(ctx).User
	key, err := s.accountVerifyKey(ctx)
	if err != nil {
		return PhoneVerificationSent{}, err
	}
	to, err := messaging.NormalizeE164(phone)
	if err != nil {
		return PhoneVerificationSent{}, s.accountOTPError(ctx, err)
	}
	if u.PhoneVerifiedAt != nil && deref(u.Phone) == to {
		return PhoneVerificationSent{}, Errorf(http.StatusConflict, CodeAlreadyVerified, "This number is already verified on your account.")
	}
	taken, err := s.q.PhoneVerifiedByOtherUser(ctx, dbq.PhoneVerifiedByOtherUserParams{Phone: &to, UserID: u.ID})
	if err != nil {
		return PhoneVerificationSent{}, err
	}
	if taken {
		return PhoneVerificationSent{}, phoneInUse()
	}
	if err := s.limit(ctx, "phone-code:"+u.ID, phoneCodesPerHour, time.Hour); err != nil {
		return PhoneVerificationSent{}, err
	}
	req := otp.SendRequest{
		ProjectID: key.ProjectID, ProjectName: key.ProjectName, Environment: key.Environment, APIKeyID: &key.ID,
		App: s.cfg.AccountVerifyApp, To: to, Metadata: map[string]any{"purpose": accountPhonePurpose, "user_id": u.ID},
	}
	if ip := ClientIPFrom(ctx); ip.IsValid() {
		req.ClientIP = ip.Unmap()
	}
	v, err := s.otp.Send(ctx, req)
	if err != nil {
		return PhoneVerificationSent{}, s.accountOTPError(ctx, err)
	}
	s.log.Info("phone verification code sent", "request_id", RequestIDFrom(ctx), "user_id", u.ID, "otp_id", v.ID,
		"project_id", key.ProjectID, "environment", key.Environment)
	out := PhoneVerificationSent{
		ID: v.ID, To: v.To, Environment: v.Environment, ExpiresAt: v.ExpiresAt, ResendAvailableAt: v.ResendAvailableAt,
	}
	if key.Environment == dbq.ApiEnvironmentTest {
		out.TestCode = v.Code
	}
	return out, nil
}

func (s *Server) confirmPhoneCode(ctx context.Context, phone, code string) (*meOutput, error) {
	u := principalFrom(ctx).User
	key, err := s.accountVerifyKey(ctx)
	if err != nil {
		return nil, err
	}
	to, err := messaging.NormalizeE164(phone)
	if err != nil {
		return nil, s.accountOTPError(ctx, err)
	}
	if err := s.limit(ctx, "phone-code-check:"+u.ID, codeChecksPerHour, time.Hour); err != nil {
		return nil, err
	}
	if !isDigits(code, 0) || len(code) < 4 || len(code) > 10 {
		return nil, Errorf(http.StatusBadRequest, CodeInvalidCode, "The code is the digits from the SMS.")
	}
	app, err := s.otp.ResolveApp(ctx, key.ProjectID, s.cfg.AccountVerifyApp)
	if err != nil {
		return nil, s.accountOTPError(ctx, err)
	}
	// Only a code this account asked for counts: the project's Verify may
	// also send codes for the operator's own apps.
	env := key.Environment
	noCode := Errorf(http.StatusBadRequest, CodeCodeExpired, "There is no code to check for this number. Ask for a new one.")
	pending, _, err := s.otp.List(ctx, otp.ListRequest{
		ProjectID: key.ProjectID, AppID: app.ID, Environment: &env, Status: "pending", To: to, Limit: 1,
	})
	if err != nil {
		return nil, s.accountOTPError(ctx, err)
	}
	if len(pending) == 0 || pending[0].Metadata["purpose"] != accountPhonePurpose || pending[0].Metadata["user_id"] != u.ID {
		return nil, noCode
	}
	res, err := s.otp.Verify(ctx, otp.VerifyRequest{ProjectID: key.ProjectID, Environment: env, AppID: app.ID, ID: pending[0].ID, Code: code})
	if errors.Is(err, otp.ErrNotFound) {
		return nil, noCode
	}
	if err != nil {
		return nil, s.accountOTPError(ctx, err)
	}
	if !res.Valid {
		switch res.Verification.Status {
		case string(dbq.OtpStatusPending), string(dbq.OtpStatusFailed):
			return nil, wrongCode(res.Verification.AttemptsRemaining)
		default:
			return nil, Errorf(http.StatusBadRequest, CodeCodeExpired, "This code has expired. Ask for a new one.")
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	user, err := q.SetUserPhoneVerified(ctx, dbq.SetUserPhoneVerifiedParams{ID: u.ID, Phone: &to})
	if db.IsUniqueViolation(err, "users_phone_verified_key") {
		return nil, phoneInUse()
	}
	if err != nil {
		return nil, err
	}
	if err := s.auditUser(ctx, q, "user.phone_verified"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.log.Info("phone verified", "request_id", RequestIDFrom(ctx), "user_id", u.ID, "otp_id", res.Verification.ID)
	return s.meResult(ctx, &user)
}

// accountOTPError explains Verify errors in terms of the user's request, and
// hides the operator's configuration problems behind a 503 (logged).
func (s *Server) accountOTPError(ctx context.Context, err error) error {
	var ve *messaging.ValidationError
	var le *billing.LimitError
	switch {
	case errors.As(err, &ve) && ve.Field == "to":
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.phone", Message: ve.Message})
	case errors.Is(err, otp.ErrAppNotFound):
		s.log.Error("BRIDGE_ACCOUNT_VERIFY_APP is not a Verify app of the key's project; phone verification is unavailable",
			"request_id", RequestIDFrom(ctx), "app", s.cfg.AccountVerifyApp)
		return Errorf(http.StatusServiceUnavailable, CodePhoneVerificationUnavailable,
			"Phone verification is unavailable on this server right now. Try again later.")
	case errors.As(err, &le):
		s.log.Error("the project of BRIDGE_ACCOUNT_VERIFY_API_KEY reached a plan limit; phone verification is unavailable",
			"request_id", RequestIDFrom(ctx), "error", err)
		return Errorf(http.StatusServiceUnavailable, CodePhoneVerificationUnavailable,
			"Phone verification is unavailable on this server right now. Try again later.")
	}
	return otpError(err)
}

func phoneInUse() error {
	return Errorf(http.StatusConflict, CodePhoneInUse, "This number is already verified on another Bridge account.")
}

// ---- Shared ---------------------------------------------------------------

func wrongCode(attemptsLeft int) error {
	if attemptsLeft <= 0 {
		return Errorf(http.StatusBadRequest, CodeInvalidCode, "That code is not right, and it was the last attempt. Ask for a new code.")
	}
	plural := "s"
	if attemptsLeft == 1 {
		plural = ""
	}
	return Errorf(http.StatusBadRequest, CodeInvalidCode,
		"That code is not right. "+strconv.Itoa(attemptsLeft)+" attempt"+plural+" left.")
}

// retryAfter is a 429 with an explanation and a Retry-After header.
func retryAfter(msg string, wait time.Duration) error {
	secs := strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
	return huma.ErrorWithHeaders(
		Errorf(http.StatusTooManyRequests, CodeRateLimited, msg+" Retry after "+secs+" seconds."),
		http.Header{"Retry-After": {secs}})
}

// randomDigits returns n uniformly random decimal digits.
func randomDigits(n int) (string, error) {
	var b strings.Builder
	for range n {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + d.Int64()))
	}
	return b.String(), nil
}

// isDigits reports whether s is only digits, and exactly n of them when n > 0.
func isDigits(s string, n int) bool {
	if s == "" || (n > 0 && len(s) != n) {
		return false
	}
	return strings.Trim(s, "0123456789") == ""
}

func (s *Server) meResult(ctx context.Context, u *dbq.User) (*meOutput, error) {
	me, err := s.meResponse(ctx, u)
	if err != nil {
		return nil, err
	}
	return &meOutput{Body: me}, nil
}
