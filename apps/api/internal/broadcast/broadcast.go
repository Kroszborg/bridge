// Package broadcast sends one templated message to many recipients.
//
// Creating a broadcast validates and renders every recipient up front, stores
// the recipients, and queues a job. The job creates the messages through the
// normal pipeline (messaging.Send), so routing, phones' send limits and
// providers apply as for any message. It paces itself: it keeps only as many
// of the broadcast's messages waiting as the project's phones can send in one
// send-limit window, so a long list never sits in the queue long enough to
// time out, and a 10,000-recipient broadcast does not flood the job queue.
//
// Broadcast messages skip the per-request project and destination hourly
// limits (they would otherwise silently drop most of a large list); the
// broadcast as a whole is admitted instead, with at most MaxRecipients
// recipients and HourlyLimit broadcasts per project per hour.
package broadcast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/message"
	"bridge/internal/messaging"
	"bridge/internal/ratelimit"
)

const (
	// MaxRecipients is the most recipients one broadcast may have.
	MaxRecipients = 10000
	// HourlyLimit is how many broadcasts a project may create per hour.
	HourlyLimit = 20
	// maxVars and maxVarLength bound each recipient's template variables.
	maxVars      = 20
	maxVarLength = 500
	// sampleCount is how many rendered messages a preview shows.
	sampleCount = 5
	// maxWindow caps the messages a broadcast keeps waiting at once.
	maxWindow = 500
	minWindow = 10
	// chunk is how many messages are created per round.
	chunk = 100
	// runBudget is how long one job run creates messages before yielding.
	runBudget = 20 * time.Second
)

// Statuses.
const (
	StatusScheduled = "scheduled"
	StatusSending   = "sending"
	StatusCompleted = "completed"
	StatusCanceled  = "canceled"
)

var (
	ErrNotFound      = errors.New("broadcast not found")
	ErrNotCancelable = errors.New("broadcast already finished")
)

// JobInserter is satisfied by *river.Client.
type JobInserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

type Service struct {
	pool    *pgxpool.Pool
	q       *dbq.Queries
	msgs    *messaging.Service
	log     *slog.Logger
	jobs    JobInserter
	limiter ratelimit.Limiter
	poll    time.Duration
	now     func() time.Time
}

type Options struct {
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Messaging *messaging.Service
	Limiter   ratelimit.Limiter // defaults to the Postgres limiter
	// PollInterval is how often a sending broadcast checks for room in its
	// window and for completion. Default 10 seconds.
	PollInterval time.Duration
}

func New(o Options) *Service {
	q := dbq.New(o.Pool)
	if o.Limiter == nil {
		o.Limiter = ratelimit.NewPostgres(q)
	}
	if o.PollInterval == 0 {
		o.PollInterval = 10 * time.Second
	}
	return &Service{pool: o.Pool, q: q, msgs: o.Messaging, log: o.Logger, limiter: o.Limiter, poll: o.PollInterval, now: time.Now}
}

// SetJobInserter wires the River client, which is created after the service.
func (s *Service) SetJobInserter(j JobInserter) { s.jobs = j }

// Recipient is one row of a broadcast request.
type Recipient struct {
	To   string
	Vars map[string]string
}

// CreateRequest describes a broadcast.
type CreateRequest struct {
	ProjectID   string
	Environment dbq.APIEnvironment
	APIKeyID    *string
	UserID      *string
	Name        string
	Template    string
	Recipients  []Recipient
	DeviceID    *string
	ScheduledAt *time.Time
	DryRun      bool
}

// BroadcastSample is one rendered message of a preview.
type BroadcastSample struct {
	To       string `json:"to" example:"+919876543210"`
	Text     string `json:"text"`
	Segments int    `json:"segments"`
	Encoding string `json:"encoding" enum:"gsm7,ucs2"`
}

// BroadcastPreview summarises what a broadcast would send.
type BroadcastPreview struct {
	DryRun          bool              `json:"dry_run" doc:"Always true: nothing was created."`
	Recipients      int               `json:"recipients" doc:"Unique numbers that would receive the message."`
	SkippedOptedOut int               `json:"skipped_opted_out" doc:"Numbers left out because they opted out."`
	Duplicates      int               `json:"duplicates" doc:"Repeated numbers removed (the first row of each number is kept)."`
	TotalSegments   int               `json:"total_segments" doc:"SMS segments for every recipient together; carriers bill per segment."`
	Samples         []BroadcastSample `json:"samples" doc:"The first rendered messages."`
}

type plannedRecipient struct {
	to   string
	vars map[string]string
}

type plan struct {
	preview    BroadcastPreview
	recipients []plannedRecipient
}

