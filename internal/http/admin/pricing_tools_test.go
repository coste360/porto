package admin_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
)

// TestPricingView_RoundTrip: the catalogue, price list and engine types carry the JSON fields of the contract,
// so the strict conversions of internal/http/view never panic and keep every value.
func TestPricingView_RoundTrip(t *testing.T) {
	en := "Transit"
	day := calendar.MustParse("2027-07-10")
	c := money.Cents(750)
	maxCm := geo.Cm(1200)
	bp := int64(500)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	nature := "N1"
	item := catalog.Item{
		ID: "i", Code: "TRN", Name: catalog.I18nText{It: "T", En: &en}, Description: catalog.I18nTextOptional{It: "d"},
		ItemType: "berth_fee", Unit: "night", VATRateBP: 0, VATNature: &nature, RevenueClassID: "r", Usage: &catalog.ItemUsage{PriceLists: 2},
		Version: 3, UpdatedAt: now,
	}
	if got := view.Item(&item); got.Usage.PriceLists != 2 || *got.VatNature != "N1" || got.Name.En == nil {
		t.Fatalf("item %+v", got)
	}
	parent := "p"
	_ = view.RevenueClass(&catalog.RevenueClass{ID: "r", Code: "transit", Name: catalog.I18nText{It: "T"}, ParentID: &parent, ItemCount: 1, Version: 1})
	_ = view.BerthCategories(&catalog.CategoryList{Items: []catalog.BerthCategory{{ID: "c", Code: "L12", AppliesTo: []string{"berth"}}}, UnknownInUse: []catalog.CodeCount{{Code: "X", Count: 1}}})
	_ = view.PricingSettings(&catalog.Settings{DefaultVATRateBP: 2200, Version: 1})

	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{pricing.ContractTransit: "i"}
	set.SeasonWindow = &pricing.DateRange{From: day, To: day}
	set.Deposits = map[pricing.ContractType]pricing.DepositPolicy{pricing.ContractAnnual: {PercentBP: &bp}}
	active := true
	rule := pricelist.Rule{Rule: pricing.Rule{
		ID: "r1", Seq: 4, Stage: pricing.StageBase, Name: "n", ItemID: "i", Basis: pricing.BasisPerMeter,
		Unit: pricing.UnitNight, UnitCents: &c, Amounts: map[string]money.Cents{"L12": 1}, Includes: []pricing.Inclusion{{ItemID: "x", Qty: 1000}},
		TargetItemTypes: []pricing.ItemType{pricing.ItemBerthFee}, Conditions: pricing.Conditions{HullTypes: []string{"catamaran"}},
		Effect: pricing.Effect{PercentBP: &bp}, Scope: pricing.Scope{Periods: []string{"ALTA"}, Channels: []pricing.Channel{pricing.ChannelDesk}},
	}, Active: &active}
	sha := "abc"
	doc := pricelist.Document{
		ID: "l", Code: "T", Name: catalog.I18nText{It: "T"}, Status: "active", ValidFrom: day, ValidTo: day,
		ContractTypes: []pricing.ContractType{pricing.ContractTransit}, Settings: set,
		Periods: []pricelist.Period{{ID: "p", Code: "ALTA", Name: catalog.I18nText{It: "A"}, Kind: pricing.PeriodSeason, Color: "chart-1", Ranges: []pricing.DateRange{{From: day, To: day}}}},
		Bands:   []pricelist.Band{{ID: "b", Code: "L12", MaxCm: &maxCm, Category: "L12"}}, Rules: []pricelist.Rule{rule}, ContentSHA256: &sha,
		ActivatedAt: &now, Version: 2, Issues: []pricing.Issue{{Code: "x", Severity: "warning", Ref: pricing.IssueRef{Kind: "rule", ID: "r1", Field: "amounts"}, Params: map[string]string{"a": "1"}}},
		UpdatedAt: now,
	}
	got := view.PriceListDocument(&doc)
	if got.Rules[0].UnitCents == nil || *got.Rules[0].UnitCents != 750 || (*got.Settings.PrimaryItems)["transit"] != "i" || got.Bands[0].MaxCm == nil {
		t.Fatalf("document %+v", got)
	}
	// Back again: the save request keeps rule fields.
	raw, _ := json.Marshal(got)
	var save api.PriceListSave
	if err := json.Unmarshal(raw, &save); err != nil {
		t.Fatal(err)
	}
	in, err := view.PriceListSaveIn(save)
	if err != nil {
		t.Fatal(err)
	}
	if in.Rules[0].Seq != 4 || *in.Rules[0].UnitCents != 750 || in.Rules[0].Active == nil || in.Periods[0].Ranges[0].From != day {
		t.Fatalf("save in %+v", in.Rules[0])
	}
	_ = view.PriceListActivateResult(&pricelist.ActivateResult{Document: &doc, Truncated: []pricelist.Truncated{{ID: "x", Code: "y", ValidFrom: day, ValidTo: day}}})

	// Breakdown with nil slices: arrays on the wire.
	b := view.PriceBreakdown(pricing.Breakdown{Lines: []pricing.Line{{Blocks: []pricing.Block{{From: day, To: day}}, ServiceFrom: day, ServiceTo: day}}},
		&pricelist.Resolved{PriceListID: "l", Channel: pricing.ChannelDesk, QuotedOn: day, DimensionOrigin: pricelist.OriginNone})
	if b.Lines[0].Blocks[0].Modifiers == nil || b.Lines[0].Discounts == nil || b.Trace == nil || b.Warnings == nil || b.VatSummary == nil || b.Resolved == nil {
		t.Fatalf("breakdown %+v", b)
	}
	q := pricelist.QuoteRequest{
		PriceListID: "l", ContractType: pricing.ContractTransit, Arrival: day, Departure: day.AddDays(1),
		Vessel: &pricelist.SimVessel{LOA: 1200, Beam: 400, HullType: "monohull_motor"}, Category: "L12", BerthLengthCm: 1, QuotedOn: &day,
		Services: []pricing.ServiceRequest{{ItemID: "s", Qty: 1000}}, Deposit: &pricing.DepositPolicy{AmountCents: &c},
	}
	probe := view.SimulateRequest(&q)
	back, err := view.SimulateRequestIn(probe)
	if err != nil || back.Vessel.LOA != 1200 || *back.QuotedOn != day || back.Services[0].Qty != 1000 {
		t.Fatalf("probe round trip %+v %v", back, err)
	}
	total := money.Cents(9000)
	unit := pricing.UnitNight
	_ = view.PriceMatrix(&pricelist.Matrix{
		PriceListID: "l", ContractType: pricing.ContractTransit, QuotedOn: day,
		Rows: []pricing.MatrixRow{{Key: "L12", Kind: "band", ProbeLOA: 1200, ProbeBeam: 400}}, Columns: []pricing.MatrixColumn{{Key: "ALTA", Arrival: day, Departure: day}},
		Cells: []pricelist.MatrixCell{{TotalCents: &total, NetCents: &total, BaseCents: &total, Unit: &unit, Probe: q}},
	})
	_ = view.PriceCoverage(&pricelist.Coverage{Gaps: []pricing.Gap{{ContractType: pricing.ContractTransit, Category: "L12", Band: "L12", Period: "ALTA", ItemID: "i", Reason: "no_rule"}}})
	delta := int64(500)
	_ = view.RenewalPreview(&pricelist.RenewalPreview{
		Document: pricelist.SaveRequest{Code: "x", Rules: []pricelist.Rule{rule}},
		Diffs:    []pricelist.RenewalDiff{{ContractType: pricing.ContractTransit, Cells: []pricing.DiffCell{{OldCents: &total, NewCents: &total, DeltaCents: &total, DeltaBP: &delta}}}},
	})
	rr, err := view.RenewalRequestIn(api.RenewalRequest{Code: "x", ShiftYears: 1, AmountRounding: "euro", Adjustments: &[]api.RenewalAdjustment{{Scope: "all", PercentBp: &delta}}})
	if err != nil || *rr.Adjustments[0].PercentBP != 500 {
		t.Fatalf("renewal in %+v %v", rr, err)
	}
}
