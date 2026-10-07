package broadcast

import (
	"regexp"
	"strings"
)

// placeholderRE matches {name}: a letter or underscore, then letters, digits
// or underscores. Other braces are literal text.
var placeholderRE = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]{0,39})\}`)

// Template is a message with {name} placeholders filled per recipient.
type Template struct {
	text  string
	names []string // distinct placeholder names, in order of first use
}

// ParseTemplate finds a template's placeholders.
func ParseTemplate(text string) Template {
	t := Template{text: text}
	seen := map[string]bool{}
	for _, m := range placeholderRE.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			t.names = append(t.names, m[1])
		}
	}
	return t
}

// Names lists the placeholders the template uses.
func (t Template) Names() []string { return t.names }

// Render fills the placeholders from vars. missing is the first placeholder
// without a value (an empty string counts as a value).
func (t Template) Render(vars map[string]string) (text, missing string) {
	for _, n := range t.names {
		if _, ok := vars[n]; !ok {
			return "", n
		}
	}
	if len(t.names) == 0 {
		return t.text, ""
	}
	var b strings.Builder
	last := 0
	for _, loc := range placeholderRE.FindAllStringSubmatchIndex(t.text, -1) {
		b.WriteString(t.text[last:loc[0]])
		b.WriteString(vars[t.text[loc[2]:loc[3]]])
		last = loc[1]
	}
	b.WriteString(t.text[last:])
	return b.String(), ""
}
