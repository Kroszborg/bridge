package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bridge/internal/config"
)

// invite creates an invite link and returns its token.
func invite(t *testing.T, c *client, orgID, role string) string {
	t.Helper()
	r := c.mustStatus(c.do("POST", "/v1/organizations/"+orgID+"/invites", map[string]any{"role": role, "email": "teammate@example.com"}), 201)
	token := r.Body["token"].(string)
	if !strings.HasPrefix(token, "bi_") || !strings.HasSuffix(r.Body["url"].(string), "/invite/"+token) {
		t.Fatalf("invite: %s", r.Raw)
	}
	return token
}

// joinWithInvite signs up a new user through an invite.
func joinWithInvite(t *testing.T, srv *httptest.Server, token string) *client {
	t.Helper()
	c := newClient(t, srv)
	c.mustStatus(c.do("POST", "/v1/auth/signup", map[string]any{
		"email": uniqueEmail(), "password": "correct horse battery", "name": "Grace Hopper", "invite_token": token,
	}), http.StatusCreated)
	return c
}

func memberID(t *testing.T, c *client, orgID, email string) string {
	t.Helper()
	r := c.mustStatus(c.do("GET", "/v1/organizations/"+orgID+"/members", nil), 200)
	var rows []map[string]any
	for _, v := range listOf(r) {
		rows = append(rows, v)
		if v["email"] == email || (email == "" && v["you"] == true) {
			return v["id"].(string)
		}
	}
	t.Fatalf("member %q not in %v", email, rows)
	return ""
}

func listOf(r response) []map[string]any {
	var out []map[string]any
	var raw []any
	_ = jsonUnmarshal(r.Raw, &raw)
	for _, v := range raw {
		out = append(out, v.(map[string]any))
	}
	return out
}

func TestInvitesAndRoles(t *testing.T) {
	srv := newServer(t)
	owner, orgID, projectID := signup(t, srv)

	// A member joins through an invite and gets no workspace of their own.
	memberToken := invite(t, owner, orgID, "member")
	preview := newClient(t, srv).mustStatus(newClient(t, srv).do("GET", "/v1/invites/"+memberToken, nil), 200).Body
	if preview["valid"] != true || preview["role"] != "member" || preview["invited_by"] != "Ada Lovelace" {
		t.Fatalf("preview: %v", preview)
	}
	member := joinWithInvite(t, srv, memberToken)
	me := member.mustStatus(member.do("GET", "/v1/me", nil), 200).Body
	if orgs := me["organizations"].([]any); len(orgs) != 1 || orgs[0].(map[string]any)["id"] != orgID || orgs[0].(map[string]any)["role"] != "member" {
		t.Fatalf("member orgs: %v", orgs)
	}
	// The link is single use.
	if r := newClient(t, srv).do("POST", "/v1/auth/signup", map[string]any{
		"email": uniqueEmail(), "password": "correct horse battery", "invite_token": memberToken,
	}); r.Status != http.StatusGone {
		t.Fatalf("reused invite: %d", r.Status)
	}
	if p := newClient(t, srv).mustStatus(newClient(t, srv).do("GET", "/v1/invites/"+memberToken, nil), 200).Body; p["valid"] != false || p["reason"] != "used" {
		t.Fatalf("used preview: %v", p)
	}

	// Members can read and send test messages, but not change anything.
	member.mustStatus(member.do("GET", "/v1/projects/"+projectID+"/messages", nil), 200)
	member.mustStatus(member.do("POST", "/v1/projects/"+projectID+"/messages", map[string]any{"to": "+15550000001", "message": "hi"}), 202)
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/projects/" + projectID + "/api-keys", map[string]any{"name": "x", "environment": "test"}},
		{"POST", "/v1/projects/" + projectID + "/webhooks", map[string]any{"url": "https://example.com/h"}},
		{"PATCH", "/v1/projects/" + projectID, map[string]any{"name": "x"}},
		{"POST", "/v1/projects/" + projectID + "/messages?environment=live", map[string]any{"to": "+15550000001", "message": "x"}},
		{"POST", "/v1/organizations/" + orgID + "/invites", map[string]any{"role": "member"}},
		{"GET", "/v1/organizations/" + orgID + "/audit-logs", nil},
		{"PATCH", "/v1/organizations/" + orgID, map[string]any{"name": "x"}},
	} {
		r := member.do(req.method, req.path, req.body)
		if r.Status != 403 || r.errCode() != "forbidden" {
			t.Errorf("member %s %s: %d %s", req.method, req.path, r.Status, r.Raw)
		}
	}

	// An admin can manage keys and invite members, but not owners.
	admin := joinWithInvite(t, srv, invite(t, owner, orgID, "admin"))
	admin.mustStatus(admin.do("POST", "/v1/projects/"+projectID+"/api-keys", map[string]any{"name": "ci", "environment": "test"}), 201)
	admin.mustStatus(admin.do("POST", "/v1/organizations/"+orgID+"/invites", map[string]any{"role": "member"}), 201)
	if r := admin.do("POST", "/v1/organizations/"+orgID+"/invites", map[string]any{"role": "owner"}); r.Status != 403 {
		t.Fatalf("admin inviting an owner: %d", r.Status)
	}
	ownerMember := memberID(t, owner, orgID, "")
	if r := admin.do("PATCH", "/v1/organizations/"+orgID+"/members/"+ownerMember, map[string]any{"role": "member"}); r.Status != 403 {
		t.Fatalf("admin demoting the owner: %d", r.Status)
	}
	if r := admin.do("DELETE", "/v1/projects/"+projectID, map[string]any{"confirm": "Default"}); r.Status != 403 {
		t.Fatalf("admin deleting a project: %d", r.Status)
	}

	// The last owner cannot step down or leave.
	if r := owner.do("PATCH", "/v1/organizations/"+orgID+"/members/"+ownerMember, map[string]any{"role": "admin"}); r.Status != 409 {
		t.Fatalf("last owner demoting themselves: %d %s", r.Status, r.Raw)
	}
	if r := owner.do("DELETE", "/v1/organizations/"+orgID+"/members/"+ownerMember, nil); r.Status != 409 {
		t.Fatalf("last owner leaving: %d", r.Status)
	}

	// Promote the admin to owner; then the first owner may step down.
	adminMember := memberID(t, admin, orgID, "")
	owner.mustStatus(owner.do("PATCH", "/v1/organizations/"+orgID+"/members/"+adminMember, map[string]any{"role": "owner"}), 200)
	owner.mustStatus(owner.do("PATCH", "/v1/organizations/"+orgID+"/members/"+ownerMember, map[string]any{"role": "admin"}), 200)

	// A member can leave on their own.
	memberMember := memberID(t, member, orgID, "")
	member.mustStatus(member.do("DELETE", "/v1/organizations/"+orgID+"/members/"+memberMember, nil), 204)
	if r := member.do("GET", "/v1/projects/"+projectID, nil); r.Status != 404 {
		t.Fatalf("after leaving: %d", r.Status)
	}

	// Revoked invites stop working.
	revoked := owner.mustStatus(owner.do("POST", "/v1/organizations/"+orgID+"/invites", map[string]any{"role": "member"}), 201).Body
	owner.mustStatus(owner.do("DELETE", "/v1/organizations/"+orgID+"/invites/"+revoked["id"].(string), nil), 204)
	if p := newClient(t, srv).mustStatus(newClient(t, srv).do("GET", "/v1/invites/"+revoked["token"].(string), nil), 200).Body; p["reason"] != "revoked" {
		t.Fatalf("revoked preview: %v", p)
	}

	// Everything above is in the audit log, readable by owners and admins.
	audit := admin.mustStatus(admin.do("GET", "/v1/organizations/"+orgID+"/audit-logs?limit=100", nil), 200)
	actions := map[string]bool{}
	for _, e := range audit.Body["data"].([]any) {
		actions[e.(map[string]any)["action"].(string)] = true
	}
	for _, want := range []string{"invite.created", "invite.accepted", "invite.revoked", "member.role_changed", "member.left", "api_key.created"} {
		if !actions[want] {
			t.Errorf("audit log has no %s: %v", want, actions)
		}
	}
	filtered := admin.mustStatus(admin.do("GET", "/v1/organizations/"+orgID+"/audit-logs?action=invite.", nil), 200)
	for _, e := range filtered.Body["data"].([]any) {
		if !strings.HasPrefix(e.(map[string]any)["action"].(string), "invite.") {
			t.Fatalf("action filter returned %v", e)
		}
	}
	first := filtered.Body["data"].([]any)[0].(map[string]any)
	if first["actor"].(map[string]any)["name"] == "" {
		t.Fatalf("audit actor has no name: %v", first)
	}

	// Other organizations see nothing.
	stranger, _, _ := signup(t, srv)
	if r := stranger.do("GET", "/v1/organizations/"+orgID+"/members", nil); r.Status != 404 {
		t.Fatalf("stranger reading members: %d", r.Status)
	}
	if r := stranger.do("GET", "/v1/organizations/"+orgID+"/audit-logs", nil); r.Status != 404 {
		t.Fatalf("stranger reading audit: %d", r.Status)
	}
}

