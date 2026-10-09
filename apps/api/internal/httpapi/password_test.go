package httpapi_test

import (
	"net/http"
	"regexp"
	"testing"
)

var resetLink = regexp.MustCompile(`/reset-password\?token=(br_[A-Za-z0-9]+)`)

func TestPasswordReset(t *testing.T) {
	srv := newServer(t)
	c := newClient(t, srv)
	email := uniqueEmail()
	c.mustStatus(c.do("POST", "/v1/auth/signup", map[string]any{"email": email, "password": "correct horse battery"}), http.StatusCreated)

	cfg := c.mustStatus(c.do("GET", "/v1/auth/config", nil), http.StatusOK).Body
	if cfg["signup_open"] != true || cfg["password_reset"] != true || cfg["hosted"] != false {
		t.Fatalf("auth config: %v", cfg)
	}

	// Unknown addresses get the same answer and no email.
	anon := newClient(t, srv)
	anon.mustStatus(anon.do("POST", "/v1/auth/password-reset", map[string]any{"email": uniqueEmail()}), http.StatusAccepted)
	anon.mustStatus(anon.do("POST", "/v1/auth/password-reset", map[string]any{"email": email}), http.StatusAccepted)
	m := sentMail.waitFor(t, email)
	match := resetLink.FindStringSubmatch(m.Text)
	if match == nil || m.Subject != "Reset your Bridge password" {
		t.Fatalf("reset email: %+v", m)
	}
	token := match[1]

	if r := anon.do("POST", "/v1/auth/password-reset/confirm", map[string]any{"token": token, "password": "short"}); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("short password: %d %s", r.Status, r.Raw)
	}
	r := anon.mustStatus(anon.do("POST", "/v1/auth/password-reset/confirm", map[string]any{"token": token, "password": "a brand new passphrase"}), http.StatusOK)
	if r.Body["user"].(map[string]any)["email"] != email {
		t.Fatalf("signed in as: %v", r.Body)
	}
	anon.mustStatus(anon.do("GET", "/v1/me", nil), http.StatusOK)

	// The old session is gone, the link works once, and the new password signs in.
	if r := c.do("GET", "/v1/me", nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("old session after reset: %d", r.Status)
	}
	if r := anon.do("POST", "/v1/auth/password-reset/confirm", map[string]any{"token": token, "password": "another passphrase!"}); r.Status != http.StatusGone || r.errCode() != "reset_link_unavailable" {
		t.Fatalf("reused link: %d %s", r.Status, r.Raw)
	}
	login := newClient(t, srv)
	if r := login.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "correct horse battery"}); r.Status != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", r.Status)
	}
	login.mustStatus(login.do("POST", "/v1/auth/login", map[string]any{"email": email, "password": "a brand new passphrase"}), http.StatusOK)
}
