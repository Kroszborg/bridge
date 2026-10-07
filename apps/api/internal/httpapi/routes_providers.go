package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"bridge/internal/auth"
	"bridge/internal/db"
	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
	"bridge/internal/provider"
)

// ProviderAccount is an SMS provider a project can send through. Secrets are
// never returned; credential_hint shows enough to recognise the account.
type ProviderAccount struct {
	ID             string            `json:"id" example:"prv_06ghc2…"`
	Kind           string            `json:"kind" enum:"msg91,twilio,vonage,plivo"`
	Name           string            `json:"name"`
	Enabled        bool              `json:"enabled"`
	Priority       int32             `json:"priority" doc:"Lower is tried first."`
	Config         map[string]string `json:"config" doc:"Non-secret settings such as the sender and templates."`
	CredentialHint string            `json:"credential_hint" doc:"For example the Twilio account SID; never a secret."`
	CallbackURL    string            `json:"callback_url" doc:"Where the provider sends delivery reports. Bridge passes it with each message, except MSG91: set it there as the delivery-report webhook."`
	Callbacks      string            `json:"callbacks" enum:"per_message,account"`
	LastUsedAt     *time.Time        `json:"last_used_at" nullable:"true"`
	LastError      *string           `json:"last_error" nullable:"true" doc:"The last refusal, cleared by the next success."`
	CreatedAt      time.Time         `json:"created_at"`
}

// Routing decides between a project's phones and its providers.
type Routing struct {
	Mode                 string `json:"mode" enum:"phones,phones_then_providers,providers" doc:"phones: never use providers. phones_then_providers: providers take messages no phone can send. providers: providers send everything."`
	FallbackAfterSeconds int    `json:"fallback_after_seconds" minimum:"0" maximum:"3600" doc:"With phones_then_providers, how long a message may wait for a phone before a provider takes it."`
	SecretKeySet         bool   `json:"secret_key_set" readOnly:"true" doc:"Whether this server can store provider credentials (BRIDGE_SECRET_KEY)."`
}

type ProviderAccountPath struct {
	ProjectPath
	ProviderID string `path:"providerId" pattern:"^prv_[0-9a-z]{26}$"`
}

