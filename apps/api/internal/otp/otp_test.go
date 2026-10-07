package otp

import (
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	tpl := "{code} is your {app} code. Valid {minutes} min."
	for _, tc := range []struct {
		name         string
		ttl          time.Duration
		hash, domain string
		want         string
	}{
		{"plain", 10 * time.Minute, "", "", "123456 is your Acme code. Valid 10 min."},
		{"rounds minutes up", 90 * time.Second, "", "", "123456 is your Acme code. Valid 2 min."},
		{"android hash last", 5 * time.Minute, "FA+9qCX9VSu", "", "123456 is your Acme code. Valid 5 min.\nFA+9qCX9VSu"},
		{"web otp line", 5 * time.Minute, "", "acme.example.com", "123456 is your Acme code. Valid 5 min.\n\n@acme.example.com #123456"},
		{"hash wins over domain", 5 * time.Minute, "FA+9qCX9VSu", "acme.example.com", "123456 is your Acme code. Valid 5 min.\nFA+9qCX9VSu"},
	} {
		if got := Render(tpl, "Acme", "123456", tc.ttl, tc.hash, tc.domain); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestValidateTemplate(t *testing.T) {
	for tpl, ok := range map[string]bool{
		DefaultTemplate:                       true,
		"Code: {code}":                        true,
		"no placeholder":                      false,
		"{code}{code}":                        false,
		"{code} for {name}":                   false,
		"{code} " + string(make([]rune, 300)): false,
	} {
		if err := ValidateTemplate(tpl); (err == nil) != ok {
			t.Errorf("ValidateTemplate(%.30q) = %v, want ok=%v", tpl, err, ok)
		}
	}
}

func TestGenerateCode(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		c, err := generateCode(6)
		if err != nil || len(c) != 6 {
			t.Fatalf("code %q, err %v", c, err)
		}
		for _, r := range c {
			if r < '0' || r > '9' {
				t.Fatalf("non-digit in %q", c)
			}
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatalf("only %d distinct codes in 200", len(seen))
	}
}
