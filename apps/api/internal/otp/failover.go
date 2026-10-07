package otp

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/message"
	"bridge/internal/messaging"
)

// FailoverWorker runs messaging.OTPFailoverArgs jobs.
type FailoverWorker struct {
	river.WorkerDefaults[messaging.OTPFailoverArgs]
	Service *Service
}

func (w *FailoverWorker) Work(ctx context.Context, job *river.Job[messaging.OTPFailoverArgs]) error {
	return w.Service.Failover(ctx, job.Args.VerificationID, job.Args.MessageID)
}

// Register adds the Verify jobs to a River worker set.
func Register(workers *river.Workers, s *Service) {
	river.AddWorker(workers, &FailoverWorker{Service: s})
}

// Failover resends a live verification's code through another route when its
// SMS was not sent: the message is still queued (no phone accepted it) or it
// failed. A message a phone accepted (sending, sent, delivered) is left alone,
// since resending could deliver the code twice. Each verification fails over
// at most once. messageID, when set, is the message that just failed.
func (s *Service) Failover(ctx context.Context, otpID, messageID string) error {
	v, err := s.q.GetOTPByID(ctx, otpID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if v.Environment != dbq.ApiEnvironmentLive || v.Status != dbq.OtpStatusPending || !s.now().Before(v.ExpiresAt) ||
		v.FailoverMessageID != nil || v.MessageID == nil || v.AppID == nil {
		return nil
	}
	if messageID != "" && *v.MessageID != messageID {
		return nil // a failover message failed; there is no third route
	}
	app, err := s.q.GetVerifyAppByID(ctx, *v.AppID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if app.FailoverAfterSeconds == 0 {
		return nil
	}
	m, err := s.q.GetMessageByID(ctx, *v.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.Status != message.Queued && m.Status != message.Failed {
		return nil
	}
	route, ok, err := s.msgs.PlanFailover(ctx, m)
	if err != nil {
		return err
	}
	if !ok {
		s.log.Info("verification code not sent, but no other route is available", "otp_id", v.ID, "message_id", m.ID)
		return nil
	}
	fresh, sent, err := s.msgs.Failover(ctx, m, route, func(ctx context.Context, q *dbq.Queries, fresh dbq.Message) (bool, error) {
		n, err := q.SetOTPFailover(ctx, dbq.SetOTPFailoverParams{ID: v.ID, FailoverMessageID: fresh.ID})
		return n == 1, err
	})
	if err != nil {
		return err
	}
	if sent {
		s.log.Info("verification failed over", "otp_id", v.ID, "message_id", m.ID, "failover_message_id", fresh.ID)
	}
	return nil
}
