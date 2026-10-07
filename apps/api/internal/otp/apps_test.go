package otp

import (
	"errors"
	"slices"
	"testing"

	"bridge/internal/messaging"
)

func TestNormalizeOrigin(t *testing.T) {
	for raw, want := range map[string]string{
		"https://Shop.Example.com":       "https://shop.example.com",
		"https://shop.example.com/":      "https://shop.example.com",
		"https://shop.example.com:8443":  "https://shop.example.com:8443",
		"http://localhost:5173":          "http://localhost:5173",
		"http://127.0.0.1:3000":          "http://127.0.0.1:3000",
		"http://shop.example.com":        "",
		"https://shop.example.com/login": "",
		"https://shop.example.com?x=1":   "",
		"https://user@shop.example.com":  "",
		"shop.example.com":               "",
		"null":                           "",
	} {
		got, ok := NormalizeOrigin(raw)
		if (want != "") != ok || got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
}

func TestNumberRangeAndCountry(t *testing.T) {
	if got := numberRange("+919876543210"); got != "+919876543" {
		t.Errorf("range = %q", got)
	}
	if numberRange("+919876543001") != numberRange("+919876543999") || numberRange("+919876543001") == numberRange("+919876544001") {
		t.Error("sequential numbers must share a range")
	}
	for n, want := range map[string]string{"+919876543210": "IN", "+12015550123": "US", "+447400123456": "GB", "+999": ""} {
		if got := CountryOf(n); got != want {
			t.Errorf("CountryOf(%s) = %q, want %q", n, got, want)
		}
	}
}

func TestAppConfigNormalize(t *testing.T) {
	c := DefaultAppConfig("  Shop  ")
	c.AllowedCountries = []string{"us", "IN", "US"}
	c.AllowedOrigins = []string{"https://A.example", "https://a.example/"}
	c.RedirectURIs = []string{" https://a.example/done ", "https://a.example/done"}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Name != "Shop" || !slices.Equal(c.AllowedCountries, []string{"IN", "US"}) ||
		!slices.Equal(c.AllowedOrigins, []string{"https://a.example"}) || !slices.Equal(c.RedirectURIs, []string{"https://a.example/done"}) {
		t.Fatalf("normalized = %+v", c)
	}

	for field, mutate := range map[string]func(*AppConfig){
		"name":                   func(c *AppConfig) { c.Name = " " },
		"allowed_countries":      func(c *AppConfig) { c.AllowedCountries = []string{"ZZ"} },
		"allowed_origins":        func(c *AppConfig) { c.AllowedOrigins = []string{"http://a.example"} },
		"redirect_uris":          func(c *AppConfig) { c.RedirectURIs = []string{"javascript:alert(1)"} },
		"failover_after_seconds": func(c *AppConfig) { c.FailoverAfter = 601 },
		"code_length":            func(c *AppConfig) { c.Settings.CodeLength = 3 },
	} {
		c := DefaultAppConfig("x")
		mutate(&c)
		var ve *messaging.ValidationError
		if err := c.normalize(); !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: %v", field, err)
		}
	}
}

func TestSlugFromName(t *testing.T) {
	for name, want := range map[string]string{"Shop Front": "shop-front", "  Acme, Inc. ": "acme-inc", "!!!": "app", "Ünïcode App": "n-code-app"} {
		if got := slugFromName(name); got != want {
			t.Errorf("slugFromName(%q) = %q, want %q", name, got, want)
		}
	}
}
