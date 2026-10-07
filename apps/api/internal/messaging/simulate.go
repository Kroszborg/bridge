package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/message"
)

// Test-mode messages never reach a phone. A simulated device walks them
// through the same lifecycle, so integrations can be built at zero cost.
//
// Deterministic test numbers:
//
//	+15550000002  fails: invalid destination
//	+15550000003  sent, but no delivery report ever arrives
//	+15550000004  fails while sending (timeout)
//	+15550000005  sent, then the carrier reports it undelivered
//	anything else delivered
const (
	TestNumberInvalid     = "+15550000002"
	TestNumberNoReport    = "+15550000003"
	TestNumberSendTimeout = "+15550000004"
	TestNumberUndelivered = "+15550000005"
)

// SimulateArgs runs a test-mode message through its lifecycle.
type SimulateArgs struct {
	MessageID string `json:"message_id"`
	Step      int    `json:"step"`
}

func (SimulateArgs) Kind() string { return "message.simulate" }

// QueueSimulator keeps simulations from competing with real dispatch.
const QueueSimulator = "simulator"

func (SimulateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueSimulator, MaxAttempts: 5}
}

// Simulate applies one step and returns the next one to schedule, if any.
func (s *Service) Simulate(ctx context.Context, args SimulateArgs) (*SimulateArgs, time.Duration, error) {
	m, err := s.q.GetMessageByID(ctx, args.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	detail := map[string]any{"simulated": true}
	next := func(d time.Duration) (*SimulateArgs, time.Duration, error) {
		return &SimulateArgs{MessageID: m.ID, Step: args.Step + 1}, d, nil
	}

	switch {
	case args.Step == 0 && m.Status == message.Queued:
		if m.Recipient == TestNumberInvalid {
			_, _, err := s.transition(ctx, s.q, m, message.Failed, "failed", detail, "invalid_destination",
				"Simulated failure: the destination is not a valid mobile number.", nil)
			return nil, 0, err
		}
		if _, _, err := s.transition(ctx, s.q, m, message.Sending, "device_accepted", detail, "", "", nil); err != nil {
			return nil, 0, err
		}
		return next(800 * time.Millisecond)

	case args.Step == 1 && m.Status == message.Sending:
		if m.Recipient == TestNumberSendTimeout {
			_, _, err := s.transition(ctx, s.q, m, message.Failed, "failed", detail, "send_timeout",
				"Simulated failure: the phone did not confirm the send in time.", nil)
			return nil, 0, err
		}
		if _, _, err := s.transition(ctx, s.q, m, message.Sent, "sent", detail, "", "", m.Segments); err != nil {
			return nil, 0, err
		}
		if m.Recipient == TestNumberNoReport {
			return nil, 0, nil
		}
		return next(1500 * time.Millisecond)

	case args.Step == 2 && m.Status == message.Sent:
		if m.Recipient == TestNumberUndelivered {
			_, _, err := s.transition(ctx, s.q, m, message.Failed, "delivery_failed", detail, "delivery_failed",
				"Simulated failure: the carrier reported the message undelivered.", nil)
			return nil, 0, err
		}
		_, _, err := s.transition(ctx, s.q, m, message.Delivered, "delivered", detail, "", "", nil)
		return nil, 0, err
	}
	return nil, 0, nil
}

// SimulateWorker runs simulation steps.
type SimulateWorker struct {
	river.WorkerDefaults[SimulateArgs]
	Service *Service
}

// Work runs every step in one job with short pauses, so test mode feels
// like a real phone without depending on scheduled-job timing.
func (w *SimulateWorker) Work(ctx context.Context, job *river.Job[SimulateArgs]) error {
	args := job.Args
	for {
		next, delay, err := w.Service.Simulate(ctx, args)
		if err != nil || next == nil {
			return err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		args = *next
	}
}

// DispatchWorker runs dispatch jobs, snoozing while no device can take the message.
type DispatchWorker struct {
	river.WorkerDefaults[DispatchArgs]
	Service *Service
}

func (w *DispatchWorker) Work(ctx context.Context, job *river.Job[DispatchArgs]) error {
	snooze, err := w.Service.Dispatch(ctx, job.Args.MessageID)
	if err != nil {
		return err
	}
	if snooze > 0 {
		return river.JobSnooze(snooze)
	}
	return nil
}

// SweepArgs is the periodic job that reclaims unaccepted assignments.
type SweepArgs struct{}

func (SweepArgs) Kind() string { return "message.sweep" }

type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	Service *Service
}

func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	return w.Service.SweepAssignments(ctx)
}

// Register adds the messaging workers.
func Register(workers *river.Workers, s *Service) {
	river.AddWorker(workers, &DispatchWorker{Service: s})
	river.AddWorker(workers, &SimulateWorker{Service: s})
	river.AddWorker(workers, &SweepWorker{Service: s})
}
