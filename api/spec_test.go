package apispec

import (
	"regexp"
	"strings"
	"testing"
)

// TestContract_NoFloats: money, measures, percentages and quantities are integers on the wire (AGENTS.md,
// m2-plan §4.1): the contract never declares a number type or a float format.
func TestContract_NoFloats(t *testing.T) {
	bad := regexp.MustCompile(`(?m)(type:\s*\[?\s*"?number"?|format:\s*(float|double))`)
	for i, line := range strings.Split(string(OpenAPI), "\n") {
		if bad.MatchString(line) {
			t.Errorf("api/openapi.yaml:%d declares a non-integer number: %s", i+1, strings.TrimSpace(line))
		}
	}
}

// TestContract_PricingUnits: M2 money fields end in _cents, percentages in _bp, quantities in _milli.
func TestContract_PricingUnits(t *testing.T) {
	src := string(OpenAPI)
	for _, name := range []string{
		"total_cents:", "amount_cents:", "unit_cents:", "vat_rate_bp:", "percent_bp:", "qty_milli:", "max_cm:",
	} {
		if !strings.Contains(src, name) {
			t.Errorf("contract has no %s field", strings.TrimSuffix(name, ":"))
		}
	}
	for _, forbidden := range []string{" price:", " total:", " amount:", " percent:"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("contract has a unitless money field %q", strings.TrimSpace(forbidden))
		}
	}
}
