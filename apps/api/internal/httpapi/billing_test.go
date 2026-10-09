package httpapi_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"bridge/internal/config"
)

func cloud(c *config.Config) { c.Cloud = true }

func TestBillingSelfHosted(t *testing.T) {
	srv := newServer(t)
	c, orgID, _ := signup(t, srv)

	b := c.mustStatus(c.do("GET", "/v1/organizations/"+orgID+"/billing", nil), http.StatusOK).Body
	if b["enabled"] != false || b["plan"].(map[string]any)["id"] != "self_hosted" {
		t.Fatalf("self-hosted billing: %v", b)
	}
	if lim := b["plan"].(map[string]any)["limits"].(map[string]any); lim["phones"] != nil || lim["live_messages"] != nil {
		t.Fatalf("self-hosted must be unlimited: %v", lim)
	}
	plans := c.mustStatus(c.do("GET", "/v1/plans", nil), http.StatusOK).Body["data"].([]any)
	if len(plans) != 0 {
		t.Fatalf("self-hosted servers list no plans: %v", plans)
	}
	// Without BRIDGE_CLOUD nothing is limited.
	c.mustStatus(c.do("POST", "/v1/organizations/"+orgID+"/projects", map[string]any{"name": "Second"}), http.StatusCreated)
}

func TestPlanLimits(t *testing.T) {
	srv := newServer(t, cloud)
	c, orgID, projectID := signup(t, srv)

	b := c.mustStatus(c.do("GET", "/v1/organizations/"+orgID+"/billing", nil), http.StatusOK).Body
	plan := b["plan"].(map[string]any)
	if b["enabled"] != true || plan["id"] != "free" || b["status"] != "none" || b["payments"] != false {
		t.Fatalf("new workspace billing: %v", b)
	}
	if lim := plan["limits"].(map[string]any); lim["projects"] != float64(1) || lim["live_messages"] != float64(300) {
		t.Fatalf("free limits: %v", lim)
	}
	if plans := c.mustStatus(c.do("GET", "/v1/plans", nil), http.StatusOK).Body["data"].([]any); len(plans) != 3 {
		t.Fatalf("plans: %v", plans)
	}

	// Projects: the default project uses the Free plan's only one.
	r := c.do("POST", "/v1/organizations/"+orgID+"/projects", map[string]any{"name": "Second"})
	if r.Status != http.StatusPaymentRequired || r.errCode() != "plan_limit_reached" || !strings.Contains(r.errMessage(), "1 project") {
		t.Fatalf("second project: %d %s", r.Status, r.Raw)
	}

	// Members: Free is for one person; teams start at Pro.
	if r := c.do("POST", "/v1/organizations/"+orgID+"/invites", map[string]any{"role": "member"}); r.Status != http.StatusPaymentRequired || !strings.Contains(r.errMessage(), "1 member") {
		t.Fatalf("second seat on Free: %d %s", r.Status, r.Raw)
	}

	// Live messages: 299 used, so one more is accepted and the next refused.
	if _, err := testDB.Pool.Exec(context.Background(),
		`INSERT INTO organization_usage (organization_id, period, live_messages) VALUES ($1, date_trunc('month', now() AT TIME ZONE 'UTC')::date, 299)`,
		orgID); err != nil {
		t.Fatal(err)
	}
	send := func() response {
		return c.do("POST", "/v1/projects/"+projectID+"/messages?environment=live", map[string]any{"to": "+919876543210", "message": "hello"})
	}
	c.mustStatus(send(), http.StatusAccepted)
	r = send()
	if r.Status != http.StatusPaymentRequired || r.errCode() != "plan_limit_reached" || !strings.Contains(r.errMessage(), "300 live SMS") {
		t.Fatalf("over the monthly allowance: %d %s", r.Status, r.Raw)
	}
	// Test messages are never limited.
	c.mustStatus(c.do("POST", "/v1/projects/"+projectID+"/messages?environment=test", map[string]any{"to": "+15550000001", "message": "hello"}), http.StatusAccepted)

	b = c.mustStatus(c.do("GET", "/v1/organizations/"+orgID+"/billing", nil), http.StatusOK).Body
	if u := b["usage"].(map[string]any); u["live_messages"] != float64(300) || u["projects"] != float64(1) || u["members"] != float64(1) {
		t.Fatalf("usage: %v", u)
	}
}

// fakeDodo plays Dodo Payments: it records requests and holds subscriptions,
// which Bridge reads back on every webhook.
type fakeDodo struct {
	mu       sync.Mutex
	requests map[string]map[string]any // "METHOD path" -> last JSON body
	subs     map[string]map[string]any // subscription_id -> subscription object
}

