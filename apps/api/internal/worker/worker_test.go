package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/messaging"
	"bridge/internal/status"
	"bridge/internal/testutil"
	"bridge/internal/webhook"
)

func TestMaintenanceRemovesExpiredRows(t *testing.T) {
	ctx := context.Background()
	d, err := testutil.NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Skipf("set %s to run integration tests", testutil.EnvVar)
	}
	defer d.Close(ctx)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := d.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users (id, email, password_hash) VALUES ('usr_1', 'a@example.com', 'x')`)
	exec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ('ses_old', 'usr_1', 'a', now() - interval '1 minute'), ('ses_new', 'usr_1', 'b', now() + interval '1 day')`)
	exec(`INSERT INTO rate_limit_counters (key, window_start, count) VALUES ('old', now() - interval '3 hours', 1), ('new', now(), 1)`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := &MaintenanceWorker{q: dbq.New(d.Pool), log: logger, messaging: testMessaging(d, logger), webhooks: webhook.New(webhook.Options{Pool: d.Pool, Logger: logger})}
	if err := w.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}

	var sessions, counters int
	_ = d.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&sessions)
	_ = d.Pool.QueryRow(ctx, `SELECT count(*) FROM rate_limit_counters`).Scan(&counters)
	if sessions != 1 || counters != 1 {
		t.Fatalf("after maintenance: %d sessions, %d counters; want 1 and 1", sessions, counters)
	}
}

func TestClientStartsAndStops(t *testing.T) {
	ctx := context.Background()
	d, err := testutil.NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Skipf("set %s to run integration tests", testutil.EnvVar)
	}
	defer d.Close(ctx)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := NewClient(d.Pool, logger, testMessaging(d, logger), webhook.New(webhook.Options{Pool: d.Pool, Logger: logger}), status.New(d.Pool, logger, "test"), Retention{})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := client.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	stopCtx, stopCancel := context.WithTimeout(ctx, 10*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func testMessaging(d *testutil.Database, logger *slog.Logger) *messaging.Service {
	return messaging.New(messaging.Options{
		Pool: d.Pool, Logger: logger,
		Publisher: func(ctx context.Context, deviceID string, f gateway.Outbound) error {
			return gateway.Publish(ctx, d.Pool, deviceID, f)
		},
	})
}
