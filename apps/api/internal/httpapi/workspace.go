package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"bridge/internal/auth"
	"bridge/internal/db/dbq"
	"bridge/internal/id"
)

// slugify turns a display name into a URL-safe slug.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == '\'' || r == '’':
			// Drop apostrophes: "Ada's workspace" becomes "adas-workspace".
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// uniqueSlug returns base, or base with a short random suffix when taken.
func uniqueSlug(ctx context.Context, base, fallback string, exists func(context.Context, string) (bool, error)) (string, error) {
	if base == "" {
		base = fallback
	}
	candidate := base
	for range 8 {
		taken, err := exists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = base + "-" + strings.ToLower(auth.RandomString(4))
	}
	return base + "-" + strings.ToLower(auth.RandomString(8)), nil
}

// createOrganization creates an organization with the user as owner.
func createOrganization(ctx context.Context, q *dbq.Queries, userID, name string) (dbq.Organization, error) {
	slug, err := uniqueSlug(ctx, slugify(name), "workspace", q.OrganizationSlugExists)
	if err != nil {
		return dbq.Organization{}, err
	}
	org, err := q.CreateOrganization(ctx, dbq.CreateOrganizationParams{ID: id.New(id.Organization), Name: name, Slug: slug})
	if err != nil {
		return dbq.Organization{}, err
	}
	_, err = q.AddOrganizationMember(ctx, dbq.AddOrganizationMemberParams{
		ID: id.New(id.Member), OrganizationID: org.ID, UserID: userID, Role: dbq.MemberRoleOwner,
	})
	return org, err
}

// createProject creates a project with a slug unique within the organization.
func createProject(ctx context.Context, q *dbq.Queries, orgID, name string) (dbq.Project, error) {
	slug, err := uniqueSlug(ctx, slugify(name), "project", func(ctx context.Context, s string) (bool, error) {
		return q.ProjectSlugExists(ctx, dbq.ProjectSlugExistsParams{OrganizationID: orgID, Slug: s})
	})
	if err != nil {
		return dbq.Project{}, err
	}
	return q.CreateProject(ctx, dbq.CreateProjectParams{ID: id.New(id.Project), OrganizationID: orgID, Name: name, Slug: slug})
}

// defaultWorkspaceName derives a first organization name for a new user.
func defaultWorkspaceName(name, email string) string {
	if name != "" {
		first, _, _ := strings.Cut(name, " ")
		return first + "'s workspace"
	}
	local, _, _ := strings.Cut(email, "@")
	return local + "'s workspace"
}

type auditEntry struct {
	OrganizationID string
	ProjectID      string
	Action         string
	TargetType     string
	TargetID       string
	Metadata       map[string]any
}

// audit records a security-relevant action. Metadata must never contain secrets.
func (s *Server) audit(ctx context.Context, q *dbq.Queries, e auditEntry) error {
	actorType, actorID := "system", ""
	if p := principalFrom(ctx); p != nil {
		switch {
		case p.User != nil:
			actorType, actorID = "user", p.User.ID
		case p.APIKey != nil:
			actorType, actorID = "api_key", p.APIKey.ID
		case p.Device != nil:
			actorType, actorID = "device", p.Device.Device.ID
		}
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	params := dbq.InsertAuditLogParams{
		ID:             id.New(id.AuditLog),
		OrganizationID: e.OrganizationID,
		ActorType:      actorType,
		Action:         e.Action,
		Metadata:       meta,
	}
	if actorID != "" {
		params.ActorID = &actorID
	}
	if e.ProjectID != "" {
		params.ProjectID = &e.ProjectID
	}
	if e.TargetType != "" {
		params.TargetType = &e.TargetType
		params.TargetID = &e.TargetID
	}
	if ip := ClientIPFrom(ctx); ip.IsValid() {
		params.IP = &ip
	}
	return q.InsertAuditLog(ctx, params)
}
