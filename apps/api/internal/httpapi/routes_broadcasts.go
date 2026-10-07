package httpapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/broadcast"
	"bridge/internal/db/dbq"
)

// Broadcast is defined with the broadcast service so the webhook carries the same shape.
type Broadcast = broadcast.Broadcast

// BroadcastPreview is what a dry run returns.
type BroadcastPreview = broadcast.BroadcastPreview

type BroadcastList struct {
	Data    []Broadcast `json:"data"`
	HasMore bool        `json:"has_more" doc:"Pass the last broadcast's ID as starting_after to fetch the next page."`
}

type BroadcastRecipientInput struct {
	To   string            `json:"to" minLength:"3" maxLength:"32" example:"+919876543210"`
	Vars map[string]string `json:"vars,omitempty" doc:"Values for the template's {placeholders}. Every placeholder needs a value for every recipient."`
}

type BroadcastCreateInput struct {
	Name        string                    `json:"name,omitempty" maxLength:"100" example:"October newsletter"`
	Template    string                    `json:"template" minLength:"1" maxLength:"1600" example:"Hi {name}, your order {order} has shipped."`
	Recipients  []BroadcastRecipientInput `json:"recipients" minItems:"1" maxItems:"10000" doc:"Up to 10,000 rows. Numbers are normalised to E.164; repeated numbers are sent once; opted-out numbers are skipped."`
	DeviceID    string                    `json:"device_id,omitempty" pattern:"^dev_[0-9a-z]{26}$" doc:"Send every message through this phone. Leave out to let Bridge pick per message."`
	ScheduledAt *time.Time                `json:"scheduled_at,omitempty" doc:"Send later, at this time (RFC 3339), at most a year ahead. Leave out to start now."`
	DryRun      bool                      `json:"dry_run,omitempty" doc:"Validate and preview without creating anything: returns 200 with a preview instead of 201 with the broadcast."`
}

