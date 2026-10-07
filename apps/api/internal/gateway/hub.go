package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
)

const (
	notifyChannel = "bridge_devices"
	writeTimeout  = 10 * time.Second
	readLimit     = 64 << 10
	// Status writes to the database are throttled for chatty devices.
	minStatusWriteGap = 5 * time.Second
)

// WebSocket close codes in the private range (4000–4999).
const (
	CloseReplaced         websocket.StatusCode = 4001 // a newer connection took over
	CloseUnpaired         websocket.StatusCode = 4003 // the credential was revoked
	CloseHeartbeatTimeout websocket.StatusCode = 4008
	CloseProtocolError    websocket.StatusCode = 4400
)

// Handler receives device events that concern messages.
type Handler interface {
	// DeviceConnected runs after a device's connection is established, e.g. to redeliver its work.
	DeviceConnected(ctx context.Context, deviceID string)
	// DeviceReport stores a message status report. The report is acknowledged
	// to the device only when this returns nil, so the device resends otherwise.
	DeviceReport(ctx context.Context, deviceID string, in Inbound) error
	// HydrateSend turns a lean send_sms notification (message ID only) into the
	// full frame, loaded fresh from the database. It returns nil when the
	// message is no longer waiting for this device.
	HydrateSend(ctx context.Context, deviceID, messageID string) (*Outbound, error)
	// DeviceInbound stores an incoming SMS. Like reports, it is acknowledged only on nil.
	DeviceInbound(ctx context.Context, deviceID string, in Inbound) error
}

// Hub owns this instance's device connections.
type Hub struct {
	pool    *pgxpool.Pool
	q       *dbq.Queries
	log     *slog.Logger
	handler Handler

	mu    sync.Mutex
	conns map[string]*client // device ID → connection on this instance

	baseCtx context.Context
}

type client struct {
	deviceID string
	connID   string
	ws       *websocket.Conn
	out      chan Outbound
	done     chan struct{}
	once     sync.Once
}

func (c *client) close(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		close(c.done)
		_ = c.ws.Close(code, reason)
	})
}

// enqueue queues a frame without blocking; a full queue means the device is
// not reading, so the connection is dropped and the device will reconnect.
func (c *client) enqueue(m Outbound) {
	select {
	case c.out <- m:
	case <-c.done:
	default:
		c.close(websocket.StatusPolicyViolation, "send queue full")
	}
}

// NewHub creates a hub. Connections end when ctx is cancelled. Call Run to
// start cross-instance delivery.
func NewHub(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) *Hub {
	return &Hub{pool: pool, q: dbq.New(pool), log: logger, conns: map[string]*client{}, baseCtx: ctx}
}

// SetHandler installs the message handler. Call before serving connections.
func (h *Hub) SetHandler(handler Handler) { h.handler = handler }

// Serve upgrades the request and runs the connection until it ends. The
// caller has already authenticated the device.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, device dbq.Device) {
	// Hijacked connections must not inherit the HTTP server's timeouts.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		h.log.Warn("device websocket upgrade failed", "device_id", device.ID, "error", err)
		return
	}
	ws.SetReadLimit(readLimit)

	ctx, cancel := context.WithCancel(h.baseCtx)
	defer cancel()

	c := &client{deviceID: device.ID, connID: id.New("conn"), ws: ws, out: make(chan Outbound, 32), done: make(chan struct{})}
	if old := h.register(c); old != nil {
		old.close(CloseReplaced, "replaced by a newer connection")
	}
	defer h.unregister(c)

	if err := h.q.MarkDeviceConnected(ctx, dbq.MarkDeviceConnectedParams{ID: device.ID, ConnectionID: &c.connID}); err != nil {
		h.log.Error("could not mark device online", "device_id", device.ID, "error", err)
		c.close(websocket.StatusInternalError, "server error")
		return
	}
	h.log.Info("device connected", "device_id", device.ID, "project_id", device.ProjectID, "connection_id", c.connID)
	defer func() {
		// Detached context: the connection context is already cancelled here.
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		if _, err := h.q.MarkDeviceDisconnected(dctx, dbq.MarkDeviceDisconnectedParams{ID: device.ID, ConnectionID: &c.connID}); err != nil {
			h.log.Error("could not mark device offline", "device_id", device.ID, "error", err)
		}
		h.log.Info("device disconnected", "device_id", device.ID, "connection_id", c.connID)
	}()

	now := time.Now().UTC()
	c.enqueue(Outbound{
		Type: TypeWelcome, DeviceID: device.ID, ProtocolVersion: ProtocolVersion, ServerTime: &now,
		MinHeartbeatSeconds: int(MinHeartbeat.Seconds()), MaxHeartbeatSeconds: int(MaxHeartbeat.Seconds()),
		ForwardInbound: &device.ForwardInbound,
	})

	go h.writeLoop(ctx, c)
	if h.handler != nil {
		go h.handler.DeviceConnected(ctx, device.ID)
	}
	h.readLoop(ctx, c, ClampHeartbeat(int(device.HeartbeatInterval)))
}

