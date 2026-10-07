// Package reqlog records developer API requests for the dashboard's request
// log. Entries are buffered and written in batches, so logging adds no
// database round trip to a request. Only metadata is stored: never headers,
// query strings or bodies.
package reqlog

import (
	"context"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one API request.
type Entry struct {
	ID          string
	RequestID   string
	ProjectID   string
	APIKeyID    string
	Environment string
	Method      string
	Path        string
	Status      int
	ErrorCode   string
	Duration    time.Duration
	IP          netip.Addr
	UserAgent   string
	ResourceID  string
	At          time.Time
}

const (
	bufferSize = 4096
	batchSize  = 500
)

// Recorder buffers entries and writes them to api_request_logs.
type Recorder struct {
	pool     *pgxpool.Pool
	log      *slog.Logger
	ch       chan Entry
	interval time.Duration
	dropped  atomic.Int64
}

// New creates a recorder that flushes at least every interval (default 1s).
// Call Run to start writing.
func New(pool *pgxpool.Pool, logger *slog.Logger, interval time.Duration) *Recorder {
	if interval <= 0 {
		interval = time.Second
	}
	return &Recorder{pool: pool, log: logger, ch: make(chan Entry, bufferSize), interval: interval}
}

// Record queues an entry without blocking. When the buffer is full (the
// database is slow or down) the entry is dropped and counted.
func (r *Recorder) Record(e Entry) {
	if r == nil {
		return
	}
	select {
	case r.ch <- e:
	default:
		if n := r.dropped.Add(1); n == 1 || n%1000 == 0 {
			r.log.Warn("request log buffer full; dropping entries", "dropped_total", n)
		}
	}
}

// Run writes batches until ctx ends, then flushes what is left.
func (r *Recorder) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	batch := make([]Entry, 0, batchSize)
	flush := func(ctx context.Context) {
		if len(batch) == 0 {
			return
		}
		if err := r.write(ctx, batch); err != nil {
			r.log.Error("could not write request logs", "entries", len(batch), "error", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case e := <-r.ch:
			batch = append(batch, e)
			if len(batch) >= batchSize {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		case <-ctx.Done():
			// Drain what is buffered with a short, detached deadline.
			dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		drain:
			for {
				select {
				case e := <-r.ch:
					batch = append(batch, e)
				default:
					break drain
				}
			}
			flush(dctx)
			return nil
		}
	}
}

var columns = []string{
	"id", "request_id", "project_id", "api_key_id", "environment", "method", "path", "status",
	"error_code", "duration_ms", "ip", "user_agent", "resource_id", "created_at",
}

func (r *Recorder) write(ctx context.Context, batch []Entry) error {
	rows := make([][]any, len(batch))
	for i, e := range batch {
		var ip *netip.Addr
		if e.IP.IsValid() {
			ip = &e.IP
		}
		rows[i] = []any{
			e.ID, e.RequestID, e.ProjectID, opt(e.APIKeyID), e.Environment, e.Method, e.Path, int32(e.Status),
			opt(e.ErrorCode), int32(e.Duration / time.Millisecond), ip, opt(e.UserAgent), opt(e.ResourceID), e.At,
		}
	}
	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"api_request_logs"}, columns, pgx.CopyFromRows(rows))
	if err == nil {
		return nil
	}
	// COPY is all or nothing. A row can fail on its own, for example when its
	// project was deleted meanwhile, so retry one by one and skip failures.
	r.log.Warn("batched request log write failed; retrying row by row", "entries", len(rows), "error", err)
	failed := 0
	for _, row := range rows {
		if _, err := r.pool.Exec(ctx, insertOne, row...); err != nil {
			failed++
		}
	}
	if failed > 0 {
		r.log.Warn("skipped request log entries", "skipped", failed)
	}
	return nil
}

const insertOne = `INSERT INTO api_request_logs (id, request_id, project_id, api_key_id, environment, method, path,
    status, error_code, duration_ms, ip, user_agent, resource_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (id) DO NOTHING`

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
