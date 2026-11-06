package customer

import (
	"strings"
)

// Fiscal identifiers (m1-plan §4.4 "Customer rules"). The checks are pure functions over normalised strings;
// the web client implements the same rules and both sides share the vectors in
// testdata/registry/fiscal-codes.json.

// NormalizeCode upper-cases s and removes spaces, dots and dashes (fiscal codes, VAT numbers, IBANs).
func NormalizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case ' ', '\t', '.', '-':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// omocodia letters replace digits in a person's fiscal code when two codes would collide: L=0 … V=9.
const omocodia = "LMNPQRSTUV"

// months are the month letters of a person's fiscal code.
const months = "ABCDEHLMPRST"

// oddValues are the values of the characters in odd (1-based) positions: index = digit or letter offset.
var oddValues = [26]int{1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23}

func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

// digitOrOmocodia returns the digit value of c, accepting the omocodia letters.
func digitOrOmocodia(c byte) (int, bool) {
	if isDigit(c) {
		return int(c - '0'), true
	}
	if i := strings.IndexByte(omocodia, c); i >= 0 {
		return i, true
	}
	return 0, false
}

// PersonCheckChar computes the check character of the first 15 characters of a person's fiscal code. It
// reports false when the input is not 15 upper-case alphanumeric characters.
func PersonCheckChar(first15 string) (byte, bool) {
	if len(first15) != 15 {
		return 0, false
	}
	sum := 0
	for i := range 15 {
		c := first15[i]
		var v int
		switch {
		case isDigit(c):
			v = int(c - '0')
		case isLetter(c):
			v = int(c - 'A')
		default:
			return 0, false
		}
		if i%2 == 0 { // 1-based odd position
			sum += oddValues[v]
		} else {
			sum += v
		}
	}
	return byte('A' + sum%26), true
}

// ValidPersonFiscalCode reports whether s (normalised) is a well-formed Italian fiscal code of a person: 16
// characters with surname, name, year, month letter, day (1–31, +40 for women), municipality code and check
// character. Omocodia letters are accepted in the numeric positions.
func ValidPersonFiscalCode(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := range 6 {
		if !isLetter(s[i]) {
			return false
		}
	}
	for _, i := range []int{6, 7, 9, 10, 12, 13, 14} {
		if _, ok := digitOrOmocodia(s[i]); !ok {
			return false
		}
	}
	if strings.IndexByte(months, s[8]) < 0 || !isLetter(s[11]) || !isLetter(s[15]) {
		return false
	}
	d1, _ := digitOrOmocodia(s[9])
	d2, _ := digitOrOmocodia(s[10])
	day := d1*10 + d2
	if day > 40 {
		day -= 40
	}
	if day < 1 || day > 31 {
		return false
	}
	want, ok := PersonCheckChar(s[:15])
	return ok && want == s[15]
}

// ValidItalianVAT reports whether s is an 11-digit Italian VAT number (partita IVA) with a valid check digit.
// A company's numeric fiscal code uses the same rule.
func ValidItalianVAT(s string) bool {
	if len(s) != 11 {
		return false
	}
	sum := 0
	for i := range 11 {
		c := s[i]
		if !isDigit(c) {
			return false
		}
		d := int(c - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0 && s[:7] != "0000000"
}

// ValidCompanyFiscalCode reports whether s is a company's fiscal code: 11 digits with the VAT check digit, or
// a 16-character personal code (sole proprietorships).
func ValidCompanyFiscalCode(s string) bool {
	if len(s) == 11 {
		return ValidItalianVAT(s)
	}
	return ValidPersonFiscalCode(s)
}

// ValidVAT reports whether s (normalised) is a VAT number: Italian (with prefix IT or none) with its check
// digit, or another country's number (two-letter prefix followed by 2–13 letters or digits), checked for format
// only.
func ValidVAT(s string) bool {
	if len(s) >= 2 && isLetter(s[0]) && isLetter(s[1]) {
		country, rest := s[:2], s[2:]
		if country == "IT" {
			return ValidItalianVAT(rest)
		}
		if len(rest) < 2 || len(rest) > 13 {
			return false
		}
		for i := range len(rest) {
			if !isDigit(rest[i]) && !isLetter(rest[i]) {
				return false
			}
		}
		return true
	}
	return ValidItalianVAT(s)
}

// ibanLengths holds the IBAN length of common countries; other countries are checked for 15–34 characters.
var ibanLengths = map[string]int{
	"AD": 24, "AT": 20, "BE": 16, "BG": 22, "CH": 21, "CY": 28, "CZ": 24, "DE": 22, "DK": 18, "EE": 20,
	"ES": 24, "FI": 18, "FR": 27, "GB": 22, "GR": 27, "HR": 21, "HU": 28, "IE": 22, "IS": 26, "IT": 27,
	"LI": 21, "LT": 20, "LU": 20, "LV": 21, "MC": 27, "MT": 31, "NL": 18, "NO": 15, "PL": 28, "PT": 25,
	"RO": 24, "SE": 24, "SI": 19, "SK": 24, "SM": 27, "VA": 22,
}

// ValidIBAN reports whether s (normalised) is an IBAN with a valid ISO 13616 mod-97 checksum.
func ValidIBAN(s string) bool {
	if len(s) < 15 || len(s) > 34 || !isLetter(s[0]) || !isLetter(s[1]) || !isDigit(s[2]) || !isDigit(s[3]) {
		return false
	}
	if n, ok := ibanLengths[s[:2]]; ok && len(s) != n {
		return false
	}
	rearranged := s[4:] + s[:4]
	rem := 0
	for i := range len(rearranged) {
		c := rearranged[i]
		switch {
		case isDigit(c):
			rem = (rem*10 + int(c-'0')) % 97
		case isLetter(c):
			v := int(c-'A') + 10
			rem = (rem*100 + v) % 97
		default:
			return false
		}
	}
	return rem == 1
}

// MaskIBAN returns the masked form shown to users ("IT•• •••• 1234") from the country code and last four
// characters.
func MaskIBAN(country, last4 string) string {
	if last4 == "" {
		return ""
	}
	return country + "•• •••• " + last4
}
