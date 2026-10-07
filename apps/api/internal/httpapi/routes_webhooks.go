package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/webhook"
)

const maxWebhooksPerProject = 10

const eventTypesEnum = "message.sent,message.delivered,message.failed,message.received,message.auto_replied,device.online,device.offline,otp.verified,otp.failed,otp.expired,otp.blocked,broadcast.completed"

// WebhookEndpoint is a URL that receives signed event notifications.
type WebhookEndpoint struct {
	ID             string     `json:"id" example:"whk_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	URL            string     `json:"url" example:"https://example.com/webhooks/bridge"`
	Description    string     `json:"description"`
	Events         []string   `json:"events" enum:"message.sent,message.delivered,message.failed,message.received,message.auto_replied,device.online,device.offline,otp.verified,otp.failed,otp.expired,otp.blocked,broadcast.completed" doc:"Subscribed event types. Empty means every event."`
	Enabled        bool       `json:"enabled"`
	DisabledReason *string    `json:"disabled_reason" nullable:"true" doc:"Why Bridge disabled the endpoint, if it did."`
	FailingSince   *time.Time `json:"failing_since" nullable:"true" doc:"Start of the current run of failed deliveries. Bridge disables the endpoint after 5 days."`
	LastSuccessAt  *time.Time `json:"last_success_at" nullable:"true"`
	LastFailureAt  *time.Time `json:"last_failure_at" nullable:"true"`
	CreatedAt      time.Time  `json:"created_at"`
}

type CreatedWebhookEndpoint struct {
	WebhookEndpoint
	Secret string `json:"secret" example:"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw" doc:"Signing secret. Verify the webhook-signature header with it."`
}

type WebhookSecret struct {
	Secret string `json:"secret" example:"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"`
}

// WebhookDelivery is one attempt to deliver an event.
type WebhookDelivery struct {
	ID             string    `json:"id"`
	EventID        string    `json:"event_id" doc:"Sent as the webhook-id header; the same for every attempt of an event."`
	EventType      string    `json:"event_type" example:"message.delivered"`
	Attempt        int32     `json:"attempt"`
	Succeeded      bool      `json:"succeeded"`
	ResponseStatus *int32    `json:"response_status" nullable:"true"`
	ResponseBody   *string   `json:"response_body" nullable:"true" doc:"The first 500 characters of the response."`
	Error          *string   `json:"error" nullable:"true" doc:"Why the request failed before a response arrived."`
	DurationMs     int32     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at"`
}

type WebhookTestResult struct {
	EventID string `json:"event_id"`
}

