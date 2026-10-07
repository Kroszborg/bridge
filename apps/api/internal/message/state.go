// Package message holds the message lifecycle rules. Every status change in
// Bridge must be validated here; no other code decides which transitions are
// legal.
//
//	created ─► queued ─► sending ─► sent ─► delivered
//	   │         │          │  ▲      │
//	   └────►────┴────►─────┤  │      └──► failed
//	                        │  └─ queued   (device dropped before confirming; retry)
//	                        └──► failed
//
// Inbound messages are created directly in the terminal "received" state.
package message

import (
	"fmt"

	"bridge/internal/db/dbq"
)

type Status = dbq.MessageStatus

const (
	Created   = dbq.MessageStatusCreated
	Queued    = dbq.MessageStatusQueued
	Sending   = dbq.MessageStatusSending
	Sent      = dbq.MessageStatusSent
	Delivered = dbq.MessageStatusDelivered
	Failed    = dbq.MessageStatusFailed
	Received  = dbq.MessageStatusReceived
)

var transitions = map[Status][]Status{
	Created: {Queued, Failed},
	Queued:  {Sending, Failed},
	Sending: {Sent, Failed, Queued},
	Sent:    {Delivered, Failed},
}

// InvalidTransitionError is returned for an illegal status change.
type InvalidTransitionError struct{ From, To Status }

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("message cannot move from %s to %s", e.From, e.To)
}

// CanTransition reports whether a message may move from one status to another.
func CanTransition(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Transition validates a status change.
func Transition(from, to Status) error {
	if !CanTransition(from, to) {
		return &InvalidTransitionError{From: from, To: to}
	}
	return nil
}

// IsTerminal reports whether no further transitions are possible.
func IsTerminal(s Status) bool { return len(transitions[s]) == 0 }
