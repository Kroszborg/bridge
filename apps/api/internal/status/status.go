// Package status measures the health of a Bridge installation for the public
// status page and the operator's System health view. API and worker processes
// check in every few seconds; the worker samples every component once a
// minute and keeps 90 days of samples for uptime history.
package status

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
)

// States, best to worst.
const (
	Operational = "operational"
	Degraded    = "degraded"
	Outage      = "outage"
)

var rank = map[string]int{Operational: 0, Degraded: 1, Outage: 2}

// Worst returns the more severe of two states.
func Worst(a, b string) string {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

const (
	heartbeatEvery = 15 * time.Second
	// A process that has not checked in for this long is considered gone.
	heartbeatStale = 45 * time.Second
	// SampleEvery is how often the worker records component health.
	SampleEvery = time.Minute
	// HistoryDays of samples are kept.
	HistoryDays = 90
)

// Component is one part of the system as the status page shows it.
type Component struct {
	ID          string
	Name        string
	Description string
	// Informational components are shown but never change the overall state:
	// phones going offline is the owner's business, not an outage of Bridge.
	Informational bool
}

// Components in display order.
var Components = []Component{
	{ID: "api", Name: "API", Description: "REST API, dashboard sign-in and the phone gateway connection."},
	{ID: "database", Name: "Database", Description: "PostgreSQL, which stores messages and runs the job queue."},
	{ID: "jobs", Name: "Message processing", Description: "Background workers that assign messages to phones and retry them."},
	{ID: "webhooks", Name: "Webhook delivery", Description: "Signed event notifications to your endpoints."},
	{ID: "phones", Name: "Phones", Description: "Paired Android gateways. Shown for information; a phone going offline is not a Bridge outage.", Informational: true},
}

// Check is the current health of one component.
type Check struct {
	Component string
	Status    string
	Value     *float64 // the measured number, if any (latency, lag, phones online)
	Detail    string
}

// Service measures health. It is safe for concurrent use.
type Service struct {
	pool    *pgxpool.Pool
	q       *dbq.Queries
	log     *slog.Logger
	version string

	mu       sync.Mutex
	cached   []Check
	cachedAt time.Time
}

func New(pool *pgxpool.Pool, logger *slog.Logger, version string) *Service {
	return &Service{pool: pool, q: dbq.New(pool), log: logger, version: version}
}

// Heartbeat records this process as alive until ctx ends, then removes it.
func (s *Service) Heartbeat(ctx context.Context, kind string) {
	host, _ := os.Hostname()
	instance := id.New(kind)
	started := time.Now()
	beat := func() {
		if err := s.q.UpsertHeartbeat(ctx, dbq.UpsertHeartbeatParams{
			InstanceID: instance, Kind: kind, Version: s.version, Hostname: host, StartedAt: started,
		}); err != nil && ctx.Err() == nil {
			s.log.Warn("could not record heartbeat", "kind", kind, "error", err)
		}
	}
	beat()
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			dctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.q.DeleteHeartbeat(dctx, instance)
			cancel()
			return
		case <-t.C:
			beat()
		}
	}
}

// Current returns every component's health, measured at most 10 seconds ago.
func (s *Service) Current(ctx context.Context) []Check {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cachedAt) < 10*time.Second {
		return s.cached
	}
	s.cached, s.cachedAt = s.measure(ctx), time.Now()
	return s.cached
}

// QueueStat is River's backlog for one queue and job state.
type QueueStat struct {
	Queue                  string
	State                  string
	Jobs                   int
	OldestAvailableSeconds float64
}

