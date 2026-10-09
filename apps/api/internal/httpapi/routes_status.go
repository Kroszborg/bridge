package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/status"
)

type StatusDay struct {
	Date   string   `json:"date" example:"2026-10-07"`
	Status string   `json:"status" enum:"operational,degraded,outage,no_data"`
	Uptime *float64 `json:"uptime" nullable:"true" doc:"Share of samples without an outage, 0 to 1. Null when there is no data."`
}

type StatusComponent struct {
	ID            string      `json:"id" example:"api"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	Informational bool        `json:"informational" doc:"Shown for information; does not affect the overall status."`
	Status        string      `json:"status" enum:"operational,degraded,outage"`
	Detail        string      `json:"detail"`
	Uptime90d     *float64    `json:"uptime_90d" nullable:"true"`
	Days          []StatusDay `json:"days" doc:"The last 90 UTC days, oldest first."`
}

type StatusPage struct {
	Status     string            `json:"status" enum:"operational,degraded,outage"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Components []StatusComponent `json:"components"`
}

type SystemInstance struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind" enum:"api,worker"`
	Version   string    `json:"version"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at"`
	SeenAt    time.Time `json:"seen_at"`
	Alive     bool      `json:"alive"`
}

type SystemQueue struct {
	Queue                  string  `json:"queue"`
	State                  string  `json:"state" enum:"available,running,retryable,scheduled"`
	Jobs                   int     `json:"jobs"`
	OldestAvailableSeconds float64 `json:"oldest_available_seconds"`
}

type SystemHealth struct {
	Status     string            `json:"status" enum:"operational,degraded,outage"`
	Version    string            `json:"version"`
	Components []StatusComponent `json:"components"`
	Instances  []SystemInstance  `json:"instances"`
	Queues     []SystemQueue     `json:"queues"`
	Phones     struct {
		Online int `json:"online"`
		Total  int `json:"total"`
	} `json:"phones"`
	Backlog struct {
		Waiting       int     `json:"waiting" doc:"Live messages waiting for a phone."`
		OldestSeconds float64 `json:"oldest_seconds"`
	} `json:"backlog"`
	Database struct {
		SizeBytes   int64  `json:"size_bytes"`
		Connections int    `json:"connections"`
		Version     string `json:"version"`
	} `json:"database"`
	Retention struct {
		MessageBodiesSeconds int64 `json:"message_bodies_seconds" doc:"How long message bodies are kept before redaction."`
		RequestLogsSeconds   int64 `json:"request_logs_seconds" doc:"How long request logs are kept."`
	} `json:"retention"`
}

func (s *Server) registerStatus(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getStatus", Method: http.MethodGet, Path: "/v1/status", Tags: []string{"Status"},
		Summary:     "Get the service status",
		Description: "Public, no authentication. Current state of each component and 90 days of daily uptime. Contains no customer data.",
	}, func(ctx context.Context, _ *struct{}) (*struct {
		CacheControl string `header:"Cache-Control"`
		Body         StatusPage
	}, error) {
		page, err := s.statusPage(ctx)
		if err != nil {
			return nil, err
		}
		return &struct {
			CacheControl string `header:"Cache-Control"`
			Body         StatusPage
		}{CacheControl: "public, max-age=15", Body: page}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getSystemHealth", Method: http.MethodGet, Path: "/v1/system", Tags: []string{"Status"},
		Summary:     "Get system health",
		Description: "Instance-wide internals for operators: processes, job queues, phones, backlog and database. Operators are BRIDGE_OPERATOR_EMAILS, or the first account when that is unset. Anyone else gets the same 404 as an unknown route.",
		Security:    sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body SystemHealth }, error) {
		if !s.isOperator(ctx) {
			return nil, noRoute(http.MethodGet, "/v1/system")
		}
		return s.systemHealth(ctx)
	})
}

// noRoute is the error for a path no route matches. Operator-only routes
// answer everyone else with it too, so they cannot tell the route exists.
func noRoute(method, path string) error {
	return Errorf(http.StatusNotFound, CodeNotFound, noRouteMessage(method, path))
}

func noRouteMessage(method, path string) string {
	return "No route matches " + method + " " + path + ". See /docs for the API reference."
}

