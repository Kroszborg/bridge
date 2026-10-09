package httpapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"bridge/internal/config"
)

func TestSystemInsights(t *testing.T) {
	opEmail := uniqueEmail()
	srv := newServer(t, func(c *config.Config) { c.OperatorEmails = []string{opEmail}; c.Cloud = true })

	op := newClient(t, srv)
	op.mustStatus(op.do("POST", "/v1/auth/signup", map[string]any{
		"email": opEmail, "password": "correct horse battery", "name": "Operator",
	}), 201)

	// A customer: two test messages (one delivered, one failed) and a live
	// message that fails because no phone is paired.
	cust, orgID, projectID := signup(t, srv)
	custEmail := cust.mustStatus(cust.do("GET", "/v1/me", nil), 200).Body["user"].(map[string]any)["email"].(string)
	test := apiKeyClient(t, srv, cust, projectID, "test")
	ok := test.mustStatus(test.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202)
	bad := test.mustStatus(test.do("POST", "/v1/messages", map[string]any{"to": "+15550000002", "message": "x"}), 202)
	waitStatus(t, test, ok.Body["id"].(string), "delivered")
	waitStatus(t, test, bad.Body["id"].(string), "failed")
	live := apiKeyClient(t, srv, cust, projectID, "live")
	l := live.mustStatus(live.do("POST", "/v1/messages", map[string]any{"to": "+919876543210", "message": "hi"}), 202)
	waitStatus(t, live, l.Body["id"].(string), "failed")
	if _, err := testDB.Pool.Exec(context.Background(),
		`INSERT INTO subscriptions (organization_id, plan_id, status) VALUES ($1, 'pro', 'active')`, orgID); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	r := op.mustStatus(op.do("GET", "/v1/system/insights?days=7", nil), 200)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("insights took %s", d)
	}
	b := r.Body
	num := func(m map[string]any, k string) int { return int(m[k].(float64)) }
	obj := func(m map[string]any, k string) map[string]any { return m[k].(map[string]any) }
	list := func(m map[string]any, k string) []map[string]any {
		var out []map[string]any
		for _, v := range m[k].([]any) {
			out = append(out, v.(map[string]any))
		}
		return out
	}

	if rg := obj(b, "range"); num(rg, "days") != 7 || !strings.HasSuffix(rg["from"].(string), "T00:00:00Z") {
		t.Fatalf("range: %v", rg)
	}
	totals := obj(b, "totals")
	if num(totals, "users") < 2 || num(totals, "users_in_range") < 2 || num(totals, "organizations") < 2 ||
		num(totals, "projects") < 2 || num(totals, "api_keys") < 2 || num(totals, "subscriptions_active") < 1 ||
		num(totals, "users_in_range") > num(totals, "users") {
		t.Fatalf("totals: %v", totals)
	}
	if ph := obj(totals, "phones"); num(ph, "online") > num(ph, "total") {
		t.Fatalf("phones: %v", ph)
	}

	signups := list(b, "signups")
	today := time.Now().UTC().Format(time.DateOnly)
	if len(signups) != 7 || signups[6]["date"] != today || num(signups[6], "users") < 2 {
		t.Fatalf("signups: %v", signups)
	}
	if a := obj(b, "activity"); num(a, "active_organizations_7d") < 1 ||
		num(a, "active_organizations_30d") < num(a, "active_organizations_7d") || num(a, "active_users_7d") < 2 {
		t.Fatalf("activity: %v", a)
	}

	msgs := obj(b, "messages")
	mt := obj(msgs, "totals")
	if num(mt, "test") < 2 || num(mt, "live") < 1 || num(mt, "failed") < 1 {
		t.Fatalf("message totals: %v", mt)
	}
	daily := list(msgs, "daily")
	if len(daily) != 7 || daily[6]["date"] != today || num(daily[6], "test") < 2 || num(daily[6], "failed") < 1 {
		t.Fatalf("daily: %v", daily)
	}
	sumLive := 0
	for _, d := range daily {
		sumLive += num(d, "live")
	}
	if sumLive != num(mt, "live") {
		t.Fatalf("daily live %d != total %d", sumLive, num(mt, "live"))
	}
	if rate, _ := msgs["delivery_rate"].(float64); msgs["delivery_rate"] == nil || rate < 0 || rate > 1 {
		t.Fatalf("delivery rate: %v", msgs["delivery_rate"])
	}
	has := func(rows []map[string]any, key, want string) bool {
		for _, row := range rows {
			if row[key] == want {
				return true
			}
		}
		return false
	}
	if p := list(msgs, "by_provider"); !has(p, "provider", "simulator") {
		t.Fatalf("providers: %v", p)
	}
	if e := list(msgs, "top_errors"); !has(e, "error_code", "no_device") || len(e) > 8 {
		t.Fatalf("top errors: %v", e)
	}
	if v := obj(b, "verify"); num(v, "verified") > num(v, "started") {
		t.Fatalf("verify: %v", v)
	}

	// Earlier tests in the package may have busier workspaces, so the customer
	// is listed unless ten others sent more.
	top := list(b, "top_organizations")
	found := false
	for _, o := range top {
		if o["organization_id"] == orgID {
			found = true
			if num(o, "messages") < 1 || o["plan"] != "Pro" {
				t.Fatalf("top organization: %v", o)
			}
		}
	}
	if !found && (len(top) < 10 || num(top[9], "messages") < 1) {
		t.Fatalf("customer missing from top organizations: %v", top)
	}

	recent := list(b, "recent_signups")
	if len(recent) > 15 || recent[0]["email"] != custEmail || recent[0]["email_verified"] != false || num(recent[0], "organizations") != 1 {
		t.Fatalf("recent sign-ups: %v", recent)
	}
	if recent[1]["email"] != opEmail {
		t.Fatalf("operator not second newest: %v", recent[1])
	}
	if strings.Contains(string(r.Raw), "+919876543210") || strings.Contains(string(r.Raw), `"hi"`) {
		t.Fatal("insights expose phone numbers or message text")
	}

	bill := obj(b, "billing")
	if num(bill, "mrr_cents") < 500 || bill["currency"] != "USD" {
		t.Fatalf("billing: %v", bill)
	}
	if mix := list(bill, "plan_mix"); !has(mix, "plan", "free") || !has(mix, "plan", "pro") {
		t.Fatalf("plan mix: %v", mix)
	}
	if ch := list(bill, "recent_changes"); len(ch) == 0 || len(ch) > 10 || !has(ch, "organization_id", orgID) {
		t.Fatalf("recent changes: %v", ch)
	}

	// The default range is 30 days; 7 to 180 are allowed.
	def := op.mustStatus(op.do("GET", "/v1/system/insights", nil), 200).Body
	if num(obj(def, "range"), "days") != 30 || len(def["signups"].([]any)) != 30 || len(obj(def, "messages")["daily"].([]any)) != 30 {
		t.Fatalf("default range: %v", def["range"])
	}
	op.mustStatus(op.do("GET", "/v1/system/insights?days=180", nil), 200)
	for _, q := range []string{"6", "181", "0", "-1"} {
		if r := op.do("GET", "/v1/system/insights?days="+q, nil); r.Status != 422 || !strings.Contains(string(r.Raw), "query.days") {
			t.Errorf("days=%s: %d %s", q, r.Status, r.Raw)
		}
	}

	// Anyone else, even with an invalid range, gets the answer for a route that
	// does not exist; API keys and signed-out callers are not signed in.
	unknown := cust.do("GET", "/v1/system/nope", nil)
	for _, path := range []string{"/v1/system/insights", "/v1/system/insights?days=1000"} {
		r := cust.do("GET", path, nil)
		if r.Status != 404 || r.errCode() != "not_found" ||
			r.errMessage() != "No route matches GET /v1/system/insights. See /docs for the API reference." ||
			r.Header.Get("Content-Type") != unknown.Header.Get("Content-Type") {
			t.Fatalf("non-operator %s: %d %s (%s)", path, r.Status, r.Raw, r.Header.Get("Content-Type"))
		}
	}
	if r := live.do("GET", "/v1/system/insights", nil); r.Status != 401 {
		t.Fatalf("API key: %d %s", r.Status, r.Raw)
	}
	if r := newClient(t, srv).do("GET", "/v1/system/insights", nil); r.Status != 401 {
		t.Fatalf("signed out: %d %s", r.Status, r.Raw)
	}

	// Without billing (self-hosted) there are no plans.
	self := newServer(t, func(c *config.Config) { c.OperatorEmails = []string{opEmail} })
	selfOp := login(t, self, opEmail, "correct horse battery")
	sb := selfOp.mustStatus(selfOp.do("GET", "/v1/system/insights", nil), 200)
	if sb.Body["billing"] != nil || !strings.Contains(string(sb.Raw), `"billing":null`) {
		t.Fatalf("self-hosted billing: %v", sb.Body["billing"])
	}
	for _, o := range list(sb.Body, "top_organizations") {
		if o["plan"] != nil {
			t.Fatalf("self-hosted plan: %v", o)
		}
	}
}