// ProviderInput is a provider account's editable settings.
type ProviderInput struct {
	Name        *string           `json:"name,omitempty" maxLength:"60"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Priority    *int32            `json:"priority,omitempty" minimum:"0" maximum:"100"`
	Credentials map[string]string `json:"credentials,omitempty" doc:"Replaces every stored credential. Write-only."`
	Config      map[string]string `json:"config,omitempty" doc:"Replaces the whole config."`
}

func (s *Server) toProviderAccount(row dbq.ProviderAccount) ProviderAccount {
	out := ProviderAccount{
		ID: row.ID, Kind: row.Kind, Name: row.Name, Enabled: row.Enabled, Priority: row.Priority, Config: map[string]string{},
		CredentialHint: row.CredentialHint, CallbackURL: s.providers.CallbackURL(row),
		LastUsedAt: row.LastUsedAt, LastError: row.LastError, CreatedAt: row.CreatedAt,
	}
	_ = json.Unmarshal(row.Config, &out.Config)
	if spec, ok := provider.SpecFor(provider.Kind(row.Kind)); ok {
		out.Callbacks = spec.Callbacks
	}
	return out
}

func noSecretKey() error {
	return Errorf(http.StatusConflict, CodeConflict,
		"This server cannot store provider credentials yet. Set BRIDGE_SECRET_KEY (openssl rand -base64 32) and restart Bridge.")
}

func providerFieldError(err error) error {
	var fe *provider.FieldError
	if errors.As(err, &fe) {
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body." + fe.Field, Message: fe.Message})
	}
	return err
}

func (s *Server) registerProviders(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listProviderKinds", Method: http.MethodGet, Path: "/v1/provider-kinds", Tags: []string{"Providers"},
		Summary: "Supported SMS providers and their settings", Security: sessionAuth,
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body []provider.ProviderSpec }, error) {
		return &struct{ Body []provider.ProviderSpec }{Body: provider.Specs}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getRouting", Method: http.MethodGet, Path: "/v1/projects/{projectId}/routing", Tags: []string{"Providers"},
		Summary: "Get routing", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body Routing }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		out := Routing{Mode: messaging.RoutePhones, FallbackAfterSeconds: 60, SecretKeySet: s.providers.Ready()}
		if r, err := s.q.GetRouting(ctx, in.ProjectID); err == nil {
			out.Mode, out.FallbackAfterSeconds = r.Mode, int(r.FallbackAfterSeconds)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return &struct{ Body Routing }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateRouting", Metadata: adminOnly, Method: http.MethodPut, Path: "/v1/projects/{projectId}/routing", Tags: []string{"Providers"},
		Summary: "Update routing", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body Routing
	}) (*struct{ Body Routing }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		r, err := s.q.UpsertRouting(ctx, dbq.UpsertRoutingParams{ProjectID: in.ProjectID, Mode: in.Body.Mode, FallbackAfterSeconds: int32(in.Body.FallbackAfterSeconds)})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "routing.updated",
			TargetType: "project", TargetID: p.ID, Metadata: map[string]any{"mode": r.Mode, "fallback_after_seconds": r.FallbackAfterSeconds}}); err != nil {
			return nil, err
		}
		return &struct{ Body Routing }{Body: Routing{Mode: r.Mode, FallbackAfterSeconds: int(r.FallbackAfterSeconds), SecretKeySet: s.providers.Ready()}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listProviders", Method: http.MethodGet, Path: "/v1/projects/{projectId}/providers", Tags: []string{"Providers"},
		Summary: "List providers", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body []ProviderAccount }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		rows, err := s.q.ListProviderAccounts(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out := make([]ProviderAccount, 0, len(rows))
		for _, r := range rows {
			out = append(out, s.toProviderAccount(r))
		}
		return &struct{ Body []ProviderAccount }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "addProvider", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/providers", Tags: []string{"Providers"},
		Summary: "Add a provider", Description: "One account per provider kind. Credentials are encrypted with BRIDGE_SECRET_KEY and never returned.",
		Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body struct {
			Kind string `json:"kind" enum:"msg91,twilio,vonage,plivo"`
			ProviderInput
		}
	}) (*struct{ Body ProviderAccount }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		if !s.providers.Ready() {
			return nil, noSecretKey()
		}
		kind := provider.Kind(in.Body.Kind)
		if err := provider.Validate(kind, in.Body.Credentials, in.Body.Config); err != nil {
			return nil, providerFieldError(err)
		}
		spec, _ := provider.SpecFor(kind)
		accountID := id.New(id.ProviderAccount)
		sealed, err := s.providers.Seal(accountID, in.Body.Credentials)
		if err != nil {
			return nil, err
		}
		config, _ := json.Marshal(in.Body.Config)
		name := spec.Name
		if in.Body.Name != nil && strings.TrimSpace(*in.Body.Name) != "" {
			name = strings.TrimSpace(*in.Body.Name)
		}
		var priority int32
		if in.Body.Priority != nil {
			priority = *in.Body.Priority
		}
		row, err := s.q.InsertProviderAccount(ctx, dbq.InsertProviderAccountParams{
			ID: accountID, ProjectID: in.ProjectID, Kind: string(kind), Name: name, Priority: priority,
			Credentials: sealed, Config: config, CredentialHint: provider.Hint(kind, in.Body.Credentials),
			CallbackToken: auth.RandomString(32),
		})
		if db.IsUniqueViolation(err, "") {
			return nil, Errorf(http.StatusConflict, CodeConflict, spec.Name+" is already set up for this project. Edit it instead.")
		}
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "provider.added",
			TargetType: "provider", TargetID: row.ID, Metadata: map[string]any{"kind": row.Kind, "name": row.Name}}); err != nil {
			return nil, err
		}
		return &struct{ Body ProviderAccount }{Body: s.toProviderAccount(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateProvider", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/providers/{providerId}", Tags: []string{"Providers"},
		Summary: "Update a provider", Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProviderAccountPath
		Body ProviderInput
	}) (*struct{ Body ProviderAccount }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		row, err := s.q.GetProviderAccount(ctx, dbq.GetProviderAccountParams{ID: in.ProviderID, ProjectID: in.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Provider " + in.ProviderID)
		}
		if err != nil {
			return nil, err
		}
		params := dbq.UpdateProviderAccountParams{ID: row.ID, ProjectID: row.ProjectID, Enabled: in.Body.Enabled, Priority: in.Body.Priority}
		if in.Body.Name != nil && strings.TrimSpace(*in.Body.Name) != "" {
			name := strings.TrimSpace(*in.Body.Name)
			params.Name = &name
		}
		if in.Body.Credentials != nil || in.Body.Config != nil {
			if !s.providers.Ready() {
				return nil, noSecretKey()
			}
			creds := in.Body.Credentials
			if creds == nil {
				if creds, err = s.providers.Open(row); err != nil {
					return nil, Errorf(http.StatusConflict, CodeConflict, "The stored credentials cannot be read with this server's BRIDGE_SECRET_KEY. Enter them again.")
				}
			}
			config := in.Body.Config
			if config == nil {
				_ = json.Unmarshal(row.Config, &config)
			}
			if err := provider.Validate(provider.Kind(row.Kind), creds, config); err != nil {
				return nil, providerFieldError(err)
			}
			if in.Body.Credentials != nil {
				sealed, err := s.providers.Seal(row.ID, creds)
				if err != nil {
					return nil, err
				}
				hint := provider.Hint(provider.Kind(row.Kind), creds)
				params.Credentials, params.CredentialHint = sealed, &hint
			}
			if in.Body.Config != nil {
				params.Config, _ = json.Marshal(config)
			}
		}
		row, err = s.q.UpdateProviderAccount(ctx, params)
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "provider.updated",
			TargetType: "provider", TargetID: row.ID, Metadata: map[string]any{
				"kind": row.Kind, "enabled": row.Enabled, "credentials_changed": in.Body.Credentials != nil,
			}}); err != nil {
			return nil, err
		}
		return &struct{ Body ProviderAccount }{Body: s.toProviderAccount(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "removeProvider", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/providers/{providerId}", Tags: []string{"Providers"},
		Summary: "Remove a provider", Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProviderAccountPath) (*struct{}, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		n, err := s.q.DeleteProviderAccount(ctx, dbq.DeleteProviderAccountParams{ID: in.ProviderID, ProjectID: in.ProjectID})
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, notFound("Provider " + in.ProviderID)
		}
		if err := s.audit(ctx, s.q, auditEntry{OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "provider.removed",
			TargetType: "provider", TargetID: in.ProviderID}); err != nil {
			return nil, err
		}
		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "checkProvider", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/providers/{providerId}/check", Tags: []string{"Providers"},
		Summary: "Check a provider's credentials", Description: "Asks the provider about the account (name or balance) without sending anything.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *ProviderAccountPath) (*struct {
		Body struct {
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		}
	}, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		row, err := s.q.GetProviderAccount(ctx, dbq.GetProviderAccountParams{ID: in.ProviderID, ProjectID: in.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Provider " + in.ProviderID)
		}
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
			}
		}{}
		client, err := s.providers.Client(row)
		if err != nil {
			out.Body.Detail = err.Error()
			return out, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		detail, err := client.Check(ctx)
		var pe *provider.Error
		switch {
		case errors.As(err, &pe):
			out.Body.Detail = pe.Message
		case err != nil:
			out.Body.Detail = err.Error()
		default:
			out.Body.OK, out.Body.Detail = true, detail
		}
		return out, nil
	})
}

// providerCallback receives delivery reports at /v1/provider-callbacks/{id}/{token}.
// The token in the URL is the account's secret; it is compared in constant time.
func (s *Server) providerCallback(w http.ResponseWriter, r *http.Request) {
	row, err := s.q.GetProviderAccountByID(r.Context(), chi.URLParam(r, "providerId"))
	if err != nil || subtle.ConstantTimeCompare([]byte(row.CallbackToken), []byte(chi.URLParam(r, "token"))) != 1 {
		writeRawError(w, r, http.StatusNotFound, CodeNotFound, "Unknown callback URL.")
		return
	}
	updates, err := provider.ParseCallback(provider.Kind(row.Kind), r)
	if err != nil {
		writeRawError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Could not read the delivery report.")
		return
	}
	if err := s.msgs.ProviderReport(r.Context(), row.ID, updates); err != nil {
		s.log.Error("apply provider report", "provider_id", row.ID, "error", err)
		writeRawError(w, r, http.StatusInternalServerError, CodeInternal, "Could not apply the delivery report.")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