// Queues reads River's job table directly (it is not part of Bridge's schema).
func (s *Service) Queues(ctx context.Context) ([]QueueStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT queue, state::text, count(*)::int,
		       COALESCE(EXTRACT(EPOCH FROM now() - min(scheduled_at) FILTER (WHERE state = 'available')), 0)::float8
		FROM river_job
		WHERE state IN ('available', 'running', 'retryable', 'scheduled')
		GROUP BY 1, 2 ORDER BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueStat
	for rows.Next() {
		var q QueueStat
		if err := rows.Scan(&q.Queue, &q.State, &q.Jobs, &q.OldestAvailableSeconds); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Service) measure(ctx context.Context) []Check {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	checks := make([]Check, 0, len(Components))

	// Database: round trip latency.
	start := time.Now()
	_, dbErr := s.pool.Exec(ctx, "SELECT 1")
	latency := float64(time.Since(start).Microseconds()) / 1000
	switch {
	case dbErr != nil:
		checks = append(checks, Check{Component: "database", Status: Outage, Detail: "The database is unreachable."})
	case latency > 1000:
		checks = append(checks, Check{Component: "database", Status: Degraded, Value: &latency, Detail: fmt.Sprintf("Slow: %.0f ms round trip.", latency)})
	default:
		checks = append(checks, Check{Component: "database", Status: Operational, Value: &latency})
	}
	if dbErr != nil {
		// Nothing else can be measured without the database.
		for _, c := range []string{"api", "jobs", "webhooks", "phones"} {
			checks = append(checks, Check{Component: c, Status: Outage, Detail: "Unknown while the database is unreachable."})
		}
		return checks
	}

	// API and workers: recent heartbeats.
	beats, err := s.q.ListHeartbeats(ctx)
	if err != nil {
		s.log.Warn("could not read heartbeats", "error", err)
	}
	alive := map[string]int{}
	for _, b := range beats {
		if time.Since(b.SeenAt) < heartbeatStale {
			alive[b.Kind]++
		}
	}
	apiN := float64(alive["api"])
	if alive["api"] == 0 {
		checks = append(checks, Check{Component: "api", Status: Outage, Value: &apiN, Detail: "No API server has checked in recently."})
	} else {
		checks = append(checks, Check{Component: "api", Status: Operational, Value: &apiN})
	}

	queues, err := s.Queues(ctx)
	if err != nil {
		s.log.Warn("could not read job queues", "error", err)
	}
	lag := func(queue string) float64 {
		for _, q := range queues {
			if q.Queue == queue && q.State == "available" {
				return q.OldestAvailableSeconds
			}
		}
		return 0
	}
	queueCheck := func(component, queue, what string) Check {
		l := lag(queue)
		c := Check{Component: component, Status: Operational, Value: &l}
		switch {
		case alive["worker"] == 0:
			c.Status, c.Detail = Outage, "No background worker is running. Messages and "+what+" wait until one starts."
		case l > 300:
			c.Status, c.Detail = Outage, fmt.Sprintf("Work is waiting %.0f minutes to start.", l/60)
		case l > 60:
			c.Status, c.Detail = Degraded, fmt.Sprintf("Work is waiting %.0f seconds to start.", l)
		}
		return c
	}
	checks = append(checks, queueCheck("jobs", river.QueueDefault, "webhooks"))
	checks = append(checks, queueCheck("webhooks", "webhooks", "webhooks"))

	fleet, err := s.q.FleetStats(ctx)
	if err == nil {
		online := float64(fleet.Online)
		c := Check{Component: "phones", Status: Operational, Value: &online, Detail: fmt.Sprintf("%d of %d phones online.", fleet.Online, fleet.Total)}
		if fleet.Total == 0 {
			c.Detail = "No phones paired yet."
		} else if fleet.Online == 0 {
			c.Status = Degraded
		}
		checks = append(checks, c)
	}
	return checks
}

// Overall is the worst state of the non-informational components.
func Overall(checks []Check) string {
	info := map[string]bool{}
	for _, c := range Components {
		info[c.ID] = c.Informational
	}
	out := Operational
	for _, c := range checks {
		if !info[c.Component] {
			out = Worst(out, c.Status)
		}
	}
	return out
}

// SampleArgs is the worker's periodic health sample.
type SampleArgs struct{}

func (SampleArgs) Kind() string { return "status.sample" }

type SampleWorker struct {
	river.WorkerDefaults[SampleArgs]
	svc *Service
}

func (w *SampleWorker) Work(ctx context.Context, _ *river.Job[SampleArgs]) error {
	return w.svc.Sample(ctx)
}

// Register adds the sampling worker.
func Register(workers *river.Workers, svc *Service) {
	river.AddWorker(workers, &SampleWorker{svc: svc})
}

// Sample records every component's current health for the history.
func (s *Service) Sample(ctx context.Context) error {
	at := time.Now().UTC().Truncate(SampleEvery)
	for _, c := range s.measure(ctx) {
		if err := s.q.InsertStatusSample(ctx, dbq.InsertStatusSampleParams{
			Component: c.Component, SampledAt: at, Status: c.Status, Value: c.Value, Detail: c.Detail,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Prune deletes samples older than the history window.
func (s *Service) Prune(ctx context.Context) (int64, error) {
	return s.q.DeleteOldStatusSamples(ctx, time.Now().AddDate(0, 0, -HistoryDays-1))
}
