package webhook

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// The reference vector from the Standard Webhooks test suite.
func TestSignMatchesStandardWebhooksVector(t *testing.T) {
	const (
		secret  = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
		msgID   = "msg_p5jXN8AQM9LWM0D4loKWxJek"
		payload = `{"test": 2432232314}`
		want    = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
	)
	got, err := Sign(secret, msgID, time.Unix(1614265330, 0), []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("signature %s, want %s", got, want)
	}
}

func TestVerify(t *testing.T) {
	secret := NewSecret()
	now := time.Now()
	body := []byte(`{"type":"message.delivered"}`)
	sig, err := Sign(secret, "evt_1", now, body)
	if err != nil {
		t.Fatal(err)
	}
	headers := func(sig string, ts time.Time) http.Header {
		h := http.Header{}
		h.Set(HeaderID, "evt_1")
		h.Set(HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10))
		h.Set(HeaderSignature, sig)
		return h
	}
	if err := Verify(secret, headers(sig, now), body, DefaultTolerance, now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	// Receivers must accept any matching signature in a space-separated list.
	if err := Verify(secret, headers("v1,bm90LWl0 "+sig, now), body, DefaultTolerance, now); err != nil {
		t.Fatalf("signature list rejected: %v", err)
	}
	cases := map[string]struct {
		h    http.Header
		body []byte
		want error
	}{
		"tampered body": {headers(sig, now), []byte(`{"type":"message.failed"}`), ErrInvalidSignature},
		"other secret":  {headers(mustSign(t, NewSecret(), now, body), now), body, ErrInvalidSignature},
		"old timestamp": {headers(sig, now.Add(-10*time.Minute)), body, ErrTimestamp},
		"no headers":    {http.Header{}, body, ErrMissingHeaders},
	}
	for name, c := range cases {
		if err := Verify(secret, c.h, c.body, DefaultTolerance, now); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func mustSign(t *testing.T, secret string, ts time.Time, body []byte) string {
	t.Helper()
	s, err := Sign(secret, "evt_1", ts, body)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRetryScheduleSpansAboutThreeDays(t *testing.T) {
	var total time.Duration
	for _, d := range retrySchedule {
		total += d
	}
	if total < 60*time.Hour || total > 80*time.Hour {
		t.Fatalf("retries span %s; want about 3 days", total)
	}
	if MaxAttempts != 12 {
		t.Fatalf("MaxAttempts = %d", MaxAttempts)
	}
	for attempt, want := range map[int]time.Duration{1: 5 * time.Second, 2: 5 * time.Minute, 5: 5 * time.Hour, 11: 10 * time.Hour, 50: 10 * time.Hour} {
		if got := RetryDelay(attempt); got < want || got > want+want/10 {
			t.Errorf("RetryDelay(%d) = %s, want %s plus at most 10%%", attempt, got, want)
		}
	}
}

func TestValidateURL(t *testing.T) {
	strict := &Service{}
	for raw, ok := range map[string]bool{
		"https://example.com/hooks":         true,
		"http://hooks.example.com:8080/x?a": true,
		"ftp://example.com":                 false,
		"https://user:pw@example.com":       false,
		"https://example.com/#frag":         false,
		"http://localhost:3000/hooks":       false,
		"http://127.0.0.1/hooks":            false,
		"http://10.0.0.5/hooks":             false,
		"http://[::1]/hooks":                false,
		"http://169.254.169.254/latest":     false,
		"http://app.internal/hooks":         false,
		"example.com/hooks":                 false,
	} {
		if _, err := strict.ValidateURL(raw); (err == nil) != ok {
			t.Errorf("ValidateURL(%q) error = %v, want ok=%v", raw, err, ok)
		}
	}
	lenient := &Service{allowPrivate: true}
	if _, err := lenient.ValidateURL("http://app:3000/hooks"); err != nil {
		t.Errorf("private URL rejected with AllowPrivate: %v", err)
	}
}
