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
	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/id"
)

type MeResponse struct {
	User          User           `json:"user"`
	Organizations []Organization `json:"organizations"`
}

type signupInput struct {
	Body struct {
		Email    string `json:"email" format:"email" maxLength:"254" example:"ada@example.com"`
		Password string `json:"password" minLength:"10" maxLength:"128" doc:"At least 10 characters."`
		Name     string `json:"name,omitempty" maxLength:"100" example:"Ada Lovelace"`
		// An invite joins its organization instead of creating a new workspace,
		// and works even when sign-ups are disabled.
		InviteToken string `json:"invite_token,omitempty" maxLength:"128" doc:"Join the organization of this invite instead of creating a workspace."`
	}
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" format:"email" maxLength:"254" example:"ada@example.com"`
		Password string `json:"password" minLength:"1" maxLength:"512"`
	}
}

type sessionOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      MeResponse
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

type meOutput struct {
	Body MeResponse
}

func (s *Server) registerAuth(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "signup", Method: http.MethodPost, Path: "/v1/auth/signup", Tags: []string{"Auth"},
		Summary:       "Create an account",
		Description:   "Creates a user, a first organization and a default project, and starts a session.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusConflict, http.StatusForbidden, http.StatusTooManyRequests},
	}, s.signup)

	huma.Register(api, huma.Operation{
		OperationID: "login", Method: http.MethodPost, Path: "/v1/auth/login", Tags: []string{"Auth"},
		Summary: "Sign in",
		Errors:  []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, s.login)

	huma.Register(api, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/v1/auth/logout", Tags: []string{"Auth"},
		Summary: "Sign out", Security: sessionAuth, DefaultStatus: http.StatusNoContent,
	}, s.logout)

	huma.Register(api, huma.Operation{
		OperationID: "getMe", Method: http.MethodGet, Path: "/v1/me", Tags: []string{"Auth"},
		Summary: "Get the signed-in user and their organizations", Security: sessionAuth,
	}, s.me)
}

func (s *Server) signup(ctx context.Context, in *signupInput) (*sessionOutput, error) {
	if err := s.limit(ctx, "signup:ip:"+ClientIPFrom(ctx).String(), 10, time.Hour); err != nil {
		return nil, err
	}
	if err := auth.ValidatePassword(in.Body.Password); err != nil {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.password", Message: err.Error()})
	}
	if in.Body.InviteToken != "" {
		inv, err := s.inviteByToken(ctx, in.Body.InviteToken)
		if err != nil || inv.AcceptedAt != nil || inv.RevokedAt != nil || time.Now().After(inv.ExpiresAt) {
			return nil, Errorf(http.StatusGone, "invite_unavailable", "This invite link was already used, revoked or has expired. Ask for a new one.")
		}
	} else if !s.cfg.AllowSignup {
		n, err := s.q.CountUsers(ctx)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, Errorf(http.StatusForbidden, CodeForbidden, "Sign-ups are disabled on this Bridge instance. Ask an administrator to create your account.")
		}
	}

	email := strings.TrimSpace(in.Body.Email)
	name := strings.TrimSpace(in.Body.Name)
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

	user, err := q.CreateUser(ctx, dbq.CreateUserParams{ID: id.New(id.User), Email: email, Name: name, PasswordHash: hash})
	if db.IsUniqueViolation(err, "users_email_key") {
		return nil, Errorf(http.StatusConflict, CodeConflict, "An account with this email already exists. Sign in instead.")
	}
	if err != nil {
		return nil, err
	}
	if in.Body.InviteToken != "" {
		joined, err := s.acceptInvite(ctx, q, in.Body.InviteToken, &user)
		if err != nil {
			return nil, err
		}
		cookie, err := s.startSession(ctx, q, user.ID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		s.log.Info("user signed up from an invite", "request_id", RequestIDFrom(ctx), "user_id", user.ID, "organization_id", joined.ID)
		me, err := s.meResponse(ctx, &user)
		if err != nil {
			return nil, err
		}
		return &sessionOutput{SetCookie: cookie, Body: me}, nil
	}
	org, err := createOrganization(ctx, q, user.ID, defaultWorkspaceName(name, email))
	if err != nil {
		return nil, err
	}
	project, err := createProject(ctx, q, org.ID, "Default")
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, principalKey, &Principal{User: &user})
	if err := s.audit(ctx, q, auditEntry{OrganizationID: org.ID, Action: "user.signed_up", TargetType: "user", TargetID: user.ID}); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, q, auditEntry{OrganizationID: org.ID, ProjectID: project.ID, Action: "project.created", TargetType: "project", TargetID: project.ID}); err != nil {
		return nil, err
	}
	cookie, err := s.startSession(ctx, q, user.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	s.log.Info("user signed up", "request_id", RequestIDFrom(ctx), "user_id", user.ID, "organization_id", org.ID)
	return &sessionOutput{SetCookie: cookie, Body: MeResponse{
		User:          toUser(&user),
		Organizations: []Organization{toOrganization(org.ID, org.Name, org.Slug, dbq.MemberRoleOwner, org.CreatedAt)},
	}}, nil
}

