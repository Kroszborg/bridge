package billing

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
)

var (
	// ErrSamePlan means the organization already pays for the requested plan.
	ErrSamePlan = errors.New("already on this plan")
	// ErrNoCustomer means the organization never paid, so it has no portal.
	ErrNoCustomer = errors.New("no billing customer")
	// ErrPaymentFailing means the last renewal failed; fix the card before changing plans.
	ErrPaymentFailing = errors.New("the last payment failed")
	// ErrCancelScheduled means the subscription ends at the period end; resume it first.
	ErrCancelScheduled = errors.New("cancellation is scheduled")
	// ErrNothingToResume means there is no scheduled cancellation to withdraw.
	ErrNothingToResume = errors.New("no scheduled cancellation")
)

// CheckoutInput asks to move an organization to a paid plan.
type CheckoutInput struct {
	OrganizationID string
	Plan           string
	Email          string
	Name           string
	ReturnURL      string // after a Dodo checkout; Dodo appends subscription_id and status
	ChangedURL     string // after an in-place plan change, which Dodo confirms by webhook
	CancelURL      string
}

// Checkout returns where to send the customer. Without a live subscription it
// is a Dodo checkout. With one, the subscription is moved to the new plan in
// place (prorated; Dodo refuses the change if the charge fails) and the
// customer goes back to ChangedURL while Dodo confirms.
func (s *Service) Checkout(ctx context.Context, in CheckoutInput) (string, error) {
	product := s.cfg.Dodo.Products[in.Plan]
	req := CheckoutRequest{
		ProductID: product, Email: in.Email, Name: in.Name, ReturnURL: in.ReturnURL, CancelURL: in.CancelURL,
		Metadata: map[string]string{"organization_id": in.OrganizationID, "plan": in.Plan},
	}
	sub, err := s.q.GetSubscription(ctx, in.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", err
	default:
		if sub.ProviderCustomerID != nil {
			req.CustomerID = *sub.ProviderCustomerID // keep cards and invoices together
		}
		if grants(sub.Status) && sub.ProviderSubscriptionID != nil {
			switch {
			case sub.Status == StatusPastDue:
				return "", ErrPaymentFailing
			case sub.CancelAtPeriodEnd:
				return "", ErrCancelScheduled
			case sub.PlanID == in.Plan:
				return "", ErrSamePlan
			}
			if err := s.dodo.ChangePlan(ctx, *sub.ProviderSubscriptionID, product); err != nil {
				return "", err
			}
			return in.ChangedURL, nil
		}
	}
	return s.dodo.CreateCheckout(ctx, req)
}

// Resume withdraws a scheduled cancellation, so the plan renews as usual.
func (s *Service) Resume(ctx context.Context, orgID string) error {
	sub, err := s.q.GetSubscription(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNothingToResume
	}
	if err != nil {
		return err
	}
	if !sub.CancelAtPeriodEnd || !grants(sub.Status) || sub.ProviderSubscriptionID == nil {
		return ErrNothingToResume
	}
	updated, err := s.dodo.SetCancelAtPeriodEnd(ctx, *sub.ProviderSubscriptionID, false)
	if err != nil {
		return err
	}
	return s.store(ctx, updated, orgID)
}

// Sync reads a subscription from Dodo and stores it on the workspace, for the
// return from checkout (Dodo appends subscription_id) before the webhook lands.
// A subscription that belongs to another workspace is refused.
func (s *Service) Sync(ctx context.Context, orgID, subscriptionID string) error {
	sub, err := s.dodo.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return err
	}
	return s.store(ctx, sub, orgID)
}

func (s *Service) store(ctx context.Context, sub Subscription, orgID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := s.apply(ctx, s.q.WithTx(tx), sub, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Portal returns a Dodo customer portal link for the organization.
func (s *Service) Portal(ctx context.Context, orgID, returnURL string) (string, error) {
	sub, err := s.q.GetSubscription(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && sub.ProviderCustomerID == nil) {
		return "", ErrNoCustomer
	}
	if err != nil {
		return "", err
	}
	return s.dodo.PortalLink(ctx, *sub.ProviderCustomerID, returnURL)
}

// CheckJoin returns a *LimitError when the organization has no room for one
// more member. Unlike CheckAdd(Members) it ignores open invitations, since the
// person joining holds one of them.
func (s *Service) CheckJoin(ctx context.Context, q *dbq.Queries, orgID string) error {
	if !s.Enabled() {
		return nil
	}
	plan, _, err := s.Current(ctx, q, orgID)
	if err != nil || plan.Limits.Members == nil {
		return err
	}
	n, err := q.CountOrganizationMembers(ctx, orgID)
	if err != nil {
		return err
	}
	if n >= int64(*plan.Limits.Members) {
		return &LimitError{Resource: Members, Limit: *plan.Limits.Members, Plan: plan.Name}
	}
	return nil
}
