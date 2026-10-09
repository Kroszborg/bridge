package schedule

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/billing"
	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
)

// TickArgs is the periodic job that claims due schedules.
type TickArgs struct{}

func (TickArgs) Kind() string { return "schedule.tick" }

type TickWorker struct {
	river.WorkerDefaults[TickArgs]
	svc *Service
}

func (w *TickWorker) Work(ctx context.Context, _ *river.Job[TickArgs]) error {
	for {
		n, err := w.svc.Tick(ctx)
		if err != nil || n < claimBatch {
			return err
		}
	}
}

// SendArgs sends one run of a schedule. RunKey identifies the run, so a
// retried job never sends it twice.
type SendArgs struct {
	ScheduleID string `json:"schedule_id"`
	RunKey     string `json:"run_key"`
}

func (SendArgs) Kind() string { return "schedule.send" }

func (SendArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 5} }

type SendWorker struct {
	river.WorkerDefaults[SendArgs]
	svc *Service
}

func (w *SendWorker) Work(ctx context.Context, job *river.Job[SendArgs]) error {
	return w.svc.Send(ctx, job.Args, job.Attempt >= job.MaxAttempts)
}

// Register adds the schedule workers.
func Register(workers *river.Workers, s *Service) {
	river.AddWorker(workers, &TickWorker{svc: s})
	river.AddWorker(workers, &SendWorker{svc: s})
}

// Tick claims every due schedule, moves it to its next run and queues the
// send, all in one transaction, so a run is queued exactly once even with
// several workers. A schedule that was due long ago (the worker was down)
// sends once and continues from now: missed runs are not replayed.
func (s *Service) Tick(ctx context.Context) (int, error) {
	now := s.now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	due, err := q.ClaimDueSchedules(ctx, dbq.ClaimDueSchedulesParams{Now: now, RowLimit: claimBatch})
	if err != nil {
		return 0, err
	}
	for _, m := range due {
		f := FieldsOf(m)
		var next *time.Time
		if loc, err := f.Spec.Normalize(); err == nil && f.Spec.Kind != KindOnce {
			next = nextRun(f, loc, now)
		} else if err != nil {
			s.log.Error("schedule has an invalid timetable; stopping it", "schedule_id", m.ID, "error", err)
		}
		if err := q.AdvanceSchedule(ctx, dbq.AdvanceScheduleParams{ID: m.ID, NextRunAt: next, RanAt: now}); err != nil {
			return 0, err
		}
		key := strconv.FormatInt(m.NextRunAt.Unix(), 10)
		if _, err := s.jobs.InsertTx(ctx, tx, SendArgs{ScheduleID: m.ID, RunKey: key}, nil); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if len(due) > 0 {
		s.log.Info("scheduled messages due", "count", len(due))
	}
	return len(due), nil
}

// Send creates the message for one run. Refusals (an opted-out number, a
// removed phone) are recorded on the schedule and not retried; a rate limit
// is retried until the job's last attempt.
func (s *Service) Send(ctx context.Context, a SendArgs, lastAttempt bool) error {
	m, err := s.q.GetScheduleByID(ctx, a.ScheduleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // deleted since
	}
	if err != nil {
		return err
	}
	msg, _, err := s.msgs.Send(ctx, messaging.SendRequest{
		ProjectID: m.ProjectID, Environment: m.Environment, APIKeyID: m.APIKeyID, To: m.Recipient, Body: m.Body,
		DeviceID: m.DeviceID, Metadata: map[string]any{"schedule_id": m.ID},
		IdempotencyKey: "schedule:" + m.ID + ":" + a.RunKey,
	})
	var rl *messaging.RateLimitError
	switch {
	case err == nil:
		return s.q.RecordScheduleResult(ctx, dbq.RecordScheduleResultParams{ID: m.ID, LastMessageID: &msg.ID})
	case errors.As(err, &rl) && !lastAttempt:
		return err
	case errors.Is(err, messaging.ErrIdempotencyConflict):
		return nil // this run was sent before the message was edited
	}
	reason := describe(err)
	s.log.Warn("scheduled message not sent", "schedule_id", m.ID, "error", err)
	if rerr := s.q.RecordScheduleResult(ctx, dbq.RecordScheduleResultParams{ID: m.ID, LastError: &reason}); rerr != nil {
		return rerr
	}
	var ve *messaging.ValidationError
	var oe *messaging.OptedOutError
	var le *billing.LimitError
	if errors.As(err, &ve) || errors.As(err, &oe) || errors.As(err, &rl) || errors.As(err, &le) {
		return nil
	}
	return err
}

func describe(err error) string {
	var ve *messaging.ValidationError
	var oe *messaging.OptedOutError
	var rl *messaging.RateLimitError
	var le *billing.LimitError
	switch {
	case errors.As(err, &le):
		return "Not sent: " + le.Error()
	case errors.As(err, &oe):
		return "Not sent: " + oe.Number + " opted out of messages from this project."
	case errors.As(err, &ve):
		return "Not sent: " + ve.Message
	case errors.As(err, &rl):
		return "Not sent: the project's hourly sending limit was reached."
	}
	return "Not sent: an internal error occurred."
}
