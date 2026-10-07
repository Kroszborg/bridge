package httpapi

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Ada's workspace":      "adas-workspace",
		"Ada’s workspace":      "adas-workspace",
		"  Checkout  Service ": "checkout-service",
		"API_v2.prod":          "api-v2-prod",
		"Ünïcode & symbols!":   "ncode-symbols",
		"---":                  "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	if got := slugify("a very long project name that keeps going and going forever"); len(got) > 40 {
		t.Errorf("slug too long: %q", got)
	}
}
