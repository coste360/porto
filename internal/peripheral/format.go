package peripheral

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Text helpers shared by the printer drivers. Amounts are formatted the Italian way (1.234,56), which is the
// source language of every document Porto prints.

// FormatCents formats cents as "1.234,56" (negative with a leading minus).
func FormatCents(c int64) string {
	neg := c < 0
	if neg {
		c = -c
	}
	units, cents := c/100, c%100
	digits := strconv.FormatInt(units, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(d)
	}
	out := b.String() + "," + pad2(cents)
	if neg {
		return "-" + out
	}
	return out
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// FormatRate formats basis points as a percentage without useless decimals ("22%", "5,5%").
func FormatRate(bp int64) string {
	whole, frac := bp/100, bp%100
	if frac == 0 {
		return strconv.FormatInt(whole, 10) + "%"
	}
	f := pad2(frac)
	f = strings.TrimRight(f, "0")
	return strconv.FormatInt(whole, 10) + "," + f + "%"
}

// FormatQty formats thousandths ("1", "2,5").
func FormatQty(milli int64) string {
	whole, frac := milli/1000, milli%1000
	if frac < 0 {
		frac = -frac
	}
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	f := strconv.FormatInt(frac+1000, 10)[1:]
	return strconv.FormatInt(whole, 10) + "," + strings.TrimRight(f, "0")
}

// wrap splits s into lines of at most width runes, breaking at spaces when possible.
func wrap(s string, width int) []string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return nil
	}
	var out []string
	for utf8.RuneCountInString(s) > width {
		r := []rune(s)
		cut := width
		for i := width; i > 0; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimSpace(string(r[:cut])))
		s = strings.TrimSpace(string(r[cut:]))
	}
	return append(out, s)
}

// twoCols puts left and right on one line of width runes (left truncated when needed).
func twoCols(left, right string, width int) string {
	lr, rr := []rune(left), []rune(right)
	space := width - len(rr)
	if space < 1 {
		return string(rr)
	}
	if len(lr) > space-1 {
		lr = lr[:max(space-1, 0)]
	}
	return string(lr) + strings.Repeat(" ", space-len(lr)) + string(rr)
}

// methodLabels are the printed names of the payment methods.
var methodLabels = map[string]string{
	"cash":     "Contanti", //nolint:misspell // Italian
	"card_pos": "Carta (POS)", "bank_transfer": "Bonifico", "cheque": "Assegno",
	"other": "Altro", "card_online": "Carta online", "wallet": "Wallet", "sepa_debit": "Addebito SEPA",
	"credit_note": "Credito",
}

func methodLabel(m string) string {
	if l, ok := methodLabels[m]; ok {
		return l
	}
	return m
}
