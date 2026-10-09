// Package billing runs hosted Bridge's plans: what each organization may use,
// how much it has used this month, and the Dodo Payments subscriptions that
// move it between plans. It only applies with BRIDGE_CLOUD=true; self-hosted
// installs are never limited.
package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"bridge/internal/config"
	"bridge/internal/db/dbq"
)

// FreePlan is the plan of every organization without an active subscription.
const FreePlan = "free"

// Resource is something a plan limits.
type Resource string

const (
	Phones       Resource = "phones"
	LiveMessages Resource = "live_messages"
	Projects     Resource = "projects"
	Members      Resource = "members"
)

// Limits are a plan's allowances. Nil means unlimited.
type Limits struct {
	Phones       *int32 `json:"phones"`
	LiveMessages *int32 `json:"live_messages"`
	Projects     *int32 `json:"projects"`
	Members      *int32 `json:"members"`
}

func (l Limits) of(r Resource) *int32 {
	switch r {
	case Phones:
		return l.Phones
	case LiveMessages:
		return l.LiveMessages
	case Projects:
		return l.Projects
	case Members:
		return l.Members
	}
	return nil
}

// Plan is a row of the plans table.
type Plan struct {
	ID         string
	Name       string
	PriceCents int32
	Currency   string
	Limits     Limits
}

func planFrom(p dbq.Plan) Plan {
	return Plan{
		ID: p.ID, Name: p.Name, PriceCents: p.PriceCents, Currency: p.Currency,
		Limits: Limits{Phones: p.MaxPhones, LiveMessages: p.MaxLiveMessages, Projects: p.MaxProjects, Members: p.MaxMembers},
	}
}

// LimitError means an organization reached its plan's limit for a resource.
type LimitError struct {
	Resource Resource
	Limit    int32
	Plan     string // plan name, e.g. "Free"
}

func (e *LimitError) Error() string {
	if e.Resource == LiveMessages {
		return fmt.Sprintf("This workspace sent the %d live SMS a month included in the %s plan. "+
			"Live sending resumes on the 1st (UTC), or upgrade for more.", e.Limit, e.Plan)
	}
	var what string
	switch e.Resource {
	case Phones:
		what = plural(e.Limit, "phone", "phones")
	case Projects:
		what = plural(e.Limit, "project", "projects")
	case Members:
		what = plural(e.Limit, "member", "members")
	}
	return fmt.Sprintf("The %s plan includes %s. Upgrade the workspace to add more.", e.Plan, what)
}

func plural(n int32, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(int(n)) + " " + many
}

// Service applies plans. A Service with Cloud off allows everything.
type Service struct {
	pool *pgxpool.Pool
	q    *dbq.Queries
	cfg  *config.Config
	log  *slog.Logger
	dodo *Dodo
	now  func() time.Time
}

func New(pool *pgxpool.Pool, cfg *config.Config, log *slog.Logger) *Service {
	s := &Service{pool: pool, q: dbq.New(pool), cfg: cfg, log: log, now: time.Now}
	if cfg.Cloud && cfg.Dodo != nil {
		s.dodo = NewDodo(cfg.Dodo)
	}
	return s
}

// Enabled reports whether plans are enforced (hosted Bridge).
func (s *Service) Enabled() bool { return s != nil && s.cfg.Cloud }

// Payments reports whether organizations can pay to change plans.
func (s *Service) Payments() bool { return s.Enabled() && s.dodo != nil }

// Purchasable reports whether a plan can be bought through Dodo.
func (s *Service) Purchasable(planID string) bool {
	return s.Payments() && s.cfg.Dodo.Products[planID] != ""
}

// Plans lists every plan, cheapest first.
func (s *Service) Plans(ctx context.Context) ([]Plan, error) {
	rows, err := s.q.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Plan, 0, len(rows))
	for _, r := range rows {
		out = append(out, planFrom(r))
	}
	return out, nil
}

// grants reports whether a subscription status keeps its plan. past_due keeps
// access while Dodo retries the payment.
func grants(status string) bool { return status == StatusActive || status == StatusPastDue }

