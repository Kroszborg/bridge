package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/auth"
	"bridge/internal/db/dbq"
)

// Session is one signed-in browser.
type Session struct {
	ID         string    `json:"id" example:"ses_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Current    bool      `json:"current" doc:"The session making this request."`
	IP         *string   `json:"ip" nullable:"true"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type SessionPath struct {
	SessionID string `path:"sessionId" pattern:"^ses_[0-9a-z]{26}$"`
}

type RevokedSessions struct {
	Revoked int64 `json:"revoked"`
}

func (s *Server) registerAccount(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "updateMe", Method: http.MethodPatch, Path: "/v1/me", Tags: []string{"Auth"},
		Summary: "Update your profile", Security: sessionAuth,
	}, func(ctx context.Context, in *struct {
		Body struct {
			Name string `json:"name" maxLength:"100"`
		}
	}) (*meOutput, error) {
		u, err := s.q.UpdateUserName(ctx, dbq.UpdateUserNameParams{ID: principalFrom(ctx).User.ID, Name: strings.TrimSpace(in.Body.Name)})
		if err != nil {
			return nil, err
		}
		me, err := s.meResponse(ctx, &u)
		if err != nil {
			return nil, err
		}
		return &meOutput{Body: me}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "changePassword", Method: http.MethodPost, Path: "/v1/me/password", Tags: []string{"Auth"},
		Summary: "Change your password", Description: "Signs out every other session.",
		Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		Body struct {
			CurrentPassword string `json:"current_password" maxLength:"128"`
			NewPassword     string `json:"new_password" minLength:"10" maxLength:"128"`
		}
	}) (*struct{}, error) {
		p := principalFrom(ctx)
		if err := s.limit(ctx, "password-change:"+p.User.ID, 10, time.Hour); err != nil {
			return nil, err
		}
		if err := s.checkPassword(ctx, p.User, in.Body.CurrentPassword, "body.current_password"); err != nil {
			return nil, err
		}
		if err := auth.ValidatePassword(in.Body.NewPassword); err != nil {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.new_password", Message: err.Error()})
		}
		hash, err := auth.HashPassword(ctx, in.Body.NewPassword)
		if err != nil {
			return nil, err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		if err := q.UpdateUserPassword(ctx, dbq.UpdateUserPasswordParams{ID: p.User.ID, PasswordHash: hash}); err != nil {
			return nil, err
		}
		if _, err := q.DeleteOtherSessions(ctx, dbq.DeleteOtherSessionsParams{UserID: p.User.ID, KeepID: p.Session.ID}); err != nil {
			return nil, err
		}
		if err := s.auditUser(ctx, q, "user.password_changed"); err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	})

	huma.Register(api, huma.Operation{
		OperationID: "listSessions", Method: http.MethodGet, Path: "/v1/me/sessions", Tags: []string{"Auth"},
		Summary: "List your sessions", Description: "Browsers signed in to your account, most recently active first.", Security: sessionAuth,
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body []Session }, error) {
		p := principalFrom(ctx)
		rows, err := s.q.ListSessionsForUser(ctx, p.User.ID)
		if err != nil {
			return nil, err
		}
		out := make([]Session, 0, len(rows))
		for _, r := range rows {
			sess := Session{ID: r.ID, Current: r.ID == p.Session.ID, UserAgent: r.UserAgent, CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, ExpiresAt: r.ExpiresAt}
			if r.IP != nil {
				ip := r.IP.String()
				sess.IP = &ip
			}
			out = append(out, sess)
		}
		return &struct{ Body []Session }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revokeSession", Method: http.MethodDelete, Path: "/v1/me/sessions/{sessionId}", Tags: []string{"Auth"},
		Summary: "Sign out a session", Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *SessionPath) (*struct{}, error) {
		p := principalFrom(ctx)
		n, err := s.q.DeleteSession(ctx, dbq.DeleteSessionParams{ID: in.SessionID, UserID: p.User.ID})
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, notFound("Session " + in.SessionID)
		}
		return nil, s.auditUser(ctx, s.q, "user.session_revoked")
	})

	huma.Register(api, huma.Operation{
		OperationID: "revokeOtherSessions", Method: http.MethodPost, Path: "/v1/me/sessions/revoke-others", Tags: []string{"Auth"},
		Summary: "Sign out everywhere else", Security: sessionAuth,
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body RevokedSessions }, error) {
		p := principalFrom(ctx)
		n, err := s.q.DeleteOtherSessions(ctx, dbq.DeleteOtherSessionsParams{UserID: p.User.ID, KeepID: p.Session.ID})
		if err != nil {
			return nil, err
		}
		if err := s.auditUser(ctx, s.q, "user.other_sessions_revoked"); err != nil {
			return nil, err
		}
		return &struct{ Body RevokedSessions }{Body: RevokedSessions{Revoked: n}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteMe", Method: http.MethodDelete, Path: "/v1/me", Tags: []string{"Auth"},
		Summary: "Delete your account",
		Description: "Deletes your user and every organization where you are the only member, with all their projects and data. " +
			"Refused while you are the only owner of an organization with other members: make someone else an owner first.",
		Security: sessionAuth, Errors: []int{http.StatusUnauthorized, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Password string `json:"password" maxLength:"128"`
			Confirm  string `json:"confirm" doc:"Type DELETE to confirm."`
		}
	}) (*logoutOutput, error) {
		p := principalFrom(ctx)
		if in.Body.Confirm != "DELETE" {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.confirm", Message: "Type DELETE to confirm."})
		}
		if err := s.checkPassword(ctx, p.User, in.Body.Password, "body.password"); err != nil {
			return nil, err
		}
		blocking, err := s.q.OrganizationsBlockingDeletion(ctx, p.User.ID)
		if err != nil {
			return nil, err
		}
		if len(blocking) > 0 {
			names := make([]string, 0, len(blocking))
			for _, o := range blocking {
				names = append(names, o.Name)
			}
			return nil, Errorf(http.StatusConflict, CodeConflict,
				"You are the only owner of "+strings.Join(names, ", ")+". Make another member an owner, or remove the other members, first.")
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		if _, err := q.DeleteSoleMemberOrganizations(ctx, p.User.ID); err != nil {
			return nil, err
		}
		if err := q.DeleteUser(ctx, p.User.ID); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		s.log.Info("account deleted", "request_id", RequestIDFrom(ctx), "user_id", p.User.ID)
		return &logoutOutput{SetCookie: s.clearSessionCookie()}, nil
	})
}

