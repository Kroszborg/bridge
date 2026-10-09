package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"bridge/internal/billing"
	"bridge/internal/db/dbq"
)

type InsightsRange struct {
	From time.Time `json:"from" doc:"Start of the first day, 00:00 UTC."`
	To   time.Time `json:"to" doc:"When the numbers were taken."`
	Days int       `json:"days" example:"30" doc:"UTC calendar days in the range, today included."`
}

type InsightsTotals struct {
	Users                int `json:"users" doc:"Every account on this instance."`
	UsersInRange         int `json:"users_in_range" doc:"Accounts created in the range."`
	UsersEmailVerified   int `json:"users_email_verified"`
	UsersWithPhone       int `json:"users_with_phone" doc:"Accounts with a verified mobile number."`
	Organizations        int `json:"organizations" doc:"Workspaces."`
	OrganizationsInRange int `json:"organizations_in_range"`
	Projects             int `json:"projects"`
	Phones               struct {
		Total  int `json:"total" doc:"Paired phones that are not revoked."`
		Online int `json:"online" doc:"Connected to Bridge right now."`
	} `json:"phones"`
	APIKeys             int `json:"api_keys" doc:"API keys that are neither revoked nor expired."`
	SubscriptionsActive int `json:"subscriptions_active" doc:"Paid subscriptions with status active."`
}

type InsightsSignupDay struct {
	Date          string `json:"date" example:"2026-10-05" doc:"UTC calendar day."`
	Users         int    `json:"users"`
	Organizations int    `json:"organizations"`
}

type InsightsActivity struct {
	ActiveOrganizations7d  int `json:"active_organizations_7d" doc:"Workspaces that sent at least one message (live or test) in the last 7 days."`
	ActiveOrganizations30d int `json:"active_organizations_30d" doc:"The same over the last 30 days."`
	ActiveUsers7d          int `json:"active_users_7d" doc:"Accounts whose dashboard session was used in the last 7 days."`
}

type InsightsMessageTotals struct {
	Live      int `json:"live" doc:"Outgoing live messages."`
	Test      int `json:"test" doc:"Outgoing test messages (simulated)."`
	Delivered int `json:"delivered" doc:"Live messages confirmed by the carrier."`
	Sent      int `json:"sent" doc:"Live messages sent without a delivery report (yet)."`
	Failed    int `json:"failed" doc:"Live messages that failed."`
	Pending   int `json:"pending" doc:"Live messages still created, queued or sending."`
	Inbound   int `json:"inbound" doc:"Messages received by phones with forwarding on."`
}

type InsightsMessageDay struct {
	Date      string `json:"date" example:"2026-10-05" doc:"UTC calendar day."`
	Live      int    `json:"live"`
	Test      int    `json:"test"`
	Delivered int    `json:"delivered" doc:"Live only, like sent, failed and pending."`
	Sent      int    `json:"sent"`
	Failed    int    `json:"failed"`
	Pending   int    `json:"pending"`
}

type InsightsProviderCount struct {
	Provider string `json:"provider" example:"android" doc:"android is a paired phone; simulator is test mode."`
	Count    int    `json:"count"`
}

type InsightsErrorCount struct {
	ErrorCode string `json:"error_code" example:"no_device" doc:"unknown when the failure had no code."`
	Count     int    `json:"count"`
}

type InsightsMessages struct {
	Totals       InsightsMessageTotals   `json:"totals"`
	DeliveryRate *float64                `json:"delivery_rate" nullable:"true" doc:"Live delivered / (delivered + failed), 0 to 1. Null before any finished with either."`
	Daily        []InsightsMessageDay    `json:"daily" doc:"One entry per day, oldest first, including days without messages."`
	ByProvider   []InsightsProviderCount `json:"by_provider" doc:"Outgoing messages (live and test) by route, busiest first."`
	TopErrors    []InsightsErrorCount    `json:"top_errors" doc:"The most common failures of live messages, at most 8."`
}

