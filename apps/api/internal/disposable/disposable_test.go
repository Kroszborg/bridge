package disposable

import (
	"strings"
	"testing"
)

func TestEmail(t *testing.T) {
	for _, addr := range []string{
		"bot@mailinator.com", "bot@MAILINATOR.COM", "bot@10minutemail.com", "bot@guerrillamail.com",
		"bot@temp-mail.org", "bot@yopmail.com", "bot@trashmail.com", "bot@sub.mailinator.com",
		"bot@yopmail.com.", "odd@name@sharklasers.com",
	} {
		if !Email(addr) {
			t.Errorf("%s should be disposable", addr)
		}
	}
	for _, addr := range []string{
		"ada@gmail.com", "ada@outlook.com", "ada@proton.me", "ada@example.com", "ada@mozmail.com",
		"ada@duck.com", "ada@notmailinator.co", "ada@mailinator.com.example.org", "no-at-sign", "",
		"ada@", "ada@com",
	} {
		if Email(addr) {
			t.Errorf("%s should not be disposable", addr)
		}
	}
}

func TestList(t *testing.T) {
	if n := Count(); n < 100 || n > 400 {
		t.Fatalf("list has %d domains; keep it to the most common few hundred", n)
	}
	seen := map[string]bool{}
	for _, d := range list {
		if d != strings.ToLower(strings.TrimSpace(d)) || !strings.Contains(d, ".") || strings.HasPrefix(d, ".") {
			t.Errorf("malformed domain %q", d)
		}
		if seen[d] {
			t.Errorf("duplicate domain %q", d)
		}
		seen[d] = true
	}
}
