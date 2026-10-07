package broadcast

import (
	"context"
	"time"

	"bridge/internal/db/dbq"
)

// BroadcastCounts is a broadcast's progress.
type BroadcastCounts struct {
	Recipients int `json:"recipients" doc:"Unique numbers the broadcast sends to (duplicates and opted-out numbers removed)."`
	Queued     int `json:"queued" doc:"Not sent yet: waiting for their turn, for a phone, or being sent."`
	Sent       int `json:"sent" doc:"Sent, no delivery report yet."`
	Delivered  int `json:"delivered"`
	Failed     int `json:"failed"`
	Canceled   int `json:"canceled" doc:"Not sent because the broadcast was canceled."`
	Skipped    int `json:"skipped" doc:"Opted-out numbers left out, at creation or when their turn came, and rows the pipeline refused."`
	Duplicates int `json:"duplicates" doc:"Repeated numbers removed at creation."`
}

// Broadcast is one message template sent to many recipients.
type Broadcast struct {
	ID            string          `json:"id" example:"brd_01ja8z3k5wq2v7c9e4r2n0w6yb"`
	Name          string          `json:"name"`
	Environment   string          `json:"environment" enum:"live,test"`
	Status        string          `json:"status" enum:"scheduled,sending,completed,canceled" doc:"completed: every message was sent or failed (delivery reports may still arrive)."`
	Template      string          `json:"template" example:"Hi {name}, your order {order} has shipped."`
	DeviceID      *string         `json:"device_id" nullable:"true" doc:"The phone every message goes through; null lets Bridge pick per message."`
	ScheduledAt   *time.Time      `json:"scheduled_at" nullable:"true"`
	Counts        BroadcastCounts `json:"counts"`
	TotalSegments int             `json:"total_segments" doc:"SMS segments for every recipient together."`
	CreatedAt     time.Time       `json:"created_at"`
	StartedAt     *time.Time      `json:"started_at" nullable:"true"`
	CompletedAt   *time.Time      `json:"completed_at" nullable:"true"`
	CanceledAt    *time.Time      `json:"canceled_at" nullable:"true"`
}

// Views renders broadcasts with their current counts.
func (s *Service) Views(ctx context.Context, rows []dbq.Broadcast) ([]Broadcast, error) {
	ids := make([]string, 0, len(rows))
	for _, b := range rows {
		ids = append(ids, b.ID)
	}
	counts := map[string]dbq.BroadcastCountsRow{}
	if len(ids) > 0 {
		cs, err := s.q.BroadcastCounts(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			counts[c.BroadcastID] = c
		}
	}
	out := make([]Broadcast, 0, len(rows))
	for _, b := range rows {
		c := counts[b.ID]
		out = append(out, Broadcast{
			ID: b.ID, Name: b.Name, Environment: string(b.Environment), Status: b.Status, Template: b.Template,
			DeviceID: b.DeviceID, ScheduledAt: b.ScheduledAt, TotalSegments: int(b.TotalSegments),
			CreatedAt: b.CreatedAt, StartedAt: b.StartedAt, CompletedAt: b.CompletedAt, CanceledAt: b.CanceledAt,
			Counts: BroadcastCounts{
				Recipients: int(b.TotalRecipients), Queued: int(c.Waiting + c.InFlight), Sent: int(c.Sent),
				Delivered: int(c.Delivered), Failed: int(c.Failed), Canceled: int(c.Canceled),
				Skipped: int(b.SkippedOptedOut + c.Skipped), Duplicates: int(b.Duplicates),
			},
		})
	}
	return out, nil
}

// View renders one broadcast.
func (s *Service) View(ctx context.Context, b dbq.Broadcast) (Broadcast, error) {
	v, err := s.Views(ctx, []dbq.Broadcast{b})
	if err != nil {
		return Broadcast{}, err
	}
	return v[0], nil
}
