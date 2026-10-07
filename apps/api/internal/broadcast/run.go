package broadcast

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/messaging"
	"bridge/internal/webhook"
)

// RunArgs creates a broadcast's messages and finishes it. One job runs per
// broadcast for its whole life, snoozing while it waits.
type RunArgs struct {
	BroadcastID string `json:"broadcast_id"`
}

func (RunArgs) Kind() string { return "broadcast.run" }

func (RunArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type RunWorker struct {
	river.WorkerDefaults[RunArgs]
	svc *Service
}

func (w *RunWorker) Timeout(*river.Job[RunArgs]) time.Duration { return runBudget + time.Minute }

func (w *RunWorker) Work(ctx context.Context, job *river.Job[RunArgs]) error {
	wait, done, err := w.svc.Run(ctx, job.Args.BroadcastID)
	if err != nil || done {
		return err
	}
	return river.JobSnooze(wait)
}

// Register adds the broadcast worker.
func Register(workers *river.Workers, s *Service) {
	river.AddWorker(workers, &RunWorker{svc: s})
}

// errRecipientTaken means the recipient was canceled (or handled) while its
// message was being created; the message is rolled back.
var errRecipientTaken = errors.New("broadcast recipient is no longer pending")

// Run does one round of work on a broadcast: it creates messages while there
// is room in the broadcast's window, and completes the broadcast once every
// message is finished. done is false when it should run again after wait.
func (s *Service) Run(ctx context.Context, broadcastID string) (wait time.Duration, done bool, err error) {
	b, err := s.q.GetBroadcastByID(ctx, broadcastID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	switch b.Status {
	case StatusCompleted, StatusCanceled:
		return 0, true, nil
	case StatusScheduled:
		if b.ScheduledAt != nil {
			if until := b.ScheduledAt.Sub(s.now()); until > 0 {
				return until, false, nil
			}
		}
		if b, err = s.q.StartBroadcast(ctx, b.ID); errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil // canceled meanwhile; the next run sees it
		} else if err != nil {
			return 0, false, err
		}
	}
	tmpl := ParseTemplate(b.Template)
	window := s.window(ctx, b)
	deadline := time.Now().Add(runBudget)
	for {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		inFlight, err := s.q.BroadcastInFlight(ctx, b.ID)
		if err != nil {
			return 0, false, err
		}
		room := window - int(inFlight)
		pending, err := s.q.PendingBroadcastRecipients(ctx, dbq.PendingBroadcastRecipientsParams{BroadcastID: b.ID, RowLimit: int32(max(min(room, chunk), 1))})
		if err != nil {
			return 0, false, err
		}
		if len(pending) == 0 {
			if inFlight > 0 {
				return s.poll, false, nil
			}
			return 0, true, s.complete(ctx, b)
		}
		if room <= 0 {
			return s.poll, false, nil
		}
		for _, r := range pending {
			if err := s.sendOne(ctx, b, tmpl, r); err != nil {
				return 0, false, err
			}
		}
		// Stop early when the broadcast was canceled meanwhile.
		cur, err := s.q.GetBroadcastByID(ctx, b.ID)
		if err != nil {
			return 0, false, err
		}
		if cur.Status != StatusSending {
			return 0, true, nil
		}
		if time.Now().After(deadline) {
			return 0, false, nil // yield the worker; continue at once
		}
	}
}

// window is how many of a broadcast's messages may wait to be sent at once:
// what the project's phones can send in one send-limit window, so none waits
// long enough to time out in the queue. Providers and the test simulator are
// not paced by phones.
func (s *Service) window(ctx context.Context, b dbq.Broadcast) int {
	if b.Environment == dbq.ApiEnvironmentTest {
		return maxWindow
	}
	if b.DeviceID == nil {
		if r, err := s.q.GetRouting(ctx, b.ProjectID); err == nil && r.Mode != messaging.RoutePhones {
			if accounts, err := s.q.EnabledProviderAccounts(ctx, b.ProjectID); err == nil && len(accounts) > 0 {
				return maxWindow
			}
		}
	}
	capacity, err := s.q.ProjectSendCapacity(ctx, dbq.ProjectSendCapacityParams{ProjectID: b.ProjectID, DeviceID: b.DeviceID})
	if err != nil {
		return minWindow
	}
	return max(minWindow, min(int(capacity), maxWindow))
}

// sendOne creates one recipient's message. A recipient the pipeline refuses
// (opted out meanwhile, or its phone removed) is skipped with the reason.
func (s *Service) sendOne(ctx context.Context, b dbq.Broadcast, tmpl Template, r dbq.BroadcastRecipient) error {
	vars := map[string]string{}
	if len(r.Vars) > 0 {
		_ = json.Unmarshal(r.Vars, &vars)
	}
	body, _ := tmpl.Render(vars)
	_, _, err := s.msgs.Send(ctx, messaging.SendRequest{
		ProjectID: b.ProjectID, Environment: b.Environment, APIKeyID: b.APIKeyID, To: r.Recipient, Body: body,
		DeviceID: b.DeviceID, Metadata: map[string]any{"broadcast_id": b.ID}, Bulk: true,
		OnCreate: func(ctx context.Context, q *dbq.Queries, m dbq.Message) error {
			n, err := q.MarkRecipientQueued(ctx, dbq.MarkRecipientQueuedParams{BroadcastID: b.ID, Position: r.Position, MessageID: &m.ID})
			if err == nil && n == 0 {
				err = errRecipientTaken
			}
			return err
		},
	})
	var oe *messaging.OptedOutError
	var ve *messaging.ValidationError
	switch {
	case err == nil, errors.Is(err, errRecipientTaken):
		return nil
	case errors.As(err, &oe):
		return s.q.MarkRecipientSkipped(ctx, dbq.MarkRecipientSkippedParams{BroadcastID: b.ID, Position: r.Position, SkipReason: ptr("opted_out")})
	case errors.As(err, &ve):
		return s.q.MarkRecipientSkipped(ctx, dbq.MarkRecipientSkippedParams{BroadcastID: b.ID, Position: r.Position, SkipReason: ptr("invalid: " + ve.Message)})
	}
	return err
}

// complete finishes a broadcast and announces it.
func (s *Service) complete(ctx context.Context, b dbq.Broadcast) error {
	b, err := s.q.CompleteBroadcast(ctx, b.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	views, err := s.Views(ctx, []dbq.Broadcast{b})
	if err != nil {
		return err
	}
	s.log.Info("broadcast completed", "broadcast_id", b.ID, "project_id", b.ProjectID,
		"sent", views[0].Counts.Sent+views[0].Counts.Delivered, "failed", views[0].Counts.Failed)
	s.msgs.Emit(ctx, b.ProjectID, webhook.EventBroadcastCompleted, views[0])
	return nil
}

func ptr[T any](v T) *T { return &v }
