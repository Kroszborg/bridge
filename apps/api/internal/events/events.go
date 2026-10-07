// Package events delivers Bridge events (the same ones sent to webhooks) to
// live subscribers such as `bridgectl listen`, across API instances through
// PostgreSQL LISTEN/NOTIFY.
//
// Small events travel inside the notification and are never stored. Events
// too large for NOTIFY's 8 KB limit are stored in webhook_events and announced
// by ID.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bridge/internal/db/dbq"
)

const (
	channel = "bridge_events"
	// Postgres caps NOTIFY payloads at 8000 bytes.
	maxInline = 7800
	// A subscriber that falls this far behind is dropped; it reconnects.
	subscriberBuffer = 256
)

// Event is one event as subscribers receive it.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type notification struct {
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment,omitempty"` // empty: every environment (device events)
	EventID     string `json:"event_id,omitempty"`    // set when the event is stored instead of inline
	Event       *Event `json:"event,omitempty"`
}

// Fits reports whether an event can travel inline.
func Fits(projectID, environment string, ev Event) bool {
	b, _ := json.Marshal(notification{ProjectID: projectID, Environment: environment, Event: &ev})
	return len(b) <= maxInline
}

// Publish announces an event. When stored is true the event is already in
// webhook_events and only its ID is sent if it is too large to inline.
func Publish(ctx context.Context, pool *pgxpool.Pool, projectID, environment string, ev Event, stored bool) error {
	n := notification{ProjectID: projectID, Environment: environment, Event: &ev}
	payload, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if len(payload) > maxInline {
		if !stored {
			return errors.New("event too large to publish inline and not stored")
		}
		payload, _ = json.Marshal(notification{ProjectID: projectID, Environment: environment, EventID: ev.ID})
	}
	_, err = pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, string(payload))
	return err
}

// Subscription receives a project's events for one environment.
type Subscription struct {
	C           <-chan Event
	c           chan Event
	projectID   string
	environment string
	closed      bool
}

// Broker fans events out to this instance's subscribers.
type Broker struct {
	pool *pgxpool.Pool
	q    *dbq.Queries
	log  *slog.Logger

	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

func NewBroker(pool *pgxpool.Pool, logger *slog.Logger) *Broker {
	return &Broker{pool: pool, q: dbq.New(pool), log: logger, subs: map[*Subscription]struct{}{}}
}

// Subscribe starts receiving events. Call Unsubscribe when done.
func (b *Broker) Subscribe(projectID, environment string) *Subscription {
	c := make(chan Event, subscriberBuffer)
	s := &Subscription{C: c, c: c, projectID: projectID, environment: environment}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Broker) Unsubscribe(s *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		if !s.closed {
			s.closed = true
			close(s.c)
		}
	}
}

func (b *Broker) dispatch(n notification, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if s.projectID != n.ProjectID || (n.Environment != "" && n.Environment != s.environment) {
			continue
		}
		select {
		case s.c <- ev:
		default:
			// Too slow: close it so the client reconnects rather than silently missing events.
			delete(b.subs, s)
			s.closed = true
			close(s.c)
			b.log.Warn("event subscriber fell behind; disconnected", "project_id", s.projectID)
		}
	}
}

func (b *Broker) hasSubscribers(projectID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if s.projectID == projectID {
			return true
		}
	}
	return false
}

// Run listens for events until ctx ends, reconnecting with backoff.
func (b *Broker) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		err := b.listen(ctx)
		if ctx.Err() != nil {
			break
		}
		b.log.Warn("event listener stopped; retrying", "error", err, "retry_in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	b.mu.Lock()
	for s := range b.subs {
		delete(b.subs, s)
		s.closed = true
		close(s.c)
	}
	b.mu.Unlock()
	return nil
}

func (b *Broker) listen(ctx context.Context) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	for {
		msg, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var n notification
		if err := json.Unmarshal([]byte(msg.Payload), &n); err != nil {
			continue
		}
		if !b.hasSubscribers(n.ProjectID) {
			continue
		}
		if n.Event != nil {
			b.dispatch(n, *n.Event)
			continue
		}
		stored, err := b.q.GetWebhookEvent(ctx, n.EventID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			b.log.Warn("could not load event for subscribers", "event_id", n.EventID, "error", err)
			continue
		}
		b.dispatch(n, Event{ID: stored.ID, Type: stored.Type, Timestamp: stored.CreatedAt, Data: stored.Payload})
	}
}
