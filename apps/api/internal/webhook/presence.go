package webhook

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"bridge/internal/db/dbq"
)

// OfflineGrace is how long a device must stay offline before device.offline
// is sent, so a phone switching networks does not produce offline/online pairs.
const OfflineGrace = 2 * time.Minute

// PresenceInterval is how often presence changes are turned into events.
const PresenceInterval = 15 * time.Second

// Device is the data of device.online and device.offline events.
type Device struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	BatteryLevel *int16     `json:"battery_level"`
	IsCharging   *bool      `json:"is_charging"`
	NetworkType  *string    `json:"network_type"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
}

// PresenceArgs is a periodic job that emits device presence events.
type PresenceArgs struct{}

func (PresenceArgs) Kind() string { return "webhook.presence" }

func (PresenceArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: QueueWebhooks} }

type PresenceWorker struct {
	river.WorkerDefaults[PresenceArgs]
	svc *Service
}

func (w *PresenceWorker) Work(ctx context.Context, _ *river.Job[PresenceArgs]) error {
	return w.svc.EmitPresence(ctx)
}

// EmitPresence sends device.online for devices that came online and
// device.offline for devices offline longer than OfflineGrace, once per change.
func (s *Service) EmitPresence(ctx context.Context) error {
	before := s.now().Add(-OfflineGrace)
	devices, err := s.q.PresenceChanges(ctx, &before)
	if err != nil {
		return err
	}
	for _, d := range devices {
		presence, event := "offline", EventDeviceOffline
		if d.Status == dbq.DeviceStatusOnline {
			presence, event = "online", EventDeviceOnline
		}
		n, err := s.q.SetNotifiedPresence(ctx, dbq.SetNotifiedPresenceParams{ID: d.ID, Presence: &presence})
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if err := s.Emit(ctx, d.ProjectID, event, Device{
			ID: d.ID, Name: d.Name, Status: string(d.Status), BatteryLevel: d.BatteryLevel,
			IsCharging: d.IsCharging, NetworkType: d.NetworkType, LastSeenAt: d.LastSeenAt,
		}); err != nil {
			s.log.Error("could not emit presence event", "device_id", d.ID, "event", event, "error", err)
		}
	}
	return nil
}

// DeleteOld removes events (and their delivery logs) older than before.
func (s *Service) DeleteOld(ctx context.Context, before time.Time) (int64, error) {
	return s.q.DeleteOldWebhookEvents(ctx, before)
}
