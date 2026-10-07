package message

// SMS encoding and segment counting (3GPP TS 23.038 / 23.040).
//
// A single SMS carries 160 GSM-7 septets or 70 UCS-2 code units. Longer
// messages are split into concatenated segments of 153 or 67, because each
// segment spends space on the concatenation header.

const (
	EncodingGSM7 = "gsm7"
	EncodingUCS2 = "ucs2"
)

// gsm7Basic is the GSM 03.38 default alphabet (the escape character excluded).
const gsm7Basic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?" +
	"¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

// gsm7Extension characters cost two septets (escape + character).
const gsm7Extension = "\f^{}\\[~]|€"

var (
	basicSet     = runeSet(gsm7Basic)
	extensionSet = runeSet(gsm7Extension)
)

func runeSet(s string) map[rune]struct{} {
	m := make(map[rune]struct{}, len(s))
	for _, r := range s {
		m[r] = struct{}{}
	}
	return m
}

// Segments reports the encoding a phone will use for body and how many SMS
// segments it occupies.
func Segments(body string) (encoding string, segments int) {
	septets, gsm := 0, true
	for _, r := range body {
		if _, ok := basicSet[r]; ok {
			septets++
		} else if _, ok := extensionSet[r]; ok {
			septets += 2
		} else {
			gsm = false
			break
		}
	}
	if gsm {
		return EncodingGSM7, count(septets, 160, 153)
	}
	units := 0
	for _, r := range body {
		if r > 0xFFFF {
			units += 2 // surrogate pair
		} else {
			units++
		}
	}
	return EncodingUCS2, count(units, 70, 67)
}

func count(n, single, multi int) int {
	if n <= single {
		return 1
	}
	return (n + multi - 1) / multi
}
