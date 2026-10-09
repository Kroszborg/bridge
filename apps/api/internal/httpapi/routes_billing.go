package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"bridge/internal/billing"
)

type PlanLimits struct {
	Phones       *int32 `json:"phones" nullable:"true" doc:"Paired phones across the workspace. Null is unlimited."`
	LiveMessages *int32 `json:"live_messages" nullable:"true" doc:"Live SMS accepted per calendar month (UTC). Null is unlimited."`
	Projects     *int32 `json:"projects" nullable:"true" doc:"Null is unlimited."`
	Members      *int32 `json:"members" nullable:"true" doc:"Members plus open invitations. Null is unlimited."`
}

type BillingPlan struct {
	ID          string     `json:"id" example:"pro"`
	Name        string     `json:"name" example:"Pro"`
	PriceCents  int32      `json:"price_cents" example:"900" doc:"Monthly price in the currency's smallest unit."`
	Currency    string     `json:"currency" example:"USD"`
	Interval    string     `json:"interval" enum:"month"`
	Limits      PlanLimits `json:"limits"`
	Purchasable bool       `json:"purchasable" doc:"Whether the plan can be bought on this server."`
}

type BillingUsage struct {
	Phones       int64 `json:"phones"`
	LiveMessages int64 `json:"live_messages" doc:"Live SMS accepted this period."`
	Projects     int64 `json:"projects"`
	Members      int64 `json:"members"`
}

type Billing struct {
	Enabled           bool         `json:"enabled" doc:"False on self-hosted servers, which have no limits."`
	Payments          bool         `json:"payments" doc:"Whether the workspace can change plans here."`
	Plan              BillingPlan  `json:"plan"`
	Status            string       `json:"status" enum:"none,active,past_due,cancelled,expired,incomplete" doc:"The subscription's status; none on the free plan."`
	CurrentPeriodEnd  *time.Time   `json:"current_period_end" nullable:"true" doc:"When the paid period ends or renews."`
	CancelAtPeriodEnd bool         `json:"cancel_at_period_end"`
	Usage             BillingUsage `json:"usage"`
	PeriodStart       time.Time    `json:"period_start" doc:"Start of the live SMS usage period (the 1st, UTC)."`
	PeriodEnd         time.Time    `json:"period_end"`
	ManageBilling     bool         `json:"manage_billing" doc:"Whether a customer portal is available for invoices, payment methods and cancelling."`
}

type planBody struct {
	Plan string `json:"plan" enum:"pro,business" doc:"The plan to move to. Downgrade to Free by cancelling in the customer portal."`
}

type urlBody struct {
	URL string `json:"url" doc:"Open this URL in the browser."`
}

func (s *Server) toPlan(p billing.Plan) BillingPlan {
	return BillingPlan{
		ID: p.ID, Name: p.Name, PriceCents: p.PriceCents, Currency: p.Currency, Interval: "month",
		Limits: PlanLimits{
			Phones: p.Limits.Phones, LiveMessages: p.Limits.LiveMessages, Projects: p.Limits.Projects, Members: p.Limits.Members,
		},
		Purchasable: s.billing.Purchasable(p.ID),
	}
}

// planLimitError explains which limit of the plan was reached.
func planLimitError(le *billing.LimitError) error {
	return Errorf(http.StatusPaymentRequired, CodePlanLimitReached, le.Error())
}

// billingError renders plan limits; other errors pass through.
func billingError(err error) error {
	var le *billing.LimitError
	if errors.As(err, &le) {
		return planLimitError(le)
	}
	return err
}

