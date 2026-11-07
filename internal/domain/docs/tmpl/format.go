package tmpl

import (
	"strconv"
	"strings"
	"time"
)

// Locale-aware formatting of the filters (it is the source locale; en is the other built-in one). Money values
// are formatted from integer cents, never through floats.

func isEN(locale string) bool { return strings.HasPrefix(strings.ToLower(locale), "en") }

// groupDigits writes the absolute value of n with a thousands separator.
func groupDigits(n int64, sep string) string {
	neg := n < 0
	u := uint64(n)
	if neg {
		u = uint64(-n)
	}
	s := strconv.FormatUint(u, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// fixed formats n/10^decimals with the locale separators; trim drops trailing zero decimals.
func fixed(n int64, decimals int, locale string, trim bool) string {
	thousands, dec := ".", ","
	if isEN(locale) {
		thousands, dec = ",", "."
	}
	neg := n < 0
	if neg {
		n = -n
	}
	pow := int64(1)
	for range decimals {
		pow *= 10
	}
	whole, frac := n/pow, n%pow
	out := groupDigits(whole, thousands)
	if decimals > 0 {
		f := strconv.FormatInt(frac, 10)
		f = strings.Repeat("0", decimals-len(f)) + f
		if trim {
			f = strings.TrimRight(f, "0")
		}
		if f != "" {
			out += dec + f
		}
	}
	if neg && (whole != 0 || frac != 0) {
		out = "-" + out
	}
	return out
}

// formatMoney prints cents as "1.234,56 €" (it) or "€1,234.56" (en).
func formatMoney(cents int64, locale string) string {
	if isEN(locale) {
		if cents < 0 {
			return "-€" + fixed(-cents, 2, locale, false)
		}
		return "€" + fixed(cents, 2, locale, false)
	}
	return fixed(cents, 2, locale, false) + " €"
}

// formatMeters prints centimetres as "11,34 m".
func formatMeters(cm int64, locale string) string { return fixed(cm, 2, locale, false) + " m" }

// formatBP prints basis points as "30 %" (it) or "30%" (en), with up to two decimals.
func formatBP(bp int64, locale string) string {
	if isEN(locale) {
		return fixed(bp, 2, locale, true) + "%"
	}
	return fixed(bp, 2, locale, true) + " %"
}

// formatMillisShare prints a share in thousandths as "333 ‰".
func formatMillisShare(m int64, locale string) string { return fixed(m, 0, locale, false) + " ‰" }

var monthsIT = []string{
	"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre",
	"novembre", "dicembre",
}

// formatDate prints an ISO date (YYYY-MM-DD) as "07/11/2026" (short) or "7 novembre 2026" (long).
func formatDate(iso string, long bool, locale string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	if !long {
		return t.Format("02/01/2006")
	}
	if isEN(locale) {
		return t.Format("2 January 2006")
	}
	return strconv.Itoa(t.Day()) + " " + monthsIT[t.Month()-1] + " " + strconv.Itoa(t.Year())
}

// formatDateTime prints an instant in its own location.
func formatDateTime(t time.Time, long bool, locale string) string {
	if t.IsZero() {
		return ""
	}
	d := formatDate(t.Format("2006-01-02"), long, locale)
	return d + " " + t.Format("15:04 MST")
}

func formatBool(b bool, locale string) string {
	switch {
	case b && isEN(locale):
		return "yes"
	case b:
		return "sì"
	default:
		return "no"
	}
}

// plain is the default text of a scalar value without filters.
func plain(v Value, locale string) string {
	if v.Null {
		return ""
	}
	switch v.Type {
	case TText, TNotes:
		return v.S
	case TDate:
		if v.S == "" {
			return ""
		}
		return formatDate(v.S, false, locale)
	case TDateTime:
		return formatDateTime(v.T, false, locale)
	case TBool:
		return formatBool(v.B, locale)
	case TCents:
		return formatMoney(v.N, locale)
	case TBP:
		return formatBP(v.N, locale)
	case TMillis:
		return fixed(v.N, 3, locale, true)
	case TList:
		return strconv.Itoa(len(v.L))
	case TObject:
		return ""
	default:
		return strconv.FormatInt(v.N, 10)
	}
}
