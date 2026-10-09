// Package ratelimit implements fixed-window rate limits.
//
// The Limiter interface lets the Postgres implementation be swapped for Redis
// when a deployment outgrows it, without touching callers.
package ratelimit

import (
	"context"
	"time"

	"bridge/internal/db/dbq"
)

// Result describes the outcome of a single hit.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration // zero when allowed
	ResetAt    time.Time
}

// Limiter counts hits against a key.
type Limiter interface {
	// Hit records one request for key and reports whether it is within limit
	// requests per window. Implementations fail open: on a storage error they
	// return an allowed result together with the error, which callers log.
	Hit(ctx context.Context, key string, limit int, window time.Duration) (Result, error)
	// Count returns the hits recorded for key in the current window without
	// adding one. On a storage error it returns 0 with the error.
	Count(ctx context.Context, key string, window time.Duration) (int, error)
}

// Postgres stores counters in the UNLOGGED rate_limit_counters table.
type Postgres struct {
	q   *dbq.Queries
	now func() time.Time
}

func NewPostgres(q *dbq.Queries) *Postgres {
	return &Postgres{q: q, now: time.Now}
}

func (p *Postgres) Hit(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	now := p.now().UTC()
	start := now.Truncate(window)
	reset := start.Add(window)
	count, err := p.q.HitRateLimit(ctx, dbq.HitRateLimitParams{
		Key:         key + "|" + window.String(),
		WindowStart: start,
	})
	if err != nil {
		return Result{Allowed: true, Limit: limit, Remaining: limit, ResetAt: reset}, err
	}
	res := Result{Limit: limit, Remaining: max(0, limit-int(count)), ResetAt: reset}
	res.Allowed = int(count) <= limit
	if !res.Allowed {
		res.RetryAfter = reset.Sub(now).Round(time.Second)
		if res.RetryAfter < time.Second {
			res.RetryAfter = time.Second
		}
	}
	return res, nil
}

func (p *Postgres) Count(ctx context.Context, key string, window time.Duration) (int, error) {
	n, err := p.q.PeekRateLimit(ctx, dbq.PeekRateLimitParams{
		Key:         key + "|" + window.String(),
		WindowStart: p.now().UTC().Truncate(window),
	})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
