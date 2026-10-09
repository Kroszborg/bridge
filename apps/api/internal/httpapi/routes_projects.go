package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/billing"
	"bridge/internal/db/dbq"
)

type OrgPath struct {
	OrganizationID string `path:"organizationId" pattern:"^org_[0-9a-z]{26}$" example:"org_01j9tq4m2xk3v8c7e5r2n0w6yb"`
}

type ProjectPath struct {
	ProjectID string `path:"projectId" pattern:"^prj_[0-9a-z]{26}$" example:"prj_01j9tq4m2xk3v8c7e5r2n0w6yb"`
}

type nameBody struct {
	Name string `json:"name" minLength:"1" maxLength:"80" example:"Checkout"`
}

type orgOutput struct{ Body Organization }
type orgListOutput struct{ Body ListResponse[Organization] }
type projectOutput struct{ Body Project }
type projectListOutput struct{ Body ListResponse[Project] }

func (s *Server) registerOrganizations(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listOrganizations", Method: http.MethodGet, Path: "/v1/organizations", Tags: []string{"Organizations"},
		Summary: "List your organizations", Security: sessionAuth,
	}, func(ctx context.Context, _ *struct{}) (*orgListOutput, error) {
		rows, err := s.q.ListOrganizationsForUser(ctx, principalFrom(ctx).User.ID)
		if err != nil {
			return nil, err
		}
		out := &orgListOutput{Body: ListResponse[Organization]{Data: make([]Organization, 0, len(rows))}}
		for _, r := range rows {
			out.Body.Data = append(out.Body.Data, toOrganization(r.ID, r.Name, r.Slug, r.Role, r.CreatedAt))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createOrganization", Method: http.MethodPost, Path: "/v1/organizations", Tags: []string{"Organizations"},
		Summary: "Create an organization", Security: sessionAuth, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *struct{ Body nameBody }) (*orgOutput, error) {
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, blankName()
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		org, err := createOrganization(ctx, q, principalFrom(ctx).User.ID, name)
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{OrganizationID: org.ID, Action: "organization.created", TargetType: "organization", TargetID: org.ID}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &orgOutput{Body: toOrganization(org.ID, org.Name, org.Slug, dbq.MemberRoleOwner, org.CreatedAt)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getOrganization", Method: http.MethodGet, Path: "/v1/organizations/{organizationId}", Tags: []string{"Organizations"},
		Summary: "Get an organization", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *OrgPath) (*orgOutput, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		return &orgOutput{Body: toOrganization(org.ID, org.Name, org.Slug, org.Role, org.CreatedAt)}, nil
	})
}

func (s *Server) registerProjects(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listProjects", Method: http.MethodGet, Path: "/v1/organizations/{organizationId}/projects", Tags: []string{"Projects"},
		Summary: "List projects in an organization", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *OrgPath) (*projectListOutput, error) {
		if _, err := s.orgForUser(ctx, in.OrganizationID); err != nil {
			return nil, err
		}
		rows, err := s.q.ListProjects(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		out := &projectListOutput{Body: ListResponse[Project]{Data: make([]Project, 0, len(rows))}}
		for _, p := range rows {
			out.Body.Data = append(out.Body.Data, toProject(p))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createProject", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/organizations/{organizationId}/projects", Tags: []string{"Projects"},
		Summary: "Create a project", Security: sessionAuth, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		OrgPath
		Body nameBody
	}) (*projectOutput, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		if org.Role == dbq.MemberRoleMember {
			return nil, Errorf(http.StatusForbidden, CodeForbidden, "Only organization owners and admins can create projects.")
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, blankName()
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		if err := s.billing.CheckAdd(ctx, q, org.ID, billing.Projects); err != nil {
			return nil, billingError(err)
		}
		p, err := createProject(ctx, q, org.ID, name)
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{OrganizationID: org.ID, ProjectID: p.ID, Action: "project.created", TargetType: "project", TargetID: p.ID}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &projectOutput{Body: toProject(p)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProject", Method: http.MethodGet, Path: "/v1/projects/{projectId}", Tags: []string{"Projects"},
		Summary: "Get a project", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*projectOutput, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		return &projectOutput{Body: toProject(projectFromRow(p))}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateProject", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}", Tags: []string{"Projects"},
		Summary: "Rename a project", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body nameBody
	}) (*projectOutput, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, blankName()
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		updated, err := q.UpdateProjectName(ctx, dbq.UpdateProjectNameParams{ID: p.ID, Name: name})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "project.renamed", TargetType: "project", TargetID: p.ID,
			Metadata: map[string]any{"from": p.Name, "to": name},
		}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &projectOutput{Body: toProject(updated)}, nil
	})
}

// orgForUser loads an organization the caller belongs to. Organizations the
// caller cannot see return 404, never 403, so IDs cannot be probed.
func (s *Server) orgForUser(ctx context.Context, orgID string) (dbq.GetOrganizationForUserRow, error) {
	row, err := s.q.GetOrganizationForUser(ctx, dbq.GetOrganizationForUserParams{OrganizationID: orgID, UserID: principalFrom(ctx).User.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, notFound("Organization " + orgID)
	}
	return row, err
}

// projectForUser loads a project the caller can access through organization membership.
func (s *Server) projectForUser(ctx context.Context, projectID string) (dbq.GetProjectForUserRow, error) {
	row, err := s.q.GetProjectForUser(ctx, dbq.GetProjectForUserParams{ProjectID: projectID, UserID: principalFrom(ctx).User.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, notFound("Project " + projectID)
	}
	return row, err
}

func projectFromRow(r dbq.GetProjectForUserRow) dbq.Project {
	return dbq.Project{ID: r.ID, OrganizationID: r.OrganizationID, Name: r.Name, Slug: r.Slug, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func blankName() error {
	return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.name", Message: "Name cannot be blank."})
}
