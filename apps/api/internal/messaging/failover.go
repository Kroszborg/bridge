package messaging

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/message"
)

// OTPFailoverArgs asks the Verify service to check whether a verification's
// code was sent, and to resend it through another route if it was not. It is
// queued when the code is sent (to run after the app's failover delay) and
// again as soon as the code's message fails. The job carries IDs only, never
// the code.
type OTPFailoverArgs struct {
	VerificationID string `json:"verification_id"`
	// MessageID is the message that failed; empty for the scheduled check.
	MessageID string `json:"message_id,omitempty"`
}

func (OTPFailoverArgs) Kind() string { return "otp.failover" }

func (OTPFailoverArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

// FailoverReason is the error code of a message replaced by a failover.
const FailoverReason = "superseded_by_failover"

// otpMessageFailed queues the failover check for a live Verify code whose
// message just failed.
func (s *Service) otpMessageFailed(ctx context.Context, m dbq.Message) {
	if m.Purpose != PurposeOTP || m.Environment != dbq.ApiEnvironmentLive || s.jobs == nil {
		return
	}
	var meta struct {
		OTPID string `json:"otp_id"`
	}
	if json.Unmarshal(m.Metadata, &meta) != nil || meta.OTPID == "" {
		return // a code delivered for another system: nothing to resend
	}
	if _, err := s.jobs.Insert(ctx, OTPFailoverArgs{VerificationID: meta.OTPID, MessageID: m.ID}, nil); err != nil {
		s.log.Warn("could not queue verification failover", "message_id", m.ID, "error", err)
	}
}

// FailoverRoute is how a failover message is sent: through the project's
// providers, or by one specific phone.
type FailoverRoute struct {
	Providers bool
	DeviceID  string
}

// PlanFailover finds a route other than the one m took: the project's enabled
// SMS providers (unless m already went to them), or else another online phone
// with capacity. ok is false when there is none.
func (s *Service) PlanFailover(ctx context.Context, m dbq.Message) (FailoverRoute, bool, error) {
	onProviders := m.Provider != ProviderAndroid
	// Providers cost money: failover uses them only where the project's
	// routing already allows providers. Otherwise it tries another phone.
	providersAllowed := false
	if r, err := s.q.GetRouting(ctx, m.ProjectID); err == nil && r.Mode != RoutePhones {
		providersAllowed = true
	}
	if s.providers != nil && providersAllowed && m.Environment == dbq.ApiEnvironmentLive && !onProviders {
		accounts, err := s.q.EnabledProviderAccounts(ctx, m.ProjectID)
		if err != nil {
			return FailoverRoute{}, false, err
		}
		if len(accounts) > 0 {
			return FailoverRoute{Providers: true}, true, nil
		}
	}
	// Every phone that already had this message is out: the current assignment
	// may have been taken back by the sweep, so read the assignment history.
	tried := map[string]bool{}
	if m.DeviceID != nil {
		tried[*m.DeviceID] = true
	}
	if m.RequestedDeviceID != nil {
		tried[*m.RequestedDeviceID] = true
	}
	events, err := s.q.ListMessageEvents(ctx, m.ID)
	if err != nil {
		return FailoverRoute{}, false, err
	}
	for _, e := range events {
		var d struct {
			DeviceID string `json:"device_id"`
		}
		if json.Unmarshal(e.Detail, &d) == nil && d.DeviceID != "" {
			tried[d.DeviceID] = true
		}
	}
	rows, err := s.q.DispatchCandidates(ctx, m.ProjectID)
	if err != nil {
		return FailoverRoute{}, false, err
	}
	cands := make([]candidate, 0, len(rows))
	for _, r := range rows {
		if tried[r.Device.ID] {
			continue // a phone that already had this message
		}
		cands = append(cands, candidateFrom(r))
	}
	if sel := choose(cands, nil, s.now()); sel.Device != nil {
		return FailoverRoute{DeviceID: sel.Device.ID}, true, nil
	}
	return FailoverRoute{}, false, nil
}

// Failover resends original's text as a new message through route. A
// still-queued original is first marked failed (superseded_by_failover); if a
// phone accepted it in the meantime nothing is sent. claim runs in the same
// transaction and may decline (false), for example when another failover won.
// sent is false when nothing was sent.
func (s *Service) Failover(ctx context.Context, original dbq.Message, route FailoverRoute,
	claim func(ctx context.Context, q *dbq.Queries, fresh dbq.Message) (bool, error),
) (fresh dbq.Message, sent bool, err error) {
	if original.BodyRedactedAt != nil || original.Body == "" {
		return fresh, false, nil
	}
	if original.Status != message.Queued && original.Status != message.Failed {
		return fresh, false, nil
	}
	if original.Status == message.Failed && original.ErrorCode != nil && AmbiguousFailure(*original.ErrorCode) {
		return fresh, false, nil // the code may already be on its way: a second SMS would duplicate it
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fresh, false, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	superseded := false
	if original.Status == message.Queued {
		updated, ok, err := s.applyTransition(ctx, q, original, message.Failed, "failed", map[string]any{"reason": "failover"},
			FailoverReason, "Not sent in time; the code was resent through another route.", nil)
		if err != nil || !ok {
			return fresh, false, err // a phone took it meanwhile: let it send
		}
		original, superseded = updated, true
	}

	encoding, segments := message.Segments(original.Body)
	seg := int16(segments)
	meta := map[string]any{}
	_ = json.Unmarshal(original.Metadata, &meta)
	meta["failover_of"] = original.ID
	metaJSON, _ := json.Marshal(meta)
	params := dbq.InsertMessageParams{
		ID: id.New(id.Message), ProjectID: original.ProjectID, Environment: original.Environment, Provider: ProviderAndroid,
		APIKeyID: original.APIKeyID, Recipient: original.Recipient, Body: original.Body, Segments: &seg, Encoding: &encoding,
		Metadata: metaJSON, BodySha256: original.BodySha256, BodyLength: original.BodyLength,
		Purpose: cmp.Or(original.Purpose, PurposeMessage), DisplayBody: original.DisplayBody, BodyVars: original.BodyVars,
	}
	routeName := "phone"
	if route.Providers {
		params.Provider, routeName = ProviderFallback, "providers"
	} else {
		if route.DeviceID == "" {
			return fresh, false, errors.New("failover route has neither providers nor a phone")
		}
		params.RequestedDeviceID = &route.DeviceID
	}
	if fresh, err = q.InsertMessage(ctx, params); err != nil {
		return fresh, false, err
	}
	var job river.JobArgs = DispatchArgs{MessageID: fresh.ID}
	if route.Providers {
		job = ProviderSendArgs{MessageID: fresh.ID}
	}
	created := message.Created
	if err := s.event(ctx, q, fresh, "created", nil, &created, map[string]any{"encoding": encoding, "segments": segments}); err != nil {
		return fresh, false, err
	}
	if err := s.event(ctx, q, fresh, "queued", &created, ptr(message.Queued), nil); err != nil {
		return fresh, false, err
	}
	detail := map[string]any{"original_message_id": original.ID, "route": routeName}
	if route.DeviceID != "" {
		detail["device_id"] = route.DeviceID
	}
	if err := s.event(ctx, q, fresh, "failover", nil, nil, detail); err != nil {
		return fresh, false, err
	}
	if ok, err := claim(ctx, q, fresh); err != nil || !ok {
		return fresh, false, err
	}
	if _, err := s.jobs.InsertTx(ctx, tx, job, nil); err != nil {
		return fresh, false, fmt.Errorf("queue failover job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fresh, false, err
	}
	if superseded {
		s.emit(ctx, original, statusEvent(message.Failed))
	}
	s.log.Info("verification code resent through another route", "message_id", fresh.ID, "original_message_id", original.ID,
		"route", routeName, "to", maskNumber(fresh.Recipient))
	return fresh, true, nil
}

// AmbiguousFailure reports failures after which the SMS may still have gone
// out: Android's generic or unknown errors, and a provider that timed out or
// answered 5xx after possibly accepting it. Failover skips these.
func AmbiguousFailure(code string) bool {
	return code == "generic_failure" || strings.HasPrefix(code, "android_error_") ||
		strings.HasSuffix(code, "_unreachable") || strings.Contains(code, "_http_5")
}
