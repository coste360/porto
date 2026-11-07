package pricing

import (
	"slices"
	"testing"

	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

func hasIssue(issues []Issue, code, severity string) bool {
	return slices.ContainsFunc(issues, func(i Issue) bool { return i.Code == code && i.Severity == severity })
}

// TestValidate_Scenario: each issue code of m2-plan §4.8 is reported on a catalog that has the problem and not
// on the clean base catalog.
func TestValidate_Scenario(t *testing.T) {
	baseCat := func() Catalog {
		cat := buildCatalog(t, corpusCase{Catalog: "periods"})
		cat.PriceList.Bands = []Band{{Code: "L10", MaxCm: ptr(geo.Cm(1000))}, {Code: "LXX"}}
		cat.Items = append(cat.Items, Item{ID: "svc", Code: "SRV", Type: ItemService, VATRateBP: 2200, Active: true})
		cat.Rules = append(cat.Rules,
			Rule{ID: "r-lb", Seq: 20, Stage: StageBase, Active: true, ItemID: "trn", Basis: BasisLengthBand, Unit: UnitNight, Priority: -1, Amounts: map[string]money.Cents{"L10": 1, "LXX": 2}},
			Rule{
				ID: "r-svc", Seq: 21, Stage: StageBase, Active: true, ItemID: "svc", Basis: BasisMetered, Unit: UnitStay, UnitCents: ptr(money.Cents(10)),
				Includes: nil,
			},
			Rule{ID: "m-1", Seq: 22, Stage: StageModifier, Active: true, Stackable: true, Effect: Effect{PercentBP: ptr(int64(1000))}},
			Rule{ID: "d-1", Seq: 23, Stage: StageDiscount, Active: true, DiscountKind: DiscountPromo, PromoCode: "X", Effect: Effect{PercentBP: ptr(int64(1000))}},
			Rule{ID: "d-2", Seq: 24, Stage: StageDiscount, Active: true, DiscountKind: DiscountCustomer, Scope: Scope{CustomerCategories: []string{"member"}}, Effect: Effect{AmountCents: ptr(money.Cents(100))}},
		)
		return cat
	}
	clean := Validate(baseCat())
	if len(clean) != 0 {
		t.Fatalf("base catalog has issues: %+v", clean)
	}
	rule := func(c *Catalog, id string) *Rule {
		for i := range c.Rules {
			if c.Rules[i].ID == id {
				return &c.Rules[i]
			}
		}
		t.Fatalf("no rule %s", id)
		return nil
	}
	cases := []struct {
		code, severity string
		mutate         func(c *Catalog)
	}{
		{IssuePeriodOverlap, SeverityError, func(c *Catalog) {
			c.PriceList.Periods[1].Ranges = append(c.PriceList.Periods[1].Ranges, DateRange{From: d("2027-05-20"), To: d("2027-06-05")})
		}},
		{IssueRangeInvalid, SeverityError, func(c *Catalog) { c.PriceList.Periods[3].Ranges[0].To = d("2027-09-01") }},
		{IssuePeriodCodeUnknown, SeverityError, func(c *Catalog) { rule(c, "r-low").Scope.Periods = []string{"NOPE"} }},
		{IssueBandOrder, SeverityError, func(c *Catalog) {
			c.PriceList.Bands = []Band{{Code: "L10", MaxCm: ptr(geo.Cm(1000))}, {Code: "L08", MaxCm: ptr(geo.Cm(800))}, {Code: "LXX"}}
		}},
		{IssueBandOpenNotLast, SeverityError, func(c *Catalog) { c.PriceList.Bands = []Band{{Code: "LXX"}, {Code: "L10", MaxCm: ptr(geo.Cm(1000))}} }},
		{IssueBandCodeUnknown, SeverityError, func(c *Catalog) { rule(c, "r-lb").Amounts["L99"] = 5 }},
		{IssueRuleItemUnknown, SeverityError, func(c *Catalog) { rule(c, "r-svc").ItemID = "ghost" }},
		{IssueRuleItemUnknown, SeverityError, func(c *Catalog) { rule(c, "r-low").Includes = []Inclusion{{ItemID: "ghost", Qty: qty.One}} }},
		{IssueRuleItemInactive, SeverityError, func(c *Catalog) { c.Items[1].Active = false }},
		{IssueRuleBasisInvalid, SeverityError, func(c *Catalog) { rule(c, "r-svc").UnitCents = nil }},
		{IssueRuleBasisInvalid, SeverityError, func(c *Catalog) {
			rule(c, "r-low").Basis = BasisMetered
			rule(c, "r-low").UnitCents = ptr(money.Cents(1))
		}},
		{IssueRuleUnitInvalid, SeverityError, func(c *Catalog) { rule(c, "r-low").Unit = UnitStay }},
		{IssueRuleUnitInvalid, SeverityError, func(c *Catalog) { rule(c, "r-low").Unit = UnitSeason }},
		{IssueRuleEffectInvalid, SeverityError, func(c *Catalog) { rule(c, "m-1").Effect.AmountCents = ptr(money.Cents(5)) }},
		{IssueRuleEffectInvalid, SeverityError, func(c *Catalog) { rule(c, "d-1").Effect.PercentBP = ptr(int64(0)) }},
		{IssuePromoWithoutCode, SeverityError, func(c *Catalog) { rule(c, "d-1").PromoCode = " " }},
		{IssueCustomerNoCategory, SeverityError, func(c *Catalog) { rule(c, "d-2").Scope.CustomerCategories = nil }},
		{IssueSeasonalWithoutWindow, SeverityError, func(c *Catalog) {
			c.PriceList.ContractTypes = append(c.PriceList.ContractTypes, ContractSeasonal)
			c.PriceList.Settings.PrimaryItems[ContractSeasonal] = "trn"
		}},
		{IssuePrimaryItemMissing, SeverityError, func(c *Catalog) { c.PriceList.ContractTypes = append(c.PriceList.ContractTypes, ContractMonthly) }},
		{IssueContractTypeUnknown, SeverityError, func(c *Catalog) { c.PriceList.ContractTypes = append(c.PriceList.ContractTypes, "weekly") }},
		{IssueContractTypeUnknown, SeverityError, func(c *Catalog) { rule(c, "r-low").Scope.ContractTypes = []ContractType{"daily"} }},
		{IssueAmountOutOfRange, SeverityError, func(c *Catalog) { rule(c, "r-low").AmountCents = ptr(money.Cents(MaxAmountCents + 1)) }},
		{IssueAmountOutOfRange, SeverityError, func(c *Catalog) { rule(c, "m-1").Effect.PercentBP = ptr(int64(-MaxModifierBP - 1)) }},
		{IssueAmountOutOfRange, SeverityError, func(c *Catalog) { c.PriceList.Settings.LengthRounding.StepCm = 7 }},
		{IssueAmountOutOfRange, SeverityError, func(c *Catalog) { rule(c, "r-low").Priority = 1001 }},
		{IssuePeriodOutsideValidity, SeverityWarning, func(c *Catalog) { c.PriceList.Periods[0].Ranges[0].From = d("2026-12-01") }},
		{IssueValidityNotCovered, SeverityWarning, func(c *Catalog) { c.PriceList.Periods[0].Ranges[1].To = d("2027-12-30") }},
		{IssueSplitUnitMissing, SeverityWarning, func(c *Catalog) {
			c.PriceList.Settings.Split = map[ContractType]SplitRule{AnyContractType: SplitBestCombination}
			for i := range c.Rules {
				if c.Rules[i].Unit == UnitNight && c.Rules[i].ItemID == "trn" {
					c.Rules[i].Unit = UnitWeek
				}
			}
		}},
		{IssueNoBaseRuleForContract, SeverityWarning, func(c *Catalog) {
			for i := range c.Rules {
				if c.Rules[i].ItemID == "trn" {
					c.Rules[i].Active = false
				}
			}
		}},
		{IssueDuplicateRule, SeverityWarning, func(c *Catalog) {
			dup := *rule(c, "r-low")
			dup.ID, dup.Seq = "r-low-2", 99
			c.Rules = append(c.Rules, dup)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			if hasIssue(clean, tc.code, tc.severity) {
				t.Fatalf("clean catalog reports %s", tc.code)
			}
			c := baseCat()
			tc.mutate(&c)
			issues := Validate(c)
			if !hasIssue(issues, tc.code, tc.severity) {
				t.Fatalf("want %s (%s), got %+v", tc.code, tc.severity, issues)
			}
		})
	}
	// Every issue code except category_unknown (ValidateCategories) is exercised above.
	seen := map[string]bool{IssueCategoryUnknown: true}
	for _, tc := range cases {
		seen[tc.code] = true
	}
	for _, code := range IssueCodes() {
		if !seen[code] {
			t.Errorf("issue code %s has no scenario", code)
		}
	}
}

func TestValidateCategories(t *testing.T) {
	cat := buildCatalog(t, corpusCase{Catalog: "per-berth"})
	issues := ValidateCategories(cat, []string{"A", "B"})
	// C (r-pb-n amounts) is unknown; A and B are known.
	if len(issues) != 1 || issues[0].Code != IssueCategoryUnknown || issues[0].Params["category"] != "C" || issues[0].Severity != SeverityWarning {
		t.Fatalf("%+v", issues)
	}
	if got := ValidateCategories(cat, []string{"A", "B", "C"}); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestCorpus_InvalidFixtureErrors(t *testing.T) {
	issues := Validate(buildCatalog(t, corpusCase{Catalog: "invalid-overlapping-seasons"}))
	if !hasIssue(issues, IssuePeriodOverlap, SeverityError) {
		t.Fatalf("%+v", issues)
	}
}
