package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bridge/internal/config"
)

// Dodo is a minimal Dodo Payments REST client: checkout sessions, the
// customer portal and plan changes.
type Dodo struct {
	base string
	key  string
	http *http.Client
}

func NewDodo(c *config.DodoConfig) *Dodo {
	base := c.BaseURL
	if base == "" {
		base = "https://test.dodopayments.com"
		if c.Environment == config.DodoLiveMode {
			base = "https://live.dodopayments.com"
		}
	}
	return &Dodo{base: strings.TrimSuffix(base, "/"), key: c.APIKey, http: &http.Client{Timeout: 20 * time.Second}}
}

// APIError is an error response from Dodo.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("dodo: HTTP %d: %s", e.Status, e.Body) }

// Message is Dodo's explanation, for showing to the customer when it is a 4xx.
func (e *APIError) Message() string {
	var body struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if json.Unmarshal([]byte(e.Body), &body) == nil && body.Message != "" {
		return body.Message
	}
	return ""
}

func (d *Dodo) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.key)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("dodo: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// CheckoutRequest starts a subscription checkout for one product.
type CheckoutRequest struct {
	ProductID string
	// CustomerID reuses an existing Dodo customer (their saved cards and
	// invoices stay together); otherwise Email and Name create one.
	CustomerID string
	Email      string
	Name       string
	ReturnURL  string
	CancelURL  string
	Metadata   map[string]string
}

// CreateCheckout returns the hosted checkout URL.
func (d *Dodo) CreateCheckout(ctx context.Context, r CheckoutRequest) (string, error) {
	customer := map[string]any{"email": r.Email}
	if r.Name != "" {
		customer["name"] = r.Name
	}
	if r.CustomerID != "" {
		customer = map[string]any{"customer_id": r.CustomerID}
	}
	in := map[string]any{
		"product_cart":               []map[string]any{{"product_id": r.ProductID, "quantity": 1}},
		"customer":                   customer,
		"return_url":                 r.ReturnURL,
		"cancel_url":                 r.CancelURL,
		"metadata":                   r.Metadata,
		"show_saved_payment_methods": r.CustomerID != "",
	}
	var out struct {
		CheckoutURL *string `json:"checkout_url"`
	}
	if err := d.do(ctx, http.MethodPost, "/checkouts", in, &out); err != nil {
		return "", err
	}
	if out.CheckoutURL == nil || *out.CheckoutURL == "" {
		return "", fmt.Errorf("dodo: checkout session has no checkout_url")
	}
	return *out.CheckoutURL, nil
}

// PortalLink returns a customer portal URL where the customer manages
// payment methods, invoices and cancellation.
func (d *Dodo) PortalLink(ctx context.Context, customerID, returnURL string) (string, error) {
	path := "/customers/" + url.PathEscape(customerID) + "/customer-portal/session"
	if returnURL != "" {
		path += "?return_url=" + url.QueryEscape(returnURL)
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := d.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return "", err
	}
	if out.Link == "" {
		return "", fmt.Errorf("dodo: customer portal session has no link")
	}
	return out.Link, nil
}

// Subscription is Dodo's subscription object, the parts Bridge reads. Webhooks
// carry the same object.
type Subscription struct {
	SubscriptionID string         `json:"subscription_id"`
	Status         string         `json:"status"`
	ProductID      string         `json:"product_id"`
	Metadata       map[string]any `json:"metadata"`
	NextBilling    *time.Time     `json:"next_billing_date"`
	CancelAtNext   bool           `json:"cancel_at_next_billing_date"`
	Customer       struct {
		CustomerID string `json:"customer_id"`
		Email      string `json:"email"`
	} `json:"customer"`
}

// GetSubscription reads a subscription's current state.
func (d *Dodo) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	var out Subscription
	err := d.do(ctx, http.MethodGet, "/subscriptions/"+url.PathEscape(id), nil, &out)
	return out, err
}

// SetCancelAtPeriodEnd schedules (true) or withdraws (false) cancellation at
// the end of the paid period, and returns the updated subscription.
func (d *Dodo) SetCancelAtPeriodEnd(ctx context.Context, id string, cancel bool) (Subscription, error) {
	var out Subscription
	err := d.do(ctx, http.MethodPatch, "/subscriptions/"+url.PathEscape(id),
		map[string]any{"cancel_at_next_billing_date": cancel}, &out)
	return out, err
}

// ChangePlan moves a subscription to another product, prorated immediately.
// The change arrives back as a subscription.plan_changed webhook.
func (d *Dodo) ChangePlan(ctx context.Context, subscriptionID, productID string) error {
	in := map[string]any{
		"product_id": productID, "quantity": 1, "proration_billing_mode": "prorated_immediately",
		"effective_at": "immediately", "on_payment_failure": "prevent_change",
	}
	return d.do(ctx, http.MethodPost, "/subscriptions/"+url.PathEscape(subscriptionID)+"/change-plan", in, nil)
}
