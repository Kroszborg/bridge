package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(env(map[string]string{"BRIDGE_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.SessionTTL != 720*time.Hour || c.LogFormat != "text" || !c.AllowSignup {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.DashboardOrigin() != "http://localhost:3000" {
		t.Fatalf("DashboardOrigin = %q", c.DashboardOrigin())
	}
	if c.CookieSecure {
		t.Fatal("cookies should not be Secure for an http dashboard")
	}
}

func TestLoadProductionHTTPS(t *testing.T) {
	c, err := load(env(map[string]string{
		"BRIDGE_ENV":           "production",
		"BRIDGE_DATABASE_URL":  "postgres://x",
		"BRIDGE_DASHBOARD_URL": "https://app.example.com/",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.CookieSecure || c.LogFormat != "json" {
		t.Fatalf("production over https should use Secure cookies and JSON logs: %+v", c)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := load(env(map[string]string{
		"BRIDGE_ENV":             "staging",
		"BRIDGE_PUBLIC_URL":      "not a url",
		"BRIDGE_SESSION_TTL":     "5m",
		"BRIDGE_TRUSTED_PROXIES": "10.0.0.0/8,banana",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"BRIDGE_ENV", "BRIDGE_DATABASE_URL", "BRIDGE_PUBLIC_URL", "BRIDGE_SESSION_TTL", "banana"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestFCMRequiresAllFields(t *testing.T) {
	_, err := load(env(map[string]string{"BRIDGE_DATABASE_URL": "postgres://x", "BRIDGE_FCM_PROJECT_ID": "p"}))
	if err == nil || !strings.Contains(err.Error(), "BRIDGE_FCM_CREDENTIALS_FILE") {
		t.Fatalf("partial FCM config accepted: %v", err)
	}
	c, err := load(env(map[string]string{"BRIDGE_DATABASE_URL": "postgres://x"}))
	if err != nil || c.FCM != nil {
		t.Fatalf("FCM should be off by default: %v %+v", err, c.FCM)
	}
	if c.VAPIDSubject != "mailto:bridge@localhost" {
		t.Fatalf("VAPIDSubject = %q", c.VAPIDSubject)
	}
}