type InsightsVerify struct {
	Started    int      `json:"started" doc:"Verifications started (live and test)."`
	Verified   int      `json:"verified"`
	VerifyRate *float64 `json:"verify_rate" nullable:"true" doc:"verified / started, 0 to 1. Null when none started."`
}

type InsightsPlanShare struct {
	Plan          string `json:"plan" example:"pro" doc:"Plan ID."`
	Name          string `json:"name" example:"Pro"`
	PriceCents    int32  `json:"price_cents" doc:"Monthly price in the currency's smallest unit."`
	Organizations int    `json:"organizations" doc:"Workspaces on this plan now."`
}

type InsightsBillingChange struct {
	OrganizationID    string    `json:"organization_id"`
	OrganizationName  string    `json:"organization_name"`
	Plan              string    `json:"plan" example:"Pro" doc:"Plan name."`
	Status            string    `json:"status" enum:"active,past_due,cancelled,expired,incomplete"`
	CancelAtPeriodEnd bool      `json:"cancel_at_period_end"`
	At                time.Time `json:"at" doc:"When the subscription last changed."`
}

type InsightsBilling struct {
	_             struct{}                `nullable:"true"`
	PlanMix       []InsightsPlanShare     `json:"plan_mix" doc:"Workspaces per plan in effect; active and past-due subscriptions keep their plan, the rest are on Free."`
	MRRCents      int64                   `json:"mrr_cents" doc:"Estimated monthly recurring revenue: the monthly price of every active subscription."`
	Currency      string                  `json:"currency" example:"USD"`
	RecentChanges []InsightsBillingChange `json:"recent_changes" doc:"The 10 most recently changed subscriptions."`
}

type InsightsOrganization struct {
	OrganizationID string  `json:"organization_id"`
	Name           string  `json:"name"`
	Messages       int     `json:"messages" doc:"Outgoing live messages in the range."`
	Phones         int     `json:"phones" doc:"Paired phones now."`
	Plan           *string `json:"plan" nullable:"true" example:"Pro" doc:"Name of the plan in effect. Null without billing."`
}

