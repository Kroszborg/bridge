package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"bridge/internal/db/dbq"
	"bridge/internal/id"
	"bridge/internal/messaging"
)

// TickInterval is how often due schedules are claimed by default.
const TickInterval = time.Minute

// claimBatch is how many due schedules one tick claims.
const claimBatch = 200

var ErrNotFound = errors.New("schedule not found")

// JobInserter is satisfied by *river.Client.
type JobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

type Service struct {
	pool *pgxpool.Pool
	q    *dbq.Queries
	msgs *messaging.Service
	log  *slog.Logger
	jobs JobInserter
	now  func() time.Time
}

type Options struct {
	Pool      *pgxpool.Pool
	Logger    *slog.Logger
	Messaging *messaging.Service
}

func New(o Options) *Service {
	return &Service{pool: o.Pool, q: dbq.New(o.Pool), msgs: o.Messaging, log: o.Logger, now: time.Now}
}

// SetJobInserter wires the River client, which is created after the service.
func (s *Service) SetJobInserter(j JobInserter) { s.jobs = j }

// Fields are a schedule's editable settings.
type Fields struct {
	Name     string
	To       string
	Body     string
	DeviceID *string
	Spec     Spec
	EndsAt   *time.Time
	Paused   bool
}

// CreateRequest is a new schedule.
type CreateRequest struct {
	ProjectID   string
	Environment dbq.APIEnvironment
	APIKeyID    *string
	UserID      *string
	Fields
}

func fieldErr(field, msg string) error { return &messaging.ValidationError{Field: field, Message: msg} }

