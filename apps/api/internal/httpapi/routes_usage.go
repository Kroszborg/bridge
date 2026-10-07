package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/db/dbq"
)

// UsageDay is one calendar day in the requested time zone.
type UsageDay struct {
	Date         string  `json:"date" example:"2026-10-05" doc:"YYYY-MM-DD in the requested time zone."`
	Outbound     int     `json:"outbound" doc:"Messages your application sent."`
	Delivered    int     `json:"delivered"`
	Sent         int     `json:"sent" doc:"Sent, no delivery report (yet)."`
	Failed       int     `json:"failed"`
	Pending      int     `json:"pending"`
	Inbound      int     `json:"inbound" doc:"Messages received by phones with forwarding on (live only)."`
	Segments     int     `json:"segments" doc:"SMS segments sent; carriers bill per segment."`
	Requests     int     `json:"requests" doc:"API requests, from the request log (kept 14 days by default)."`
	ClientErrors int     `json:"client_errors"`
	ServerErrors int     `json:"server_errors"`
	P95LatencyMs float64 `json:"p95_latency_ms" doc:"95th percentile API response time."`
}

// DeviceUsage is one phone's share of the period.
type DeviceUsage struct {
	DeviceID  string `json:"device_id"`
	Name      string `json:"name" doc:"Empty when the device was deleted."`
	Outbound  int    `json:"outbound"`
	Delivered int    `json:"delivered"`
	Failed    int    `json:"failed"`
	Inbound   int    `json:"inbound"`
	Segments  int    `json:"segments"`
}

type UsageHistory struct {
	Environment string        `json:"environment" enum:"live,test"`
	TimeZone    string        `json:"time_zone" example:"Asia/Kolkata"`
	Days        []UsageDay    `json:"days" doc:"One entry per day, oldest first, including days without traffic."`
	Devices     []DeviceUsage `json:"devices" doc:"Phones that handled messages in the period, busiest first."`
}

type UsageHistoryQuery struct {
	Days     int    `query:"days" minimum:"1" maximum:"90" default:"30"`
	TimeZone string `query:"tz" maxLength:"64" default:"UTC" example:"Asia/Kolkata" doc:"IANA time zone for day boundaries."`
}

func (s *Server) registerUsage(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getUsageHistory", Method: http.MethodGet, Path: "/v1/usage/history", Tags: []string{"Developer API"},
		Summary: "Get daily usage", Description: "Daily message and request counts for the key's environment.",
		Security: apiKeyAuth, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *UsageHistoryQuery) (*struct{ Body UsageHistory }, error) {
		k := principalFrom(ctx).APIKey
		return s.usageHistory(ctx, k.ProjectID, k.Environment, in)
	})

	huma.Register(api, huma.Operation{
		OperationID: "getProjectUsageHistory", Method: http.MethodGet, Path: "/v1/projects/{projectId}/usage/history", Tags: []string{"Messages"},
		Summary: "Get daily usage", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		ProjectPath
		UsageHistoryQuery
		Environment string `query:"environment" enum:"live,test" default:"live"`
	}) (*struct{ Body UsageHistory }, error) {
		if _, err := s.projectForUser(ctx, in.ProjectID); err != nil {
			return nil, err
		}
		env := in.Environment
		if env == "" {
			env = "live"
		}
		return s.usageHistory(ctx, in.ProjectID, dbq.APIEnvironment(env), &in.UsageHistoryQuery)
	})
}

func (s *Server) usageHistory(ctx context.Context, projectID string, env dbq.APIEnvironment, in *UsageHistoryQuery) (*struct{ Body UsageHistory }, error) {
	days := in.Days
	if days == 0 {
		days = 30
	}
	tzName := in.TimeZone
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil || tzName == "Local" {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Location: "query.tz", Message: "Unknown time zone. Use an IANA name such as Asia/Kolkata or UTC.",
		})
	}
	now := time.Now().In(loc)
	first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))

	msgs, err := s.q.DailyMessageUsage(ctx, dbq.DailyMessageUsageParams{Tz: tzName, ProjectID: projectID, Environment: env, Since: first})
	if err != nil {
		return nil, err
	}
	reqs, err := s.q.DailyRequestUsage(ctx, dbq.DailyRequestUsageParams{Tz: tzName, ProjectID: projectID, Environment: string(env), Since: first})
	if err != nil {
		return nil, err
	}
	devices, err := s.q.DeviceMessageUsage(ctx, dbq.DeviceMessageUsageParams{ProjectID: projectID, Environment: env, Since: first})
	if err != nil {
		return nil, err
	}

	byDay := make(map[string]*UsageDay, days)
	out := UsageHistory{Environment: string(env), TimeZone: tzName, Days: make([]UsageDay, days), Devices: make([]DeviceUsage, 0, len(devices))}
	for i := range days {
		d := first.AddDate(0, 0, i).Format(time.DateOnly)
		out.Days[i] = UsageDay{Date: d}
		byDay[d] = &out.Days[i]
	}
	for _, r := range msgs {
		if d := byDay[r.Day]; d != nil {
			d.Outbound, d.Delivered, d.Sent, d.Failed = int(r.Outbound), int(r.Delivered), int(r.Sent), int(r.Failed)
			d.Pending, d.Inbound, d.Segments = int(r.Pending), int(r.Inbound), int(r.Segments)
		}
	}
	for _, r := range reqs {
		if d := byDay[r.Day]; d != nil {
			d.Requests, d.ClientErrors, d.ServerErrors, d.P95LatencyMs = int(r.Requests), int(r.ClientErrors), int(r.ServerErrors), r.P95Ms
		}
	}
	for _, r := range devices {
		out.Devices = append(out.Devices, DeviceUsage{
			DeviceID: r.DeviceID, Name: r.DeviceName, Outbound: int(r.Outbound), Delivered: int(r.Delivered),
			Failed: int(r.Failed), Inbound: int(r.Inbound), Segments: int(r.Segments),
		})
	}
	return &struct{ Body UsageHistory }{Body: out}, nil
}