type InsightsUser struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"created_at"`
	EmailVerified bool      `json:"email_verified"`
	PhoneVerified bool      `json:"phone_verified"`
	Organizations int       `json:"organizations" doc:"Workspaces the account belongs to."`
}

type SystemInsights struct {
	GeneratedAt      time.Time              `json:"generated_at"`
	Range            InsightsRange          `json:"range"`
	Totals           InsightsTotals         `json:"totals" doc:"Counts across the whole instance; *_in_range fields cover the range only."`
	Signups          []InsightsSignupDay    `json:"signups" doc:"New accounts and workspaces per day, oldest first, including days without any."`
	Activity         InsightsActivity       `json:"activity" doc:"Fixed 7 and 30 day windows, whatever the range."`
	Messages         InsightsMessages       `json:"messages"`
	Verify           InsightsVerify         `json:"verify"`
	Billing          *InsightsBilling       `json:"billing" doc:"Plans and revenue. Null unless billing is on (hosted Bridge, BRIDGE_CLOUD)."`
	TopOrganizations []InsightsOrganization `json:"top_organizations" doc:"The 10 workspaces that sent the most live messages in the range."`
	RecentSignups    []InsightsUser         `json:"recent_signups" doc:"The 15 newest accounts."`
}

const (
	insightsMinDays = 7
	insightsMaxDays = 180
)

func (s *Server) registerInsights(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getSystemInsights", Method: http.MethodGet, Path: "/v1/system/insights", Tags: []string{"Status"},
		Summary: "Get operator insights",
		Description: "How this Bridge instance is used, for its operators: accounts, workspaces, phones, message volume and " +
			"delivery, Verify, top workspaces, newest sign-ups and, with billing on, plans and revenue. Computed from Bridge's " +
			"own database (no third-party analytics); it never includes message text or phone numbers. Days are UTC. " +
			"Operators are BRIDGE_OPERATOR_EMAILS, or the first account when that is unset. Anyone else gets the same 404 as an unknown route.",
		Security: sessionAuth, Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *struct {
		// The 7 to 180 bounds are checked in the handler, after the operator
		// check, so a validation error cannot reveal the route to anyone else.
		Days int `query:"days" default:"30" doc:"Days to cover, today included: 7 to 180."`
	}) (*struct{ Body SystemInsights }, error) {
		if !s.isOperator(ctx) {
			return nil, noRoute(http.MethodGet, "/v1/system/insights")
		}
		if in.Days < insightsMinDays || in.Days > insightsMaxDays {
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
				Location: "query.days", Message: fmt.Sprintf("Expected a number of days from %d to %d.", insightsMinDays, insightsMaxDays), Value: in.Days,
			})
		}
		body, err := s.systemInsights(ctx, in.Days, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return &struct{ Body SystemInsights }{Body: body}, nil
	})
}

func rate(n, of int) *float64 {
	if of == 0 {
		return nil
	}
	r := float64(n) / float64(of)
	return &r
}

// systemInsights runs every aggregate in one read-only snapshot so the numbers
// agree with each other.
func (s *Server) systemInsights(ctx context.Context, days int, now time.Time) (SystemInsights, error) {
	today := now.Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -(days - 1))
	out := SystemInsights{
		GeneratedAt: now,
		Range:       InsightsRange{From: first, To: now, Days: days},
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	t, err := q.InsightsTotals(ctx, first)
	if err != nil {
		return out, err
	}
	out.Totals = InsightsTotals{
		Users: int(t.Users), UsersInRange: int(t.UsersInRange), UsersEmailVerified: int(t.UsersEmailVerified),
		UsersWithPhone: int(t.UsersWithPhone), Organizations: int(t.Organizations), OrganizationsInRange: int(t.OrganizationsInRange),
		Projects: int(t.Projects), APIKeys: int(t.APIKeys), SubscriptionsActive: int(t.SubscriptionsActive),
	}
	fleet, err := q.FleetStats(ctx)
	if err != nil {
		return out, err
	}
	out.Totals.Phones.Total, out.Totals.Phones.Online = int(fleet.Total), int(fleet.Online)

	active, err := q.InsightsActiveOrganizations(ctx)
	if err != nil {
		return out, err
	}
	out.Activity = InsightsActivity{
		ActiveOrganizations7d: int(active.Active7d), ActiveOrganizations30d: int(active.Active30d), ActiveUsers7d: int(t.ActiveUsers7d),
	}

	firstDay := pgtype.Date{Time: first, Valid: true}
	lastDay := pgtype.Date{Time: today, Valid: true}
	signups, err := q.InsightsSignupsDaily(ctx, dbq.InsightsSignupsDailyParams{FirstDay: firstDay, LastDay: lastDay, Since: first})
	if err != nil {
		return out, err
	}
	out.Signups = make([]InsightsSignupDay, 0, len(signups))
	for _, d := range signups {
		out.Signups = append(out.Signups, InsightsSignupDay{Date: d.Date, Users: int(d.Users), Organizations: int(d.Organizations)})
	}

	daily, err := q.InsightsMessagesDaily(ctx, dbq.InsightsMessagesDailyParams{FirstDay: firstDay, LastDay: lastDay, Since: first})
	if err != nil {
		return out, err
	}
	m := &out.Messages
	m.Daily = make([]InsightsMessageDay, 0, len(daily))
	for _, d := range daily {
		m.Daily = append(m.Daily, InsightsMessageDay{
			Date: d.Date, Live: int(d.Live), Test: int(d.Test), Delivered: int(d.Delivered),
			Sent: int(d.Sent), Failed: int(d.Failed), Pending: int(d.Pending),
		})
		m.Totals.Live += int(d.Live)
		m.Totals.Test += int(d.Test)
		m.Totals.Delivered += int(d.Delivered)
		m.Totals.Sent += int(d.Sent)
		m.Totals.Failed += int(d.Failed)
		m.Totals.Pending += int(d.Pending)
		m.Totals.Inbound += int(d.Inbound)
	}
	m.DeliveryRate = rate(m.Totals.Delivered, m.Totals.Delivered+m.Totals.Failed)

	providers, err := q.InsightsMessagesByProvider(ctx, first)
	if err != nil {
		return out, err
	}
	m.ByProvider = make([]InsightsProviderCount, 0, len(providers))
	for _, p := range providers {
		m.ByProvider = append(m.ByProvider, InsightsProviderCount{Provider: p.Provider, Count: int(p.Messages)})
	}
	errs, err := q.InsightsTopErrors(ctx, first)
	if err != nil {
		return out, err
	}
	m.TopErrors = make([]InsightsErrorCount, 0, len(errs))
	for _, e := range errs {
		m.TopErrors = append(m.TopErrors, InsightsErrorCount{ErrorCode: e.ErrorCode, Count: int(e.Messages)})
	}

	v, err := q.InsightsVerify(ctx, first)
	if err != nil {
		return out, err
	}
	out.Verify = InsightsVerify{Started: int(v.Started), Verified: int(v.Verified), VerifyRate: rate(int(v.Verified), int(v.Started))}

	hosted := s.billing.Enabled()
	top, err := q.InsightsTopOrganizations(ctx, dbq.InsightsTopOrganizationsParams{FreePlan: billing.FreePlan, Since: first})
	if err != nil {
		return out, err
	}
	out.TopOrganizations = make([]InsightsOrganization, 0, len(top))
	for _, o := range top {
		org := InsightsOrganization{OrganizationID: o.OrganizationID, Name: o.Name, Messages: int(o.Messages), Phones: int(o.Phones)}
		if hosted {
			org.Plan = &o.PlanName
		}
		out.TopOrganizations = append(out.TopOrganizations, org)
	}

	users, err := q.InsightsRecentUsers(ctx)
	if err != nil {
		return out, err
	}
	out.RecentSignups = make([]InsightsUser, 0, len(users))
	for _, u := range users {
		out.RecentSignups = append(out.RecentSignups, InsightsUser{
			ID: u.ID, Email: u.Email, Name: u.Name, CreatedAt: u.CreatedAt.UTC(),
			EmailVerified: u.EmailVerifiedAt != nil, PhoneVerified: u.PhoneVerifiedAt != nil, Organizations: int(u.Organizations),
		})
	}

	if hosted {
		b, err := insightsBilling(ctx, q)
		if err != nil {
			return out, err
		}
		out.Billing = b
	}
	return out, nil
}

func insightsBilling(ctx context.Context, q *dbq.Queries) (*InsightsBilling, error) {
	mix, err := q.InsightsPlanMix(ctx, billing.FreePlan)
	if err != nil {
		return nil, err
	}
	b := &InsightsBilling{Currency: "USD", PlanMix: make([]InsightsPlanShare, 0, len(mix))}
	for _, p := range mix {
		b.PlanMix = append(b.PlanMix, InsightsPlanShare{Plan: p.ID, Name: p.Name, PriceCents: p.PriceCents, Organizations: int(p.Organizations)})
		if p.PriceCents > 0 && p.Currency != "" {
			b.Currency = p.Currency
		}
	}
	if b.MRRCents, err = q.InsightsMRR(ctx); err != nil {
		return nil, err
	}
	changes, err := q.InsightsRecentSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	b.RecentChanges = make([]InsightsBillingChange, 0, len(changes))
	for _, c := range changes {
		b.RecentChanges = append(b.RecentChanges, InsightsBillingChange{
			OrganizationID: c.OrganizationID, OrganizationName: c.OrganizationName, Plan: c.PlanName,
			Status: c.Status, CancelAtPeriodEnd: c.CancelAtPeriodEnd, At: c.UpdatedAt.UTC(),
		})
	}
	return b, nil
}
