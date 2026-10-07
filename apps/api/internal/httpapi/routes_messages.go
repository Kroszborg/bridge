package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
)

// Message is defined with the messaging pipeline so webhooks send the same shape.
type Message = messaging.Message

// MessageEvent is one entry in a message's timeline.
type MessageEvent struct {
	Type       string         `json:"type" example:"device_accepted"`
	FromStatus *string        `json:"from_status" nullable:"true"`
	ToStatus   *string        `json:"to_status" nullable:"true"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  time.Time      `json:"created_at"`
}

type MessageDetail struct {
	Message
	Events []MessageEvent `json:"events"`
}

type MessageList struct {
	Data    []Message `json:"data"`
	HasMore bool      `json:"has_more" doc:"Pass the last message's ID as starting_after to fetch the next page."`
}

type UsagePeriod struct {
	Total          int      `json:"total"`
	Delivered      int      `json:"delivered"`
	Sent           int      `json:"sent" doc:"Sent but no delivery report yet."`
	Failed         int      `json:"failed"`
	Pending        int      `json:"pending"`
	SuccessRate    *float64 `json:"success_rate" nullable:"true" doc:"(delivered + sent) / finished messages, 0 to 1."`
	AvgSendSeconds float64  `json:"avg_send_seconds" doc:"Average time from API request to the phone reporting it sent."`
}

type Usage struct {
	Environment   string      `json:"environment" enum:"live,test"`
	Last24Hours   UsagePeriod `json:"last_24_hours"`
	Last30Days    UsagePeriod `json:"last_30_days"`
	DevicesOnline int         `json:"devices_online"`
	DevicesTotal  int         `json:"devices_total"`
}

func toMessage(m dbq.Message) Message { return messaging.View(m) }

func toEvent(e dbq.MessageEvent) MessageEvent {
	out := MessageEvent{Type: e.Type, Detail: map[string]any{}, CreatedAt: e.CreatedAt}
	if e.FromStatus != nil {
		s := string(*e.FromStatus)
		out.FromStatus = &s
	}
	if e.ToStatus != nil {
		s := string(*e.ToStatus)
		out.ToStatus = &s
	}
	_ = json.Unmarshal(e.Detail, &out.Detail)
	return out
}

type sendBody struct {
	To       string         `json:"to" minLength:"3" maxLength:"32" example:"+919876543210" doc:"Destination in E.164 format."`
	Message  string         `json:"message" minLength:"1" maxLength:"1600" example:"Your order has shipped."`
	DeviceID string         `json:"device_id,omitempty" pattern:"^dev_[0-9a-z]{26}$" doc:"Send through this phone only. Leave out to let Bridge pick."`
	SimSlot  int16          `json:"sim_slot,omitempty" minimum:"1" maximum:"2" doc:"SIM to use. Leave out for the device's chosen SIM."`
	Metadata map[string]any `json:"metadata,omitempty" doc:"Your own key-value data, returned with the message. At most 32 keys and 4 KB."`
}

type sendMessageInput struct {
	IdempotencyKey string `header:"Idempotency-Key" maxLength:"255" doc:"Retrying with the same key returns the original message instead of sending twice."`
	Body           sendBody
}

type messageOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed"`
	Body     Message
}

type TestSendInput struct {
	Body struct {
		To      string `json:"to" minLength:"3" maxLength:"32" example:"+919876543210"`
		Message string `json:"message" minLength:"1" maxLength:"1600"`
	}
}

