package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"bridge/internal/db/dbq"
)

// Bridge's subscription statuses.
const (
	StatusActive     = "active"
	StatusPastDue    = "past_due"
	StatusCancelled  = "cancelled"
	StatusExpired    = "expired"
	StatusIncomplete = "incomplete"
)

// NormalizeStatus maps a Dodo subscription status to Bridge's. on_hold keeps
// access while Dodo retries the payment; unknown statuses grant nothing.
func NormalizeStatus(dodo string) string {
	switch dodo {
	case "active":
		return StatusActive
	case "on_hold", "past_due":
		return StatusPastDue
	case "cancelled", "paused":
		return StatusCancelled
	case "expired":
		return StatusExpired
	}
	return StatusIncomplete
}

// ErrBadSignature means a webhook did not verify.
var ErrBadSignature = errors.New("webhook signature does not verify")

// webhookTolerance is how far a webhook's timestamp may be from now.
const webhookTolerance = 5 * time.Minute

// VerifyWebhook checks a Standard Webhooks signature over the raw body.
func VerifyWebhook(secret, id, timestamp, signatures string, body []byte, now time.Time) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil || len(key) == 0 {
		return fmt.Errorf("webhook secret is not valid base64 (whsec_…)")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || id == "" {
		return ErrBadSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > webhookTolerance || d < -webhookTolerance {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for sig := range strings.FieldsSeq(signatures) {
		version, value, ok := strings.Cut(sig, ",")
		if !ok || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(value)
		if err == nil && hmac.Equal(got, want) {
			return nil
		}
	}
	return ErrBadSignature
}

// Event is the part of a Dodo webhook Bridge reads. Subscription events carry
// the subscription object.
type Event struct {
	Type string `json:"type"`
	Data struct {
		PayloadType string `json:"payload_type"`
		Subscription
	} `json:"data"`
}

// ErrUnmatched means a subscription for one of Bridge's products could not be
// tied to a workspace. The webhook fails so Dodo delivers it again.
var ErrUnmatched = errors.New("subscription does not belong to a known workspace")

// HandleWebhook verifies and applies one Dodo webhook. A subscription event is
// only a signal: Bridge reads the subscription's current state from Dodo and
// applies that, so late or out-of-order deliveries can never undo a newer
// change. Deliveries are claimed atomically by webhook-id.
func (s *Service) HandleWebhook(ctx context.Context, id, timestamp, signatures string, body []byte) error {
	if !s.Payments() {
		return errors.New("billing is not configured")
	}
	if err := VerifyWebhook(s.cfg.Dodo.WebhookSecret, id, timestamp, signatures, body, s.now()); err != nil {
		return err
	}
	var ev Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return fmt.Errorf("webhook body: %w", err)
	}
	isSub := strings.HasPrefix(ev.Type, "subscription.") && ev.Data.PayloadType == "Subscription" &&
		ev.Data.SubscriptionID != ""
	var sub Subscription
	if isSub {
		if s.planForProduct(ev.Data.ProductID) == "" {
			// Another product on the same Dodo account; nothing for Bridge to do.
			s.log.Info("billing webhook for another product", "product_id", ev.Data.ProductID, "type", ev.Type)
			isSub = false
		} else {
			current, err := s.dodo.GetSubscription(ctx, ev.Data.SubscriptionID)
			if err != nil {
				return fmt.Errorf("read subscription %s: %w", ev.Data.SubscriptionID, err)
			}
			sub = current
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	if _, err := q.ClaimBillingWebhookEvent(ctx, dbq.ClaimBillingWebhookEventParams{ID: id, Type: ev.Type}); errors.Is(err, pgx.ErrNoRows) {
		return nil // already processed
	} else if err != nil {
		return err
	}
	if isSub {
		if _, err := s.apply(ctx, q, sub, ""); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// apply stores a subscription's current state on its workspace. wantOrg, when
// set, is the workspace the caller expects; a subscription of another
// workspace is refused. It returns the workspace ID.
func (s *Service) apply(ctx context.Context, q *dbq.Queries, sub Subscription, wantOrg string) (string, error) {
	plan := s.planForProduct(sub.ProductID)
	if plan == "" {
		return "", fmt.Errorf("subscription %s is for product %s, which is not in BRIDGE_DODO_PRODUCTS", sub.SubscriptionID, sub.ProductID)
	}
	orgID, err := s.resolveOrganization(ctx, q, sub)
	if err != nil {
		return "", err
	}
	if orgID == "" || (wantOrg != "" && orgID != wantOrg) {
		s.log.Error("billing: subscription without a matching workspace", "subscription_id", sub.SubscriptionID,
			"customer_id", sub.Customer.CustomerID, "found", orgID, "want", wantOrg)
		return "", ErrUnmatched
	}
	status := NormalizeStatus(sub.Status)
	cur, err := q.GetSubscriptionForUpdate(ctx, orgID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// A workspace has one subscription at a time. An event about an older one
	// that no longer grants access must not replace a newer, active one.
	if err == nil && cur.ProviderSubscriptionID != nil && *cur.ProviderSubscriptionID != sub.SubscriptionID &&
		grants(cur.Status) && !grants(status) {
		return orgID, nil
	}
	var customer *string
	if sub.Customer.CustomerID != "" {
		customer = &sub.Customer.CustomerID
	}
	_, err = q.UpsertSubscription(ctx, dbq.UpsertSubscriptionParams{
		OrganizationID: orgID, PlanID: plan, Status: status, ProviderCustomerID: customer,
		ProviderSubscriptionID: &sub.SubscriptionID, CurrentPeriodEnd: sub.NextBilling, CancelAtPeriodEnd: sub.CancelAtNext,
	})
	if err != nil {
		return "", err
	}
	s.log.Info("subscription updated", "organization_id", orgID, "plan", plan, "status", status,
		"cancel_at_period_end", sub.CancelAtNext)
	return orgID, nil
}

// resolveOrganization finds the workspace a subscription belongs to: checkout
// metadata first (the first webhook can beat the checkout redirect), then the
// stored subscription, then the stored customer.
func (s *Service) resolveOrganization(ctx context.Context, q *dbq.Queries, sub Subscription) (string, error) {
	if org, ok := sub.Metadata["organization_id"].(string); ok && org != "" {
		exists, err := q.OrganizationExists(ctx, org)
		if err != nil {
			return "", err
		}
		if exists {
			return org, nil
		}
	}
	if row, err := q.GetSubscriptionByProviderID(ctx, &sub.SubscriptionID); err == nil {
		return row.OrganizationID, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if c := sub.Customer.CustomerID; c != "" {
		if row, err := q.GetSubscriptionByCustomer(ctx, &c); err == nil {
			return row.OrganizationID, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	return "", nil
}

func (s *Service) planForProduct(productID string) string {
	for plan, product := range s.cfg.Dodo.Products {
		if product == productID {
			return plan
		}
	}
	return ""
}
