package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"bridge/internal/config"
	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/messaging"
	"bridge/internal/status"
	"bridge/internal/testutil"
	"bridge/internal/tools"
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
	exec(`INSERT INTO organizations (id, name, slug) VALUES ('org_1', 'Acme', 'acme')`)
	exec(`INSERT INTO projects (id, organization_id, name, slug) VALUES ('prj_1', 'org_1', 'Default', 'default')`)
	exec(`INSERT INTO otp_verifications (id, project_id, environment, recipient, code_hash, code_length, max_attempts, expires_at, created_at)
	      VALUES ('otp_lapsed', 'prj_1', 'live', '+919800000000', decode('00', 'hex'), 6, 5, now() - interval '1 minute', now() - interval '11 minutes'),
	             ('otp_live',   'prj_1', 'live', '+919800000001', decode('00', 'hex'), 6, 5, now() + interval '9 minutes', now()),
	             ('otp_ancient','prj_1', 'live', '+919800000002', NULL,    6, 5, now() - interval '40 days', now() - interval '40 days')`)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	msgs := testMessaging(d, logger)
	hooks := webhook.New(webhook.Options{Pool: d.Pool, Logger: logger})
	w := &MaintenanceWorker{q: dbq.New(d.Pool), log: logger, messaging: msgs, webhooks: hooks, otp: otpService(d.Pool, msgs, hooks, logger)}
	if err := w.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}

	var sessions, counters int
	_ = d.Pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&sessions)
	_ = d.Pool.QueryRow(ctx, `SELECT count(*) FROM rate_limit_counters`).Scan(&counters)
	if sessions != 1 || counters != 1 {
		t.Fatalf("after maintenance: %d sessions, %d counters; want 1 and 1", sessions, counters)
	}

	// Lapsed codes expire (and lose their hash); verifications older than 30 days go.
	statuses := map[string]string{}
	rows, err := d.Pool.Query(ctx, `SELECT id, status::text || CASE WHEN code_hash IS NULL THEN '/nohash' ELSE '' END FROM otp_verifications`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, st string
		_ = rows.Scan(&id, &st)
		statuses[id] = st
	}
	if len(statuses) != 2 || statuses["otp_lapsed"] != "expired/nohash" || statuses["otp_live"] != "pending" {
		t.Fatalf("verifications after maintenance: %v", statuses)
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
	msgs := testMessaging(d, logger)
	kit, err := tools.New(tools.Options{Config: &config.Config{}, Pool: d.Pool, Logger: logger, Messaging: msgs})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(d.Pool, logger, msgs, webhook.New(webhook.Options{Pool: d.Pool, Logger: logger}), status.New(d.Pool, logger, "test"), Options{Tools: kit})
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