func (h *Hub) writeLoop(ctx context.Context, c *client) {
	for {
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			c.close(websocket.StatusGoingAway, "server shutting down")
			return
		case m := <-c.out:
			data, _ := json.Marshal(m)
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.close(websocket.StatusAbnormalClosure, "write failed")
				return
			}
			if m.Type == TypeUnpaired {
				c.close(CloseUnpaired, "device unpaired")
				return
			}
		}
	}
}

func (h *Hub) readLoop(ctx context.Context, c *client, interval time.Duration) {
	var lastWrite time.Time
	for {
		rctx, cancel := context.WithTimeout(ctx, livenessTimeout(interval))
		typ, data, err := c.ws.Read(rctx)
		timedOut := errors.Is(rctx.Err(), context.DeadlineExceeded)
		cancel()
		if err != nil {
			if timedOut {
				h.log.Info("device missed heartbeats", "device_id", c.deviceID, "interval", interval)
				c.close(CloseHeartbeatTimeout, "heartbeat timeout")
			} else {
				c.close(websocket.StatusNormalClosure, "")
			}
			return
		}
		if typ != websocket.MessageText {
			c.close(CloseProtocolError, "text frames only")
			return
		}
		var in Inbound
		if err := json.Unmarshal(data, &in); err != nil {
			c.close(CloseProtocolError, "invalid JSON")
			return
		}
		switch in.Type {
		case TypeHeartbeat:
			interval = ClampHeartbeat(in.NextIn)
			if time.Since(lastWrite) >= minStatusWriteGap {
				lastWrite = time.Now()
				if err := h.recordHeartbeat(ctx, c.deviceID, interval, in.Status); err != nil {
					h.log.Error("could not record heartbeat", "device_id", c.deviceID, "error", err)
				}
			}
			c.enqueue(Outbound{Type: TypeHeartbeatAck, Seq: in.Seq})
		case TypeSMSAccepted, TypeSMSSent, TypeSMSFailed, TypeSMSDelivery:
			if h.handler == nil || in.MessageID == "" {
				continue
			}
			if err := h.handler.DeviceReport(ctx, c.deviceID, in); err != nil {
				h.log.Error("could not store device report", "device_id", c.deviceID, "message_id", in.MessageID, "type", in.Type, "error", err)
				continue // not acknowledged: the device resends it
			}
			c.enqueue(Outbound{Type: TypeReportAck, MessageID: in.MessageID, Report: in.Type})
		case TypeSMSReceived:
			if h.handler == nil || in.InboundID == "" {
				continue
			}
			if err := h.handler.DeviceInbound(ctx, c.deviceID, in); err != nil {
				h.log.Error("could not store incoming SMS", "device_id", c.deviceID, "inbound_id", in.InboundID, "error", err)
				continue
			}
			c.enqueue(Outbound{Type: TypeReportAck, MessageID: in.InboundID, Report: in.Type})
		default:
			// Unknown frame types are ignored so newer apps work with older servers.
			h.log.Debug("ignoring unknown device frame", "device_id", c.deviceID, "type", in.Type)
		}
	}
}

func (h *Hub) recordHeartbeat(ctx context.Context, deviceID string, interval time.Duration, s *Status) error {
	return RecordStatus(ctx, h.q, deviceID, interval, s)
}