type WebhookPath struct {
	ProjectPath
	WebhookID string `path:"webhookId" pattern:"^whk_[0-9a-z]{26}$" example:"whk_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

func toWebhook(e dbq.WebhookEndpoint) WebhookEndpoint {
	events := e.Events
	if events == nil {
		events = []string{}
	}
	return WebhookEndpoint{
		ID: e.ID, URL: e.URL, Description: e.Description, Events: events, Enabled: e.Enabled,
		DisabledReason: e.DisabledReason, FailingSince: e.FailingSince, LastSuccessAt: e.LastSuccessAt,
		LastFailureAt: e.LastFailureAt, CreatedAt: e.CreatedAt,
	}
}

func (s *Server) registerWebhooks(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listWebhooks", Method: http.MethodGet, Path: "/v1/projects/{projectId}/webhooks", Tags: []string{"Webhooks"},
		Summary: "List webhook endpoints", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectPath) (*struct{ Body []WebhookEndpoint }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		rows, err := s.q.ListWebhookEndpoints(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		out := make([]WebhookEndpoint, 0, len(rows))
		for _, e := range rows {
			out = append(out, toWebhook(e))
		}
		return &struct{ Body []WebhookEndpoint }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createWebhook", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/webhooks", Tags: []string{"Webhooks"},
		Summary: "Add a webhook endpoint", Description: "Returns the signing secret. You can reveal it again later.",
		Security: sessionAuth, DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Body struct {
			URL         string   `json:"url" minLength:"8" maxLength:"2048" example:"https://example.com/webhooks/bridge"`
			Description string   `json:"description,omitempty" maxLength:"200"`
			Events      []string `json:"events,omitempty" enum:"message.sent,message.delivered,message.failed,message.received,message.auto_replied,device.online,device.offline,otp.verified,otp.failed,otp.expired,otp.blocked,broadcast.completed" doc:"Leave out to receive every event."`
		}
	}) (*struct{ Body CreatedWebhookEndpoint }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		u, err := s.hooks.ValidateURL(in.Body.URL)
		if err != nil {
			return nil, webhookURLError(err)
		}
		events, err := normalizeEvents(in.Body.Events)
		if err != nil {
			return nil, err
		}
		existing, err := s.q.ListWebhookEndpoints(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if len(existing) >= maxWebhooksPerProject {
			return nil, Errorf(http.StatusConflict, CodeConflict, "A project can have at most 10 webhook endpoints. Remove one first.")
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.q.WithTx(tx)
		e, err := q.CreateWebhookEndpoint(ctx, dbq.CreateWebhookEndpointParams{
			ID: id.New(id.Webhook), ProjectID: p.ID, URL: u, Description: strings.TrimSpace(in.Body.Description),
			Events: events, Secret: webhook.NewSecret(),
		})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "webhook.created", TargetType: "webhook", TargetID: e.ID,
			Metadata: map[string]any{"url": e.URL, "events": events},
		}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &struct{ Body CreatedWebhookEndpoint }{Body: CreatedWebhookEndpoint{WebhookEndpoint: toWebhook(e), Secret: e.Secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getWebhook", Method: http.MethodGet, Path: "/v1/projects/{projectId}/webhooks/{webhookId}", Tags: []string{"Webhooks"},
		Summary: "Get a webhook endpoint", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *WebhookPath) (*struct{ Body WebhookEndpoint }, error) {
		_, e, err := s.webhookForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		return &struct{ Body WebhookEndpoint }{Body: toWebhook(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateWebhook", Metadata: adminOnly, Method: http.MethodPatch, Path: "/v1/projects/{projectId}/webhooks/{webhookId}", Tags: []string{"Webhooks"},
		Summary:     "Update a webhook endpoint",
		Description: "Omitted fields stay unchanged. Setting `enabled` to true re-enables an endpoint Bridge disabled and clears its failure streak.",
		Security:    sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		WebhookPath
		Body struct {
			URL         *string  `json:"url,omitempty" minLength:"8" maxLength:"2048"`
			Description *string  `json:"description,omitempty" maxLength:"200"`
			Events      []string `json:"events,omitempty" enum:"message.sent,message.delivered,message.failed,message.received,message.auto_replied,device.online,device.offline,otp.verified,otp.failed,otp.expired,otp.blocked,broadcast.completed" doc:"Replaces the subscription. An empty list subscribes to every event."`
			Enabled     *bool    `json:"enabled,omitempty"`
		}
	}) (*struct{ Body WebhookEndpoint }, error) {
		p, _, err := s.webhookForUser(ctx, &in.WebhookPath)
		if err != nil {
			return nil, err
		}
		params := dbq.UpdateWebhookEndpointParams{ID: in.WebhookID, ProjectID: in.ProjectID, Enabled: in.Body.Enabled}
		if in.Body.URL != nil {
			u, err := s.hooks.ValidateURL(*in.Body.URL)
			if err != nil {
				return nil, webhookURLError(err)
			}
			params.URL = &u
		}
		if in.Body.Description != nil {
			d := strings.TrimSpace(*in.Body.Description)
			params.Description = &d
		}
		if in.Body.Events != nil {
			if params.Events, err = normalizeEvents(in.Body.Events); err != nil {
				return nil, err
			}
		}
		e, err := s.q.UpdateWebhookEndpoint(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound("Webhook " + in.WebhookID)
		}
		if err != nil {
			return nil, err
		}
		meta := map[string]any{"url": e.URL, "events": e.Events, "enabled": e.Enabled}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "webhook.updated", TargetType: "webhook", TargetID: e.ID, Metadata: meta,
		}); err != nil {
			return nil, err
		}
		return &struct{ Body WebhookEndpoint }{Body: toWebhook(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteWebhook", Metadata: adminOnly, Method: http.MethodDelete, Path: "/v1/projects/{projectId}/webhooks/{webhookId}", Tags: []string{"Webhooks"},
		Summary: "Remove a webhook endpoint", Description: "Pending deliveries to it are cancelled.",
		Security: sessionAuth, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *WebhookPath) (*struct{}, error) {
		p, e, err := s.webhookForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if _, err := s.q.DeleteWebhookEndpoint(ctx, dbq.DeleteWebhookEndpointParams{ID: e.ID, ProjectID: p.ID}); err != nil {
			return nil, err
		}
		return nil, s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "webhook.deleted", TargetType: "webhook", TargetID: e.ID,
			Metadata: map[string]any{"url": e.URL},
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "getWebhookSecret", Metadata: adminOnly, Method: http.MethodGet, Path: "/v1/projects/{projectId}/webhooks/{webhookId}/secret", Tags: []string{"Webhooks"},
		Summary: "Reveal the signing secret", Description: "Every reveal is recorded in the audit log.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *WebhookPath) (*struct{ Body WebhookSecret }, error) {
		p, e, err := s.webhookForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "webhook.secret_revealed", TargetType: "webhook", TargetID: e.ID,
		}); err != nil {
			return nil, err
		}
		return &struct{ Body WebhookSecret }{Body: WebhookSecret{Secret: e.Secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rotateWebhookSecret", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/webhooks/{webhookId}/rotate-secret", Tags: []string{"Webhooks"},
		Summary: "Rotate the signing secret", Description: "The old secret stops working at once; deliveries from now on are signed with the new one.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *WebhookPath) (*struct{ Body WebhookSecret }, error) {
		p, _, err := s.webhookForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		e, err := s.q.RotateWebhookSecret(ctx, dbq.RotateWebhookSecretParams{ID: in.WebhookID, ProjectID: p.ID, Secret: webhook.NewSecret()})
		if err != nil {
			return nil, err
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "webhook.secret_rotated", TargetType: "webhook", TargetID: e.ID,
		}); err != nil {
			return nil, err
		}
		return &struct{ Body WebhookSecret }{Body: WebhookSecret{Secret: e.Secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "testWebhook", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/webhooks/{webhookId}/test", Tags: []string{"Webhooks"},
		Summary: "Send a test event", Description: "Queues a `webhook.test` event to this endpoint only. Watch its delivery log for the result.",
		Security: sessionAuth, DefaultStatus: http.StatusAccepted, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *WebhookPath) (*struct{ Body WebhookTestResult }, error) {
		_, e, err := s.webhookForUser(ctx, in)
		if err != nil {
			return nil, err
		}
		if !e.Enabled {
			return nil, Errorf(http.StatusConflict, CodeConflict, "This endpoint is disabled. Enable it first.")
		}
		if err := s.limit(ctx, "webhook-test:"+e.ID, 20, time.Hour); err != nil {
			return nil, err
		}
		ev, err := s.hooks.SendTest(ctx, e)
		if err != nil {
			return nil, err
		}
		return &struct{ Body WebhookTestResult }{Body: WebhookTestResult{EventID: ev.ID}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listWebhookDeliveries", Method: http.MethodGet, Path: "/v1/projects/{projectId}/webhooks/{webhookId}/deliveries", Tags: []string{"Webhooks"},
		Summary: "List delivery attempts", Description: "Newest first.", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		WebhookPath
		Limit int `query:"limit" minimum:"1" maximum:"100" default:"50"`
	}) (*struct{ Body []WebhookDelivery }, error) {
		_, e, err := s.webhookForUser(ctx, &in.WebhookPath)
		if err != nil {
			return nil, err
		}
		limit := in.Limit
		if limit == 0 {
			limit = 50
		}
		rows, err := s.q.ListWebhookDeliveries(ctx, dbq.ListWebhookDeliveriesParams{EndpointID: e.ID, Limit: int32(limit)})
		if err != nil {
			return nil, err
		}
		out := make([]WebhookDelivery, 0, len(rows))
		for _, d := range rows {
			out = append(out, WebhookDelivery{
				ID: d.ID, EventID: d.EventID, EventType: d.EventType, Attempt: d.Attempt, Succeeded: d.Succeeded,
				ResponseStatus: d.ResponseStatus, ResponseBody: d.ResponseBody, Error: d.Error, DurationMs: d.DurationMs, CreatedAt: d.CreatedAt,
			})
		}
		return &struct{ Body []WebhookDelivery }{Body: out}, nil
	})
}

func (s *Server) webhookForUser(ctx context.Context, in *WebhookPath) (dbq.GetProjectForUserRow, dbq.WebhookEndpoint, error) {
	p, err := s.projectForUser(ctx, in.ProjectID)
	if err != nil {
		return p, dbq.WebhookEndpoint{}, err
	}
	e, err := s.q.GetWebhookEndpoint(ctx, dbq.GetWebhookEndpointParams{ID: in.WebhookID, ProjectID: p.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return p, e, notFound("Webhook " + in.WebhookID)
	}
	return p, e, err
}

func normalizeEvents(in []string) ([]string, error) {
	out := []string{}
	for _, e := range in {
		if !webhook.ValidEventType(e) {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
				Location: "body.events", Message: "Unknown event type " + e + ". Use one of " + strings.ReplaceAll(eventTypesEnum, ",", ", ") + ".",
			})
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	slices.Sort(out)
	return out, nil
}

func webhookURLError(err error) error {
	var ve *webhook.ValidationError
	if errors.As(err, &ve) {
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body.url", Message: ve.Message})
	}
	return err
}