// Current returns the organization's plan and its subscription, if it has one.
func (s *Service) Current(ctx context.Context, q *dbq.Queries, orgID string) (Plan, *dbq.Subscription, error) {
	planID := FreePlan
	var current *dbq.Subscription
	sub, err := q.GetSubscription(ctx, orgID)
	switch {
	case err == nil:
		current = &sub
		if grants(sub.Status) {
			planID = sub.PlanID
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return Plan{}, nil, err
	}
	p, err := q.GetPlan(ctx, planID)
	if err != nil {
		return Plan{}, nil, fmt.Errorf("plan %q: %w", planID, err)
	}
	return planFrom(p), current, nil
}

// CheckAdd returns a *LimitError when adding one more phone, project or member
// would exceed the organization's plan. Pass the transaction's queries so the
// count and the insert see the same data.
func (s *Service) CheckAdd(ctx context.Context, q *dbq.Queries, orgID string, r Resource) error {
	if !s.Enabled() {
		return nil
	}
	plan, _, err := s.Current(ctx, q, orgID)
	if err != nil {
		return err
	}
	limit := plan.Limits.of(r)
	if limit == nil {
		return nil
	}
	n, err := s.count(ctx, q, orgID, r)
	if err != nil {
		return err
	}
	if n >= int64(*limit) {
		return &LimitError{Resource: r, Limit: *limit, Plan: plan.Name}
	}
	return nil
}

func (s *Service) count(ctx context.Context, q *dbq.Queries, orgID string, r Resource) (int64, error) {
	switch r {
	case Phones:
		return q.CountOrganizationPhones(ctx, orgID)
	case Projects:
		return q.CountOrganizationProjects(ctx, orgID)
	case Members:
		return q.CountOrganizationSeats(ctx, orgID)
	case LiveMessages:
		n, err := q.GetLiveUsage(ctx, dbq.GetLiveUsageParams{OrganizationID: orgID, Period: s.period()})
		return int64(n), err
	}
	return 0, fmt.Errorf("unknown resource %q", r)
}

// period is the first day of the current month (UTC), the usage counter's key.
func (s *Service) period() pgtype.Date {
	t := s.now().UTC()
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// PeriodBounds returns the current usage period: [start, end).
func (s *Service) PeriodBounds() (time.Time, time.Time) {
	start := s.period().Time
	return start, start.AddDate(0, 1, 0)
}

// ReserveLive counts one live message for the project's organization, or
// returns a *LimitError when the month's allowance is used up. Call it in the
// transaction that creates the message so a rollback returns the allowance.
func (s *Service) ReserveLive(ctx context.Context, q *dbq.Queries, projectID string) error {
	if !s.Enabled() {
		return nil
	}
	project, err := q.GetProjectByID(ctx, projectID)
	if err != nil {
		return err
	}
	plan, _, err := s.Current(ctx, q, project.OrganizationID)
	if err != nil {
		return err
	}
	limit := int32(math.MaxInt32)
	if l := plan.Limits.LiveMessages; l != nil {
		limit = *l
	}
	_, err = q.IncrementLiveUsage(ctx, dbq.IncrementLiveUsageParams{
		OrganizationID: project.OrganizationID, Period: s.period(), MaxMessages: limit,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return &LimitError{Resource: LiveMessages, Limit: limit, Plan: plan.Name}
	}
	return err
}

// Usage is what an organization has used against its plan.
type Usage struct {
	Phones       int64
	LiveMessages int64
	Projects     int64
	Members      int64
}

// Usage counts the organization's current usage.
func (s *Service) Usage(ctx context.Context, orgID string) (Usage, error) {
	var u Usage
	var err error
	for r, dst := range map[Resource]*int64{Phones: &u.Phones, LiveMessages: &u.LiveMessages, Projects: &u.Projects} {
		if *dst, err = s.count(ctx, s.q, orgID, r); err != nil {
			return u, err
		}
	}
	u.Members, err = s.q.CountOrganizationMembers(ctx, orgID)
	return u, err
}
