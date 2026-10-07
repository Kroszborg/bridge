// Package tools wires the messaging tools (broadcasts, schedules, and the
// automation of opt-outs, auto-replies and forwarding) for the API and the
// worker, which both need them.
package tools

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"bridge/internal/automation"
	"bridge/internal/broadcast"
	"bridge/internal/config"
	"bridge/internal/messaging"
	"bridge/internal/schedule"
	"bridge/internal/secretbox"
)

type Services struct {
	Broadcasts *broadcast.Service
	Schedules  *schedule.Service
	Automation *automation.Service
}

type Options struct {
	Config    *config.Config
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Messaging *messaging.Service
	// BroadcastPoll is how often a sending broadcast re-checks its progress;
	// zero takes the default. Tests shorten it.
	BroadcastPoll time.Duration
}

func New(o Options) (*Services, error) {
	box, err := secretbox.New(o.Config.SecretKey)
	if err != nil {
		return nil, err
	}
	return &Services{
		Broadcasts: broadcast.New(broadcast.Options{Pool: o.Pool, Logger: o.Logger, Messaging: o.Messaging, PollInterval: o.BroadcastPoll}),
		Schedules:  schedule.New(schedule.Options{Pool: o.Pool, Logger: o.Logger, Messaging: o.Messaging}),
		Automation: automation.New(automation.Options{
			Pool: o.Pool, Logger: o.Logger, Messaging: o.Messaging, Box: box, AllowPrivate: o.Config.WebhookAllowPrivate,
			SMTP: o.Config.SMTP, TelegramURL: o.Config.TelegramAPIURL,
		}),
	}, nil
}

// SetJobInserter wires a River client into every service.
func (s *Services) SetJobInserter(c *river.Client[pgx.Tx]) {
	s.Broadcasts.SetJobInserter(c)
	s.Schedules.SetJobInserter(c)
	s.Automation.SetJobInserter(c)
}

// Register adds every tool's workers.
func (s *Services) Register(workers *river.Workers) {
	broadcast.Register(workers, s.Broadcasts)
	schedule.Register(workers, s.Schedules)
	automation.Register(workers, s.Automation)
}
