package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/auth"
	"bridge/internal/billing"
	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/id"
)

const inviteTTL = 7 * 24 * time.Hour

type Member struct {
	ID           string    `json:"id" example:"mem_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role" enum:"owner,admin,member"`
	JoinedAt     time.Time `json:"joined_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	You          bool      `json:"you" doc:"Whether this is the signed-in user."`
}

type Invite struct {
	ID        string    `json:"id" example:"inv_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Role      string    `json:"role" enum:"owner,admin,member"`
	Email     string    `json:"email" doc:"Who the link was created for; any account can use it."`
	InvitedBy string    `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type CreatedInvite struct {
	Invite
	Token string `json:"token" doc:"Single-use invite token. Shown once."`
	URL   string `json:"url" example:"https://sms.example.com/invite/bi_…" doc:"Send this link to the person you invite."`
}

type InvitePreview struct {
	OrganizationName string    `json:"organization_name"`
	InvitedBy        string    `json:"invited_by"`
	Role             string    `json:"role" enum:"owner,admin,member"`
	ExpiresAt        time.Time `json:"expires_at"`
	Valid            bool      `json:"valid"`
	Reason           string    `json:"reason,omitempty" doc:"Why the link cannot be used: expired, used or revoked."`
}

type AuditActor struct {
	Type  string  `json:"type" enum:"user,api_key,device,system"`
	ID    *string `json:"id" nullable:"true"`
	Name  string  `json:"name"`
	Email string  `json:"email,omitempty"`
}

type AuditEntry struct {
	ID          string         `json:"id"`
	Action      string         `json:"action" example:"api_key.created"`
	Actor       AuditActor     `json:"actor"`
	ProjectID   *string        `json:"project_id" nullable:"true"`
	ProjectName string         `json:"project_name"`
	TargetType  *string        `json:"target_type" nullable:"true"`
	TargetID    *string        `json:"target_id" nullable:"true"`
	Metadata    map[string]any `json:"metadata"`
	IP          *string        `json:"ip" nullable:"true"`
	CreatedAt   time.Time      `json:"created_at"`
}

type AuditList struct {
	Data    []AuditEntry `json:"data"`
	HasMore bool         `json:"has_more"`
}

type MemberPath struct {
	OrgPath
	MemberID string `path:"memberId" pattern:"^mem_[0-9a-z]{26}$"`
}

type InvitePath struct {
	OrgPath
	InviteID string `path:"inviteId" pattern:"^inv_[0-9a-z]{26}$"`
}

type InviteTokenPath struct {
	Token string `path:"token" maxLength:"128"`
}

func toInvite(i dbq.OrganizationInvite, inviter string) Invite {
	return Invite{ID: i.ID, Role: string(i.Role), Email: i.Email, InvitedBy: inviter, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}
}

func (s *Server) registerTeams(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "updateOrganization", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/organizations/{organizationId}", Tags: []string{"Organizations"},
		Summary: "Rename an organization", Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusForbidden},
	}, func(ctx context.Context, in *struct {
		OrgPath
		Body nameBody
	}) (*orgOutput, error) {
		org, err := s.q.GetOrganizationForUser(ctx, dbq.GetOrganizationForUserParams{OrganizationID: in.OrganizationID, UserID: principalFrom(ctx).User.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Organization " + in.OrganizationID)
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, blankName()
		}
		updated, err := s.q.UpdateOrganizationName(ctx, dbq.UpdateOrganizationNameParams{ID: org.ID, Name: name})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: "organization.renamed", TargetType: "organization", TargetID: org.ID,
			Metadata: map[string]any{"from": org.Name, "to": name}}); err != nil {
			return nil, err
		}
		return &orgOutput{Body: Organization{ID: updated.ID, Name: updated.Name, Slug: updated.Slug, Role: string(org.Role), CreatedAt: updated.CreatedAt}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listMembers", Method: http.MethodGet, Path: "/v1/organizations/{organizationId}/members", Tags: []string{"Organizations"},
		Summary: "List members", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *OrgPath) (*struct{ Body []Member }, error) {
		if _, err := s.orgForUser(ctx, in.OrganizationID); err != nil {
			return nil, err
		}
		rows, err := s.q.ListMembers(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		me := principalFrom(ctx).User.ID
		out := make([]Member, 0, len(rows))
		for _, m := range rows {
			out = append(out, Member{ID: m.ID, UserID: m.UserID, Email: m.Email, Name: m.Name, Role: string(m.Role),
				JoinedAt: m.CreatedAt, LastActiveAt: m.LastActiveAt, You: m.UserID == me})
		}
		return &struct{ Body []Member }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateMember", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/organizations/{organizationId}/members/{memberId}", Tags: []string{"Organizations"},
		Summary:     "Change a member's role",
		Description: "Admins can change members and admins. Only owners can make someone an owner or change an owner. The last owner cannot be demoted.",
		Security:    sessionAuth, Errors: []int{http.StatusNotFound, http.StatusForbidden, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		MemberPath
		Body struct {
			Role string `json:"role" enum:"owner,admin,member"`
		}
	}) (*struct{ Body Member }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		target, err := s.q.GetMember(ctx, dbq.GetMemberParams{ID: in.MemberID, OrganizationID: org.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Member " + in.MemberID)
		}
		if err != nil {
			return nil, err
		}
		newRole := dbq.MemberRole(in.Body.Role)
		if (newRole == dbq.MemberRoleOwner || target.Role == dbq.MemberRoleOwner) && org.Role != dbq.MemberRoleOwner {
			return nil, roleError(dbq.MemberRoleOwner)
		}
		if target.Role == dbq.MemberRoleOwner && newRole != dbq.MemberRoleOwner {
			if err := s.keepAnOwner(ctx, org.ID); err != nil {
				return nil, err
			}
		}
		updated, err := s.q.UpdateMemberRole(ctx, dbq.UpdateMemberRoleParams{ID: target.ID, OrganizationID: org.ID, Role: newRole})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: "member.role_changed", TargetType: "user", TargetID: target.UserID,
			Metadata: map[string]any{"from": target.Role, "to": newRole}}); err != nil {
			return nil, err
		}
		return s.memberOut(ctx, org.ID, updated.ID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "removeMember", Method: http.MethodDelete, Path: "/v1/organizations/{organizationId}/members/{memberId}", Tags: []string{"Organizations"},
		Summary:     "Remove a member, or leave",
		Description: "Anyone can remove themselves. Admins can remove members and admins; only owners can remove owners. The last owner cannot leave.",
		Security:    sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusForbidden, http.StatusConflict},
	}, func(ctx context.Context, in *MemberPath) (*struct{}, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		target, err := s.q.GetMember(ctx, dbq.GetMemberParams{ID: in.MemberID, OrganizationID: org.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Member " + in.MemberID)
		}
		if err != nil {
			return nil, err
		}
		self := target.UserID == principalFrom(ctx).User.ID
		switch {
		case self:
		case target.Role == dbq.MemberRoleOwner && org.Role != dbq.MemberRoleOwner:
			return nil, roleError(dbq.MemberRoleOwner)
		case !hasRole(org.Role, dbq.MemberRoleAdmin):
			return nil, roleError(dbq.MemberRoleAdmin)
		}
		if target.Role == dbq.MemberRoleOwner {
			if err := s.keepAnOwner(ctx, org.ID); err != nil {
				return nil, err
			}
		}
		if _, err := s.q.DeleteMember(ctx, dbq.DeleteMemberParams{ID: target.ID, OrganizationID: org.ID}); err != nil {
			return nil, err
		}
		action := "member.removed"
		if self {
			action = "member.left"
		}
		return nil, s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: action, TargetType: "user", TargetID: target.UserID,
			Metadata: map[string]any{"role": target.Role}})
	})

	huma.Register(api, huma.Operation{
		OperationID: "listInvites", Metadata: adminOnly, Method: http.MethodGet, Path: "/v1/organizations/{organizationId}/invites", Tags: []string{"Organizations"},
		Summary: "List pending invites", Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusForbidden},
	}, func(ctx context.Context, in *OrgPath) (*struct{ Body []Invite }, error) {
		if _, err := s.orgForUser(ctx, in.OrganizationID); err != nil {
			return nil, err
		}
		rows, err := s.q.ListPendingInvites(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		out := make([]Invite, 0, len(rows))
		for _, r := range rows {
			out = append(out, toInvite(dbq.OrganizationInvite{
				ID: r.ID, Role: r.Role, Email: r.Email, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt,
			}, firstNonEmpty(r.InviterName, r.InviterEmail)))
		}
		return &struct{ Body []Invite }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createInvite", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/organizations/{organizationId}/invites", Tags: []string{"Organizations"},
		Summary:     "Create an invite link",
		Description: "Returns a single-use link, valid for 7 days. Bridge does not send email: share the link yourself.",
		Security:    sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusPaymentRequired, http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		OrgPath
		Body struct {
			Role  string `json:"role" enum:"owner,admin,member" default:"member"`
			Email string `json:"email,omitempty" maxLength:"254" doc:"Optional note of who the link is for."`
		}
	}) (*struct{ Body CreatedInvite }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		role := dbq.MemberRole(in.Body.Role)
		if role == "" {
			role = dbq.MemberRoleMember
		}
		if role == dbq.MemberRoleOwner && org.Role != dbq.MemberRoleOwner {
			return nil, roleError(dbq.MemberRoleOwner)
		}
		if err := s.limit(ctx, "invite:org:"+org.ID, 50, 24*time.Hour); err != nil {
			return nil, err
		}
		if err := s.billing.CheckAdd(ctx, s.q, org.ID, billing.Members); err != nil {
			return nil, billingError(err)
		}
		me := principalFrom(ctx).User
		token := auth.InvitePrefix + auth.RandomString(32)
		inv, err := s.q.CreateInvite(ctx, dbq.CreateInviteParams{
			ID: id.New(id.Invite), OrganizationID: org.ID, TokenHash: auth.HashToken(token), Role: role,
			Email: strings.TrimSpace(in.Body.Email), InvitedBy: &me.ID, ExpiresAt: time.Now().Add(inviteTTL),
		})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: "invite.created", TargetType: "invite", TargetID: inv.ID,
			Metadata: map[string]any{"role": role, "email": inv.Email}}); err != nil {
			return nil, err
		}
		link := strings.TrimRight(s.cfg.DashboardURL.String(), "/") + "/invite/" + token
		return &struct{ Body CreatedInvite }{Body: CreatedInvite{Invite: toInvite(inv, firstNonEmpty(me.Name, me.Email)), Token: token, URL: link}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revokeInvite", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/organizations/{organizationId}/invites/{inviteId}", Tags: []string{"Organizations"},
		Summary: "Revoke an invite link", Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusForbidden},
	}, func(ctx context.Context, in *InvitePath) (*struct{}, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		n, err := s.q.RevokeInvite(ctx, dbq.RevokeInviteParams{ID: in.InviteID, OrganizationID: org.ID})
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, notFound("Invite " + in.InviteID)
		}
		return nil, s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: "invite.revoked", TargetType: "invite", TargetID: in.InviteID})
	})

	huma.Register(api, huma.Operation{
		OperationID: "getInvite", Method: http.MethodGet, Path: "/v1/invites/{token}", Tags: []string{"Organizations"},
		Summary: "Preview an invite", Description: "No sign-in needed, so the invite page can show where the link leads.",
		Errors: []int{http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *InviteTokenPath) (*struct{ Body InvitePreview }, error) {
		if err := s.limit(ctx, "invite-preview:ip:"+ClientIPFrom(ctx).String(), 60, time.Hour); err != nil {
			return nil, err
		}
		inv, err := s.inviteByToken(ctx, in.Token)
		if err != nil {
			return nil, err
		}
		p := InvitePreview{OrganizationName: inv.OrganizationName, InvitedBy: inv.InviterName, Role: string(inv.Role), ExpiresAt: inv.ExpiresAt, Valid: true}
		switch {
		case inv.RevokedAt != nil:
			p.Valid, p.Reason = false, "revoked"
		case inv.AcceptedAt != nil:
			p.Valid, p.Reason = false, "used"
		case time.Now().After(inv.ExpiresAt):
			p.Valid, p.Reason = false, "expired"
		}
		return &struct{ Body InvitePreview }{Body: p}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "acceptInvite", Method: http.MethodPost, Path: "/v1/invites/{token}/accept", Tags: []string{"Organizations"},
		Summary: "Accept an invite", Security: sessionAuth, Errors: []int{http.StatusPaymentRequired, http.StatusNotFound, http.StatusConflict, http.StatusGone},
	}, func(ctx context.Context, in *InviteTokenPath) (*orgOutput, error) {
		user := principalFrom(ctx).User
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		org, err := s.acceptInvite(ctx, s.q.WithTx(tx), in.Token, user)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &orgOutput{Body: org}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listAuditLogs", Metadata: adminOnly, Method: http.MethodGet, Path: "/v1/organizations/{organizationId}/audit-logs", Tags: []string{"Organizations"},
		Summary: "List audit log entries", Description: "Security-relevant changes in the organization, newest first.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusForbidden},
	}, func(ctx context.Context, in *struct {
		OrgPath
		ProjectID     string `query:"project_id" pattern:"^prj_[0-9a-z]{26}$"`
		Action        string `query:"action" maxLength:"64" doc:"Action or prefix, e.g. api_key or webhook.secret_revealed."`
		ActorID       string `query:"actor_id" maxLength:"64"`
		Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
		StartingAfter string `query:"starting_after"`
	}) (*struct{ Body AuditList }, error) {
		if _, err := s.orgForUser(ctx, in.OrganizationID); err != nil {
			return nil, err
		}
		limit := in.Limit
		if limit == 0 {
			limit = 50
		}
		params := dbq.ListAuditLogsParams{
			OrganizationID: in.OrganizationID, RowLimit: int32(limit + 1),
			ProjectID: optString(in.ProjectID), ActionPrefix: optString(strings.TrimSpace(in.Action)), ActorID: optString(in.ActorID),
		}
		if in.StartingAfter != "" {
			cursor, err := s.q.GetAuditLog(ctx, dbq.GetAuditLogParams{ID: in.StartingAfter, OrganizationID: in.OrganizationID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "query.starting_after", Message: "No audit entry with this ID."})
			}
			if err != nil {
				return nil, err
			}
			params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
		}
		rows, err := s.q.ListAuditLogs(ctx, params)
		if err != nil {
			return nil, err
		}
		out := &struct{ Body AuditList }{Body: AuditList{Data: make([]AuditEntry, 0, len(rows))}}
		if len(rows) > limit {
			rows, out.Body.HasMore = rows[:limit], true
		}
		for _, r := range rows {
			e := AuditEntry{
				ID: r.ID, Action: r.Action, ProjectID: r.ProjectID, ProjectName: r.ProjectName, TargetType: r.TargetType,
				TargetID: r.TargetID, Metadata: map[string]any{}, CreatedAt: r.CreatedAt,
				Actor: AuditActor{Type: r.ActorType, ID: r.ActorID, Name: r.ActorName, Email: r.ActorEmail},
			}
			_ = json.Unmarshal(r.Metadata, &e.Metadata)
			if r.IP != nil {
				ip := r.IP.String()
				e.IP = &ip
			}
			out.Body.Data = append(out.Body.Data, e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteProject", Metadata: ownerOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}", Tags: []string{"Projects"},
		Summary:     "Delete a project",
		Description: "Permanently deletes the project with its messages, API keys, phones, webhooks and logs. Paired phones are disconnected. Owners only.",
		Security:    sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusForbidden, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body struct {
			Confirm string `json:"confirm" doc:"The project's name, typed to confirm."`
		}
	}) (*struct{}, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Body.Confirm) != p.Name {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
				Location: "body.confirm", Message: "Type the project's name exactly to confirm.",
			})
		}
		devices, err := s.q.ProjectDevices(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		if _, err := q.DeleteProject(ctx, p.ID); err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{OrganizationID: p.OrganizationID, Action: "project.deleted", TargetType: "project", TargetID: p.ID,
			Metadata: map[string]any{"name": p.Name}}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		// The device rows are gone; tell connected phones so they forget the server.
		for _, d := range devices {
			if s.hub != nil {
				_ = s.hub.Send(ctx, d, gateway.Outbound{Type: gateway.TypeUnpaired, Reason: "project deleted"})
			}
		}
		return nil, nil
	})
}

// keepAnOwner refuses a change that would leave the organization without an owner.
func (s *Server) keepAnOwner(ctx context.Context, orgID string) error {
	n, err := s.q.CountOwners(ctx, orgID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return Errorf(http.StatusConflict, CodeConflict, "An organization needs at least one owner. Make someone else an owner first.")
	}
	return nil
}

func (s *Server) memberOut(ctx context.Context, orgID, memberID string) (*struct{ Body Member }, error) {
	rows, err := s.q.ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		if m.ID == memberID {
			return &struct{ Body Member }{Body: Member{ID: m.ID, UserID: m.UserID, Email: m.Email, Name: m.Name, Role: string(m.Role),
				JoinedAt: m.CreatedAt, LastActiveAt: m.LastActiveAt, You: m.UserID == principalFrom(ctx).User.ID}}, nil
		}
	}
	return nil, notFound("Member " + memberID)
}

func (s *Server) inviteByToken(ctx context.Context, token string) (dbq.GetInviteByTokenRow, error) {
	if !strings.HasPrefix(token, auth.InvitePrefix) {
		return dbq.GetInviteByTokenRow{}, notFound("Invite")
	}
	inv, err := s.q.GetInviteByToken(ctx, auth.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return inv, notFound("Invite")
	}
	return inv, err
}

// acceptInvite adds the user to the invite's organization inside q's transaction.
func (s *Server) acceptInvite(ctx context.Context, q *dbq.Queries, token string, user *dbq.User) (Organization, error) {
	preview, err := s.inviteByToken(ctx, token)
	if err != nil {
		return Organization{}, err
	}
	inv, err := q.ClaimInvite(ctx, dbq.ClaimInviteParams{TokenHash: auth.HashToken(token), UserID: &user.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, Errorf(http.StatusGone, "invite_unavailable", "This invite link was already used, revoked or has expired. Ask for a new one.")
	}
	if err != nil {
		return Organization{}, err
	}
	if err := s.billing.CheckJoin(ctx, q, inv.OrganizationID); err != nil {
		return Organization{}, billingError(err)
	}
	m, err := q.AddOrganizationMember(ctx, dbq.AddOrganizationMemberParams{
		ID: id.New(id.Member), OrganizationID: inv.OrganizationID, UserID: user.ID, Role: inv.Role,
	})
	if db.IsUniqueViolation(err, "") {
		return Organization{}, Errorf(http.StatusConflict, CodeConflict, "You are already a member of "+preview.OrganizationName+".")
	}
	if err != nil {
		return Organization{}, err
	}
	ctx = context.WithValue(ctx, principalKey, &Principal{User: user})
	if err := s.audit(ctx, q, auditEntry{OrganizationID: inv.OrganizationID, Action: "invite.accepted", TargetType: "user", TargetID: user.ID,
		Metadata: map[string]any{"role": m.Role, "invite_id": inv.ID}}); err != nil {
		return Organization{}, err
	}
	return Organization{ID: inv.OrganizationID, Name: preview.OrganizationName, Role: string(m.Role)}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
