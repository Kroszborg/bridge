package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
)

// RequestLog is one developer API request. Bodies, headers and query strings
// are never stored.
type RequestLog struct {
	ID          string    `json:"id" example:"log_06ggn6t2c8v0b4n6m8q0s2u4w6"`
	RequestID   string    `json:"request_id" doc:"The X-Request-Id of the request; error responses carry it too."`
	APIKeyID    *string   `json:"api_key_id" nullable:"true"`
	Environment string    `json:"environment" enum:"live,test"`
	Method      string    `json:"method" example:"POST"`
	Path        string    `json:"path" example:"/v1/messages"`
	Status      int32     `json:"status" example:"202"`
	ErrorCode   *string   `json:"error_code" nullable:"true" example:"validation_failed"`
	DurationMs  int32     `json:"duration_ms"`
	IP          *string   `json:"ip" nullable:"true"`
	UserAgent   *string   `json:"user_agent" nullable:"true"`
	ResourceID  *string   `json:"resource_id" nullable:"true" doc:"The resource the request created or read, such as a message ID."`
	CreatedAt   time.Time `json:"created_at"`
}

type RequestLogList struct {
	Data    []RequestLog `json:"data"`
	HasMore bool         `json:"has_more" doc:"Pass the last entry's ID as starting_after to fetch the next page."`
}

func toRequestLog(l dbq.APIRequestLog) RequestLog {
	out := RequestLog{
		ID: l.ID, RequestID: l.RequestID, APIKeyID: l.APIKeyID, Environment: l.Environment, Method: l.Method,
		Path: l.Path, Status: l.Status, ErrorCode: l.ErrorCode, DurationMs: l.DurationMs, UserAgent: l.UserAgent,
		ResourceID: l.ResourceID, CreatedAt: l.CreatedAt,
	}
	if l.IP != nil {
		ip := l.IP.String()
		out.IP = &ip
	}
	return out
}

// RequestLogQuery filters request logs.
type RequestLogQuery struct {
	Status        string `query:"status" enum:"success,error,2xx,4xx,5xx" doc:"success is 2xx; error is 4xx and 5xx."`
	Method        string `query:"method" enum:"GET,POST,PUT,PATCH,DELETE"`
	APIKeyID      string `query:"api_key_id" pattern:"^key_[0-9a-z]{26}$"`
	Path          string `query:"path" maxLength:"200" doc:"Only paths starting with this, e.g. /v1/messages."`
	Limit         int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
	StartingAfter string `query:"starting_after" doc:"A log entry ID; returns entries before it."`
}

func (s *Server) registerLogs(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listProjectRequestLogs", Method: http.MethodGet, Path: "/v1/projects/{projectId}/request-logs", Tags: []string{"Logs"},
		Summary:     "List API requests",
		Description: "Requests made with the project's API keys, newest first. Kept for BRIDGE_REQUEST_LOG_RETENTION (default 14 days).",
		Security:    sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		RequestLogQuery
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body RequestLogList }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		return s.requestLogs(ctx, in.ProjectID, cmp.Or(in.Environment, "live"), &in.RequestLogQuery)
	})

	huma.Register(api, huma.Operation{
		OperationID: "listRequestLogs", Method: http.MethodGet, Path: "/v1/request-logs", Tags: []string{"Developer API"},
		Summary: "List API requests",
		Description: "Requests made with the project's keys of this key's environment, newest first: method, path, status, " +
			"error code, duration and the resource they touched. Never bodies or headers.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *RequestLogQuery) (*struct{ Body RequestLogList }, error) {
		k := principalFrom(ctx).APIKey
		return s.requestLogs(ctx, k.ProjectID, string(k.Environment), in)
	})
}

func (s *Server) requestLogs(ctx context.Context, projectID, env string, in *RequestLogQuery) (*struct{ Body RequestLogList }, error) {
	limit := in.Limit
	if limit == 0 {
		limit = 50
	}
	params := dbq.ListRequestLogsParams{
		ProjectID: projectID, Environment: env, RowLimit: int32(limit + 1),
		Method: optString(in.Method), APIKeyID: optString(in.APIKeyID), PathPrefix: optString(strings.TrimSpace(in.Path)),
	}
	lo, hi := statusRange(in.Status)
	params.StatusMin, params.StatusMax = lo, hi
	if in.StartingAfter != "" {
		cursor, err := s.q.GetRequestLog(ctx, dbq.GetRequestLogParams{ID: in.StartingAfter, ProjectID: projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
				Location: "query.starting_after", Message: "No log entry with this ID in the project.",
			})
		}
		if err != nil {
			return nil, err
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListRequestLogs(ctx, params)
	if err != nil {
		return nil, err
	}
	out := &struct{ Body RequestLogList }{Body: RequestLogList{Data: make([]RequestLog, 0, len(rows))}}
	if len(rows) > limit {
		rows, out.Body.HasMore = rows[:limit], true
	}
	for _, l := range rows {
		out.Body.Data = append(out.Body.Data, toRequestLog(l))
	}
	return out, nil
}

func statusRange(class string) (lo, hi *int32) {
	r := func(a, b int32) (*int32, *int32) { return &a, &b }
	switch class {
	case "success", "2xx":
		return r(200, 299)
	case "error":
		return r(400, 599)
	case "4xx":
		return r(400, 499)
	case "5xx":
		return r(500, 599)
	}
	return nil, nil
}
