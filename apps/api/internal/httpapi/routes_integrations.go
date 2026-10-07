package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
	"bridge/internal/otp"
	"bridge/internal/webhook"
)

// IntegrationSupabase receives Supabase Auth's Send SMS hook.
const IntegrationSupabase = "supabase_send_sms"

// Integration connects another service to the project.
type Integration struct {
	ID          string     `json:"id" example:"int_06ghc2…"`
	Kind        string     `json:"kind" enum:"supabase_send_sms"`
	Environment string     `json:"environment" enum:"live,test" doc:"test sends nothing: messages go to the simulator."`
	HookURL     string     `json:"hook_url" doc:"Paste this into the other service."`
	SecretSet   bool       `json:"secret_set" doc:"Whether the other service's signing secret is stored. Requests are refused until it is."`
	LastUsedAt  *time.Time `json:"last_used_at" nullable:"true"`
	LastError   *string    `json:"last_error" nullable:"true"`
	CreatedAt   time.Time  `json:"created_at"`
}

type IntegrationPath struct {
	ProjectPath
	IntegrationID string `path:"integrationId" pattern:"^int_[0-9a-z]{26}$"`
}

func (s *Server) toIntegration(row dbq.Integration) Integration {
	return Integration{
		ID: row.ID, Kind: row.Kind, Environment: string(row.Environment), SecretSet: row.Secret != nil,
		HookURL:    strings.TrimRight(s.cfg.PublicURL.String(), "/") + "/v1/hooks/supabase/" + row.ID,
		LastUsedAt: row.LastUsedAt, LastError: row.LastError, CreatedAt: row.CreatedAt,
	}
}

// supabaseSecret accepts Supabase's "v1,whsec_<base64>" format and returns the whsec_ part.
func supabaseSecret(raw string) (string, bool) {
	secret := strings.TrimPrefix(strings.TrimSpace(raw), "v1,")
	b64, ok := strings.CutPrefix(secret, webhook.SecretPrefix)
	if !ok {
		return "", false
	}
	if key, err := base64.StdEncoding.DecodeString(b64); err != nil || len(key) < 16 {
		return "", false
	}
	return secret, true
}

func (s *Server) registerIntegrations(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listIntegrations", Method: http.MethodGet, Path: "/v1/projects/{projectId}/integrations", Tags: []string{"Integrations"},
		Summary: "List integrations", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body []Integration }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		rows, err := s.q.ListIntegrations(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out := make([]Integration, 0, len(rows))
		for _, r := range rows {
			out = append(out, s.toIntegration(r))
		}
		return &struct{ Body []Integration }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createIntegration", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/integrations", Tags: []string{"Integrations"},
		Summary: "Create an integration", Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body struct {
			Kind        string `json:"kind" enum:"supabase_send_sms"`
			Environment string `json:"environment,omitempty" enum:"live,test" default:"live"`
		}
	}) (*struct{ Body Integration }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		if !s.providers.Ready() {
			return nil, noSecretKey()
		}
		env := dbq.ApiEnvironmentLive
		if in.Body.Environment == "test" {
			env = dbq.ApiEnvironmentTest
		}
		row, err := s.q.InsertIntegration(ctx, dbq.InsertIntegrationParams{ID: id.New(id.Integration), ProjectID: in.ProjectID, Kind: in.Body.Kind, Environment: env})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "integration.created",
			TargetType: "integration", TargetID: row.ID, Metadata: map[string]any{"kind": row.Kind, "environment": row.Environment}}); err != nil {
			return nil, err
		}
		return &struct{ Body Integration }{Body: s.toIntegration(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateIntegration", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/integrations/{integrationId}", Tags: []string{"Integrations"},
		Summary: "Update an integration", Description: "Set the signing secret the other service generated, or switch between live and test.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		IntegrationPath
		Body struct {
			Secret      string `json:"secret,omitempty" maxLength:"200" doc:"Supabase's hook secret, v1,whsec_…. Write-only."`
			Environment string `json:"environment,omitempty" enum:"live,test"`
		}
	}) (*struct{ Body Integration }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		row, err := s.q.GetIntegration(ctx, dbq.GetIntegrationParams{ID: in.IntegrationID, ProjectID: in.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Integration " + in.IntegrationID)
		}
		if err != nil {
			return nil, err
		}
		if in.Body.Secret != "" {
			secret, ok := supabaseSecret(in.Body.Secret)
			if !ok {
				return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
					Location: "body.secret", Message: "Paste the secret exactly as Supabase shows it: v1,whsec_ followed by base64."})
			}
			sealed, err := s.providers.SealBytes(row.ID, []byte(secret))
			if err != nil {
				return nil, noSecretKey()
			}
			if row, err = s.q.SetIntegrationSecret(ctx, dbq.SetIntegrationSecretParams{ID: row.ID, ProjectID: row.ProjectID, Secret: sealed}); err != nil {
				return nil, err
			}
		}
		if in.Body.Environment != "" {
			if row, err = s.q.SetIntegrationEnvironment(ctx, dbq.SetIntegrationEnvironmentParams{
				ID: row.ID, ProjectID: row.ProjectID, Environment: dbq.APIEnvironment(in.Body.Environment)}); err != nil {
				return nil, err
			}
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "integration.updated",
			TargetType: "integration", TargetID: row.ID, Metadata: map[string]any{"secret_changed": in.Body.Secret != "", "environment": row.Environment}}); err != nil {
			return nil, err
		}
		return &struct{ Body Integration }{Body: s.toIntegration(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteIntegration", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/integrations/{integrationId}", Tags: []string{"Integrations"},
		Summary: "Delete an integration", Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *IntegrationPath) (*struct{}, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		n, err := s.q.DeleteIntegration(ctx, dbq.DeleteIntegrationParams{ID: in.IntegrationID, ProjectID: in.ProjectID})
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, notFound("Integration " + in.IntegrationID)
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "integration.deleted",
			TargetType: "integration", TargetID: in.IntegrationID}); err != nil {
			return nil, err
		}
		return &struct{}{}, nil
	})
}

