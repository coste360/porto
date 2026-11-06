package customer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"
)

type vectorSet struct {
	Valid   []string `json:"valid"`
	Invalid []string `json:"invalid"`
}

type vectors struct {
	Format      string    `json:"format"`
	Person      vectorSet `json:"person_fiscal_code"`
	Company     vectorSet `json:"company_fiscal_code"`
	VAT         vectorSet `json:"vat_number"`
	IBAN        vectorSet `json:"iban"`
	Description string    `json:"description"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "registry", "fiscal-codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Format != "porto-fiscal-vectors/1" {
		t.Fatalf("format %q", v.Format)
	}
	return v
}

// The shared vectors (also read by the web client) agree with the Go checks.
func TestCustomer_FiscalCodeVectors(t *testing.T) {
	v := loadVectors(t)
	for _, c := range []struct {
		name  string
		set   vectorSet
		check func(string) bool
	}{
		{"person", v.Person, ValidPersonFiscalCode},
		{"company", v.Company, ValidCompanyFiscalCode},
		{"vat", v.VAT, ValidVAT},
		{"iban", v.IBAN, ValidIBAN},
	} {
		if len(c.set.Valid) == 0 || len(c.set.Invalid) == 0 {
			t.Fatalf("%s: empty vectors", c.name)
		}
		for _, s := range c.set.Valid {
			if !c.check(NormalizeCode(s)) {
				t.Errorf("%s %q: want valid", c.name, s)
			}
		}
		for _, s := range c.set.Invalid {
			if c.check(NormalizeCode(s)) {
				t.Errorf("%s %q: want invalid", c.name, s)
			}
		}
	}
}

// Any code built with PersonCheckChar validates, and changing its check character breaks it.
func TestRapid_PersonFiscalCodeCheckChar(t *testing.T) {
	letter := rapid.SampledFrom([]byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	digit := rapid.SampledFrom([]byte("0123456789"))
	rapid.Check(t, func(t *rapid.T) {
		b := make([]byte, 0, 16)
		for range 6 {
			b = append(b, letter.Draw(t, "l"))
		}
		b = append(b, digit.Draw(t, "y1"), digit.Draw(t, "y2"), months[rapid.IntRange(0, 11).Draw(t, "m")])
		day := rapid.IntRange(1, 31).Draw(t, "d") + rapid.SampledFrom([]int{0, 40}).Draw(t, "sex")
		b = append(b, byte('0'+day/10), byte('0'+day%10), letter.Draw(t, "town"), digit.Draw(t, "t1"), digit.Draw(t, "t2"), digit.Draw(t, "t3"))
		c, ok := PersonCheckChar(string(b))
		if !ok {
			t.Fatal("check char")
		}
		code := string(append(b, c))
		if !ValidPersonFiscalCode(code) {
			t.Fatalf("%s invalid", code)
		}
		wrong := byte('A' + (c-'A'+1)%26)
		if ValidPersonFiscalCode(code[:15] + string(wrong)) {
			t.Fatalf("%s with wrong check char accepted", code)
		}
	})
}

func TestNormalize_Scenario(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  Niccolò   D'Àlba ", "niccolo d'alba"},
		{"ÉRIC ß", "eric ss"},
		{"", ""},
	} {
		if got := Fold(c.in); got != c.want {
			t.Errorf("Fold(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, c := range []struct{ in, want string }{
		{"+39 333 123 4567", "393331234567"},
		{"(333) 123-4567", "3331234567"},
		{"Rossi", "rossi"},
		{"RM12345", "rm12345"},
		{"  ", ""},
	} {
		if got := SearchTerm(c.in); got != c.want {
			t.Errorf("SearchTerm(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"+39 333 123 4567", "+393331234567", true},
		{"0039 333 1234567", "+393331234567", true},
		{"333 1234567", "+393331234567", true},
		{"06 1234567", "+39061234567", true},
		{"12345", "12345", true},
		{"12", "", false},
	} {
		got, ok := NormalizePhone(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizePhone(%q) = %q %v, want %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{" Mario.Rossi@Example.COM ", "mario.rossi@example.com", true},
		{"Mario <m@example.com>", "", false},
		{"nobody@localhost", "", false},
		{"not an email", "", false},
	} {
		got, ok := NormalizeEmail(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeEmail(%q) = %q %v, want %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if got := MaskIBAN("IT", "3456"); got != "IT•• •••• 3456" {
		t.Errorf("mask %q", got)
	}
}