// RecordStatus stores a heartbeat, shared by the WebSocket and HTTP paths.
func RecordStatus(ctx context.Context, q *dbq.Queries, deviceID string, interval time.Duration, s *Status) error {
	if s == nil {
		s = &Status{}
	}
	if s.BatteryLevel != nil && (*s.BatteryLevel < 0 || *s.BatteryLevel > 100) {
		s.BatteryLevel = nil
	}
	var sims []byte
	if s.Sims != nil {
		if len(s.Sims) > 4 {
			s.Sims = s.Sims[:4]
		}
		for i := range s.Sims {
			s.Sims[i].Carrier = clip(s.Sims[i].Carrier, 64)
			s.Sims[i].DisplayName = clip(s.Sims[i].DisplayName, 64)
		}
		sims, _ = json.Marshal(s.Sims)
	}
	return q.RecordHeartbeat(ctx, dbq.RecordHeartbeatParams{
		Sims:              sims,
		ID:                deviceID,
		HeartbeatInterval: int32(interval / time.Second),
		BatteryLevel:      s.BatteryLevel,
		IsCharging:        s.IsCharging,
		NetworkType:       truncate(s.NetworkType, 32),
		CarrierName:       truncate(s.CarrierName, 64),
		SimCount:          s.SimCount,
		DeviceModel:       truncate(s.DeviceModel, 80),
		AndroidVersion:    truncate(s.AndroidVersion, 32),
		AppVersion:        truncate(s.AppVersion, 32),
	})
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func truncate(s *string, n int) *string {
	if s == nil || len(*s) <= n {
		return s
	}
	t := (*s)[:n]
	return &t
}

func (h *Hub) register(c *client) (old *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	old = h.conns[c.deviceID]
	h.conns[c.deviceID] = c
	return old
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	if h.conns[c.deviceID] == c {
		delete(h.conns, c.deviceID)
	}
	h.mu.Unlock()
	c.close(websocket.StatusNormalClosure, "")
}

// deliverLocal sends a frame to a device connected to this instance.
// send_sms frames travel as message IDs and are filled in here.
func (h *Hub) deliverLocal(ctx context.Context, deviceID string, m Outbound) bool {
	h.mu.Lock()
	c := h.conns[deviceID]
	h.mu.Unlock()
	if c == nil {
		return false
	}
	if m.Type == TypeSendSMS && m.Body == "" {
		if h.handler == nil {
			return true
		}
		full, err := h.handler.HydrateSend(ctx, deviceID, m.MessageID)
		if err != nil {
			h.log.Error("could not load message for device", "device_id", deviceID, "message_id", m.MessageID, "error", err)
			return true
		}
		if full == nil {
			return true // no longer pending for this device
		}
		m = *full
	}
	c.enqueue(m)
	return true
}

type notification struct {
	DeviceID string   `json:"device_id"`
	Frame    Outbound `json:"frame"`
}

// Send delivers a frame to a device wherever it is connected. It returns
// once the frame is queued locally or published to other instances; it does
// not confirm the device received it.
func (h *Hub) Send(ctx context.Context, deviceID string, m Outbound) error {
	if h.deliverLocal(ctx, deviceID, m) {
		return nil
	}
	return Publish(ctx, h.pool, deviceID, m)
}

// Publish hands a frame to whichever API instance holds the device's
// connection. Processes without a hub (the worker) use it directly. Message
// bodies never travel through NOTIFY: send_sms frames carry only the message ID.
func Publish(ctx context.Context, pool *pgxpool.Pool, deviceID string, m Outbound) error {
	if m.Type == TypeSendSMS {
		m = Outbound{Type: TypeSendSMS, MessageID: m.MessageID}
	}
	payload, err := json.Marshal(notification{DeviceID: deviceID, Frame: m})
	if err != nil {
		return err
	}
	if len(payload) > 7900 {
		return errors.New("frame too large for NOTIFY")
	}
	_, err = pool.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, string(payload))
	return err
}

// Run listens for frames published by other instances until ctx ends, and
// closes all local connections on shutdown.
func (h *Hub) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listen(ctx)
		if ctx.Err() != nil {
			break
		}
		h.log.Warn("device notification listener stopped; retrying", "error", err, "retry_in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	h.mu.Lock()
	for _, c := range h.conns {
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
	h.mu.Unlock()
	return nil
}

func (h *Hub) listen(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var msg notification
		if err := json.Unmarshal([]byte(n.Payload), &msg); err != nil {
			h.log.Warn("ignoring malformed device notification", "error", err)
			continue
		}
		h.deliverLocal(ctx, msg.DeviceID, msg.Frame)
	}
}

// ConnectedLocally reports whether the device holds a connection on this instance.
func (h *Hub) ConnectedLocally(deviceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[deviceID] != nil
}