func TestInviteWorksWhenSignupsAreDisabled(t *testing.T) {
	open := newServer(t)
	owner, orgID, _ := signup(t, open)
	token := invite(t, owner, orgID, "member")
	closed := newServer(t, func(c *config.Config) { c.AllowSignup = false })
	if r := newClient(t, closed).do("POST", "/v1/auth/signup", map[string]any{"email": uniqueEmail(), "password": "correct horse battery"}); r.Status != 403 {
		t.Fatalf("signup without invite: %d", r.Status)
	}
	joinWithInvite(t, closed, token)
}

func TestDeleteProject(t *testing.T) {
	srv := newServer(t)
	owner, orgID, projectID := signup(t, srv)
	phone := connectPhone(t, srv, owner, projectID)
	if r := owner.do("DELETE", "/v1/projects/"+projectID, map[string]any{"confirm": "wrong"}); r.Status != 422 {
		t.Fatalf("wrong confirmation: %d", r.Status)
	}
	owner.mustStatus(owner.do("DELETE", "/v1/projects/"+projectID, map[string]any{"confirm": "Default"}), 204)
	owner.mustStatus(owner.do("GET", "/v1/projects/"+projectID, nil), 404)
	phone.expect("unpaired")
	projects := owner.mustStatus(owner.do("GET", "/v1/organizations/"+orgID+"/projects", nil), 200).Body["data"].([]any)
	if len(projects) != 0 {
		t.Fatalf("projects left: %v", projects)
	}
}

func TestOrganizationRename(t *testing.T) {
	srv := newServer(t)
	owner, orgID, _ := signup(t, srv)
	r := owner.mustStatus(owner.do("PATCH", "/v1/organizations/"+orgID, map[string]any{"name": "Acme SMS"}), 200)
	if r.Body["name"] != "Acme SMS" || r.Body["role"] != "owner" {
		t.Fatalf("rename: %s", r.Raw)
	}
}
