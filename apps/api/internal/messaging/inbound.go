package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/id"
	"bridge/internal/message"
	"bridge/internal/webhook"
)

// Emitter sends webhook events. *webhook.Service satisfies it.
type Emitter interface {
	Emit(ctx context.Context, projectID, eventType string, data any) error
}

const (
	maxInboundBody         = 5000 // characters; long concatenated messages are kept whole
	maxSenderLength        = 64
	inboundHourlyPerDevice = 1000
)

// DeviceInbound stores an SMS a phone received and announces it with a
// message.received webhook. Resends of the same inbound ID are ignored.
func (s *Service) DeviceInbound(ctx context.Context, deviceID string, in gateway.Inbound) error {
	d, err := s.q.GetDeviceByID(ctx, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.RevokedAt != nil || !d.ForwardInbound {
		// Forwarding was turned off after the phone queued this SMS: drop it.
		s.log.Info("dropping incoming SMS: forwarding is off", "device_id", deviceID)
		return nil
	}
	if !validInboundID(in.InboundID) {
		s.log.Warn("dropping incoming SMS with an invalid inbound_id", "device_id", deviceID)
		return nil
	}
	if res, err := s.limiter.Hit(ctx, "inbound:"+deviceID, inboundHourlyPerDevice, time.Hour); err == nil && !res.Allowed {
		// Not acknowledged: the phone keeps it and retries on a later connection.
		return &RateLimitError{Scope: "inbound", RetryAfter: res.RetryAfter}
	}

	from := strings.TrimSpace(in.From)
	if from == "" {
		from = "unknown"
	}
	from = clipText(from, maxSenderLength, "unknown")
	body := in.Body
	if r := []rune(body); len(r) > maxInboundBody {
		body = string(r[:maxInboundBody])
	}
	var sim *int16
	if in.SimSlot != nil && (*in.SimSlot == 1 || *in.SimSlot == 2) {
		sim = in.SimSlot
	}
	meta := map[string]any{}
	if in.ReceivedAt > 0 {
		meta["device_received_at"] = time.UnixMilli(in.ReceivedAt).UTC().Format(time.RFC3339Nano)
	}
	rawMeta, _ := json.Marshal(meta)
	encoding, segments := message.Segments(body)
	seg := int16(segments)
	sum := sha256.Sum256([]byte(body))
	key := "inbound:" + deviceID + ":" + in.InboundID

	m, err := s.q.InsertInboundMessage(ctx, dbq.InsertInboundMessageParams{
		ID: id.New(id.Message), ProjectID: d.ProjectID, DeviceID: &d.ID, Sender: &from, Body: body,
		Segments: &seg, Encoding: &encoding, IdempotencyKey: &key, SimSlot: sim,
		BodySha256: sum[:], BodyLength: ptr(int32(len([]rune(body)))), Metadata: rawMeta,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already stored: the phone missed our acknowledgement
	}
	if err != nil {
		return err
	}
	received := message.Received
	if err := s.event(ctx, s.q, m, "received", nil, &received, map[string]any{"device_id": d.ID, "device_name": d.Name}); err != nil {
		return err
	}
	s.log.Info("incoming SMS stored", "message_id", m.ID, "device_id", d.ID, "from", maskNumber(from), "segments", segments)
	s.emit(ctx, m, webhook.EventMessageReceived)
	return nil
}

// emit announces a message event. Failures are logged: the message itself is already stored.
func (s *Service) emit(ctx context.Context, m dbq.Message, eventType string) {
	if s.emitter == nil {
		return
	}
	if err := s.emitter.Emit(ctx, m.ProjectID, eventType, View(m)); err != nil {
		s.log.Error("could not queue webhook event", "message_id", m.ID, "type", eventType, "error", err)
	}
}

// statusEvent maps a message status to the webhook announcing it.
func statusEvent(st message.Status) string {
	switch st {
	case message.Sent:
		return webhook.EventMessageSent
	case message.Delivered:
		return webhook.EventMessageDelivered
	case message.Failed:
		return webhook.EventMessageFailed
	}
	return ""
}

func validInboundID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	return strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == ""
}
