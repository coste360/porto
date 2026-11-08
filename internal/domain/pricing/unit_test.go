package pricing

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

func unitCatalog(id string, from, to string, rules ...Rule) Catalog {
	return Catalog{
		PriceList: PriceList{
			ID: id, Code: id, ValidFrom: calendar.MustParse(from), ValidTo: calendar.MustParse(to),
			ContractTypes: []ContractType{ContractAnnual},
			Periods: []RatePeriod{{Code: "HIGH", Kind: PeriodSeason, Ranges: []DateRange{{
				From: calendar.MustParse("2027-06-01"), To: calendar.MustParse("2027-08-31"),
			}}}},
		},
		Items: []Item{
			{ID: "itm", Code: "SRV", Type: ItemService, VATRateBP: 2200, Active: true},
			{ID: "gross", Code: "GRS", Type: ItemMaterial, VATRateBP: 2200, PriceIncludesVAT: true, Active: true},
		},
		Rules: rules,
	}
}

func baseRule(id string, seq, prio int64, item string, unit, amount int64, scope Scope) Rule {
	r := Rule{ID: id, Seq: seq, Stage: StageBase, Active: true, Priority: prio, ItemID: item, Basis: BasisFlat, Unit: UnitStay, Scope: scope}
	if unit >= 0 {
		u := money.Cents(unit)
		r.UnitCents = &u
	}
	if amount >= 0 {
		a := money.Cents(amount)
		r.AmountCents = &a
	}
	return r
}

func TestUnitPrice_Selection(t *testing.T) {
	on := calendar.MustParse("2027-03-10")
	high := calendar.MustParse("2027-07-10")
	cases := []struct {
		name     string
		cats     []Catalog
		on       calendar.Date
		category string
		item     string
		want     money.Cents
		rule     string
		incl     bool
	}{
		{"unit_before_amount", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31", baseRule("r1", 1, 0, "itm", 1800, 2500, Scope{}))}, on, "", "itm", 1800, "r1", false},
		{"amount_when_no_unit", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31", baseRule("r1", 1, 0, "itm", -1, 2500, Scope{}))}, on, "", "itm", 2500, "r1", false},
		{"priority_wins", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 2, 0, "itm", 1000, -1, Scope{}), baseRule("r2", 1, 10, "itm", 2000, -1, Scope{}))}, on, "", "itm", 2000, "r2", false},
		{"specificity_wins", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 2, 0, "itm", 1000, -1, Scope{}), baseRule("r2", 1, 0, "itm", 900, -1, Scope{CustomerCategories: []string{"vip"}}))}, on, "vip", "itm", 900, "r2", false},
		{"category_mismatch_skipped", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 2, 0, "itm", 1000, -1, Scope{}), baseRule("r2", 1, 0, "itm", 900, -1, Scope{CustomerCategories: []string{"vip"}}))}, on, "", "itm", 1000, "r1", false},
		{"recency_wins", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 1, 0, "itm", 1000, -1, Scope{}), baseRule("r2", 5, 0, "itm", 1100, -1, Scope{}))}, on, "", "itm", 1100, "r2", false},
		{"ignored_scopes", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 1, 0, "itm", 1000, -1, Scope{ContractTypes: []ContractType{ContractTransit}, Channels: []Channel{ChannelOnline}, Categories: []string{"A"}, GroupIDs: []string{"g"}}))}, on, "", "itm", 1000, "r1", false},
		{"period_scope", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 1, 0, "itm", 1000, -1, Scope{}), baseRule("r2", 1, 0, "itm", 1500, -1, Scope{Periods: []string{"HIGH"}}))}, high, "", "itm", 1500, "r2", false},
		{"period_scope_off_season", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31",
			baseRule("r1", 1, 0, "itm", 1000, -1, Scope{}), baseRule("r2", 1, 0, "itm", 1500, -1, Scope{Periods: []string{"HIGH"}}))}, on, "", "itm", 1000, "r1", false},
		{"validity", []Catalog{
			unitCatalog("OLD", "2026-01-01", "2026-12-31", baseRule("r1", 9, 0, "itm", 900, -1, Scope{})),
			unitCatalog("NEW", "2027-01-01", "2027-12-31", baseRule("r2", 1, 0, "itm", 1000, -1, Scope{})),
		}, on, "", "itm", 1000, "r2", false},
		{"gross_item", []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31", baseRule("r1", 1, 0, "gross", 12200, -1, Scope{}))}, on, "", "gross", 12200, "r1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := UnitPrice(c.cats, c.item, c.on, c.category)
			if err != nil {
				t.Fatal(err)
			}
			if got.UnitCents != c.want || got.RuleID != c.rule || got.PriceIncludesVAT != c.incl || got.PriceListID == "" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestUnitPrice_NoRule(t *testing.T) {
	inactive := baseRule("r1", 1, 0, "itm", 1000, -1, Scope{})
	inactive.Active = false
	mod := baseRule("r2", 1, 0, "itm", 1000, -1, Scope{})
	mod.Stage = StageModifier
	cats := []Catalog{unitCatalog("L1", "2027-01-01", "2027-12-31", inactive, mod, baseRule("r3", 1, 0, "itm", -1, -1, Scope{}))}
	for _, item := range []string{"itm", "other"} {
		_, err := UnitPrice(cats, item, calendar.MustParse("2027-03-01"), "")
		var np *NoPriceError
		if !errors.As(err, &np) || np.Reason != ReasonNoRule || !errors.Is(err, ErrNoPrice) {
			t.Fatalf("%s: %v", item, err)
		}
	}
	if _, err := UnitPrice(nil, "itm", calendar.MustParse("2027-03-01"), ""); err == nil {
		t.Fatal("no catalogue priced")
	}
	if EngineVersion != 1 {
		t.Fatal("EngineVersion changed")
	}
}
