package customer

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

// foldTable maps accented Latin letters to their base letter (search is accent-insensitive, m1-plan §4.4).
var foldTable = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'č': "c", 'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i", 'į': "i", 'ı': "i",
	'ł': "l", 'ñ': "n", 'ń': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ő': "o",
	'ŕ': "r", 'ř': "r", 'ś': "s", 'š': "s", 'ş': "s", 'ß': "ss", 'ť': "t", 'ţ': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ū': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ý': "y", 'ÿ': "y", 'ź': "z", 'ż': "z", 'ž': "z", 'æ': "ae", 'œ': "oe",
}

// Fold lower-cases s, folds accents and collapses runs of white space into one space.
func Fold(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		if f, ok := foldTable[r]; ok {
			b.WriteString(f)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Digits returns the decimal digits of s.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SearchTerm normalises a search query: folded text, or only the digits when the query looks like a phone
// number (digits with + ( ) - . and spaces). "" means no search.
func SearchTerm(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	phoneLike := true
	digits := 0
	for _, r := range q {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune("+()-. ", r):
		default:
			phoneLike = false
		}
	}
	if phoneLike && digits >= 3 && strings.ContainsAny(q, "+()- ") {
		return Digits(q)
	}
	return Fold(q)
}

// NormalizeEmail returns the lower-case address, or false when it is not a plain address.
func NormalizeEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 254 {
		return "", false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" || !strings.Contains(s[strings.LastIndexByte(s, '@')+1:], ".") {
		return "", false
	}
	return s, true
}

// NormalizePhone returns the E.164 form ("+<digits>") when the number is parseable, else its digits. Numbers
// without an international prefix that look Italian (9 to 11 digits starting with 0 or 3) get +39. It reports
// false when fewer than 5 or more than 15 digits remain.
func NormalizePhone(s string) (string, bool) {
	s = strings.TrimSpace(s)
	d := Digits(s)
	switch {
	case strings.HasPrefix(s, "+"):
	case strings.HasPrefix(d, "00"):
		d = d[2:]
		s = "+" + d
	}
	if len(d) < 5 || len(d) > 15 {
		return "", false
	}
	if strings.HasPrefix(s, "+") {
		return "+" + d, true
	}
	if len(d) >= 9 && len(d) <= 11 && (d[0] == '0' || d[0] == '3') {
		return "+39" + d, true
	}
	return d, true
}

// clean trims s and reports whether it fits in maxRunes characters.
func clean(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= maxRunes
}
