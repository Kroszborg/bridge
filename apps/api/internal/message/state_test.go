package message

import (
	"errors"
	"testing"
)

func TestHappyPath(t *testing.T) {
	path := []Status{Created, Queued, Sending, Sent, Delivered}
	for i := 0; i < len(path)-1; i++ {
		if err := Transition(path[i], path[i+1]); err != nil {
			t.Fatalf("happy path step %s→%s rejected: %v", path[i], path[i+1], err)
		}
	}
}

func TestFailurePaths(t *testing.T) {
	for _, from := range []Status{Created, Queued, Sending, Sent} {
		if !CanTransition(from, Failed) {
			t.Errorf("%s→failed should be allowed", from)
		}
	}
}

func TestRetryRequeue(t *testing.T) {
	if !CanTransition(Sending, Queued) {
		t.Fatal("sending→queued (retry) should be allowed")
	}
	if CanTransition(Sent, Queued) {
		t.Fatal("sent→queued would re-send an SMS the carrier already accepted")
	}
}

func TestNoFalseDelivery(t *testing.T) {
	for _, from := range []Status{Created, Queued, Sending, Failed} {
		if CanTransition(from, Delivered) {
			t.Errorf("%s→delivered must be impossible; only sent messages can be delivered", from)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []Status{Delivered, Failed, Received} {
		if !IsTerminal(s) {
			t.Errorf("%s should be terminal", s)
		}
		for _, to := range []Status{Created, Queued, Sending, Sent, Delivered, Failed, Received} {
			if CanTransition(s, to) {
				t.Errorf("terminal %s→%s allowed", s, to)
			}
		}
	}
}

func TestInvalidTransitionError(t *testing.T) {
	err := Transition(Delivered, Queued)
	var ite *InvalidTransitionError
	if !errors.As(err, &ite) || ite.From != Delivered || ite.To != Queued {
		t.Fatalf("unexpected error %v", err)
	}
}