func fieldErr(field, format string, args ...any) error {
	return &messaging.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// prepare validates a request and renders every recipient.
func (s *Service) prepare(ctx context.Context, r CreateRequest) (plan, error) {
	var p plan
	text := r.Template
	if strings.TrimSpace(text) == "" {
		return p, fieldErr("template", "The template cannot be empty.")
	}
	if n := len([]rune(text)); n > messaging.MaxBodyLength {
		return p, fieldErr("template", "The template is %d characters; the limit is %d.", n, messaging.MaxBodyLength)
	}
	if len(r.Recipients) == 0 {
		return p, fieldErr("recipients", "Add at least one recipient.")
	}
	if len(r.Recipients) > MaxRecipients {
		return p, fieldErr("recipients", "A broadcast can have at most %d recipients; split the list.", MaxRecipients)
	}
	tmpl := ParseTemplate(text)
	seen := make(map[string]bool, len(r.Recipients))
	type rendered struct {
		to, text string
		segments int
		encoding string
		vars     map[string]string
	}
	rows := make([]rendered, 0, len(r.Recipients))
	for i, rc := range r.Recipients {
		to, err := messaging.NormalizeE164(rc.To)
		if err != nil {
			var ve *messaging.ValidationError
			if errors.As(err, &ve) {
				return p, fieldErr(fmt.Sprintf("recipients[%d].to", i), "Row %d (%q): %s", i+1, rc.To, ve.Message)
			}
			return p, err
		}
		if len(rc.Vars) > maxVars {
			return p, fieldErr(fmt.Sprintf("recipients[%d].vars", i), "Row %d (%s): use at most %d variables.", i+1, to, maxVars)
		}
		for k, v := range rc.Vars {
			if len([]rune(v)) > maxVarLength {
				return p, fieldErr(fmt.Sprintf("recipients[%d].vars", i), "Row %d (%s): the value of %s is longer than %d characters.", i+1, to, k, maxVarLength)
			}
		}
		body, missing := tmpl.Render(rc.Vars)
		if missing != "" {
			return p, fieldErr(fmt.Sprintf("recipients[%d].vars", i), "Row %d (%s): the template uses {%s} but this recipient has no value for it.", i+1, to, missing)
		}
		if strings.TrimSpace(body) == "" {
			return p, fieldErr(fmt.Sprintf("recipients[%d].vars", i), "Row %d (%s): the message is empty.", i+1, to)
		}
		if n := len([]rune(body)); n > messaging.MaxBodyLength {
			return p, fieldErr(fmt.Sprintf("recipients[%d].vars", i), "Row %d (%s): the message is %d characters; the limit is %d.", i+1, to, n, messaging.MaxBodyLength)
		}
		if seen[to] {
			p.preview.Duplicates++
			continue
		}
		seen[to] = true
		enc, seg := message.Segments(body)
		rows = append(rows, rendered{to: to, text: body, segments: seg, encoding: enc, vars: rc.Vars})
	}

	numbers := make([]string, 0, len(rows))
	for _, row := range rows {
		numbers = append(numbers, row.to)
	}
	optedOut, err := s.q.OptedOutAmong(ctx, dbq.OptedOutAmongParams{ProjectID: r.ProjectID, Numbers: numbers})
	if err != nil {
		return p, err
	}
	out := make(map[string]bool, len(optedOut))
	for _, n := range optedOut {
		out[n] = true
	}
	p.preview.DryRun = true
	p.preview.Samples = []BroadcastSample{}
	for _, row := range rows {
		if out[row.to] {
			p.preview.SkippedOptedOut++
			continue
		}
		p.preview.Recipients++
		p.preview.TotalSegments += row.segments
		if len(p.preview.Samples) < sampleCount {
			p.preview.Samples = append(p.preview.Samples, BroadcastSample{To: row.to, Text: row.text, Segments: row.segments, Encoding: row.encoding})
		}
		p.recipients = append(p.recipients, plannedRecipient{to: row.to, vars: row.vars})
	}
	return p, nil
}

// Create validates a broadcast and, unless it is a dry run, stores it and
// queues its job. The preview is returned either way.
func (s *Service) Create(ctx context.Context, r CreateRequest) (dbq.Broadcast, BroadcastPreview, error) {
	var b dbq.Broadcast
	r.Name = strings.TrimSpace(r.Name)
	if len([]rune(r.Name)) > 100 {
		return b, BroadcastPreview{}, fieldErr("name", "Use at most 100 characters.")
	}
	now := s.now()
	if r.ScheduledAt != nil {
		if r.ScheduledAt.Before(now.Add(-time.Minute)) {
			return b, BroadcastPreview{}, fieldErr("scheduled_at", "This time has already passed. Leave scheduled_at out to send now.")
		}
		if r.ScheduledAt.After(now.AddDate(1, 0, 0)) {
			return b, BroadcastPreview{}, fieldErr("scheduled_at", "Schedule at most one year ahead.")
		}
	}
	if r.DeviceID != nil {
		d, err := s.q.GetDevice(ctx, dbq.GetDeviceParams{ID: *r.DeviceID, ProjectID: r.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && d.RevokedAt != nil) {
			return b, BroadcastPreview{}, fieldErr("device_id", "No active device with this ID in the project.")
		}
		if err != nil {
			return b, BroadcastPreview{}, err
		}
	}
	p, err := s.prepare(ctx, r)
	if err != nil || r.DryRun {
		return b, p.preview, err
	}
	if res, err := s.limiter.Hit(ctx, "broadcast:project:"+r.ProjectID, HourlyLimit, time.Hour); err == nil && !res.Allowed {
		return b, p.preview, &messaging.RateLimitError{Scope: "broadcast", RetryAfter: res.RetryAfter}
	}

	status := StatusSending
	var opts *river.InsertOpts
	if r.ScheduledAt != nil && r.ScheduledAt.After(now) {
		status = StatusScheduled
		opts = &river.InsertOpts{ScheduledAt: *r.ScheduledAt}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return b, p.preview, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	b, err = q.InsertBroadcast(ctx, dbq.InsertBroadcastParams{
		ID: id.New(id.Broadcast), ProjectID: r.ProjectID, Environment: r.Environment, Name: r.Name, Template: r.Template,
		DeviceID: r.DeviceID, Status: status, ScheduledAt: r.ScheduledAt, TotalRecipients: int32(p.preview.Recipients),
		SkippedOptedOut: int32(p.preview.SkippedOptedOut), Duplicates: int32(p.preview.Duplicates),
		TotalSegments: int32(p.preview.TotalSegments), APIKeyID: r.APIKeyID, CreatedBy: r.UserID,
	})
	if err != nil {
		return b, p.preview, err
	}
	rows := make([]dbq.InsertBroadcastRecipientsParams, 0, len(p.recipients))
	for i, rc := range p.recipients {
		row := dbq.InsertBroadcastRecipientsParams{BroadcastID: b.ID, Position: int32(i), Recipient: rc.to}
		if len(rc.vars) > 0 {
			row.Vars, _ = json.Marshal(rc.vars)
		}
		rows = append(rows, row)
	}
	if _, err := q.InsertBroadcastRecipients(ctx, rows); err != nil {
		return b, p.preview, fmt.Errorf("store broadcast recipients: %w", err)
	}
	if _, err := s.jobs.InsertTx(ctx, tx, RunArgs{BroadcastID: b.ID}, opts); err != nil {
		return b, p.preview, fmt.Errorf("queue broadcast: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return b, p.preview, err
	}
	s.log.Info("broadcast created", "broadcast_id", b.ID, "project_id", b.ProjectID, "environment", b.Environment,
		"recipients", b.TotalRecipients, "status", b.Status)
	return b, p.preview, nil
}

// Get returns a project's broadcast.
func (s *Service) Get(ctx context.Context, projectID, broadcastID string) (dbq.Broadcast, error) {
	b, err := s.q.GetBroadcast(ctx, dbq.GetBroadcastParams{ID: broadcastID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// Cancel stops a scheduled or sending broadcast: recipients without a message
// are not sent to, and messages still waiting for a phone are canceled.
// Messages a phone or provider already took are left to finish.
func (s *Service) Cancel(ctx context.Context, projectID, broadcastID string) (dbq.Broadcast, error) {
	b, err := s.q.CancelBroadcast(ctx, dbq.CancelBroadcastParams{ID: broadcastID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.Get(ctx, projectID, broadcastID); err != nil {
			return b, err
		}
		return b, ErrNotCancelable
	}
	if err != nil {
		return b, err
	}
	pending, err := s.q.CancelPendingRecipients(ctx, b.ID)
	if err != nil {
		return b, err
	}
	waiting, err := s.q.UndispatchedBroadcastMessages(ctx, b.ID)
	if err != nil {
		return b, err
	}
	canceled := 0
	for _, m := range waiting {
		ok, err := s.msgs.CancelQueued(ctx, m.ID, "broadcast_canceled")
		if err != nil {
			return b, err
		}
		if ok {
			canceled++
		}
	}
	s.log.Info("broadcast canceled", "broadcast_id", b.ID, "recipients_not_sent", pending, "messages_canceled", canceled)
	return b, nil
}

// ListRequest pages through a project's broadcasts, newest first.
type ListRequest struct {
	ProjectID     string
	Environment   dbq.APIEnvironment
	Status        string
	StartingAfter string
	Limit         int
}

func (s *Service) List(ctx context.Context, r ListRequest) ([]dbq.Broadcast, bool, error) {
	params := dbq.ListBroadcastsParams{ProjectID: r.ProjectID, Environment: r.Environment, RowLimit: int32(r.Limit + 1)}
	if r.Status != "" {
		params.Status = &r.Status
	}
	if r.StartingAfter != "" {
		cursor, err := s.Get(ctx, r.ProjectID, r.StartingAfter)
		if errors.Is(err, ErrNotFound) {
			return nil, false, fieldErr("starting_after", "No broadcast with this ID in the project.")
		}
		if err != nil {
			return nil, false, err
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListBroadcasts(ctx, params)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > r.Limit
	if more {
		rows = rows[:r.Limit]
	}
	return rows, more, nil
}
