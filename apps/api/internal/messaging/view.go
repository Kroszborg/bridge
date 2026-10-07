package messaging

import (
	"encoding/json"
	"time"

	"bridge/internal/db/dbq"
)

// Message is an SMS and its delivery state.
type Message struct {
	ID           string         `json:"id" example:"msg_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Status       string         `json:"status" enum:"created,queued,sending,sent,delivered,failed,received"`
	Direction    string         `json:"direction" enum:"outbound,inbound"`
	Environment  string         `json:"environment" enum:"live,test"`
	To           string         `json:"to" example:"+919876543210" doc:"Recipient of an outbound message; empty for inbound messages."`
	From         *string        `json:"from" nullable:"true" example:"+919876543210" doc:"Sender of an inbound message: a number or an alphanumeric sender ID."`
	Body         *string        `json:"body" nullable:"true" doc:"Null once redacted after the retention period."`
	BodyRedacted bool           `json:"body_redacted"`
	Encoding     *string        `json:"encoding" nullable:"true" enum:"gsm7,ucs2"`
	Segments     *int16         `json:"segments" nullable:"true" doc:"SMS segments the message occupies; carriers bill per segment."`
	Provider     string         `json:"provider" enum:"android,simulator"`
	DeviceID     *string        `json:"device_id" nullable:"true" doc:"The phone currently or finally responsible for the message."`
	SimSlot      *int16         `json:"sim_slot" nullable:"true"`
	Attempts     int32          `json:"attempts"`
	ErrorCode    *string        `json:"error_code" nullable:"true" example:"no_device"`
	ErrorMessage *string        `json:"error_message" nullable:"true"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	QueuedAt     *time.Time     `json:"queued_at" nullable:"true"`
	SendingAt    *time.Time     `json:"sending_at" nullable:"true"`
	SentAt       *time.Time     `json:"sent_at" nullable:"true"`
	DeliveredAt  *time.Time     `json:"delivered_at" nullable:"true"`
	FailedAt     *time.Time     `json:"failed_at" nullable:"true"`
}

// View renders a message for the API and webhooks.
func View(m dbq.Message) Message {
	out := Message{
		ID: m.ID, Status: string(m.Status), Direction: string(m.Direction), Environment: string(m.Environment),
		To: m.Recipient, From: m.Sender, BodyRedacted: m.BodyRedactedAt != nil, Encoding: m.Encoding, Segments: m.Segments,
		Provider: m.Provider, DeviceID: m.DeviceID, SimSlot: m.SimSlot, Attempts: m.Attempts,
		ErrorCode: m.ErrorCode, ErrorMessage: m.ErrorMessage, Metadata: map[string]any{},
		CreatedAt: m.CreatedAt, QueuedAt: m.QueuedAt, SendingAt: m.SendingAt, SentAt: m.SentAt,
		DeliveredAt: m.DeliveredAt, FailedAt: m.FailedAt,
	}
	if m.BodyRedactedAt == nil {
		body := m.Body
		out.Body = &body
	}
	_ = json.Unmarshal(m.Metadata, &out.Metadata)
	return out
}
