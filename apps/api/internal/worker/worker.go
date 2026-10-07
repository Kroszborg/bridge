// Package worker runs Bridge's background jobs on River, a Postgres-backed queue.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
	"bridge/internal/otp"
	"bridge/internal/schedule"
	"bridge/internal/status"
	"bridge/internal/tools"
	"bridge/internal/webhook"
)

// MaintenanceArgs is a periodic job that removes expired sessions, stale
// rate-limit counters and expired pairing tokens, and marks devices offline
// whose connection vanished without a clean close.
type MaintenanceArgs struct{}

func (MaintenanceArgs) Kind() string { return "maintenance" }

type MaintenanceWorker struct {
	river.WorkerDefaults[MaintenanceArgs]
	q         *dbq.Queries
	log       *slog.Logger
	messaging *messaging.Service
	webhooks  *webhook.Service
	otp       *otp.Service
	status    *status.Service
	tools     *tools.Services
	retention Retention
}

// Retention sets how long the maintenance job keeps operational records.
type Retention struct {
	RequestLogs time.Duration // default 14 days
}

func (w *MaintenanceWorker) Work(ctx context.Context, _ *river.Job[MaintenanceArgs]) error {
	sessions, err := w.q.DeleteExpiredSessions(ctx)
	if err != nil {
		return err
	}
	counters, err := w.q.DeleteStaleRateLimits(ctx, time.Now().Add(-2*time.Hour))
	if err != nil {
		return err
	}
	tokens, err := w.q.DeleteExpiredPairingTokens(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	devices, err := w.q.SweepStaleDevices(ctx)
	if err != nil {
		return err
	}
	redacted, err := w.messaging.RedactBodies(ctx)
	if err != nil {
		return err
	}
	// Events carry message bodies, so they follow the message retention period.
	events, err := w.webhooks.DeleteOld(ctx, time.Now().Add(-w.messaging.Retention()))
	if err != nil {
		return err
	}
	logRetention := w.retention.RequestLogs
	if logRetention == 0 {
		logRetention = 14 * 24 * time.Hour
	}
	logs, err := w.q.DeleteOldRequestLogs(ctx, time.Now().Add(-logRetention))
	if err != nil {
		return err
	}
	// Expiring codes also announces otp.expired.
	otps, err := w.otp.Maintain(ctx)
	if err != nil {
		return err
	}
	// Forwarding logs name messages, so they follow the message retention period too.
	var forwards int64
	if w.tools != nil {
		if forwards, err = w.tools.Automation.DeleteOld(ctx, time.Now().Add(-w.messaging.Retention())); err != nil {
			return err
		}
	}
	var samples int64
	if w.status != nil {
		if samples, err = w.status.Prune(ctx); err != nil {
			return err
		}
	}
	w.log.Info("maintenance complete", "status_samples_deleted", samples, "expired_sessions", sessions, "stale_rate_limits", counters,
		"expired_pairing_tokens", tokens, "stale_devices_marked_offline", devices, "message_bodies_redacted", redacted,
		"webhook_events_deleted", events, "request_logs_deleted", logs, "otps_expired", otps.Expired, "otps_deleted", otps.Deleted,
		"otp_blocks_deleted", otps.BlocksDeleted, "forwarding_deliveries_deleted", forwards)
	return nil
}

func otpService(pool *pgxpool.Pool, svc *messaging.Service, hooks *webhook.Service, logger *slog.Logger) *otp.Service {
	var emitter otp.Emitter
	if hooks != nil {
		emitter = hooks
	}
	return otp.New(otp.Options{Pool: pool, Messaging: svc, Emitter: emitter, Logger: logger})
}

// Options configures the worker.
type Options struct {
	Retention Retention
	// Tools are the messaging tools' services (required).
	Tools *tools.Services
	// ScheduleInterval is how often due scheduled messages are claimed.
	// Default schedule.TickInterval (a minute); tests shorten it.
	ScheduleInterval time.Duration
}

// NewClient builds a River client that processes jobs, and wires it into the
// messaging, webhook and tools services so jobs can schedule follow-up jobs.
func NewClient(pool *pgxpool.Pool, logger *slog.Logger, svc *messaging.Service, hooks *webhook.Service, health *status.Service, o Options) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	verify := otpService(pool, svc, hooks, logger)
	river.AddWorker(workers, &MaintenanceWorker{q: dbq.New(pool), log: logger, messaging: svc, webhooks: hooks, otp: verify, status: health, tools: o.Tools, retention: o.Retention})
	otp.Register(workers, verify)
	o.Tools.Register(workers)
	scheduleEvery := o.ScheduleInterval
	if scheduleEvery == 0 {
		scheduleEvery = schedule.TickInterval
	}
	periodic := []*river.PeriodicJob{}
	if health != nil {
		status.Register(workers, health)
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(status.SampleEvery),
			func() (river.JobArgs, *river.InsertOpts) { return status.SampleArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}
	messaging.Register(workers, svc)
	webhook.Register(workers, hooks)

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault:       {MaxWorkers: 20},
			messaging.QueueSimulator: {MaxWorkers: 50},
			webhook.QueueWebhooks:    {MaxWorkers: 50},
		},
		Workers: workers,
		PeriodicJobs: append(periodic,
			river.NewPeriodicJob(
				river.PeriodicInterval(2*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return MaintenanceArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(svc.SweepInterval()),
				func() (river.JobArgs, *river.InsertOpts) { return messaging.SweepArgs{}, nil },
				nil,
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(webhook.PresenceInterval),
				func() (river.JobArgs, *river.InsertOpts) { return webhook.PresenceArgs{}, nil },
				nil,
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(scheduleEvery),
				func() (river.JobArgs, *river.InsertOpts) { return schedule.TickArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		),
	})
	if err != nil {
		return nil, err
	}
	svc.SetJobInserter(client)
	hooks.SetJobInserter(client)
	o.Tools.SetJobInserter(client)
	return client, nil
}

// NewInsertOnlyClient builds a River client that only inserts jobs (for the API process).
func NewInsertOnlyClient(pool *pgxpool.Pool, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: logger})
}
