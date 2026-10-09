package messaging

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/message"
	"bridge/internal/push"
)

// DispatchArgs assigns a queued message to a device.
type DispatchArgs struct {
	MessageID string `json:"message_id"`
}

func (DispatchArgs) Kind() string { return "message.dispatch" }

// InsertOpts: dispatch jobs are deliberately not unique. Dispatch is
// idempotent (it only assigns a queued, unassigned message through a
// conditional update), so an extra job simply finds nothing to do. Uniqueness
// would instead drop a needed retry when a phone fails a message while the
// first dispatch job is still running.
func (DispatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10}
}

// Dispatch assigns a message to the best device and sends it the job. It
// returns a snooze duration when no device can take the message yet.
func (s *Service) Dispatch(ctx context.Context, messageID string) (time.Duration, error) {
	m, err := s.q.GetMessageByID(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if m.Status != message.Queued || m.DeviceID != nil || m.Provider != ProviderAndroid {
		return 0, nil // already assigned, handed to a provider, or finished
	}
	now := s.now()
	rt := s.routeFor(ctx, m)
	if rt.only {
		return 0, s.handoff(ctx, m, "providers_only")
	}
	rows, err := s.q.DispatchCandidates(ctx, m.ProjectID)
	if err != nil {
		return 0, err
	}
	cands := make([]candidate, 0, len(rows))
	for _, r := range rows {
		cands = append(cands, candidateFrom(r))
	}
	sel := choose(cands, m.RequestedDeviceID, now)

	if now.Sub(m.CreatedAt) > s.cfg.QueueTimeout {
		if rt.fallback {
			return 0, s.handoff(ctx, m, "no_phone_in_time")
		}
		code, msg := "no_device_available",
			"No phone could send this message within "+s.cfg.QueueTimeout.String()+". Check that a paired phone is online."
		if sel.Reason == "devices_at_daily_limit" {
			code, msg = "daily_limit_reached",
				"Every online phone already sent its daily SMS limit, which keeps SIMs within their operator's allowance. "+
					"Send it again later, pair another phone, or raise the phone's daily limit if its SIM plan allows more."
		}
		_, _, err := s.transition(ctx, s.q, m, message.Failed, "failed", nil, code, msg, nil)
		return 0, err
	}

	switch sel.Reason {
	case "no_device":
		if rt.fallback {
			return 0, s.handoff(ctx, m, "no_paired_phone")
		}
		_, _, err := s.transition(ctx, s.q, m, message.Failed, "failed", nil, "no_device",
			"This project has no paired phones. Pair one under Devices.", nil)
		return 0, err
	case "device_not_found":
		_, _, err := s.transition(ctx, s.q, m, message.Failed, "failed", nil, "device_not_found",
			"The requested device was removed from the project.", nil)
		return 0, err
	}
	if sel.Device == nil {
		if rt.fallback {
			waited := now.Sub(m.CreatedAt)
			if waited >= rt.wait {
				return 0, s.handoff(ctx, m, "no_phone_available")
			}
			s.wakeOffline(ctx, sel.Offline)
			return min(sel.RetryIn, rt.wait-waited), nil
		}
		s.wakeOffline(ctx, sel.Offline)
		return sel.RetryIn, nil
	}

	assigned, err := s.q.AssignMessage(ctx, dbq.AssignMessageParams{ID: m.ID, DeviceID: &sel.Device.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // someone else assigned it
	}
	if err != nil {
		return 0, err
	}
	if err := s.event(ctx, s.q, assigned, "assigned", nil, nil, map[string]any{
		"device_id": sel.Device.ID, "device_name": sel.Device.Name, "attempt": assigned.Attempts,
	}); err != nil {
		return 0, err
	}
	if err := s.publish(ctx, sel.Device.ID, gateway.Outbound{Type: gateway.TypeSendSMS, MessageID: m.ID}); err != nil {
		// The assignment sweep re-dispatches if the phone never accepts it.
		s.log.Warn("could not publish message to device", "message_id", m.ID, "device_id", sel.Device.ID, "error", err)
	}
	return 0, nil
}

// wakeOffline asks offline devices to reconnect, at most once a minute each.
func (s *Service) wakeOffline(ctx context.Context, devices []dbq.Device) {
	if s.waker == nil {
		return
	}
	for _, d := range devices {
		if d.PushProvider == nil || d.PushEndpoint == nil {
			continue
		}
		res, _ := s.limiter.Hit(ctx, "wake:"+d.ID, 1, time.Minute)
		if !res.Allowed {
			continue
		}
		t := push.Target{Provider: *d.PushProvider, Endpoint: *d.PushEndpoint}
		if d.PushP256dh != nil && d.PushAuth != nil {
			t.P256dh, t.Auth = *d.PushP256dh, *d.PushAuth
		}
		if err := s.waker.Wake(ctx, t, "queued"); err != nil {
			s.log.Warn("push wake failed", "device_id", d.ID, "error", err)
			if errors.Is(err, push.ErrGone) || errors.Is(err, push.ErrInvalidTarget) {
				_ = s.q.UpdateDevicePush(ctx, dbq.UpdateDevicePushParams{ID: d.ID})
			}
		}
	}
}

// HydrateSend builds the full send_sms frame for a device, if the message is still its to send.
func (s *Service) HydrateSend(ctx context.Context, deviceID, messageID string) (*gateway.Outbound, error) {
	m, err := s.q.GetMessageByID(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.DeviceID == nil || *m.DeviceID != deviceID || (m.Status != message.Queued && m.Status != message.Sending) {
		return nil, nil
	}
	sim := m.SimSlot
	if sim == nil {
		if d, err := s.q.GetDevice(ctx, dbq.GetDeviceParams{ID: deviceID, ProjectID: m.ProjectID}); err == nil {
			sim = d.PreferredSimSlot
		}
	}
	return &gateway.Outbound{Type: gateway.TypeSendSMS, MessageID: m.ID, To: m.Recipient, Body: m.Body, SimSlot: sim, Attempt: int(m.Attempts)}, nil
}

// KickProject dispatches the project's waiting messages now. Call it when
// capacity may have grown: a phone connected or its send limit was raised.
// Waiting messages otherwise sleep until their snooze ends.
func (s *Service) KickProject(ctx context.Context, projectID string) {
	ids, err := s.q.WaitingMessages(ctx, projectID)
	if err != nil {
		s.log.Error("could not list waiting messages", "project_id", projectID, "error", err)
		return
	}
	for _, id := range ids {
		// A fresh job runs now; the message's snoozed job finds nothing left to do.
		if _, err := s.jobs.Insert(ctx, DispatchArgs{MessageID: id}, nil); err != nil {
			s.log.Warn("could not kick dispatch", "message_id", id, "error", err)
		}
	}
}

// DeviceConnected redelivers everything assigned to a device that just
// connected and lets waiting messages use its capacity.
func (s *Service) DeviceConnected(ctx context.Context, deviceID string) {
	if d, err := s.q.GetDeviceByID(ctx, deviceID); err == nil {
		defer s.KickProject(ctx, d.ProjectID)
	}
	pending, err := s.q.PendingForDevice(ctx, &deviceID)
	if err != nil {
		s.log.Error("could not load pending messages", "device_id", deviceID, "error", err)
		return
	}
	for _, m := range pending {
		if err := s.publish(ctx, deviceID, gateway.Outbound{Type: gateway.TypeSendSMS, MessageID: m.ID}); err != nil {
			s.log.Warn("could not redeliver message", "message_id", m.ID, "device_id", deviceID, "error", err)
		}
	}
	if len(pending) > 0 {
		s.log.Info("redelivered pending messages", "device_id", deviceID, "count", len(pending))
	}
}

// DeviceReport applies a status report from the device the message is assigned to.
func (s *Service) DeviceReport(ctx context.Context, deviceID string, in gateway.Inbound) error {
	m, err := s.q.GetMessageByID(ctx, in.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // unknown message: acknowledge so the phone drops the report
	}
	if err != nil {
		return err
	}
	if m.DeviceID == nil || *m.DeviceID != deviceID {
		s.log.Warn("ignoring report from a device that does not own the message", "message_id", m.ID, "device_id", deviceID)
		return nil
	}
	// A late report from an earlier assignment must not affect the current one.
	if in.Attempt != 0 && in.Attempt != int(m.Attempts) {
		s.log.Info("ignoring report for an earlier attempt", "message_id", m.ID, "report_attempt", in.Attempt, "current_attempt", m.Attempts)
		return nil
	}
	detail := map[string]any{"device_id": deviceID}

	switch in.Type {
	case gateway.TypeSMSAccepted:
		if m.Status == message.Queued {
			_, _, err = s.transition(ctx, s.q, m, message.Sending, "device_accepted", detail, "", "", nil)
		}
		return err

	case gateway.TypeSMSSent:
		if m.Status == message.Queued {
			if m, _, err = s.transition(ctx, s.q, m, message.Sending, "device_accepted", detail, "", "", nil); err != nil {
				return err
			}
		}
		if m.Status == message.Sending {
			if in.Segments != nil {
				detail["segments"] = *in.Segments
			}
			_, _, err = s.transition(ctx, s.q, m, message.Sent, "sent", detail, "", "", in.Segments)
		}
		return err

	case gateway.TypeSMSFailed:
		if m.Status != message.Queued && m.Status != message.Sending {
			return nil
		}
		code := sanitizeCode(in.ErrorCode, "send_failed")
		msg := clipText(in.ErrorMessage, 300, "The phone could not send the message.")
		if in.Retryable && int(m.Attempts) < s.cfg.MaxAttempts {
			return s.requeue(ctx, m, "send_failed_retrying", map[string]any{"device_id": deviceID, "error_code": code, "error_message": msg})
		}
		// Hand over to a provider only when the phone certainly did not send:
		// its retries ran out, or the failure is the phone's own (SIM, SMS
		// centre, fixed dialing). After an ambiguous failure the SMS may have
		// gone out, and a provider send would deliver it twice.
		if (in.Retryable || phoneSideFailures[code]) && s.routeFor(ctx, m).fallback {
			return s.handoff(ctx, m, "phone_failed")
		}
		_, _, err = s.transition(ctx, s.q, m, message.Failed, "failed", detail, code, msg, nil)
		return err

	case gateway.TypeSMSDelivery:
		if m.Status != message.Sent {
			return nil
		}
		if in.Delivered != nil && *in.Delivered {
			_, _, err = s.transition(ctx, s.q, m, message.Delivered, "delivered", detail, "", "", nil)
		} else {
			_, _, err = s.transition(ctx, s.q, m, message.Failed, "delivery_failed", detail,
				sanitizeCode(in.ErrorCode, "delivery_failed"),
				clipText(in.ErrorMessage, 300, "The carrier reported that the message was not delivered."), nil)
		}
		return err
	}
	return nil
}

// requeue returns a message to the queue for another device and schedules a dispatch.
func (s *Service) requeue(ctx context.Context, m dbq.Message, eventType string, detail map[string]any) error {
	return s.requeueWith(ctx, m, eventType, detail, DispatchArgs{MessageID: m.ID}, nil)
}

// requeueWith releases a message from its phone, records eventType, runs
// extra in the same transaction, and schedules job.
func (s *Service) requeueWith(ctx context.Context, m dbq.Message, eventType string, detail map[string]any,
	job river.JobArgs, extra func(q *dbq.Queries) error,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	switch {
	case m.Status == message.Queued && m.DeviceID == nil:
		// Not with any phone: nothing to release.
		if err := s.event(ctx, q, m, eventType, nil, nil, detail); err != nil {
			return err
		}
	case m.Status == message.Sending:
		if _, ok, err := s.transition(ctx, q, m, message.Queued, eventType, detail, "", "", nil); err != nil || !ok {
			return err
		}
	default:
		released, err := q.ReleaseMessage(ctx, dbq.ReleaseMessageParams{ID: m.ID, DeviceID: m.DeviceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.event(ctx, q, released, eventType, nil, nil, detail); err != nil {
			return err
		}
	}
	if extra != nil {
		if err := extra(q); err != nil {
			return err
		}
	}
	if _, err := s.jobs.InsertTx(ctx, tx, job, &river.InsertOpts{ScheduledAt: s.now().Add(2 * time.Second)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SweepAssignments takes back messages that phones did not accept in time.
func (s *Service) SweepAssignments(ctx context.Context) error {
	cutoff := s.now().Add(-s.cfg.AssignTimeout)
	stale, err := s.q.StaleAssignments(ctx, &cutoff)
	if err != nil {
		return err
	}
	for _, m := range stale {
		if int(m.Attempts) >= s.cfg.MaxAttempts {
			if s.routeFor(ctx, m).fallback {
				if err := s.handoff(ctx, m, "phone_unresponsive"); err != nil {
					return err
				}
				continue
			}
			if _, _, err := s.transition(ctx, s.q, m, message.Failed, "failed", map[string]any{"device_id": deref(m.DeviceID)},
				"device_unresponsive", "Phones did not accept this message after several attempts.", nil); err != nil {
				return err
			}
			continue
		}
		if err := s.requeue(ctx, m, "assignment_timed_out", map[string]any{"device_id": deref(m.DeviceID)}); err != nil {
			return err
		}
	}
	return nil
}

// OTPBodyRetention is how long a Verify code's message keeps its text when
// its verification has not finished: the longest code lifetime. Until then the
// code can be resent through another route.
const OTPBodyRetention = time.Hour

// RedactBodies removes message bodies older than the retention period, and
// Verify codes' bodies older than OTPBodyRetention.
func (s *Service) RedactBodies(ctx context.Context) (int64, error) {
	now := s.now()
	return s.q.RedactMessageBodies(ctx, dbq.RedactMessageBodiesParams{
		Before: now.Add(-s.cfg.Retention), OtpBefore: now.Add(-OTPBodyRetention),
	})
}

func sanitizeCode(code, fallback string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" || len(code) > 40 || strings.Trim(code, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
		return fallback
	}
	return code
}

func clipText(s string, n int, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// phoneSideFailures are refusals by the phone or its SIM that say nothing
// about the destination, so another route can still deliver the message.
var phoneSideFailures = map[string]bool{
	"sim_absent": true, "sim_unavailable": true, "permission_denied": true, "invalid_smsc_address": true,
	"fdn_check_failure": true, "operation_not_allowed": true, "access_barred": true, "not_supported": true,
	"blocked_during_emergency": true, "null_pdu": true, "encoding_error": true,
}
