package httpapi

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
)

// Organization roles, lowest to highest. Members can see everything in the
// organization's projects and send test messages; admins also manage keys,
// phones, webhooks, projects and members; owners can also delete projects,
// manage other owners and delete the organization.
var roleRank = map[dbq.MemberRole]int{
	dbq.MemberRoleMember: 1,
	dbq.MemberRoleAdmin:  2,
	dbq.MemberRoleOwner:  3,
}

const metaMinRole = "bridge:min-role"

// adminOnly and ownerOnly mark an operation as needing that role in the
// organization named by the operation's {projectId} or {organizationId}.
var (
	adminOnly = map[string]any{metaMinRole: dbq.MemberRoleAdmin}
	ownerOnly = map[string]any{metaMinRole: dbq.MemberRoleOwner}
)

func hasRole(have, want dbq.MemberRole) bool { return roleRank[have] >= roleRank[want] }

func roleError(want dbq.MemberRole) error {
	msg := "Only organization admins and owners can do this. Ask an admin to change your role."
	if want == dbq.MemberRoleOwner {
		msg = "Only organization owners can do this."
	}
	return Errorf(http.StatusForbidden, CodeForbidden, msg)
}

// checkRole enforces an operation's minimum role for a session user. Callers
// who are not members at all pass through: the handler answers 404, so other
// organizations' resources stay invisible.
func (s *Server) checkRole(ctx huma.Context, p *Principal) error {
	op := ctx.Operation()
	want, ok := op.Metadata[metaMinRole].(dbq.MemberRole)
	if !ok || p == nil || p.User == nil {
		return nil
	}
	var (
		role dbq.MemberRole
		err  error
	)
	switch {
	case ctx.Param("projectId") != "":
		role, err = s.q.RoleForProject(ctx.Context(), dbq.RoleForProjectParams{ProjectID: ctx.Param("projectId"), UserID: p.User.ID})
	case ctx.Param("organizationId") != "":
		role, err = s.q.RoleForOrganization(ctx.Context(), dbq.RoleForOrganizationParams{OrganizationID: ctx.Param("organizationId"), UserID: p.User.ID})
	default:
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !hasRole(role, want) {
		return roleError(want)
	}
	return nil
}
