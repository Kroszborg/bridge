package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"
	"time"
)

func sign(secret []byte, id string, ts int64, body string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(id + "." + strconv.FormatInt(ts, 10) + "." + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(key)
	now := time.Unix(1_760_000_000, 0)
	body := `{"type":"subscription.active"}`
	good := sign(key, "msg_1", now.Unix(), body)
	ts := strconv.FormatInt(now.Unix(), 10)

	cases := []struct {
		name, id, ts, sigs, body string
		ok                       bool
	}{
		{"valid", "msg_1", ts, "v1," + good, body, true},
		{"one of several", "msg_1", ts, "v1,bm9wZQ== v1," + good, body, true},
		{"tampered body", "msg_1", ts, "v1," + good, body + " ", false},
		{"other id", "msg_2", ts, "v1," + good, body, false},
		{"unknown version", "msg_1", ts, "v2," + good, body, false},
		{"stale", "msg_1", strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10), "v1," + good, body, false},
		{"no id", "", ts, "v1," + good, body, false},
	}
	for _, c := range cases {
		err := VerifyWebhook(secret, c.id, c.ts, c.sigs, []byte(c.body), now)
		if c.ok != (err == nil) {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: want ErrBadSignature, got %v", c.name, err)
		}
	}
	// A secret without the prefix works too; one that is not base64 is a configuration error.
	if err := VerifyWebhook(base64.StdEncoding.EncodeToString(key), "msg_1", ts, "v1,"+good, []byte(body), now); err != nil {
		t.Errorf("bare secret: %v", err)
	}
	if err := VerifyWebhook("whsec_***", "msg_1", ts, "v1,"+good, []byte(body), now); err == nil || errors.Is(err, ErrBadSignature) {
		t.Errorf("invalid secret should be a configuration error, got %v", err)
	}
}

func TestNormalizeStatus(t *testing.T) {
	for in, want := range map[string]string{
		"active": StatusActive, "on_hold": StatusPastDue, "past_due": StatusPastDue, "cancelled": StatusCancelled,
		"paused": StatusCancelled, "expired": StatusExpired, "failed": StatusIncomplete, "pending": StatusIncomplete,
		"something_new": StatusIncomplete,
	} {
		if got := NormalizeStatus(in); got != want {
			t.Errorf("NormalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
	if !grants(StatusPastDue) || grants(StatusCancelled) || grants(StatusIncomplete) {
		t.Error("only active and past_due subscriptions keep their plan")
	}
}

func TestLimitErrorMessages(t *testing.T) {
	for _, c := range []struct {
		err  LimitError
		want string
	}{
		{LimitError{Phones, 1, "Free"}, "The Free plan includes 1 phone. Upgrade the workspace to add more."},
		{LimitError{Projects, 5, "Pro"}, "The Pro plan includes 5 projects. Upgrade the workspace to add more."},
		{LimitError{Members, 2, "Free"}, "The Free plan includes 2 members. Upgrade the workspace to add more."},
		{LimitError{LiveMessages, 300, "Free"}, "This workspace sent the 300 live SMS a month included in the Free plan. Live sending resumes on the 1st (UTC), or upgrade for more."},
	} {
		if got := c.err.Error(); got != c.want {
			t.Errorf("got %q\nwant %q", got, c.want)
		}
	}
}
