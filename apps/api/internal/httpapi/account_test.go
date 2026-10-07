package httpapi_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"bridge/internal/config"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// login signs in an existing user on a fresh client (another browser).
func login(t *testing.T, srv *httptest.Server, email, password string) *client {
	t.Helper()
	c := newClient(t, srv)
	c.mustStatus(c.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": password}), 200)
	return c
}

func TestAccountSecurity(t *testing.T) {
	srv := newServer(t)
	c, _, _ := signup(t, srv)
	email := c.mustStatus(c.do("GET", "/v1/me", nil), 200).Body["user"].(map[string]any)["email"].(string)

	laptop := login(t, srv, email, "correct horse battery")
	sessions := listOf(c.mustStatus(c.do("GET", "/v1/me/sessions", nil), 200))
	if len(sessions) != 2 {
		t.Fatalf("sessions: %v", sessions)
	}
	current := 0
	for _, s := range sessions {
		if s["current"] == true {
			current++
		}
		if s["user_agent"] == "" {
			t.Errorf("session without user agent: %v", s)
		}
	}
	if current != 1 {
		t.Fatalf("current sessions: %d", current)
	}

	// Profile.
	r := c.mustStatus(c.do("PATCH", "/v1/me", map[string]any{"name": "Ada King"}), 200)
	if r.Body["user"].(map[string]any)["name"] != "Ada King" {
		t.Fatalf("rename: %s", r.Raw)
	}

	// Wrong current password is rejected; a good change signs out the other browser.
	if r := c.do("POST", "/v1/me/password", map[string]any{"current_password": "nope nope nope", "new_password": "a brand new password"}); r.Status != 422 {
		t.Fatalf("wrong current password: %d", r.Status)
	}
	c.mustStatus(c.do("POST", "/v1/me/password", map[string]any{"current_password": "correct horse battery", "new_password": "a brand new password"}), 204)
	if r := laptop.do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("other session after password change: %d", r.Status)
	}
	c.mustStatus(c.do("GET", "/v1/me", nil), 200)
	if r := newClient(t, srv).do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "correct horse battery"}); r.Status != 401 {
		t.Fatalf("old password still works: %d", r.Status)
	}

	// Sign out one session, then everywhere else.
	phone := login(t, srv, email, "a brand new password")
	tablet := login(t, srv, email, "a brand new password")
	var phoneID string
	for _, s := range listOf(c.mustStatus(c.do("GET", "/v1/me/sessions", nil), 200)) {
		if s["current"] != true {
			phoneID = s["id"].(string)
			break
		}
	}
	c.mustStatus(c.do("DELETE", "/v1/me/sessions/"+phoneID, nil), 204)
	n := c.mustStatus(c.do("POST", "/v1/me/sessions/revoke-others", nil), 200).Body["revoked"]
	if n != float64(1) {
		t.Fatalf("revoked %v", n)
	}
	for _, other := range []*client{phone, tablet} {
		if r := other.do("GET", "/v1/me", nil); r.Status != 401 {
			t.Fatalf("revoked session still works: %d", r.Status)
		}
	}
}

func TestDeleteAccount(t *testing.T) {
	srv := newServer(t)
	owner, orgID, _ := signup(t, srv)
	teammate := joinWithInvite(t, srv, invite(t, owner, orgID, "member"))

	// The only owner of a shared organization must hand it over first.
	if r := owner.do("DELETE", "/v1/me", map[string]any{"password": "correct horse battery", "confirm": "DELETE"}); r.Status != 409 || !strings.Contains(r.errMessage(), "only owner") {
		t.Fatalf("sole owner deleting: %d %s", r.Status, r.Raw)
	}
	if r := teammate.do("DELETE", "/v1/me", map[string]any{"password": "wrong password!", "confirm": "DELETE"}); r.Status != 422 {
		t.Fatalf("wrong password: %d", r.Status)
	}
	if r := teammate.do("DELETE", "/v1/me", map[string]any{"password": "correct horse battery", "confirm": "yes"}); r.Status != 422 {
		t.Fatalf("missing confirmation: %d", r.Status)
	}
	// The teammate (a plain member) can delete their account; the shared organization stays.
	teammate.mustStatus(teammate.do("DELETE", "/v1/me", map[string]any{"password": "correct horse battery", "confirm": "DELETE"}), 204)
	if r := teammate.do("GET", "/v1/me", nil); r.Status != 401 {
		t.Fatalf("deleted user still signed in: %d", r.Status)
	}
	members := listOf(owner.mustStatus(owner.do("GET", "/v1/organizations/"+orgID+"/members", nil), 200))
	if len(members) != 1 {
		t.Fatalf("members after teammate deleted: %d", len(members))
	}
	// Now alone, the owner can delete the account and its organization.
	owner.mustStatus(owner.do("DELETE", "/v1/me", map[string]any{"password": "correct horse battery", "confirm": "DELETE"}), 204)
}

func TestStatusEndpoints(t *testing.T) {
	srv := newServer(t)
	first, _, _ := signup(t, srv) // the first account operates the instance unless BRIDGE_OPERATOR_EMAILS is set
	other, _, _ := signup(t, srv)

	var page response
	waitFor(t, "status with live components", func() bool {
		page = newClient(t, srv).mustStatus(newClient(t, srv).do("GET", "/v1/status", nil), 200)
		return page.Body["status"] != nil
	})
	if !strings.Contains(page.Header.Get("Cache-Control"), "max-age") {
		t.Fatalf("cache header: %q", page.Header.Get("Cache-Control"))
	}
	comps := page.Body["components"].([]any)
	if len(comps) != 5 {
		t.Fatalf("components: %v", comps)
	}
	for _, c := range comps {
		m := c.(map[string]any)
		if len(m["days"].([]any)) != 90 {
			t.Fatalf("%s has %d days", m["id"], len(m["days"].([]any)))
		}
		if m["id"] == "database" && m["status"] != "operational" {
			t.Fatalf("database: %v", m)
		}
	}
	if strings.Contains(string(page.Raw), "@example.com") {
		t.Fatal("the public status page exposes account data")
	}

	// The operator (pinned here; by default the first account) sees System health; others do not.
	firstEmail := first.mustStatus(first.do("GET", "/v1/me", nil), 200).Body["user"].(map[string]any)["email"].(string)
	ops := newServer(t, func(c *config.Config) { c.OperatorEmails = []string{firstEmail} })
	opClient := login(t, ops, firstEmail, "correct horse battery")
	if me := opClient.mustStatus(opClient.do("GET", "/v1/me", nil), 200).Body["user"].(map[string]any); me["operator"] != true {
		t.Fatalf("operator flag: %v", me)
	}
	sys := opClient.mustStatus(opClient.do("GET", "/v1/system", nil), 200).Body
	if len(sys["instances"].([]any)) == 0 || sys["database"].(map[string]any)["version"] == "" {
		t.Fatalf("system health: %v", sys)
	}
	otherEmail := other.mustStatus(other.do("GET", "/v1/me", nil), 200).Body["user"].(map[string]any)["email"].(string)
	notOp := login(t, ops, otherEmail, "correct horse battery")
	if r := notOp.do("GET", "/v1/system", nil); r.Status != 403 {
		t.Fatalf("non-operator system health: %d", r.Status)
	}
}
