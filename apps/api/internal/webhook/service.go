package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"bridge/internal/db/dbq"
	"bridge/internal/events"
	"bridge/internal/id"
	"bridge/internal/push"
)

// Event types.
const (
	EventMessageSent      = "message.sent"
	EventMessageDelivered = "message.delivered"
	EventMessageFailed    = "message.failed"
	EventMessageReceived  = "message.received"
	EventDeviceOnline     = "device.online"
	EventDeviceOffline    = "device.offline"
	EventOTPVerified      = "otp.verified"
	EventOTPFailed        = "otp.failed"
	EventOTPExpired       = "otp.expired"
	EventOTPBlocked       = "otp.blocked"
	// EventTest is sent only by "Send test event", to one endpoint.
	EventTest = "webhook.test"
)

// EventTypes lists the events an endpoint can subscribe to.
var EventTypes = []string{
	EventMessageSent, EventMessageDelivered, EventMessageFailed, EventMessageReceived,
	EventDeviceOnline, EventDeviceOffline,
	EventOTPVerified, EventOTPFailed, EventOTPExpired, EventOTPBlocked,
}

// ValidEventType reports whether t can be subscribed to.
func ValidEventType(t string) bool { return slices.Contains(EventTypes, t) }

// JobInserter is satisfied by *river.Client.
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

type Service struct {
	pool         *pgxpool.Pool
	q            *dbq.Queries
	log          *slog.Logger
	jobs         JobInserter
	client       *http.Client
	allowPrivate bool
	now          func() time.Time
}

type Options struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	// AllowPrivate lets endpoints resolve to loopback and private addresses.
	AllowPrivate bool
}

func New(o Options) *Service {
	client := push.SafeClient(o.AllowPrivate)
	client.Timeout = requestTimeout
	return &Service{
		pool: o.Pool, q: dbq.New(o.Pool), log: o.Logger, client: client, allowPrivate: o.AllowPrivate, now: time.Now,
	}
}

// SetJobInserter wires the River client, which is created after the service.
func (s *Service) SetJobInserter(j JobInserter) { s.jobs = j }

// Emit records an event, queues a delivery to every enabled endpoint of the
// project subscribed to it, and publishes it to live subscribers (the event
// stream). Events are stored only when an endpoint needs them, or when they
// are too large to publish inline.
func (s *Service) Emit(ctx context.Context, projectID, eventType string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", eventType, err)
	}
	endpoints, err := s.q.SubscribedEndpoints(ctx, dbq.SubscribedEndpointsParams{ProjectID: projectID, EventType: eventType})
	if err != nil {
		return err
	}
	ev := events.Event{ID: id.New(id.Event), Type: eventType, Timestamp: s.now().UTC(), Data: payload}
	env := environmentOf(payload)
	stored := false
	if len(endpoints) > 0 || !events.Fits(projectID, env, ev) {
		row, err := s.enqueue(ctx, ev.ID, projectID, eventType, payload, endpoints)
		if err != nil {
			return err
		}
		ev.Timestamp, stored = row.CreatedAt, true
	}
	// The stream is best effort: a subscriber that misses an event can read the API.
	if err := events.Publish(ctx, s.pool, projectID, env, ev, stored); err != nil {
		s.log.Warn("could not publish event to live subscribers", "event_id", ev.ID, "type", eventType, "error", err)
	}
	return nil
}

// environmentOf returns a message or verification event's environment; device events have none.
func environmentOf(payload []byte) string {
	var v struct {
		Environment string `json:"environment"`
	}
	_ = json.Unmarshal(payload, &v)
	return v.Environment
}

// SendTest queues a webhook.test event to one endpoint, whatever it subscribes to.
func (s *Service) SendTest(ctx context.Context, ep dbq.WebhookEndpoint) (dbq.WebhookEvent, error) {
	payload, _ := json.Marshal(map[string]any{
		"endpoint_id": ep.ID,
		"message":     "This is a test event from Bridge. Verify its signature with your endpoint's signing secret.",
	})
	return s.enqueue(ctx, id.New(id.Event), ep.ProjectID, EventTest, payload, []string{ep.ID})
}

func (s *Service) enqueue(ctx context.Context, eventID, projectID, eventType string, payload []byte, endpoints []string) (dbq.WebhookEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbq.WebhookEvent{}, err
	}
	defer tx.Rollback(ctx)
	ev, err := s.q.WithTx(tx).InsertWebhookEvent(ctx, dbq.InsertWebhookEventParams{
		ID: eventID, ProjectID: projectID, Type: eventType, Payload: payload,
	})
	if err != nil {
		return ev, err
	}
	for _, endpointID := range endpoints {
		if _, err := s.jobs.InsertTx(ctx, tx, DeliverArgs{EndpointID: endpointID, EventID: ev.ID}, nil); err != nil {
			return ev, fmt.Errorf("queue webhook delivery: %w", err)
		}
	}
	return ev, tx.Commit(ctx)
}

// Envelope is the JSON body of every webhook, in the Standard Webhooks shape.
type Envelope struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// ValidationError explains why an endpoint URL is not accepted.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// ValidateURL checks an endpoint URL. Addresses are checked again when
// connecting, after DNS resolution, so this only catches obvious mistakes.
func (s *Service) ValidateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", &ValidationError{"Use an absolute http:// or https:// URL, for example https://example.com/webhooks/bridge."}
	}
	if u.User != nil {
		return "", &ValidationError{"Remove the username and password from the URL. Verify requests with the signing secret instead."}
	}
	if u.Fragment != "" {
		return "", &ValidationError{"Remove the #fragment from the URL."}
	}
	if !s.allowPrivate && !publicHost(u.Hostname()) {
		return "", &ValidationError{"This URL points at a private or local address. Use a public URL, or set " +
			"BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true on the server to deliver inside your network."}
	}
	return u.String(), nil
}

func publicHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return false
	}
	if ip, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return push.IsPublic(ip.Unmap())
	}
	return !strings.Contains(h, ":") && net.ParseIP(h) == nil
}

var errEndpointGone = errors.New("endpoint no longer exists or is disabled")
