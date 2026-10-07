package automation

import (
	"strings"
	"testing"

	"bridge/internal/db/dbq"
	"bridge/internal/message"
)

func TestMatchRule(t *testing.T) {
	rules := []dbq.AutoReplyRule{
		{ID: "off", Enabled: false, MatchType: MatchExact, Keywords: []string{"STOP"}},
		{ID: "stop", Enabled: true, MatchType: MatchExact, Keywords: []string{"STOP", "UNSUBSCRIBE"}},
		{ID: "price", Enabled: true, MatchType: MatchContains, Keywords: []string{"price"}},
		{ID: "info", Enabled: true, MatchType: MatchStartsWith, Keywords: []string{"info"}},
	}
	for body, want := range map[string]string{
		"STOP":               "stop",
		"  stop \n":          "stop",
		"Unsubscribe":        "stop",
		"stop please":        "",
		"What is the PRICE?": "price",
		"info about plans":   "info",
		"more info":          "",
		"":                   "",
	} {
		r, _ := MatchRule(rules, body)
		got := ""
		if r != nil {
			got = r.ID
		}
		if got != want {
			t.Errorf("MatchRule(%q) = %q, want %q", body, got, want)
		}
	}
}

func TestReplyNumber(t *testing.T) {
	for sender, want := range map[string]string{
		"+919876543210":   "+919876543210",
		"+1 (415) 555-26": "",
		"00919876543210":  "+919876543210",
		"AX-HDFCBK":       "",
		"57575":           "",
		"9876543210":      "", // national format: the country is unknown
		"+12345":          "",
	} {
		got, ok := ReplyNumber(sender)
		if got != want || ok != (want != "") {
			t.Errorf("ReplyNumber(%q) = %q %v, want %q", sender, got, ok, want)
		}
	}
}

func TestForwardingMatches(t *testing.T) {
	kw := "otp"
	r := dbq.ForwardingRule{Senders: []string{"AX-*", "+919800000001"}, Contains: &kw}
	for _, c := range []struct {
		sender, body string
		want         bool
	}{
		{"AX-HDFCBK", "Your OTP is 1234", true},
		{"ax-icici", "otp 99", true},
		{"+919800000001", "OTP", true},
		{"+919800000002", "OTP", false},
		{"AX-HDFCBK", "Balance low", false},
	} {
		if got := Matches(r, c.sender, c.body); got != c.want {
			t.Errorf("Matches(%q, %q) = %v", c.sender, c.body, got)
		}
	}
	if !Matches(dbq.ForwardingRule{}, "anyone", "anything") {
		t.Fatal("an empty match must match everything")
	}
}

func TestTruncateSegments(t *testing.T) {
	short := "From +91: hi"
	if TruncateSegments(short, 3) != short {
		t.Fatal("short text changed")
	}
	long := strings.Repeat("abcdefghij ", 100)
	got := TruncateSegments(long, 3)
	if _, segs := message.Segments(got); segs != 3 || !strings.HasSuffix(got, "...") || len(got) < 400 {
		t.Fatalf("gsm: %d chars, %d segments", len(got), segs)
	}
	uni := strings.Repeat("नमस्ते ", 100)
	got = TruncateSegments(uni, 3)
	if enc, segs := message.Segments(got); segs != 3 || enc != message.EncodingUCS2 {
		t.Fatalf("ucs2: %d segments %s", segs, enc)
	}
}

func TestNormalizeRule(t *testing.T) {
	reply := "  Thanks  "
	f := RuleFields{Name: " Hi ", Match: MatchExact, Keywords: []string{" hello ", "HELLO", ""}, Reply: &reply}
	if err := normalizeRule(&f); err != nil {
		t.Fatal(err)
	}
	if f.Name != "Hi" || len(f.Keywords) != 1 || *f.Reply != "Thanks" || f.Action != ActionNone {
		t.Fatalf("normalized %+v", f)
	}
	empty := ""
	if err := normalizeRule(&RuleFields{Name: "x", Match: MatchExact, Keywords: []string{"a"}, Reply: &empty}); err == nil {
		t.Fatal("a rule without reply or action was accepted")
	}
}
