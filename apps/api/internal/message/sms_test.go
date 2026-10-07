package message

import (
	"strings"
	"testing"
)

func TestSegments(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		encoding string
		segments int
	}{
		{"empty", "", EncodingGSM7, 1},
		{"plain", "Your order has shipped.", EncodingGSM7, 1},
		{"160 GSM chars fit one", strings.Repeat("a", 160), EncodingGSM7, 1},
		{"161 GSM chars need two", strings.Repeat("a", 161), EncodingGSM7, 2},
		{"306 GSM chars fit two", strings.Repeat("a", 306), EncodingGSM7, 2},
		{"307 GSM chars need three", strings.Repeat("a", 307), EncodingGSM7, 3},
		{"extension chars cost two", strings.Repeat("€", 80), EncodingGSM7, 1},
		{"extension overflow", strings.Repeat("€", 81), EncodingGSM7, 2},
		{"accented GSM letters", "Café à Zürich: ¿Sì?", EncodingGSM7, 1},
		{"í is not GSM-7", "Sí", EncodingUCS2, 1},
		{"rupee forces UCS-2", "Paid ₹1,499", EncodingUCS2, 1},
		{"hindi", strings.Repeat("न", 70), EncodingUCS2, 1},
		{"hindi overflow", strings.Repeat("न", 71), EncodingUCS2, 2},
		{"emoji is a surrogate pair", strings.Repeat("🙂", 35), EncodingUCS2, 1},
		{"emoji overflow", strings.Repeat("🙂", 36), EncodingUCS2, 2},
	}
	for _, c := range cases {
		enc, seg := Segments(c.body)
		if enc != c.encoding || seg != c.segments {
			t.Errorf("%s: got %s/%d, want %s/%d", c.name, enc, seg, c.encoding, c.segments)
		}
	}
}