// check validates and normalizes fields and returns the schedule's location.
func (s *Service) check(ctx context.Context, projectID string, f *Fields) (*time.Location, error) {
	to, err := messaging.NormalizeE164(f.To)
	if err != nil {
		return nil, err
	}
	f.To = to
	f.Name = strings.TrimSpace(f.Name)
	if len([]rune(f.Name)) > 100 {
		return nil, fieldErr("name", "Use at most 100 characters.")
	}
	if strings.TrimSpace(f.Body) == "" {
		return nil, fieldErr("message", "The message cannot be empty.")
	}
	if n := len([]rune(f.Body)); n > messaging.MaxBodyLength {
		return nil, fieldErr("message", fmt.Sprintf("The message is %d characters; the limit is %d.", n, messaging.MaxBodyLength))
	}
	if f.DeviceID != nil {
		d, err := s.q.GetDevice(ctx, dbq.GetDeviceParams{ID: *f.DeviceID, ProjectID: projectID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && d.RevokedAt != nil) {
			return nil, fieldErr("device_id", "No active device with this ID in the project.")
		}
		if err != nil {
			return nil, err
		}
	}
	loc, err := f.Spec.Normalize()
	if err != nil {
		var se *SpecError
		if errors.As(err, &se) {
			return nil, fieldErr("schedule."+se.Field, se.Message)
		}
		return nil, err
	}
	return loc, nil
}

// nextRun is the first run after now, or nil when nothing is left to run.
func nextRun(f Fields, loc *time.Location, now time.Time) *time.Time {
	next, ok := f.Spec.Next(now, loc)
	if !ok || (f.EndsAt != nil && next.After(*f.EndsAt)) {
		return nil
	}
	return &next
}

// mustRun explains why a schedule that is being set up would never run.
func (s *Service) mustRun(f Fields, next *time.Time) error {
	switch {
	case next != nil:
		return nil
	case f.Spec.Kind == KindOnce && (f.EndsAt == nil || !f.EndsAt.Before(s.now())):
		return fieldErr("schedule.date", "This date and time have already passed in "+f.Spec.TimeZone+".")
	case f.EndsAt != nil && f.EndsAt.Before(s.now()):
		return fieldErr("ends_at", "ends_at has already passed.")
	}
	return fieldErr("ends_at", "The schedule would never run before ends_at.")
}

// Create validates and stores a schedule.
func (s *Service) Create(ctx context.Context, r CreateRequest) (dbq.ScheduledMessage, error) {
	loc, err := s.check(ctx, r.ProjectID, &r.Fields)
	if err != nil {
		return dbq.ScheduledMessage{}, err
	}
	next := nextRun(r.Fields, loc, s.now())
	if err := s.mustRun(r.Fields, next); err != nil {
		return dbq.ScheduledMessage{}, err
	}
	return s.q.InsertSchedule(ctx, insertParams(r, next))
}

func insertParams(r CreateRequest, next *time.Time) dbq.InsertScheduleParams {
	p := dbq.InsertScheduleParams{
		ID: id.New(id.Schedule), ProjectID: r.ProjectID, Environment: r.Environment, Name: r.Name, Recipient: r.To, Body: r.Body,
		DeviceID: r.DeviceID, EndsAt: r.EndsAt, Paused: r.Paused, NextRunAt: next, APIKeyID: r.APIKeyID, CreatedBy: r.UserID,
	}
	setSpec(&p.Kind, &p.AtTime, &p.Days, &p.DayOfMonth, &p.RunDate, &p.TimeZone, r.Spec)
	return p
}

func setSpec(kind, at *string, days *[]string, dom **int16, date *pgtype.Date, tz *string, sp Spec) {
	*kind, *at, *tz = sp.Kind, sp.At, sp.TimeZone
	*days = sp.Days
	if *days == nil {
		*days = []string{}
	}
	*dom = nil
	if sp.DayOfMonth > 0 {
		v := int16(sp.DayOfMonth)
		*dom = &v
	}
	*date = pgtype.Date{}
	if d, err := time.Parse(time.DateOnly, sp.Date); err == nil && sp.Kind == KindOnce {
		*date = pgtype.Date{Time: d, Valid: true}
	}
}

// SpecOf reads a stored schedule's spec.
func SpecOf(m dbq.ScheduledMessage) Spec {
	sp := Spec{Kind: m.Kind, At: m.AtTime, Days: m.Days, TimeZone: m.TimeZone}
	if m.DayOfMonth != nil {
		sp.DayOfMonth = int(*m.DayOfMonth)
	}
	if m.RunDate.Valid {
		sp.Date = m.RunDate.Time.Format(time.DateOnly)
	}
	return sp
}

// FieldsOf reads a stored schedule's editable settings.
func FieldsOf(m dbq.ScheduledMessage) Fields {
	return Fields{Name: m.Name, To: m.Recipient, Body: m.Body, DeviceID: m.DeviceID, Spec: SpecOf(m), EndsAt: m.EndsAt, Paused: m.Paused}
}

// Get returns a project's schedule.
func (s *Service) Get(ctx context.Context, projectID, scheduleID string) (dbq.ScheduledMessage, error) {
	m, err := s.q.GetSchedule(ctx, dbq.GetScheduleParams{ID: scheduleID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Update stores changed settings. The next run is computed again from now
// when the timing or the end changes; a change to the message alone keeps it.
func (s *Service) Update(ctx context.Context, cur dbq.ScheduledMessage, f Fields) (dbq.ScheduledMessage, error) {
	before := FieldsOf(cur)
	loc, err := s.check(ctx, cur.ProjectID, &f)
	if err != nil {
		return cur, err
	}
	next := cur.NextRunAt
	timing := f.Spec.Kind != before.Spec.Kind || f.Spec.At != before.Spec.At || strings.Join(f.Spec.Days, ",") != strings.Join(before.Spec.Days, ",") ||
		f.Spec.DayOfMonth != before.Spec.DayOfMonth || f.Spec.Date != before.Spec.Date || f.Spec.TimeZone != before.Spec.TimeZone ||
		!timePtrEqual(f.EndsAt, before.EndsAt) || f.Paused != before.Paused
	if timing {
		next = nextRun(f, loc, s.now())
		if !f.Paused {
			if err := s.mustRun(f, next); err != nil {
				return cur, err
			}
		}
	}
	return s.q.UpdateSchedule(ctx, updateParams(cur, f, next))
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// Delete removes a schedule.
func (s *Service) Delete(ctx context.Context, projectID, scheduleID string) error {
	n, err := s.q.DeleteSchedule(ctx, dbq.DeleteScheduleParams{ID: scheduleID, ProjectID: projectID})
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// SetPaused pauses or resumes a schedule. Resuming skips the runs missed
// while paused; a schedule with nothing left to run resumes as finished.
func (s *Service) SetPaused(ctx context.Context, cur dbq.ScheduledMessage, paused bool) (dbq.ScheduledMessage, error) {
	if cur.Paused == paused {
		return cur, nil
	}
	f := FieldsOf(cur)
	f.Paused = paused
	next := cur.NextRunAt
	if !paused {
		loc, err := f.Spec.Normalize()
		if err != nil {
			return cur, err
		}
		next = nextRun(f, loc, s.now())
	}
	return s.q.UpdateSchedule(ctx, updateParams(cur, f, next))
}

func updateParams(cur dbq.ScheduledMessage, f Fields, next *time.Time) dbq.UpdateScheduleParams {
	p := dbq.UpdateScheduleParams{
		ID: cur.ID, ProjectID: cur.ProjectID, Name: f.Name, Recipient: f.To, Body: f.Body, DeviceID: f.DeviceID,
		EndsAt: f.EndsAt, Paused: f.Paused, NextRunAt: next,
	}
	setSpec(&p.Kind, &p.AtTime, &p.Days, &p.DayOfMonth, &p.RunDate, &p.TimeZone, f.Spec)
	return p
}

// RunNow sends a schedule's message at once, outside its timetable. The next
// regular run is unchanged.
func (s *Service) RunNow(ctx context.Context, cur dbq.ScheduledMessage) (dbq.ScheduledMessage, error) {
	now := s.now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return cur, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	if err := q.AdvanceSchedule(ctx, dbq.AdvanceScheduleParams{ID: cur.ID, NextRunAt: cur.NextRunAt, RanAt: now}); err != nil {
		return cur, err
	}
	if _, err := s.jobs.InsertTx(ctx, tx, SendArgs{ScheduleID: cur.ID, RunKey: "manual-" + strconv.FormatInt(now.UnixMilli(), 10)}, nil); err != nil {
		return cur, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cur, err
	}
	return s.Get(ctx, cur.ProjectID, cur.ID)
}

// ListRequest pages through a project's schedules, newest first.
type ListRequest struct {
	ProjectID     string
	Environment   dbq.APIEnvironment
	StartingAfter string
	Limit         int
}

func (s *Service) List(ctx context.Context, r ListRequest) ([]dbq.ScheduledMessage, bool, error) {
	params := dbq.ListSchedulesParams{ProjectID: r.ProjectID, Environment: r.Environment, RowLimit: int32(r.Limit + 1)}
	if r.StartingAfter != "" {
		cursor, err := s.Get(ctx, r.ProjectID, r.StartingAfter)
		if errors.Is(err, ErrNotFound) {
			return nil, false, fieldErr("starting_after", "No schedule with this ID in the project.")
		}
		if err != nil {
			return nil, false, err
		}
		params.BeforeCreated, params.BeforeID = &cursor.CreatedAt, &cursor.ID
	}
	rows, err := s.q.ListSchedules(ctx, params)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > r.Limit
	if more {
		rows = rows[:r.Limit]
	}
	return rows, more, nil
}
