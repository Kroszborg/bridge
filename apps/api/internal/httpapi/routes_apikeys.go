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
)

type apiKeyListOutput struct{ Body ListResponse[APIKey] }
type createdAPIKeyOutput struct{ Body CreatedAPIKey }

type createAPIKeyInput struct {
	ProjectPath
	Body struct {
		Name          string `json:"name" minLength:"1" maxLength:"80" example:"Production server"`
		Environment   string `json:"environment" enum:"live,test" doc:"Test keys never send real SMS."`
		ExpiresInDays int    `json:"expires_in_days,omitempty" minimum:"1" maximum:"3650" doc:"Leave empty for a key that does not expire."`
	}
}

type revokeAPIKeyInput struct {
	ProjectPath
	KeyID string `path:"keyId" pattern:"^key_[0-9a-z]{26}$" example:"key_01j9tq4m2xk3v8c7e5r2n0w6yb"`
}

func (s *Server) registerAPIKeys(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listAPIKeys", Method: http.MethodGet, Path: "/v1/projects/{projectId}/api-keys", Tags: []string{"API keys"},
		Summary: "List API keys", Description: "Secrets are never returned; only the display prefix.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*apiKeyListOutput, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		keys, err := s.q.ListAPIKeys(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out := &apiKeyListOutput{Body: ListResponse[APIKey]{Data: make([]APIKey, 0, len(keys))}}
		for _, k := range keys {
			out.Body.Data = append(out.Body.Data, toAPIKey(k))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createAPIKey", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/api-keys", Tags: []string{"API keys"},
		Summary:     "Create an API key",
		Description: "Returns the secret once. Bridge stores only a SHA-256 hash and cannot show the key again.",
		Security:    sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *createAPIKeyInput) (*createdAPIKeyOutput, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, blankName()
		}
		env := auth.Environment(in.Body.Environment)
		secret, display := auth.NewAPIKey(env)
		params := dbq.CreateAPIKeyParams{
			ID: id.New(id.APIKey), ProjectID: p.ID, Name: name, Environment: dbq.APIEnvironment(env),
			KeyPrefix: display, KeyHash: auth.HashToken(secret), CreatedBy: &principalFrom(ctx).User.ID,
		}
		if in.Body.ExpiresInDays > 0 {
			exp := time.Now().AddDate(0, 0, in.Body.ExpiresInDays)
			params.ExpiresAt = &exp
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		key, err := q.CreateAPIKey(ctx, params)
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "api_key.created", TargetType: "api_key", TargetID: key.ID,
			Metadata: map[string]any{"name": name, "environment": env, "prefix": display},
		}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &createdAPIKeyOutput{Body: CreatedAPIKey{APIKey: toAPIKey(key), Secret: secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revokeAPIKey", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/api-keys/{keyId}", Tags: []string{"API keys"},
		Summary:     "Revoke an API key",
		Description: "Requests using the key fail immediately. Revoking an already-revoked key succeeds.",
		Security:    sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *revokeAPIKeyInput) (*struct{}, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		key, err := q.RevokeAPIKey(ctx, dbq.RevokeAPIKeyParams{ID: in.KeyID, ProjectID: p.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, getErr := q.GetAPIKey(ctx, dbq.GetAPIKeyParams{ID: in.KeyID, ProjectID: p.ID}); getErr == nil {
				return nil, nil // already revoked
			}
			return nil, notFound("API key " + in.KeyID)
		}
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "api_key.revoked", TargetType: "api_key", TargetID: key.ID,
			Metadata: map[string]any{"name": key.Name, "prefix": key.KeyPrefix},
		}); err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	})
}

type WhoAmI struct {
	ProjectID      string `json:"project_id"`
	ProjectName    string `json:"project_name"`
	OrganizationID string `json:"organization_id"`
	Environment    string `json:"environment" enum:"live,test"`
	APIKeyID       string `json:"api_key_id"`
	APIKeyName     string `json:"api_key_name"`
}

func (s *Server) registerDeveloper(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "whoami", Method: http.MethodGet, Path: "/v1/whoami", Tags: []string{"Developer API"},
		Summary:     "Check an API key",
		Description: "Returns the project and environment the API key belongs to. Use it to verify your setup.",
		Security:    apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body WhoAmI }, error) {
		k := principalFrom(ctx).APIKey
		return &struct{ Body WhoAmI }{Body: WhoAmI{
			ProjectID: k.ProjectID, ProjectName: k.ProjectName, OrganizationID: k.OrganizationID,
			Environment: string(k.Environment), APIKeyID: k.ID, APIKeyName: k.Name,
		}}, nil
	})
}