func newFakeDodo(t *testing.T) (*fakeDodo, string) {
	f := &fakeDodo{requests: map[string]map[string]any{}, subs: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dodo-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests[r.Method+" "+r.URL.Path] = body
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/checkouts":
			_, _ = w.Write([]byte(`{"session_id":"cks_1","checkout_url":"https://checkout.example/cks_1"}`))
		case strings.HasSuffix(r.URL.Path, "/customer-portal/session"):
			_, _ = w.Write([]byte(`{"link":"https://portal.example/` + parts[1] + `"}`))
		case strings.HasSuffix(r.URL.Path, "/change-plan"):
			if sub := f.subs[parts[1]]; sub != nil {
				sub["product_id"] = body["product_id"]
			}
			_, _ = w.Write([]byte(`{}`))
		case len(parts) == 2 && parts[0] == "subscriptions":
			sub := f.subs[parts[1]]
			if sub == nil {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"Subscription not found"}`))
				return
			}
			if r.Method == http.MethodPatch {
				if v, ok := body["cancel_at_next_billing_date"]; ok {
					sub["cancel_at_next_billing_date"] = v
				}
			}
			_ = json.NewEncoder(w).Encode(sub)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func (f *fakeDodo) request(key string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[key]
}

// setSub stores what Dodo answers for GET /subscriptions/{id}.
func (f *fakeDodo) setSub(id, status, product, orgID string, cancelAtNext bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	meta := map[string]any{}
	if orgID != "" {
		meta["organization_id"] = orgID
	}
	f.subs[id] = map[string]any{
		"subscription_id": id, "status": status, "product_id": product, "metadata": meta,
		"customer":                    map[string]any{"customer_id": "cus_" + id, "email": "ada@example.com"},
		"next_billing_date":           time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"cancel_at_next_billing_date": cancelAtNext,
	}
}

const webhookSecret = "whsec_" + "c2VjcmV0LWtleS1mb3ItYnJpZGdlLXRlc3Rz" // base64("secret-key-for-bridge-tests")

func signedWebhook(t *testing.T, srv *httptest.Server, id string, payload map[string]any, secret string) int {
	t.Helper()
	body, _ := json.Marshal(payload)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + string(body)))
	req, _ := http.NewRequest("POST", srv.URL+"/v1/billing/dodo/webhook", strings.NewReader(string(body)))
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", ts)
	req.Header.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// subscriptionEvent is a Dodo subscription webhook. Its snapshot may be stale:
// Bridge reads the subscription's current state back from Dodo.
func subscriptionEvent(typ, subID, status, product string) map[string]any {
	return map[string]any{"business_id": "bus_1", "type": typ, "timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]any{"payload_type": "Subscription", "subscription_id": subID, "status": status, "product_id": product}}
}

func TestDodoBilling(t *testing.T) {
	dodo, dodoURL := newFakeDodo(t)
	srv := newServer(t, func(c *config.Config) {
		c.Cloud = true
		c.Dodo = &config.DodoConfig{
			APIKey: "dodo-key", WebhookSecret: webhookSecret, Environment: config.DodoTestMode, BaseURL: dodoURL,
			Products: map[string]string{"pro": "pdt_pro", "business": "pdt_business"},
		}
	})
	c, orgID, _ := signup(t, srv)
	billingPath := "/v1/organizations/" + orgID + "/billing"
	sub := "sub_" + orgID
	plan := func() (string, string, bool) {
		b := c.mustStatus(c.do("GET", billingPath, nil), http.StatusOK).Body
		return b["plan"].(map[string]any)["id"].(string), b["status"].(string), b["cancel_at_period_end"].(bool)
	}
	hook := func(id, typ, status, product string) int {
		return signedWebhook(t, srv, id, subscriptionEvent(typ, sub, status, product), webhookSecret)
	}

	// Checkout starts a Dodo session tagged with the workspace.
	r := c.mustStatus(c.do("POST", billingPath+"/checkout", map[string]any{"plan": "pro"}), http.StatusOK)
	if r.Body["url"] != "https://checkout.example/cks_1" {
		t.Fatalf("checkout url: %v", r.Body)
	}
	sent := dodo.request("POST /checkouts")
	if meta := sent["metadata"].(map[string]any); meta["organization_id"] != orgID || meta["plan"] != "pro" {
		t.Fatalf("checkout metadata: %v", sent)
	}
	if !strings.HasSuffix(sent["return_url"].(string), "/billing?checkout=return") {
		t.Fatalf("return url: %v", sent["return_url"])
	}
	if r := c.do("POST", billingPath+"/portal", nil); r.Status != http.StatusNotFound {
		t.Fatalf("portal before paying: %d %s", r.Status, r.Raw)
	}

	// Bad signatures are refused.
	bad := "whsec_" + base64.StdEncoding.EncodeToString([]byte("wrong"))
	if code := signedWebhook(t, srv, "evt_bad", subscriptionEvent("subscription.active", sub, "active", "pdt_pro"), bad); code != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", code)
	}

	// Back from checkout: the sync reads the subscription before any webhook.
	dodo.setSub(sub, "active", "pdt_pro", orgID, false)
	b := c.mustStatus(c.do("POST", billingPath+"/sync", map[string]any{"subscription_id": sub}), http.StatusOK).Body
	if b["plan"].(map[string]any)["id"] != "pro" || b["status"] != "active" || b["manage_billing"] != true {
		t.Fatalf("after sync: %v", b)
	}
	// A subscription of another workspace cannot be synced into this one.
	dodo.setSub("sub_other", "active", "pdt_business", "org_06ghaaaaaaaaaaaaaaaaaaaaaa", false)
	if r := c.do("POST", billingPath+"/sync", map[string]any{"subscription_id": "sub_other"}); r.Status != http.StatusNotFound {
		t.Fatalf("foreign sync: %d %s", r.Status, r.Raw)
	}

	// Webhooks apply Dodo's current state, once per delivery.
	for range 2 {
		if code := hook("evt_1", "subscription.active", "active", "pdt_pro"); code != http.StatusOK {
			t.Fatalf("webhook: %d", code)
		}
	}
	c.mustStatus(c.do("POST", "/v1/organizations/"+orgID+"/projects", map[string]any{"name": "Second"}), http.StatusCreated)

	// Paying workspaces change plans in place; the page waits for Dodo to confirm.
	if r := c.do("POST", billingPath+"/checkout", map[string]any{"plan": "pro"}); r.Status != http.StatusConflict {
		t.Fatalf("same plan: %d %s", r.Status, r.Raw)
	}
	r = c.mustStatus(c.do("POST", billingPath+"/checkout", map[string]any{"plan": "business"}), http.StatusOK)
	if !strings.HasSuffix(r.Body["url"].(string), "/billing?change=requested") {
		t.Fatalf("plan change returns to billing: %v", r.Body)
	}
	if change := dodo.request("POST /subscriptions/" + sub + "/change-plan"); change["product_id"] != "pdt_business" {
		t.Fatalf("change-plan request: %v", change)
	}
	hook("evt_2", "subscription.plan_changed", "active", "pdt_business")
	if id, _, _ := plan(); id != "business" {
		t.Fatalf("after plan_changed: %s", id)
	}
	// A late snapshot from before the change does not undo it.
	hook("evt_3", "subscription.renewed", "active", "pdt_pro")
	if id, _, _ := plan(); id != "business" {
		t.Fatalf("stale event reverted the plan to %s", id)
	}

	// Scheduled cancellation: plan changes wait; Keep my plan withdraws it.
	dodo.setSub(sub, "active", "pdt_business", orgID, true)
	hook("evt_4", "subscription.updated", "active", "pdt_business")
	if _, _, cancel := plan(); !cancel {
		t.Fatal("cancel_at_period_end not stored")
	}
	if r := c.do("POST", billingPath+"/checkout", map[string]any{"plan": "pro"}); r.Status != http.StatusConflict || !strings.Contains(r.errMessage(), "Keep my plan") {
		t.Fatalf("change while cancelling: %d %s", r.Status, r.Raw)
	}
	b = c.mustStatus(c.do("POST", billingPath+"/resume", nil), http.StatusOK).Body
	if b["cancel_at_period_end"] != false {
		t.Fatalf("after resume: %v", b)
	}
	if patch := dodo.request("PATCH /subscriptions/" + sub); patch["cancel_at_next_billing_date"] != false {
		t.Fatalf("resume request: %v", patch)
	}

	// A failing renewal keeps access but blocks plan changes.
	dodo.setSub(sub, "on_hold", "pdt_business", orgID, false)
	hook("evt_5", "subscription.on_hold", "on_hold", "pdt_business")
	if id, st, _ := plan(); id != "business" || st != "past_due" {
		t.Fatalf("on hold: %s %s", id, st)
	}
	if r := c.do("POST", billingPath+"/checkout", map[string]any{"plan": "pro"}); r.Status != http.StatusConflict || !strings.Contains(r.errMessage(), "payment") {
		t.Fatalf("change while past due: %d %s", r.Status, r.Raw)
	}
	portal := c.mustStatus(c.do("POST", billingPath+"/portal", nil), http.StatusOK)
	if portal.Body["url"] != "https://portal.example/cus_"+sub {
		t.Fatalf("portal: %v", portal.Body)
	}

	// Cancelled: back to Free. A new checkout reuses the Dodo customer.
	dodo.setSub(sub, "cancelled", "pdt_business", orgID, false)
	hook("evt_6", "subscription.cancelled", "cancelled", "pdt_business")
	if id, st, _ := plan(); id != "free" || st != "cancelled" {
		t.Fatalf("after cancelling: %s %s", id, st)
	}
	c.mustStatus(c.do("POST", billingPath+"/checkout", map[string]any{"plan": "pro"}), http.StatusOK)
	if cust := dodo.request("POST /checkouts")["customer"].(map[string]any); cust["customer_id"] != "cus_"+sub {
		t.Fatalf("returning customer: %v", cust)
	}

	// A subscription for one of our products with no known workspace is retried, not dropped.
	dodo.setSub("sub_orphan", "active", "pdt_pro", "", false)
	if code := signedWebhook(t, srv, "evt_7", subscriptionEvent("subscription.active", "sub_orphan", "active", "pdt_pro"), webhookSecret); code != http.StatusInternalServerError {
		t.Fatalf("orphan subscription: %d", code)
	}
	// Other products on the same Dodo account are ignored.
	if code := signedWebhook(t, srv, "evt_8", subscriptionEvent("subscription.active", "sub_x", "active", "pdt_someone_else"), webhookSecret); code != http.StatusOK {
		t.Fatalf("foreign product: %d", code)
	}
}