// isOperator reports whether the signed-in user runs this instance.
func (s *Server) isOperator(ctx context.Context) bool {
	p := principalFrom(ctx)
	if p == nil || p.User == nil || s.status == nil {
		return false
	}
	email := strings.ToLower(p.User.Email)
	if len(s.cfg.OperatorEmails) > 0 {
		return slices.Contains(s.cfg.OperatorEmails, email)
	}
	first, err := s.q.FirstUserEmail(ctx)
	return err == nil && strings.EqualFold(first, email)
}

func (s *Server) statusComponents(ctx context.Context) ([]StatusComponent, string, error) {
	checks := s.status.Current(ctx)
	byID := map[string]status.Check{}
	for _, c := range checks {
		byID[c.Component] = c
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -(status.HistoryDays - 1))
	type agg struct{ samples, up, operational int }
	days := map[string]map[string]agg{}
	if rows, err := s.q.DailyStatus(ctx, first); err == nil {
		for _, r := range rows {
			if days[r.Component] == nil {
				days[r.Component] = map[string]agg{}
			}
			days[r.Component][r.Day] = agg{int(r.Samples), int(r.Up), int(r.Operational)}
		}
	}
	out := make([]StatusComponent, 0, len(status.Components))
	for _, comp := range status.Components {
		c, ok := byID[comp.ID]
		if !ok {
			continue
		}
		sc := StatusComponent{ID: comp.ID, Name: comp.Name, Description: comp.Description, Informational: comp.Informational,
			Status: c.Status, Detail: c.Detail, Days: make([]StatusDay, 0, status.HistoryDays)}
		var totalSamples, totalUp int
		for i := range status.HistoryDays {
			date := first.AddDate(0, 0, i).Format(time.DateOnly)
			a := days[comp.ID][date]
			d := StatusDay{Date: date, Status: "no_data"}
			if a.samples > 0 {
				u := float64(a.up) / float64(a.samples)
				d.Uptime = &u
				switch {
				case u < 0.95:
					d.Status = status.Outage
				case float64(a.operational)/float64(a.samples) < 0.99:
					d.Status = status.Degraded
				default:
					d.Status = status.Operational
				}
				totalSamples += a.samples
				totalUp += a.up
			}
			sc.Days = append(sc.Days, d)
		}
		if totalSamples > 0 {
			u := float64(totalUp) / float64(totalSamples)
			sc.Uptime90d = &u
		}
		out = append(out, sc)
	}
	return out, status.Overall(checks), nil
}

func (s *Server) statusPage(ctx context.Context) (StatusPage, error) {
	comps, overall, err := s.statusComponents(ctx)
	if err != nil {
		return StatusPage{}, err
	}
	return StatusPage{Status: overall, UpdatedAt: time.Now().UTC(), Components: comps}, nil
}

func (s *Server) systemHealth(ctx context.Context) (*struct{ Body SystemHealth }, error) {
	comps, overall, err := s.statusComponents(ctx)
	if err != nil {
		return nil, err
	}
	h := SystemHealth{Status: overall, Version: s.version, Components: comps, Instances: []SystemInstance{}, Queues: []SystemQueue{}}
	beats, err := s.q.ListHeartbeats(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range beats {
		h.Instances = append(h.Instances, SystemInstance{ID: b.InstanceID, Kind: b.Kind, Version: b.Version, Hostname: b.Hostname,
			StartedAt: b.StartedAt, SeenAt: b.SeenAt, Alive: time.Since(b.SeenAt) < 45*time.Second})
	}
	queues, err := s.status.Queues(ctx)
	if err != nil {
		return nil, err
	}
	for _, q := range queues {
		h.Queues = append(h.Queues, SystemQueue{Queue: q.Queue, State: q.State, Jobs: q.Jobs, OldestAvailableSeconds: q.OldestAvailableSeconds})
	}
	if fleet, err := s.q.FleetStats(ctx); err == nil {
		h.Phones.Online, h.Phones.Total = int(fleet.Online), int(fleet.Total)
	}
	if b, err := s.q.MessageBacklog(ctx); err == nil {
		h.Backlog.Waiting, h.Backlog.OldestSeconds = int(b.Waiting), b.OldestSeconds
	}
	_ = s.pool.QueryRow(ctx, `SELECT pg_database_size(current_database()),
		(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database())::int,
		current_setting('server_version')`).Scan(&h.Database.SizeBytes, &h.Database.Connections, &h.Database.Version)
	h.Retention.MessageBodiesSeconds = int64(s.cfg.MessageRetention.Seconds())
	h.Retention.RequestLogsSeconds = int64(s.cfg.RequestLogRetention.Seconds())
	return &struct{ Body SystemHealth }{Body: h}, nil
}