type BroadcastPath struct {
	BroadcastID string `path:"broadcastId" pattern:"^brd_[0-9a-z]{26}$" example:"brd_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

type ProjectBroadcastPath struct {
	ProjectPath
	BroadcastPath
}

type ListBroadcastsQuery struct {
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
	StartingAfter string `query:"starting_after" doc:"A broadcast ID; returns broadcasts created before it."`
	Status        string `query:"status" enum:"scheduled,sending,completed,canceled"`
}

type broadcastCreateOutput struct {
	Status int
	Body   any
}

// createResponses documents the two shapes of the create endpoint.
func createResponses(api huma.API) map[string]*huma.Response {
	reg := api.OpenAPI().Components.Schemas
	resp := func(desc string, t reflect.Type) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{
			"application/json": {Schema: reg.Schema(t, true, "")},
		}}
	}
	return map[string]*huma.Response{
		"201": resp("The broadcast was created.", reflect.TypeFor[Broadcast]()),
		"200": resp("dry_run: what the broadcast would send. Nothing was created.", reflect.TypeFor[BroadcastPreview]()),
	}
}

func broadcastError(err error, broadcastID string) error {
	switch {
	case errors.Is(err, broadcast.ErrNotFound):
		return notFound("Broadcast " + broadcastID)
	case errors.Is(err, broadcast.ErrNotCancelable):
		return Errorf(http.StatusConflict, CodeConflict, "This broadcast already finished or was canceled.")
	}
	return messagingError(err)
}

func (s *Server) registerBroadcasts(api huma.API) {
	errs := []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests}

	// ---- Developer API -------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "createBroadcast", Method: http.MethodPost, Path: "/v1/broadcasts", Tags: []string{"Broadcasts"},
		Summary: "Send a broadcast",
		Description: "Sends one template to up to 10,000 recipients, filling `{placeholders}` from each recipient's `vars`. " +
			"Messages go through the normal pipeline (routing, phones' send limits, providers) with `metadata.broadcast_id`, " +
			"created gradually so phones are not overloaded. Broadcast messages do not count against the per-request hourly " +
			"message limits; a project may create 20 broadcasts an hour. Use `dry_run` to preview.",
		Security: apiKeyAuth, DefaultStatus: http.StatusCreated, MaxBodyBytes: 4 << 20, Responses: createResponses(api),
		Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct{ Body BroadcastCreateInput }) (*broadcastCreateOutput, error) {
		k := principalFrom(ctx).APIKey
		return s.createBroadcast(ctx, broadcast.CreateRequest{ProjectID: k.ProjectID, Environment: k.Environment, APIKeyID: &k.ID}, in.Body, nil)
	})

	huma.Register(api, huma.Operation{
		OperationID: "listBroadcasts", Method: http.MethodGet, Path: "/v1/broadcasts", Tags: []string{"Broadcasts"},
		Summary: "List broadcasts", Description: "Newest first, in the key's environment.", Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *ListBroadcastsQuery) (*struct{ Body BroadcastList }, error) {
		k := principalFrom(ctx).APIKey
		return s.listBroadcasts(ctx, k.ProjectID, k.Environment, in)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getBroadcast", Method: http.MethodGet, Path: "/v1/broadcasts/{broadcastId}", Tags: []string{"Broadcasts"},
		Summary: "Get a broadcast", Description: "Includes live counts. The broadcast's messages carry `metadata.broadcast_id`.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *BroadcastPath) (*struct{ Body Broadcast }, error) {
		k := principalFrom(ctx).APIKey
		setResource(ctx, in.BroadcastID)
		b, err := s.tools.Broadcasts.Get(ctx, k.ProjectID, in.BroadcastID)
		if err != nil || b.Environment != k.Environment {
			return nil, broadcastError(cmpErr(err, broadcast.ErrNotFound), in.BroadcastID)
		}
		return s.broadcastOut(ctx, b)
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancelBroadcast", Method: http.MethodPost, Path: "/v1/broadcasts/{broadcastId}/cancel", Tags: []string{"Broadcasts"},
		Summary:     "Cancel a broadcast",
		Description: "Recipients not yet sent to are skipped and messages still waiting for a phone are canceled. Messages a phone or provider already took finish normally.",
		Security:    apiKeyAuth, Errors: errs,
	}, func(ctx context.Context, in *BroadcastPath) (*struct{ Body Broadcast }, error) {
		k := principalFrom(ctx).APIKey
		setResource(ctx, in.BroadcastID)
		if b, err := s.tools.Broadcasts.Get(ctx, k.ProjectID, in.BroadcastID); err != nil || b.Environment != k.Environment {
			return nil, broadcastError(cmpErr(err, broadcast.ErrNotFound), in.BroadcastID)
		}
		b, err := s.tools.Broadcasts.Cancel(ctx, k.ProjectID, in.BroadcastID)
		if err != nil {
			return nil, broadcastError(err, in.BroadcastID)
		}
		return s.broadcastOut(ctx, b)
	})

	// ---- Dashboard ------------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "createProjectBroadcast", Method: http.MethodPost, Path: "/v1/projects/{projectId}/broadcasts", Tags: []string{"Broadcasts"},
		Summary: "Send a broadcast from the dashboard", Description: "Same as `POST /v1/broadcasts`. Live broadcasts need an admin.",
		Security: sessionAuth, DefaultStatus: http.StatusCreated, MaxBodyBytes: 4 << 20, Responses: createResponses(api),
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment string `query:"environment" enum:"live,test" default:"test"`
		Body        BroadcastCreateInput
	}) (*broadcastCreateOutput, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		env := dbq.APIEnvironment(in.Environment)
		if err := liveNeedsAdmin(p, env); err != nil {
			return nil, err
		}
		userID := principalFrom(ctx).User.ID
		return s.createBroadcast(ctx, broadcast.CreateRequest{ProjectID: p.ID, Environment: env, UserID: &userID}, in.Body, &p)
	})

	huma.Register(api, huma.Operation{
		OperationID: "listProjectBroadcasts", Method: http.MethodGet, Path: "/v1/projects/{projectId}/broadcasts", Tags: []string{"Broadcasts"},
		Summary: "List broadcasts", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		ListBroadcastsQuery
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body BroadcastList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.listBroadcasts(ctx, in.ProjectID, dbq.APIEnvironment(in.Environment), &in.ListBroadcastsQuery)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectBroadcast", Method: http.MethodGet, Path: "/v1/projects/{projectId}/broadcasts/{broadcastId}", Tags: []string{"Broadcasts"},
		Summary: "Get a broadcast", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *ProjectBroadcastPath) (*struct{ Body Broadcast }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		b, err := s.tools.Broadcasts.Get(ctx, in.ProjectID, in.BroadcastID)
		if err != nil {
			return nil, broadcastError(err, in.BroadcastID)
		}
		return s.broadcastOut(ctx, b)
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancelProjectBroadcast", Method: http.MethodPost, Path: "/v1/projects/{projectId}/broadcasts/{broadcastId}/cancel", Tags: []string{"Broadcasts"},
		Summary: "Cancel a broadcast", Description: "Live broadcasts need an admin.", Security: sessionAuth,
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *ProjectBroadcastPath) (*struct{ Body Broadcast }, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		b, err := s.tools.Broadcasts.Get(ctx, in.ProjectID, in.BroadcastID)
		if err != nil {
			return nil, broadcastError(err, in.BroadcastID)
		}
		if err := liveNeedsAdmin(p, b.Environment); err != nil {
			return nil, err
		}
		if b, err = s.tools.Broadcasts.Cancel(ctx, in.ProjectID, in.BroadcastID); err != nil {
			return nil, broadcastError(err, in.BroadcastID)
		}
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "broadcast.canceled", TargetType: "broadcast", TargetID: b.ID,
			Metadata: map[string]any{"environment": b.Environment, "name": b.Name},
		}); err != nil {
			return nil, err
		}
		return s.broadcastOut(ctx, b)
	})
}

// liveNeedsAdmin: live sends cost money and reach real people, so changing
// them from the dashboard needs an admin.
func liveNeedsAdmin(p dbq.GetProjectForUserRow, env dbq.APIEnvironment) error {
	if env == dbq.ApiEnvironmentLive && !hasRole(p.Role, dbq.MemberRoleAdmin) {
		return roleError(dbq.MemberRoleAdmin)
	}
	return nil
}

// cmpErr returns err, or fallback when err is nil.
func cmpErr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (s *Server) createBroadcast(ctx context.Context, req broadcast.CreateRequest, in BroadcastCreateInput, p *dbq.GetProjectForUserRow) (*broadcastCreateOutput, error) {
	req.Name, req.Template, req.DeviceID, req.ScheduledAt, req.DryRun = in.Name, in.Template, optString(in.DeviceID), in.ScheduledAt, in.DryRun
	req.Recipients = make([]broadcast.Recipient, 0, len(in.Recipients))
	for _, r := range in.Recipients {
		req.Recipients = append(req.Recipients, broadcast.Recipient{To: r.To, Vars: r.Vars})
	}
	b, preview, err := s.tools.Broadcasts.Create(ctx, req)
	if err != nil {
		return nil, messagingError(err)
	}
	if in.DryRun {
		return &broadcastCreateOutput{Status: http.StatusOK, Body: preview}, nil
	}
	setResource(ctx, b.ID)
	if p != nil {
		if err := s.audit(ctx, s.q, auditEntry{
			OrganizationID: p.OrganizationID, ProjectID: p.ID, Action: "broadcast.created", TargetType: "broadcast", TargetID: b.ID,
			Metadata: map[string]any{"environment": b.Environment, "name": b.Name, "recipients": b.TotalRecipients, "scheduled_at": b.ScheduledAt},
		}); err != nil {
			return nil, err
		}
	}
	view, err := s.tools.Broadcasts.View(ctx, b)
	if err != nil {
		return nil, err
	}
	return &broadcastCreateOutput{Status: http.StatusCreated, Body: view}, nil
}

func (s *Server) broadcastOut(ctx context.Context, b dbq.Broadcast) (*struct{ Body Broadcast }, error) {
	view, err := s.tools.Broadcasts.View(ctx, b)
	if err != nil {
		return nil, err
	}
	return &struct{ Body Broadcast }{Body: view}, nil
}

func (s *Server) listBroadcasts(ctx context.Context, projectID string, env dbq.APIEnvironment, in *ListBroadcastsQuery) (*struct{ Body BroadcastList }, error) {
	rows, more, err := s.tools.Broadcasts.List(ctx, broadcast.ListRequest{
		ProjectID: projectID, Environment: env, Status: in.Status, StartingAfter: in.StartingAfter, Limit: cmpOrInt(in.Limit, 25),
	})
	if err != nil {
		return nil, queryError(err)
	}
	views, err := s.tools.Broadcasts.Views(ctx, rows)
	if err != nil {
		return nil, err
	}
	return &struct{ Body BroadcastList }{Body: BroadcastList{Data: views, HasMore: more}}, nil
}

func cmpOrInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