type ListMessagesQuery struct {
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"25"`
	StartingAfter string `query:"starting_after" doc:"A message ID; returns messages created before it."`
	Status        string `query:"status" enum:"created,queued,sending,sent,delivered,failed,received"`
	Direction     string `query:"direction" enum:"outbound,inbound"`
	To            string `query:"to" doc:"Filter by recipient (E.164)."`
	From          string `query:"from" doc:"Filter incoming messages by sender."`
	DeviceID      string `query:"device_id"`
}

type MessagePath struct {
	MessageID string `path:"messageId" pattern:"^msg_[0-9a-z]{26}$" example:"msg_01ja8z3k5wq2v7c9e4r2n0w6yb"`
}

func (s *Server) registerMessages(api huma.API) {
	// ---- Developer API -------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "sendMessage", Method: http.MethodPost, Path: "/v1/messages", Tags: []string{"Developer API"},
		Summary: "Send an SMS",
		Description: "Queues a message and returns immediately with status `queued`. Bridge picks an online phone " +
			"(or the `device_id` you name) and reports every status change. Test keys simulate the whole lifecycle without sending.",
		Security: apiKeyAuth, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusUnauthorized, http.StatusConflict, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *sendMessageInput) (*messageOutput, error) {
		k := principalFrom(ctx).APIKey
		return s.send(ctx, messaging.SendRequest{
			ProjectID: k.ProjectID, Environment: k.Environment, APIKeyID: &k.ID,
			To: in.Body.To, Body: in.Body.Message, DeviceID: optString(in.Body.DeviceID), SimSlot: optInt16(in.Body.SimSlot),
			Metadata: in.Body.Metadata, IdempotencyKey: in.IdempotencyKey,
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "listMessages", Method: http.MethodGet, Path: "/v1/messages", Tags: []string{"Developer API"},
		Summary: "List messages", Description: "Newest first, sent and received. Live keys see live messages; test keys see test messages. Incoming SMS are always live.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *ListMessagesQuery) (*struct{ Body MessageList }, error) {
		k := principalFrom(ctx).APIKey
		return s.listMessages(ctx, k.ProjectID, k.Environment, in)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getMessage", Method: http.MethodGet, Path: "/v1/messages/{messageId}", Tags: []string{"Developer API"},
		Summary: "Get a message", Description: "Includes the full status timeline.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *MessagePath) (*struct{ Body MessageDetail }, error) {
		setResource(ctx, in.MessageID)
		return s.messageDetail(ctx, principalFrom(ctx).APIKey.ProjectID, in.MessageID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "testDevice", Method: http.MethodPost, Path: "/v1/devices/{deviceId}/test", Tags: []string{"Developer API"},
		Summary: "Send a test SMS through a device", Security: apiKeyAuth, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		DevicePath
		TestSendInput
	}) (*messageOutput, error) {
		k := principalFrom(ctx).APIKey
		if _, err := s.deviceInProject(ctx, k.ProjectID, in.DeviceID); err != nil {
			return nil, err
		}
		return s.send(ctx, messaging.SendRequest{
			ProjectID: k.ProjectID, Environment: k.Environment, APIKeyID: &k.ID, To: in.Body.To, Body: in.Body.Message,
			DeviceID: &in.DeviceID, Metadata: map[string]any{"source": "device_test"},
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "getUsage", Method: http.MethodGet, Path: "/v1/usage", Tags: []string{"Developer API"},
		Summary: "Get usage", Description: "Message counts for the key's environment over the last 24 hours and 30 days.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body Usage }, error) {
		k := principalFrom(ctx).APIKey
		return s.usage(ctx, k.ProjectID, k.Environment)
	})

	// ---- Dashboard ------------------------------------------------------
	huma.Register(api, huma.Operation{
		OperationID: "listProjectMessages", Method: http.MethodGet, Path: "/v1/projects/{projectId}/messages", Tags: []string{"Messages"},
		Summary: "List messages", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		ListMessagesQuery
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body MessageList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.listMessages(ctx, in.ProjectID, dbq.APIEnvironment(in.Environment), &in.ListMessagesQuery)
	})

	huma.Register(api, huma.Operation{
		OperationID: "sendProjectMessage", Method: http.MethodPost, Path: "/v1/projects/{projectId}/messages", Tags: []string{"Messages"},
		Summary:     "Send an SMS from the dashboard",
		Description: "The playground's send: same as `POST /v1/messages`, but authenticated by the session and with the environment chosen per request.",
		Security:    sessionAuth, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment    string `query:"environment" enum:"live,test" default:"test"`
		IdempotencyKey string `header:"Idempotency-Key" maxLength:"255"`
		Body           sendBody
	}) (*messageOutput, error) {
		p, err := s.projectForUser(ctx, in.ProjectID)
		if err != nil {
			return nil, err
		}
		env := dbq.ApiEnvironmentTest
		if in.Environment == "live" {
			// Live sends cost money and reach real people: admins only.
			if !hasRole(p.Role, dbq.MemberRoleAdmin) {
				return nil, roleError(dbq.MemberRoleAdmin)
			}
			env = dbq.ApiEnvironmentLive
		}
		return s.send(ctx, messaging.SendRequest{
			ProjectID: in.ProjectID, Environment: env, To: in.Body.To, Body: in.Body.Message,
			DeviceID: optString(in.Body.DeviceID), SimSlot: optInt16(in.Body.SimSlot),
			Metadata: in.Body.Metadata, IdempotencyKey: in.IdempotencyKey,
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectMessage", Method: http.MethodGet, Path: "/v1/projects/{projectId}/messages/{messageId}", Tags: []string{"Messages"},
		Summary: "Get a message", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		MessagePath
	}) (*struct{ Body MessageDetail }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.messageDetail(ctx, in.ProjectID, in.MessageID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "testProjectDevice", Metadata: adminOnly, Method: http.MethodPost, Path: "/v1/projects/{projectId}/devices/{deviceId}/test", Tags: []string{"Devices"},
		Summary: "Send a test SMS through a device", Description: "Sends a real (live) SMS through this phone.",
		Security: sessionAuth, DefaultStatus: http.StatusAccepted, Errors: []int{http.StatusNotFound, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct {
		ProjectDevicePath
		TestSendInput
	}) (*messageOutput, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		if _, err := s.deviceInProject(ctx, in.ProjectID, in.DeviceID); err != nil {
			return nil, err
		}
		return s.send(ctx, messaging.SendRequest{
			ProjectID: in.ProjectID, Environment: dbq.ApiEnvironmentLive, To: in.Body.To, Body: in.Body.Message,
			DeviceID: &in.DeviceID, Metadata: map[string]any{"source": "dashboard_test", "user_id": principalFrom(ctx).User.ID},
		})
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectUsage", Method: http.MethodGet, Path: "/v1/projects/{projectId}/usage", Tags: []string{"Messages"},
		Summary: "Get usage", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body Usage }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.usage(ctx, in.ProjectID, dbq.APIEnvironment(in.Environment))
	})
}

func (s *Server) send(ctx context.Context, req messaging.SendRequest) (*messageOutput, error) {
	m, replayed, err := s.msgs.Send(ctx, req)
	if err != nil {
		return nil, messagingError(err)
	}
	setResource(ctx, m.ID)
	out := &messageOutput{Status: http.StatusAccepted, Body: toMessage(m)}
	if replayed {
		out.Status, out.Replayed = http.StatusOK, "true"
	}
	return out, nil
}

func messagingError(err error) error {
	var ve *messaging.ValidationError
	var rl *messaging.RateLimitError
	switch {
	case errors.As(err, &ve):
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "body." + ve.Field, Message: ve.Message})
	case errors.As(err, &rl):
		secs := max(1, int(rl.RetryAfter.Seconds()))
		msg := "This project queued too many messages in the last hour."
		if rl.Scope == "destination" {
			msg = "Too many messages to this number in the last hour. This limit protects recipients from floods."
		}
		return huma.ErrorWithHeaders(
			Errorf(http.StatusTooManyRequests, CodeRateLimited, msg+" Retry after "+strconv.Itoa(secs)+" seconds."),
			http.Header{"Retry-After": {strconv.Itoa(secs)}})
	case errors.Is(err, messaging.ErrIdempotencyConflict):
		return Errorf(http.StatusConflict, CodeConflict, "This Idempotency-Key was already used with a different request. Use a new key for a new message.")
	}
	return err
}

func (s *Server) listMessages(ctx context.Context, projectID string, env dbq.APIEnvironment, in *ListMessagesQuery) (*struct{ Body MessageList }, error) {
	limit := in.Limit
	if limit == 0 {
		limit = 25
	}
	params := dbq.ListMessagesParams{
		ProjectID: projectID, Environment: &env, RowLimit: int32(limit + 1),
		Recipient: optString(in.To), Sender: optString(in.From), DeviceID: optString(in.DeviceID),
	}
	if in.Direction != "" {
		dir := dbq.MessageDirection(in.Direction)
		params.Direction = &dir
	}
	if in.Status != "" {
		st := dbq.MessageStatus(in.Status)
		params.Status = &st
	}
	if in.StartingAfter != "" {
		cursor, err := s.q.GetMessage(ctx, dbq.GetMessageParams{ID: in.StartingAfter, ProjectID: projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{Location: "query.starting_after", Message: "No message with this ID in the project."})
		}
		if err != nil {
			return nil, err
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListMessages(ctx, params)
	if err != nil {
		return nil, err
	}
	out := &struct{ Body MessageList }{Body: MessageList{Data: make([]Message, 0, len(rows))}}
	if len(rows) > limit {
		rows, out.Body.HasMore = rows[:limit], true
	}
	for _, m := range rows {
		out.Body.Data = append(out.Body.Data, toMessage(m))
	}
	return out, nil
}

func (s *Server) messageDetail(ctx context.Context, projectID, messageID string) (*struct{ Body MessageDetail }, error) {
	m, err := s.q.GetMessage(ctx, dbq.GetMessageParams{ID: messageID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("Message " + messageID)
	}
	if err != nil {
		return nil, err
	}
	events, err := s.q.ListMessageEvents(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	out := &struct{ Body MessageDetail }{Body: MessageDetail{Message: toMessage(m), Events: make([]MessageEvent, 0, len(events))}}
	for _, e := range events {
		out.Body.Events = append(out.Body.Events, toEvent(e))
	}
	return out, nil
}

func (s *Server) usage(ctx context.Context, projectID string, env dbq.APIEnvironment) (*struct{ Body Usage }, error) {
	now := time.Now()
	period := func(since time.Time) (UsagePeriod, error) {
		st, err := s.q.MessageStats(ctx, dbq.MessageStatsParams{ProjectID: projectID, Environment: env, Since: since})
		if err != nil {
			return UsagePeriod{}, err
		}
		p := UsagePeriod{Total: int(st.Total), Delivered: int(st.Delivered), Sent: int(st.Sent), Failed: int(st.Failed), Pending: int(st.Pending), AvgSendSeconds: st.AvgSendSeconds}
		if finished := p.Delivered + p.Sent + p.Failed; finished > 0 {
			rate := float64(p.Delivered+p.Sent) / float64(finished)
			p.SuccessRate = &rate
		}
		return p, nil
	}
	day, err := period(now.Add(-24 * time.Hour))
	if err != nil {
		return nil, err
	}
	month, err := period(now.Add(-30 * 24 * time.Hour))
	if err != nil {
		return nil, err
	}
	devices, err := s.q.ListDevices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	u := Usage{Environment: string(env), Last24Hours: day, Last30Days: month}
	for _, d := range devices {
		if d.RevokedAt != nil {
			continue
		}
		u.DevicesTotal++
		if d.Status == dbq.DeviceStatusOnline {
			u.DevicesOnline++
		}
	}
	return &struct{ Body Usage }{Body: u}, nil
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optInt16(v int16) *int16 {
	if v == 0 {
		return nil
	}
	return &v
}
