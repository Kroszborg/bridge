// Package gateway holds the live WebSocket connections to paired Android
// devices, tracks their presence, and delivers commands to them, including
// across API instances via PostgreSQL LISTEN/NOTIFY.
package gateway

import "time"

// ProtocolVersion is bumped on incompatible changes to the frames below.
const ProtocolVersion = 1

// Heartbeat bounds. Devices choose their own interval inside these limits
// (short while charging, long on battery) and declare it in every heartbeat.
const (
	MinHeartbeat     = 15 * time.Second
	MaxHeartbeat     = 10 * time.Minute
	DefaultHeartbeat = time.Minute
	// A device is considered gone after missing two heartbeats plus this grace.
	heartbeatGrace = 30 * time.Second
)

// Frame types.
const (
	// Device → server
	TypeHeartbeat   = "heartbeat"
	TypeSMSAccepted = "sms_accepted" // the phone took the job and is handing it to Android
	TypeSMSSent     = "sms_sent"     // Android reported every segment sent
	TypeSMSFailed   = "sms_failed"   // the phone could not send; may be retryable
	TypeSMSDelivery = "sms_delivery" // the carrier's delivery report arrived
	TypeSMSReceived = "sms_received" // an incoming SMS, sent only while forwarding is on

	// Server → device
	TypeWelcome      = "welcome"
	TypeHeartbeatAck = "heartbeat_ack"
	TypeSync         = "sync"       // report status now and fetch pending work
	TypeUnpaired     = "unpaired"   // credential revoked; forget it and stop
	TypeSendSMS      = "send_sms"   // send a message; the phone de-duplicates by message_id
	TypeReportAck    = "report_ack" // a report was stored; the phone can drop it
	TypeConfig       = "config"     // settings changed in the dashboard
)

// IsReport reports whether t is a message status report from a device.
func IsReport(t string) bool {
	return t == TypeSMSAccepted || t == TypeSMSSent || t == TypeSMSFailed || t == TypeSMSDelivery
}

// Inbound is a frame sent by a device.
type Inbound struct {
	Type   string  `json:"type"`
	Seq    int64   `json:"seq,omitempty"`
	NextIn int     `json:"next_in,omitempty"` // seconds until the next heartbeat
	Status *Status `json:"status,omitempty"`

	// Message reports.
	MessageID    string `json:"message_id,omitempty"`
	Attempt      int    `json:"attempt,omitempty"` // the assignment the report belongs to
	Segments     *int16 `json:"segments,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	Retryable    bool   `json:"retryable,omitempty"`
	Delivered    *bool  `json:"delivered,omitempty"` // sms_delivery: true delivered, false failed

	// sms_received. InboundID is chosen by the phone and makes resends idempotent;
	// the acknowledgement echoes it as message_id.
	InboundID  string `json:"inbound_id,omitempty"`
	From       string `json:"from,omitempty"`
	Body       string `json:"body,omitempty"`
	ReceivedAt int64  `json:"received_at,omitempty"` // Unix milliseconds, phone clock
	SimSlot    *int16 `json:"sim_slot,omitempty"`
}

// SIM is a SIM slot reported by the phone. Phone numbers are never collected.
type SIM struct {
	Slot        int16  `json:"slot"`
	Carrier     string `json:"carrier,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// Status is the device health snapshot carried by heartbeats.
type Status struct {
	BatteryLevel   *int16  `json:"battery_level,omitempty"`
	IsCharging     *bool   `json:"is_charging,omitempty"`
	NetworkType    *string `json:"network_type,omitempty"`
	CarrierName    *string `json:"carrier_name,omitempty"`
	SimCount       *int16  `json:"sim_count,omitempty"`
	DeviceModel    *string `json:"device_model,omitempty"`
	AndroidVersion *string `json:"android_version,omitempty"`
	AppVersion     *string `json:"app_version,omitempty"`
	Sims           []SIM   `json:"sims,omitempty"`
}

// Outbound is a frame sent to a device.
type Outbound struct {
	Type                string     `json:"type"`
	Seq                 int64      `json:"seq,omitempty"`
	DeviceID            string     `json:"device_id,omitempty"`
	ProtocolVersion     int        `json:"protocol_version,omitempty"`
	ServerTime          *time.Time `json:"server_time,omitempty"`
	MaxHeartbeatSeconds int        `json:"max_heartbeat_seconds,omitempty"`
	MinHeartbeatSeconds int        `json:"min_heartbeat_seconds,omitempty"`
	Reason              string     `json:"reason,omitempty"`

	// send_sms and report_ack.
	MessageID string `json:"message_id,omitempty"`
	To        string `json:"to,omitempty"`
	Body      string `json:"body,omitempty"`
	SimSlot   *int16 `json:"sim_slot,omitempty"` // nil: the phone's default SMS SIM
	Attempt   int    `json:"attempt,omitempty"`
	Report    string `json:"report,omitempty"` // report_ack: which report was stored

	// welcome and config.
	ForwardInbound *bool `json:"forward_inbound,omitempty"`
}

// ClampHeartbeat bounds a device-declared heartbeat interval.
func ClampHeartbeat(seconds int) time.Duration {
	d := time.Duration(seconds) * time.Second
	switch {
	case seconds <= 0:
		return DefaultHeartbeat
	case d < MinHeartbeat:
		return MinHeartbeat
	case d > MaxHeartbeat:
		return MaxHeartbeat
	}
	return d
}

// livenessTimeout is how long the server waits for the next frame.
func livenessTimeout(interval time.Duration) time.Duration {
	return 2*interval + heartbeatGrace
}
