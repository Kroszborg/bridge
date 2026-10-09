package ratelimit

import (
	"context"
	"testing"
	"time"

	"bridge/internal/db/dbq"
	"bridge/internal/testutil"
)

func TestPostgresLimiter(t *testing.T) {
	ctx := context.Background()
	d, err := testutil.NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Skipf("set %s to run integration tests", testutil.EnvVar)
	}
	defer d.Close(ctx)

	now := time.Date(2026, 10, 4, 12, 0, 30, 0, time.UTC)
	l := &Postgres{q: dbq.New(d.Pool), now: func() time.Time { return now }}

	for i := 1; i <= 3; i++ {
		res, err := l.Hit(ctx, "k", 3, time.Minute)
		if err != nil || !res.Allowed || res.Remaining != 3-i {
			t.Fatalf("hit %d: %+v %v", i, res, err)
		}
	}
	res, err := l.Hit(ctx, "k", 3, time.Minute)
	if err != nil || res.Allowed || res.RetryAfter != 30*time.Second {
		t.Fatalf("4th hit should be denied with 30s retry: %+v %v", res, err)
	}

	if n, err := l.Count(ctx, "k", time.Minute); err != nil || n != 4 {
		t.Fatalf("Count = %d, %v; want 4", n, err)
	}
	if n, _ := l.Count(ctx, "k", time.Minute); n != 4 {
		t.Fatalf("Count must not add a hit, got %d", n)
	}
	if n, err := l.Count(ctx, "never-hit", time.Minute); err != nil || n != 0 {
		t.Fatalf("Count of an unknown key = %d, %v", n, err)
	}

	// Different keys and windows are independent.
	if res, _ := l.Hit(ctx, "other", 3, time.Minute); !res.Allowed {
		t.Fatal("independent key was limited")
	}
	if res, _ := l.Hit(ctx, "k", 3, time.Hour); !res.Allowed {
		t.Fatal("different window shares a counter")
	}

	// The next window resets the counter.
	now = now.Add(time.Minute)
	if n, _ := l.Count(ctx, "k", time.Minute); n != 0 {
		t.Fatalf("Count in a new window = %d", n)
	}
	if res, _ := l.Hit(ctx, "k", 3, time.Minute); !res.Allowed || res.Remaining != 2 {
		t.Fatalf("new window did not reset: %+v", res)
	}
}