// supabaseHookError answers in the shape Supabase Auth shows to its caller.
func supabaseHookError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"http_code": status, "message": message}})
}

// supabaseSendSMS handles Supabase Auth's Send SMS hook: Supabase generates
// the code and checks it later; Bridge delivers it. The request is signed
// with Standard Webhooks using the secret stored on the integration.
func (s *Server) supabaseSendSMS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	row, err := s.q.GetIntegrationByID(ctx, chi.URLParam(r, "integrationId"))
	if err != nil || row.Kind != IntegrationSupabase {
		supabaseHookError(w, http.StatusNotFound, "Unknown Bridge hook URL.")
		return
	}
	if row.Secret == nil {
		supabaseHookError(w, http.StatusServiceUnavailable, "Bridge is not set up yet: paste Supabase's hook secret into the Bridge dashboard.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		supabaseHookError(w, http.StatusBadRequest, "Could not read the request.")
		return
	}
	secret, err := s.providers.OpenBytes(row.ID, row.Secret)
	if err != nil {
		s.log.Error("open integration secret", "integration_id", row.ID, "error", err)
		supabaseHookError(w, http.StatusInternalServerError, "Bridge cannot read the stored hook secret. Paste it again in the dashboard.")
		return
	}
	if err := webhook.Verify(string(secret), r.Header, body, webhook.DefaultTolerance, time.Now()); err != nil {
		supabaseHookError(w, http.StatusUnauthorized, "The signature does not match the secret stored in Bridge.")
		return
	}
	var payload struct {
		User struct {
			ID    string `json:"id"`
			Phone string `json:"phone"`
		} `json:"user"`
		SMS struct {
			OTP string `json:"otp"`
		} `json:"sms"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.User.Phone == "" || payload.SMS.OTP == "" {
		supabaseHookError(w, http.StatusBadRequest, "Expected user.phone and sms.otp.")
		return
	}
	to := payload.User.Phone
	if !strings.HasPrefix(to, "+") {
		to = "+" + to
	}
	project, err := s.q.GetProjectByID(ctx, row.ProjectID)
	if err != nil {
		supabaseHookError(w, http.StatusInternalServerError, "Could not load the Bridge project.")
		return
	}
	msg, err := s.otp.DeliverCode(ctx, otp.DeliverRequest{
		ProjectID: row.ProjectID, ProjectName: project.Name, Environment: row.Environment, To: to, Code: payload.SMS.OTP,
		Metadata: map[string]any{"source": "supabase", "integration_id": row.ID, "supabase_user_id": payload.User.ID},
	})
	if err != nil {
		status, text := http.StatusInternalServerError, "Bridge could not queue the SMS."
		var ve *messaging.ValidationError
		var rl *messaging.RateLimitError
		switch {
		case errors.As(err, &ve):
			status, text = http.StatusBadRequest, ve.Message
		case errors.As(err, &rl):
			status, text = http.StatusTooManyRequests, "Too many messages to this number. Try again later."
		default:
			s.log.Error("supabase hook send", "integration_id", row.ID, "error", err)
		}
		_ = s.q.RecordIntegrationUse(ctx, dbq.RecordIntegrationUseParams{ID: row.ID, Error: &text})
		supabaseHookError(w, status, text)
		return
	}
	_ = s.q.RecordIntegrationUse(ctx, dbq.RecordIntegrationUseParams{ID: row.ID})
	s.log.Info("supabase hook queued sms", "integration_id", row.ID, "message_id", msg.ID)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}
