package broadcast

import (
	"slices"
	"testing"
)

func TestTemplate(t *testing.T) {
	tmpl := ParseTemplate("Hi {name}, order {order} ships {when}. {name}, reply {not a var} or {}!")
	if !slices.Equal(tmpl.Names(), []string{"name", "order", "when"}) {
		t.Fatalf("names %v", tmpl.Names())
	}
	got, missing := tmpl.Render(map[string]string{"name": "Ada", "order": "#42", "when": ""})
	if missing != "" || got != "Hi Ada, order #42 ships . Ada, reply {not a var} or {}!" {
		t.Fatalf("render %q missing %q", got, missing)
	}
	if _, missing := tmpl.Render(map[string]string{"name": "Ada"}); missing != "order" {
		t.Fatalf("missing = %q", missing)
	}
	plain := ParseTemplate("No placeholders {here}")
	if got, missing := plain.Render(nil); missing != "here" || got != "" {
		t.Fatalf("plain: %q %q", got, missing)
	}
	if got, _ := ParseTemplate("Static text").Render(nil); got != "Static text" {
		t.Fatal(got)
	}
}