func (s *Server) login(ctx context.Context, in *loginInput) (*sessionOutput, error) {
	email := strings.ToLower(strings.TrimSpace(in.Body.Email))
	if err := s.limit(ctx, "login:ip:"+ClientIPFrom(ctx).String(), 20, time.Minute); err != nil {
		return nil, err
	}
	if err := s.limit(ctx, "login:email:"+email, 10, 15*time.Minute); err != nil {
		return nil, err
	}
	invalid := Errorf(http.StatusUnauthorized, CodeUnauthenticated, "Email or password is incorrect.")

	user, err := s.q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.BurnPasswordCheck(ctx, in.Body.Password)
		return nil, invalid
	}
	if err != nil {
		return nil, err
	}
	ok, err := auth.VerifyPassword(ctx, in.Body.Password, user.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, invalid
	}
	cookie, err := s.startSession(ctx, s.q, user.ID)
	if err != nil {
		return nil, err
	}
	me, err := s.meResponse(ctx, &user)
	if err != nil {
		return nil, err
	}
	return &sessionOutput{SetCookie: cookie, Body: me}, nil
}

func (s *Server) logout(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
	p := principalFrom(ctx)
	if err := s.q.DeleteSessionByTokenHash(ctx, p.Session.TokenHash); err != nil {
		return nil, err
	}
	return &logoutOutput{SetCookie: s.clearSessionCookie()}, nil
}

func (s *Server) me(ctx context.Context, _ *struct{}) (*meOutput, error) {
	me, err := s.meResponse(ctx, principalFrom(ctx).User)
	if err != nil {
		return nil, err
	}
	return &meOutput{Body: me}, nil
}

func (s *Server) meResponse(ctx context.Context, u *dbq.User) (MeResponse, error) {
	rows, err := s.q.ListOrganizationsForUser(ctx, u.ID)
	if err != nil {
		return MeResponse{}, err
	}
	orgs := make([]Organization, 0, len(rows))
	for _, r := range rows {
		orgs = append(orgs, toOrganization(r.ID, r.Name, r.Slug, r.Role, r.CreatedAt))
	}
	user := toUser(u)
	user.Operator = s.isOperator(context.WithValue(ctx, principalKey, &Principal{User: u}))
	return MeResponse{User: user, Organizations: orgs}, nil
}

func (s *Server) startSession(ctx context.Context, q *dbq.Queries, userID string) (http.Cookie, error) {
	token := auth.NewSessionToken()
	expires := time.Now().Add(s.cfg.SessionTTL)
	params := dbq.CreateSessionParams{
		ID: id.New(id.Session), UserID: userID, TokenHash: auth.HashToken(token), ExpiresAt: expires,
		UserAgent: userAgentFrom(ctx),
	}
	if ip := ClientIPFrom(ctx); ip.IsValid() {
		params.IP = &ip
	}
	if _, err := q.CreateSession(ctx, params); err != nil {
		return http.Cookie{}, err
	}
	return s.sessionCookie(token, expires), nil
}
