package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"bridge/internal/auth"
	"bridge/internal/config"
	"bridge/internal/db/dbq"
	"bridge/internal/gateway"
	"bridge/internal/id"
	"bridge/internal/mail"
)

var emailCode = regexp.MustCompile(`verification code is (\d{6})`)

// count returns how many emails went to the address.
func (c *captureMail) count(to string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.sent {
		if m.To == to {
			n++
		}
	}
	return n
}

// nextCode waits for email number n to the address and returns its code.
func nextCode(t *testing.T, to string, n int) string {
	t.Helper()
	for range 200 {
		if sentMail.count(to) >= n {
			m := sentMail.waitFor(t, to)
			match := emailCode.FindStringSubmatch(m.Subject)
			if match == nil || !strings.Contains(m.Text, match[1]) || !strings.Contains(m.Text, "15 minutes") {
				t.Fatalf("verification email: %+v", m)
			}
			return match[1]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("email %d to %s never arrived (have %d)", n, to, sentMail.count(to))
	return ""
}

// backdateEmailCodes moves a user's codes into the past, past the resend cooldown.
func backdateEmailCodes(t *testing.T, email string) {
	t.Helper()
	if _, err := testDB.Pool.Exec(context.Background(),
		`UPDATE email_verifications SET created_at = created_at - interval '2 minutes'
		 WHERE user_id = (SELECT id FROM users WHERE lower(email) = lower($1))`, email); err != nil {
		t.Fatal(err)
	}
}

func signupAs(t *testing.T, srv *httptest.Server, email string) *client {
	t.Helper()
	c := newClient(t, srv)
	c.mustStatus(c.do("POST", "/v1/auth/signup", map[string]any{"email": email, "password": "correct horse battery"}), http.StatusCreated)
	return c
}

func meUser(c *client) map[string]any {
	return c.mustStatus(c.do("GET", "/v1/me", nil), http.StatusOK).Body["user"].(map[string]any)
}

func otherCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func TestEmailVerification(t *testing.T) {
	srv := newServer(t)
	email := uniqueEmail()
	c := signupAs(t, srv, email)

	cfg := c.mustStatus(c.do("GET", "/v1/auth/config", nil), http.StatusOK).Body
	if cfg["email_verification"] != true || cfg["phone_verification"] != false {
		t.Fatalf("auth config: %v", cfg)
	}
	if u := meUser(c); u["email_verified"] != false || u["phone"] != nil || u["phone_verified"] != false {
		t.Fatalf("new user: %v", u)
	}

	// Sign-up emails the first code; asking again right away is too soon.
	first := nextCode(t, email, 1)
	if r := c.do("POST", "/v1/me/email/verification", nil); r.Status != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" {
		t.Fatalf("resend within the cooldown: %d %s", r.Status, r.Raw)
	}

	// A wrong code uses an attempt; a malformed one does not.
	r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": otherCode(first)})
	if r.Status != http.StatusBadRequest || r.errCode() != "invalid_code" || !strings.Contains(r.errMessage(), "4 attempts left") {
		t.Fatalf("wrong code: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": "12ab"}); r.Status != http.StatusBadRequest || r.errCode() != "invalid_code" {
		t.Fatalf("malformed code: %d %s", r.Status, r.Raw)
	}

	// A new code replaces the first.
	backdateEmailCodes(t, email)
	sent := c.mustStatus(c.do("POST", "/v1/me/email/verification", nil), http.StatusAccepted).Body
	if sent["email"] != email || sent["expires_at"] == nil || sent["resend_available_at"] == nil {
		t.Fatalf("send: %v", sent)
	}
	second := nextCode(t, email, 2)
	if first != second {
		if r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": first}); r.errCode() != "invalid_code" {
			t.Fatalf("replaced code: %d %s", r.Status, r.Raw)
		}
	}

	// Five wrong codes and the code is dead, even for the right digits.
	wrong := otherCode(second)
	for i := range 5 {
		if first != second && i == 0 {
			continue // the replaced code above used the first attempt
		}
		r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": wrong})
		if r.Status != http.StatusBadRequest || r.errCode() != "invalid_code" {
			t.Fatalf("wrong code %d: %d %s", i+1, r.Status, r.Raw)
		}
		if i == 4 && !strings.Contains(r.errMessage(), "last attempt") {
			t.Fatalf("last attempt: %s", r.Raw)
		}
	}
	if r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": second}); r.Status != http.StatusBadRequest || r.errCode() != "code_expired" {
		t.Fatalf("code after 5 wrong attempts: %d %s", r.Status, r.Raw)
	}

	// An expired code does not work.
	backdateEmailCodes(t, email)
	c.mustStatus(c.do("POST", "/v1/me/email/verification", nil), http.StatusAccepted)
	third := nextCode(t, email, 3)
	if _, err := testDB.Pool.Exec(context.Background(),
		`UPDATE email_verifications SET expires_at = now() - interval '1 second' WHERE consumed_at IS NULL AND lower(email) = lower($1)`, email); err != nil {
		t.Fatal(err)
	}
	if r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": third}); r.Status != http.StatusBadRequest || r.errCode() != "code_expired" {
		t.Fatalf("expired code: %d %s", r.Status, r.Raw)
	}

	// The right code verifies the address.
	backdateEmailCodes(t, email)
	c.mustStatus(c.do("POST", "/v1/me/email/verification", nil), http.StatusAccepted)
	fourth := nextCode(t, email, 4)
	me := c.mustStatus(c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": " " + fourth + " "}), http.StatusOK).Body
	if me["user"].(map[string]any)["email_verified"] != true {
		t.Fatalf("confirm: %v", me)
	}
	if u := meUser(c); u["email_verified"] != true {
		t.Fatalf("after confirm: %v", u)
	}
	if r := c.do("POST", "/v1/me/email/verification", nil); r.Status != http.StatusConflict || r.errCode() != "already_verified" {
		t.Fatalf("send when verified: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": fourth}); r.Status != http.StatusConflict || r.errCode() != "already_verified" {
		t.Fatalf("confirm when verified: %d %s", r.Status, r.Raw)
	}

	// Codes are stored only as hashes.
	var stored int
	if err := testDB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM email_verifications WHERE lower(email) = lower($1) AND position($2::bytea in code_hash) > 0`, email, []byte(fourth)).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("plain code stored: %d %v", stored, err)
	}
}

func TestEmailVerificationHourlyLimit(t *testing.T) {
	srv := newServer(t)
	email := uniqueEmail()
	c := signupAs(t, srv, email)
	nextCode(t, email, 1)
	for n := 2; n <= 5; n++ {
		backdateEmailCodes(t, email)
		c.mustStatus(c.do("POST", "/v1/me/email/verification", nil), http.StatusAccepted)
		nextCode(t, email, n)
	}
	backdateEmailCodes(t, email)
	if r := c.do("POST", "/v1/me/email/verification", nil); r.Status != http.StatusTooManyRequests || r.errCode() != "rate_limited" {
		t.Fatalf("sixth code in an hour: %d %s", r.Status, r.Raw)
	}
}

func TestEmailVerificationWithoutMail(t *testing.T) {
	withoutMail = true
	t.Cleanup(func() { withoutMail = false })
	srv := newServer(t)
	email := uniqueEmail()
	c := signupAs(t, srv, email) // sign-up works without mail
	cfg := c.mustStatus(c.do("GET", "/v1/auth/config", nil), http.StatusOK).Body
	if cfg["email_verification"] != false || cfg["password_reset"] != false || cfg["phone_verification"] != false {
		t.Fatalf("auth config: %v", cfg)
	}
	if r := c.do("POST", "/v1/me/email/verification", nil); r.Status != http.StatusServiceUnavailable || r.errCode() != "email_verification_unavailable" {
		t.Fatalf("send without mail: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/email/change", map[string]any{"email": uniqueEmail(), "password": "correct horse battery"}); r.Status != http.StatusServiceUnavailable || r.errCode() != "email_verification_unavailable" {
		t.Fatalf("email change without mail: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/phone/verification", map[string]any{"phone": uniqueNumber()}); r.Status != http.StatusServiceUnavailable || r.errCode() != "phone_verification_unavailable" {
		t.Fatalf("phone send without a key: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": uniqueNumber(), "code": "123456"}); r.Status != http.StatusServiceUnavailable || r.errCode() != "phone_verification_unavailable" {
		t.Fatalf("phone confirm without a key: %d %s", r.Status, r.Raw)
	}
	if n := sentMail.count(email); n != 0 {
		t.Fatalf("%d emails sent without mail", n)
	}
}

// accountVerifyServer starts a server that verifies phone numbers through the
// Verify of the operator's own project, with a key of the environment. The
// key is resolved when used, so it is stored after the operator signs up.
func accountVerifyServer(t *testing.T, env string) (srv *httptest.Server, operator *client, projectID, secret string) {
	t.Helper()
	secret, prefix := auth.NewAPIKey(auth.Environment(env))
	srv = newServer(t, func(c *config.Config) { c.AccountVerifyAPIKey = secret })

	// Until the key exists, phone verification is unavailable (and logged).
	operator, _, projectID = signup(t, srv)
	if r := operator.do("POST", "/v1/me/phone/verification", map[string]any{"phone": uniqueNumber()}); r.Status != http.StatusServiceUnavailable || r.errCode() != "phone_verification_unavailable" {
		t.Fatalf("send before the key exists: %d %s", r.Status, r.Raw)
	}
	if _, err := dbq.New(testDB.Pool).CreateAPIKey(context.Background(), dbq.CreateAPIKeyParams{
		ID: id.New(id.APIKey), ProjectID: projectID, Name: "Account verification", Environment: dbq.APIEnvironment(env),
		KeyPrefix: prefix, KeyHash: auth.HashToken(secret),
	}); err != nil {
		t.Fatal(err)
	}
	return srv, operator, projectID, secret
}

func TestPhoneVerificationTestKey(t *testing.T) {
	srv, operator, projectID, secret := accountVerifyServer(t, "test")
	a := signupAs(t, srv, uniqueEmail())
	b := signupAs(t, srv, uniqueEmail())
	cfg := a.mustStatus(a.do("GET", "/v1/auth/config", nil), http.StatusOK).Body
	if cfg["phone_verification"] != true {
		t.Fatalf("auth config: %v", cfg)
	}

	if r := a.do("POST", "/v1/me/phone/verification", map[string]any{"phone": "98765 43210"}); r.Status != http.StatusUnprocessableEntity || !strings.Contains(string(r.Raw), "body.phone") {
		t.Fatalf("number without a country code: %d %s", r.Status, r.Raw)
	}

	to := uniqueNumber()
	sent := a.mustStatus(a.do("POST", "/v1/me/phone/verification", map[string]any{"phone": to[:3] + " " + to[3:8] + " " + to[8:]}), http.StatusCreated).Body
	code, _ := sent["test_code"].(string)
	if !sixDigits.MatchString(code) || sent["to"] != to || sent["environment"] != "test" || sent["expires_at"] == nil || sent["resend_available_at"] == nil {
		t.Fatalf("send: %v", sent)
	}
	if r := a.do("POST", "/v1/me/phone/verification", map[string]any{"phone": to}); r.Status != http.StatusTooManyRequests {
		t.Fatalf("resend within the cooldown: %d %s", r.Status, r.Raw)
	}

	// The code went through the operator's Verify, marked as an account code.
	list := operator.mustStatus(operator.do("GET", "/v1/projects/"+projectID+"/otp?environment=test&to="+url.QueryEscape(to), nil), http.StatusOK).Body
	items := list["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("operator's verifications: %v", list)
	}
	meta := items[0].(map[string]any)["metadata"].(map[string]any)
	if items[0].(map[string]any)["id"] != sent["id"] || meta["purpose"] != "account_phone" || !strings.HasPrefix(meta["user_id"].(string), "usr_") {
		t.Fatalf("operator's verification: %v", items[0])
	}

	// Another account cannot use (or burn) a's code.
	if r := b.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": code}); r.Status != http.StatusBadRequest || r.errCode() != "code_expired" {
		t.Fatalf("someone else's code: %d %s", r.Status, r.Raw)
	}
	if r := a.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": otherCode(code)}); r.Status != http.StatusBadRequest || r.errCode() != "invalid_code" || !strings.Contains(r.errMessage(), "4 attempts left") {
		t.Fatalf("wrong code: %d %s", r.Status, r.Raw)
	}
	if r := a.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": "12ab"}); r.Status != http.StatusBadRequest || r.errCode() != "invalid_code" {
		t.Fatalf("malformed code: %d %s", r.Status, r.Raw)
	}
	me := a.mustStatus(a.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": code}), http.StatusOK).Body
	if u := me["user"].(map[string]any); u["phone"] != to || u["phone_verified"] != true {
		t.Fatalf("confirm: %v", u)
	}
	if u := meUser(a); u["phone"] != to || u["phone_verified"] != true {
		t.Fatalf("after confirm: %v", u)
	}
	if r := a.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": code}); r.Status != http.StatusBadRequest || r.errCode() != "code_expired" {
		t.Fatalf("used code: %d %s", r.Status, r.Raw)
	}

	// One account per number.
	if r := a.do("POST", "/v1/me/phone/verification", map[string]any{"phone": to}); r.Status != http.StatusConflict || r.errCode() != "already_verified" {
		t.Fatalf("own number again: %d %s", r.Status, r.Raw)
	}
	if r := b.do("POST", "/v1/me/phone/verification", map[string]any{"phone": to}); r.Status != http.StatusConflict || r.errCode() != "phone_in_use" {
		t.Fatalf("number of another account: %d %s", r.Status, r.Raw)
	}

	// Removing it frees the number.
	removed := a.mustStatus(a.do("DELETE", "/v1/me/phone", nil), http.StatusOK).Body
	if u := removed["user"].(map[string]any); u["phone"] != nil || u["phone_verified"] != false {
		t.Fatalf("after remove: %v", u)
	}
	backdateOTPs(t, to)
	sent = b.mustStatus(b.do("POST", "/v1/me/phone/verification", map[string]any{"phone": to}), http.StatusCreated).Body
	b.mustStatus(b.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": sent["test_code"]}), http.StatusOK)

	// A revoked key turns phone verification off.
	if _, err := testDB.Pool.Exec(context.Background(), `UPDATE api_keys SET revoked_at = now() WHERE key_hash = $1`, auth.HashToken(secret)); err != nil {
		t.Fatal(err)
	}
	if r := a.do("POST", "/v1/me/phone/verification", map[string]any{"phone": uniqueNumber()}); r.Status != http.StatusServiceUnavailable || r.errCode() != "phone_verification_unavailable" {
		t.Fatalf("revoked key: %d %s", r.Status, r.Raw)
	}
}

func TestPhoneVerificationLiveThroughPhone(t *testing.T) {
	srv, operator, projectID, _ := accountVerifyServer(t, "live")
	phone := connectPhone(t, srv, operator, projectID)
	u := signupAs(t, srv, uniqueEmail())

	to := uniqueNumber()
	sent := u.mustStatus(u.do("POST", "/v1/me/phone/verification", map[string]any{"phone": to}), http.StatusCreated).Body
	if _, ok := sent["test_code"]; ok || sent["environment"] != "live" {
		t.Fatalf("live send must not return the code: %v", sent)
	}

	// The operator's paired phone sends the SMS.
	job := phone.expect(gateway.TypeSendSMS)
	m := regexp.MustCompile(`^(\d{6}) is your Default code\.`).FindStringSubmatch(job.Body)
	if m == nil || job.To != to {
		t.Fatalf("SMS to %s: %q", job.To, job.Body)
	}
	segs := int16(1)
	phone.report(gateway.Inbound{Type: gateway.TypeSMSAccepted, MessageID: job.MessageID})
	phone.report(gateway.Inbound{Type: gateway.TypeSMSSent, MessageID: job.MessageID, Segments: &segs})

	me := u.mustStatus(u.do("POST", "/v1/me/phone/verification/confirm", map[string]any{"phone": to, "code": m[1]}), http.StatusOK).Body
	if user := me["user"].(map[string]any); user["phone"] != to || user["phone_verified"] != true {
		t.Fatalf("confirm: %v", user)
	}
}

func TestEmailChange(t *testing.T) {
	srv := newServer(t)
	oldEmail, newEmail := uniqueEmail(), uniqueEmail()
	c := signupAs(t, srv, oldEmail)
	nextCode(t, oldEmail, 1) // the sign-up code
	taken := uniqueEmail()
	signupAs(t, srv, taken)

	// A reset link sent before the change stops working after it.
	anon := newClient(t, srv)
	anon.mustStatus(anon.do("POST", "/v1/auth/password-reset", map[string]any{"email": oldEmail}), http.StatusAccepted)
	reset := resetLink.FindStringSubmatch(waitForSubject(t, oldEmail, "Reset your Bridge password").Text)
	if reset == nil {
		t.Fatal("no reset link")
	}

	change := func(email, password string) response {
		return c.do("POST", "/v1/me/email/change", map[string]any{"email": email, "password": password})
	}
	if r := change(newEmail, "wrong password!"); r.Status != http.StatusUnprocessableEntity || !strings.Contains(string(r.Raw), "body.password") {
		t.Fatalf("wrong password: %d %s", r.Status, r.Raw)
	}
	if r := change(strings.ToUpper(taken), "correct horse battery"); r.Status != http.StatusConflict || r.errCode() != "email_in_use" {
		t.Fatalf("address of another account: %d %s", r.Status, r.Raw)
	}
	if r := change(strings.ToUpper(oldEmail), "correct horse battery"); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("own address: %d %s", r.Status, r.Raw)
	}

	sent := c.mustStatus(change(newEmail, "correct horse battery"), http.StatusAccepted).Body
	if sent["email"] != newEmail {
		t.Fatalf("change: %v", sent)
	}
	code := nextCode(t, newEmail, 1)
	notice := waitForSubject(t, oldEmail, "Your Bridge email address is being changed")
	if !strings.Contains(notice.Text, newEmail) || !strings.Contains(notice.Text, "/forgot-password") || strings.Contains(notice.Text, code) {
		t.Fatalf("notice to the old address: %+v", notice)
	}
	if r := change(newEmail, "correct horse battery"); r.Status != http.StatusTooManyRequests {
		t.Fatalf("resend within the cooldown: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/email/change/confirm", map[string]any{"code": otherCode(code)}); r.Status != http.StatusBadRequest || r.errCode() != "invalid_code" {
		t.Fatalf("wrong code: %d %s", r.Status, r.Raw)
	}
	// The verification of the old address is a separate code.
	if r := c.do("POST", "/v1/me/email/verification/confirm", map[string]any{"code": code}); r.Status != http.StatusBadRequest {
		t.Fatalf("change code used as a verification code: %d %s", r.Status, r.Raw)
	}

	me := c.mustStatus(c.do("POST", "/v1/me/email/change/confirm", map[string]any{"code": code}), http.StatusOK).Body
	if u := me["user"].(map[string]any); u["email"] != newEmail || u["email_verified"] != true {
		t.Fatalf("after change: %v", u)
	}
	// Still signed in; the new address signs in and the old one does not.
	if u := meUser(c); u["email"] != newEmail {
		t.Fatalf("me after change: %v", u)
	}
	login := newClient(t, srv)
	if r := login.do("POST", "/v1/auth/login", map[string]any{"email": oldEmail, "password": "correct horse battery"}); r.Status != http.StatusUnauthorized {
		t.Fatalf("old address signs in: %d", r.Status)
	}
	login.mustStatus(login.do("POST", "/v1/auth/login", map[string]any{"email": newEmail, "password": "correct horse battery"}), http.StatusOK)
	if r := anon.do("POST", "/v1/auth/password-reset/confirm", map[string]any{"token": reset[1], "password": "a brand new passphrase"}); r.Status != http.StatusGone {
		t.Fatalf("reset link from before the change: %d %s", r.Status, r.Raw)
	}
	if r := c.do("POST", "/v1/me/email/change/confirm", map[string]any{"code": code}); r.Status != http.StatusBadRequest || r.errCode() != "code_expired" {
		t.Fatalf("used code: %d %s", r.Status, r.Raw)
	}

	// Someone else taking the address between the code and the confirmation.
	third := uniqueEmail()
	backdateEmailCodes(t, newEmail)
	c.mustStatus(change(third, "correct horse battery"), http.StatusAccepted)
	thirdCode := nextCode(t, third, 1)
	signupAs(t, srv, third)
	if r := c.do("POST", "/v1/me/email/change/confirm", map[string]any{"code": thirdCode}); r.Status != http.StatusConflict || r.errCode() != "email_in_use" {
		t.Fatalf("address taken meanwhile: %d %s", r.Status, r.Raw)
	}
}

// waitForSubject returns the latest email to the address with the subject.
func waitForSubject(t *testing.T, to, subject string) mail.Message {
	t.Helper()
	for range 200 {
		sentMail.mu.Lock()
		for i := len(sentMail.sent) - 1; i >= 0; i-- {
			if m := sentMail.sent[i]; m.To == to && m.Subject == subject {
				sentMail.mu.Unlock()
				return m
			}
		}
		sentMail.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no email %q to %s", subject, to)
	return mail.Message{}
}