func (s *Server) registerBilling(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listPlans", Method: http.MethodGet, Path: "/v1/plans", Tags: []string{"Billing"},
		Summary:     "List plans",
		Description: "Public, no authentication. The hosted plans and their limits. Self-hosted servers return an empty list.",
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body ListResponse[BillingPlan] }, error) {
		out := &struct{ Body ListResponse[BillingPlan] }{Body: ListResponse[BillingPlan]{Data: []BillingPlan{}}}
		if !s.billing.Enabled() {
			return out, nil
		}
		plans, err := s.billing.Plans(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range plans {
			out.Body.Data = append(out.Body.Data, s.toPlan(p))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getBilling", Method: http.MethodGet, Path: "/v1/organizations/{organizationId}/billing", Tags: []string{"Billing"},
		Summary: "Get the workspace's plan and usage", Security: sessionAuth, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *OrgPath) (*struct{ Body Billing }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		return s.billingView(ctx, org.ID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "syncBilling", Method: http.MethodPost, Path: "/v1/organizations/{organizationId}/billing/sync", Tags: []string{"Billing"},
		Summary:     "Refresh a subscription from Dodo",
		Description: "Called on the return from checkout with the subscription_id Dodo appends, so the plan updates without waiting for the webhook.",
		Security:    sessionAuth, Errors: []int{http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct {
		OrgPath
		Body struct {
			SubscriptionID string `json:"subscription_id" minLength:"1" maxLength:"100"`
		}
	}) (*struct{ Body Billing }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		if !s.billing.Payments() {
			return nil, Errorf(http.StatusServiceUnavailable, CodeUnavailable, "Payments are not set up on this server.")
		}
		if err := s.billing.Sync(ctx, org.ID, in.Body.SubscriptionID); err != nil {
			return nil, billingProviderError(err)
		}
		return s.billingView(ctx, org.ID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "resumeBilling", Metadata: ownerOnly, Method: http.MethodPost,
		Path: "/v1/organizations/{organizationId}/billing/resume", Tags: []string{"Billing"},
		Summary:     "Keep a subscription that is set to cancel",
		Description: "Withdraws a scheduled cancellation, so the plan renews as usual.",
		Security:    sessionAuth, Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, func(ctx context.Context, in *OrgPath) (*struct{ Body Billing }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		if !s.billing.Payments() {
			return nil, Errorf(http.StatusServiceUnavailable, CodeUnavailable, "Payments are not set up on this server.")
		}
		if err := s.billing.Resume(ctx, org.ID); err != nil {
			return nil, billingProviderError(err)
		}
		_ = s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: "billing.resumed"})
		return s.billingView(ctx, org.ID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "createBillingCheckout", Metadata: ownerOnly, Method: http.MethodPost,
		Path: "/v1/organizations/{organizationId}/billing/checkout", Tags: []string{"Billing"},
		Summary: "Upgrade or change the plan",
		Description: "Returns a checkout URL for a workspace without a paid plan. For a workspace that already pays, Dodo is asked " +
			"to move the subscription to the new plan (prorated; refused if the charge fails), and the URL leads back to the " +
			"billing page, which updates once Dodo confirms.",
		Security: sessionAuth, Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *struct {
		OrgPath
		Body planBody
	}) (*struct{ Body urlBody }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		if !s.billing.Purchasable(in.Body.Plan) {
			return nil, Errorf(http.StatusServiceUnavailable, CodeUnavailable, "Payments are not set up on this server.")
		}
		back := s.cfg.DashboardOrigin() + "/organizations/" + org.ID + "/billing"
		user := principalFrom(ctx).User
		u, err := s.billing.Checkout(ctx, billing.CheckoutInput{
			OrganizationID: org.ID, Plan: in.Body.Plan, Email: user.Email, Name: user.Name,
			ReturnURL: back + "?checkout=return", ChangedURL: back + "?change=requested", CancelURL: back,
		})
		if err != nil {
			return nil, billingProviderError(err)
		}
		_ = s.audit(ctx, s.q, auditEntry{OrganizationID: org.ID, Action: "billing.checkout", Metadata: map[string]any{"plan": in.Body.Plan}})
		return &struct{ Body urlBody }{Body: urlBody{URL: u}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createBillingPortal", Metadata: ownerOnly, Method: http.MethodPost,
		Path: "/v1/organizations/{organizationId}/billing/portal", Tags: []string{"Billing"},
		Summary:     "Open the customer portal",
		Description: "Returns a link to Dodo's customer portal: invoices, payment methods and cancelling.",
		Security:    sessionAuth, Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *OrgPath) (*struct{ Body urlBody }, error) {
		org, err := s.orgForUser(ctx, in.OrganizationID)
		if err != nil {
			return nil, err
		}
		if !s.billing.Payments() {
			return nil, Errorf(http.StatusServiceUnavailable, CodeUnavailable, "Payments are not set up on this server.")
		}
		u, err := s.billing.Portal(ctx, org.ID, s.cfg.DashboardOrigin()+"/organizations/"+org.ID+"/billing")
		if err != nil {
			return nil, billingProviderError(err)
		}
		return &struct{ Body urlBody }{Body: urlBody{URL: u}}, nil
	})
}

// billingView is the workspace's plan, subscription and usage.
func (s *Server) billingView(ctx context.Context, orgID string) (*struct{ Body Billing }, error) {
	usage, err := s.billing.Usage(ctx, orgID)
	if err != nil {
		return nil, err
	}
	start, end := s.billing.PeriodBounds()
	b := Billing{
		Enabled: s.billing.Enabled(), Payments: s.billing.Payments(), Status: "none",
		Usage:       BillingUsage{Phones: usage.Phones, LiveMessages: usage.LiveMessages, Projects: usage.Projects, Members: usage.Members},
		PeriodStart: start, PeriodEnd: end,
		Plan: BillingPlan{ID: "self_hosted", Name: "Self-hosted", Currency: "USD", Interval: "month"},
	}
	if b.Enabled {
		plan, sub, err := s.billing.Current(ctx, s.q, orgID)
		if err != nil {
			return nil, err
		}
		b.Plan = s.toPlan(plan)
		if sub != nil {
			b.Status, b.CurrentPeriodEnd, b.CancelAtPeriodEnd = sub.Status, sub.CurrentPeriodEnd, sub.CancelAtPeriodEnd
			b.ManageBilling = b.Payments && sub.ProviderCustomerID != nil
		}
	}
	return &struct{ Body Billing }{Body: b}, nil
}

// billingProviderError explains billing refusals and Dodo errors in plain words.
// Dodo's own 5xx and network failures stay internal errors (logged, retryable).
func billingProviderError(err error) error {
	var ae *billing.APIError
	switch {
	case errors.Is(err, billing.ErrSamePlan):
		return Errorf(http.StatusConflict, CodeConflict, "The workspace is already on this plan.")
	case errors.Is(err, billing.ErrPaymentFailing):
		return Errorf(http.StatusConflict, CodeConflict,
			"The last payment failed. Update the payment method in Manage billing, then change plans.")
	case errors.Is(err, billing.ErrCancelScheduled):
		return Errorf(http.StatusConflict, CodeConflict,
			"This plan is set to end. Choose Keep my plan first, then change plans.")
	case errors.Is(err, billing.ErrNothingToResume):
		return Errorf(http.StatusConflict, CodeConflict, "This plan is not set to cancel.")
	case errors.Is(err, billing.ErrNoCustomer):
		return Errorf(http.StatusNotFound, CodeNotFound, "This workspace has no billing account yet. Upgrade first.")
	case errors.Is(err, billing.ErrUnmatched):
		return Errorf(http.StatusNotFound, CodeNotFound, "That subscription does not belong to this workspace.")
	case errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500:
		msg := "Dodo Payments refused the request."
		if m := ae.Message(); m != "" {
			msg = "Dodo Payments refused the request: " + m
		}
		return Errorf(http.StatusBadGateway, "payment_provider_error", msg)
	}
	return err
}

// dodoWebhook receives Dodo Payments events. It answers 401 to bad
// signatures and 500 to processing errors so Dodo retries.
func (s *Server) dodoWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.billing.Payments() {
		writeRawError(w, r, http.StatusNotFound, CodeNotFound, "Billing is not enabled on this server.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeRawError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Could not read the body.")
		return
	}
	err = s.billing.HandleWebhook(r.Context(), r.Header.Get("webhook-id"), r.Header.Get("webhook-timestamp"), r.Header.Get("webhook-signature"), body)
	switch {
	case errors.Is(err, billing.ErrBadSignature):
		writeRawError(w, r, http.StatusUnauthorized, CodeUnauthenticated, "The webhook signature does not verify.")
	case err != nil:
		s.log.Error("billing webhook failed", "request_id", RequestIDFrom(r.Context()), "error", err)
		writeRawError(w, r, http.StatusInternalServerError, CodeInternal, "The event could not be processed; it will be retried.")
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"received": true})
	}
}
