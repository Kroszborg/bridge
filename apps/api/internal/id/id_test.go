package id

import (
	"testing"
	"time"
)

func TestNewFormat(t *testing.T) {
	got := New(Message)
	if len(got) != len("msg_")+26 {
		t.Fatalf("unexpected length %d for %q", len(got), got)
	}
	if !HasPrefix(got, Message) {
		t.Fatalf("HasPrefix(%q, msg) = false", got)
	}
	if HasPrefix(got, Device) {
		t.Fatalf("HasPrefix(%q, dev) = true", got)
	}
}

func TestSortableByTime(t *testing.T) {
	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	prev := NewAt(Message, base)
	for i := 1; i < 200; i++ {
		next := NewAt(Message, base.Add(time.Duration(i)*time.Millisecond))
		if next <= prev {
			t.Fatalf("id %q generated later does not sort after %q", next, prev)
		}
		prev = next
	}
}

func TestUnique(t *testing.T) {
	seen := make(map[string]struct{}, 10000)
	for range 10000 {
		v := New(Request)
		if _, dup := seen[v]; dup {
			t.Fatalf("duplicate id %q", v)
		}
		seen[v] = struct{}{}
	}
}

func TestHasPrefixRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "msg_", "msg_short", "msg_" + "U" + "0000000000000000000000000", "msgx01j9tq4m2xk3v8c7e5r2n0w6yb"} {
		if HasPrefix(s, Message) {
			t.Errorf("HasPrefix(%q) = true, want false", s)
		}
	}
}

func TestNewIsMonotonicWithinAMillisecond(t *testing.T) {
	prev := New(MessageEvent)
	for range 5000 {
		next := New(MessageEvent)
		if next <= prev {
			t.Fatalf("%q generated after %q does not sort after it", next, prev)
		}
		prev = next
	}
}