// checkPassword verifies the signed-in user's password for a sensitive change.
func (s *Server) checkPassword(ctx context.Context, u *dbq.User, password, location string) error {
	if err := s.limit(ctx, "reauth:"+u.ID, 10, 15*time.Minute); err != nil {
		return err
	}
	ok, err := auth.VerifyPassword(ctx, password, u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: location, Message: "The password is incorrect."})
	}
	return nil
}

// auditUser records an account action in each of the user's organizations,
// so every organization's admins can see security changes of their members.
func (s *Server) auditUser(ctx context.Context, q *dbq.Queries, action string) error {
	return s.auditUserWith(ctx, q, action, nil)
}

// auditUserWith is auditUser with metadata, which must never contain secrets.
func (s *Server) auditUserWith(ctx context.Context, q *dbq.Queries, action string, metadata map[string]any) error {
	orgs, err := q.ListOrganizationsForUser(ctx, principalFrom(ctx).User.ID)
	if err != nil {
		return err
	}
	for _, o := range orgs {
		entry := auditEntry{OrganizationID: o.ID, Action: action, TargetType: "user", TargetID: principalFrom(ctx).User.ID, Metadata: metadata}
		if err := s.audit(ctx, q, entry); err != nil {
			return err
		}
	}
	return nil
}
