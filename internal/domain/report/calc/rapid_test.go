package calc

import (
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/money"
)

var base = d("2026-01-01")

func genDate(t *rapid.T, label string, lo, hi int) calendar.Date {
	return base.AddDays(rapid.IntRange(lo, hi).Draw(t, label))
}

func genOccupancy(t *rapid.T) OccupancyInput {
	nb := rapid.IntRange(1, 5).Draw(t, "berths")
	in := OccupancyInput{From: genDate(t, "from", 0, 20)}
	in.To = in.From.AddDays(rapid.IntRange(0, 30).Draw(t, "len"))
	in.Variant = rapid.SampledFrom([]string{"physical", "contractual"}).Draw(t, "variant")
	in.GroupBy = rapid.SampledFrom([]string{"", "resource", "month", "week", "night", "group"}).Draw(t, "group_by")
	for i := range nb {
		b := berth("b"+strconv.Itoa(i), "B"+strconv.Itoa(i), "m")
		b.Length = geoCm(600 + 100*int64Draw(t, "len"+strconv.Itoa(i), 0, 20))
		b.Width = 300
		if rapid.Bool().Draw(t, "grp"+strconv.Itoa(i)) {
			b.GroupIDs = []string{"g"}
		}
		in.Resources = append(in.Resources, b)
	}
	kinds := []string{KindContract, KindTransit, KindOwner, "hold", KindBlock, KindMaintenance}
	na := rapid.IntRange(0, 8).Draw(t, "allocs")
	for a := range na {
		id := "a" + strconv.Itoa(a)
		res := "b" + strconv.Itoa(rapid.IntRange(0, nb-1).Draw(t, "res"+id))
		kind := rapid.SampledFrom(kinds).Draw(t, "kind"+id)
		al := Allocation{
			ID: id, Kind: kind, ResourceID: res, HasVessel: kind != KindBlock, LOA: 500 + 10*geoCm(int64Draw(t, "loa"+id, 0, 100)),
			Beam: 250, Qualifying: rapid.Bool().Draw(t, "q"+id), ContractID: "c" + id, ContractKind: "mooring",
			Contract: ContractValue{Total: money.Cents(int64Draw(t, "tot"+id, 0, 1_000_000)), StartsOn: base, EndsOn: base.AddDays(60)},
		}
		in.Allocations = append(in.Allocations, al)
		first := genDate(t, "first"+id, 0, 50)
		n := rapid.IntRange(1, 10).Draw(t, "n"+id)
		for k := range n {
			in.Rows = append(in.Rows, NightRow{ResourceID: res, Night: first.AddDays(k), AllocationID: id, Kind: kind, Cells: 1})
		}
	}
	in.Rows = dedupeRows(in.Rows)
	if rapid.Bool().Draw(t, "closure") {
		in.Closures = []Closure{{ScopeType: "marina", From: genDate(t, "cf", 0, 50), To: genDate(t, "ct", 50, 60)}}
	}
	if rapid.Bool().Draw(t, "release") && na > 0 {
		in.Releases = []Release{{
			ID: "r", AllocationID: "a0", ResourceID: in.Allocations[0].ResourceID, Cells: 1,
			From: genDate(t, "rf", 0, 30), To: genDate(t, "rt", 30, 40),
		}}
	}
	return in
}

func int64Draw(t *rapid.T, label string, lo, hi int64) int64 {
	return rapid.Int64Range(lo, hi).Draw(t, label)
}

// dedupeRows keeps one row per (resource, night), as the unique keys of the occupancy tables do.
func dedupeRows(rs []NightRow) []NightRow {
	seen := map[string]bool{}
	var out []NightRow
	for _, r := range rs {
		k := r.ResourceID + r.Night.String()
		if !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

func genContracts(t *rapid.T) []Contract {
	n := rapid.IntRange(0, 8).Draw(t, "contracts")
	kinds := []string{ContractMooring, ContractTransit, ContractCharter}
	statuses := []string{"active", "expired", "renewed", "terminated", "confirmed", "arrived", "departed", "cancelled", "draft", "no_show"}
	var out []Contract
	for i := range n {
		id := "k" + strconv.Itoa(i)
		start := genDate(t, "s"+id, -400, 60)
		out = append(out, Contract{
			ID: id, Number: id, Kind: rapid.SampledFrom(kinds).Draw(t, "kind"+id),
			Term:   rapid.SampledFrom([]string{"annual", "seasonal", ""}).Draw(t, "term"+id),
			Status: rapid.SampledFrom(statuses).Draw(t, "st"+id), CustomerID: "c" + strconv.Itoa(rapid.IntRange(0, 3).Draw(t, "cu"+id)),
			Channel:  rapid.SampledFrom([]string{"desk", "online"}).Draw(t, "ch"+id),
			StartsOn: start, EndsOn: start.AddDays(rapid.IntRange(1, 400).Draw(t, "len"+id)),
			Total: money.Cents(int64Draw(t, "tot"+id, -1000, 5_000_000)),
		})
	}
	return out
}

func genLines(t *rapid.T) []ChargeLine {
	n := rapid.IntRange(0, 8).Draw(t, "lines")
	var out []ChargeLine
	for i := range n {
		id := "l" + strconv.Itoa(i)
		issued := genDate(t, "iss"+id, -60, 120)
		l := line(id, 1, issued.String(), "", "", money.Cents(int64Draw(t, "net"+id, -50_000, 500_000)), money.Cents(int64Draw(t, "vat"+id, -5_000, 50_000)))
		if rapid.Bool().Draw(t, "svc"+id) {
			l.ServiceFrom = genDate(t, "sf"+id, -100, 100)
			l.ServiceTo = l.ServiceFrom.AddDays(rapid.IntRange(0, 400).Draw(t, "sl"+id))
		}
		l.RevenueClass = rapid.SampledFrom([]string{"", "a", "b"}).Draw(t, "rc"+id)
		out = append(out, l)
	}
	return out
}

func checkDaily(t *rapid.T, key string, totals any, facts Facts) {
	v := TileValueOf(key, totals)
	s := facts[key].Sum()
	if v.HasFraction {
		if s.Num != v.Numerator || (!Indicators[key].ConstDen && s.Den != v.Denominator) {
			t.Fatalf("%s: Σ daily %+v != tile %+v", key, s, v)
		}
		return
	}
	if s.Num != v.Value.Value {
		t.Fatalf("%s: Σ daily %d != tile %d", key, s.Num, v.Value.Value)
	}
}

// TestCalc_TileEqualsFold (INV-62): for random rows, every tile equals the sum of the records of its report, and
// paging never changes the totals.
func TestCalc_TileEqualsFold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genOccupancy(t)
		occ, _ := BuildOccupancy(in)
		var num, den int64
		for _, r := range occ.Records {
			num += r.OccupiedUnits
			den += r.AvailableUnits
		}
		if v := TileValueOf(IndOccupancy, occ.Totals); v.Numerator != num || v.Denominator != den || v.Value.Value != Ratio(num, den).Value {
			t.Fatalf("occupancy tile %+v records %d/%d", v, num, den)
		}
		if num > den {
			t.Fatalf("occupied %d beyond available %d", num, den)
		}
		eff, _ := BuildEfficiency(in)
		num, den = 0, 0
		for _, r := range eff.Records {
			num += r.VesselUnits
			den += r.ResourceUnits
		}
		if v := TileValueOf(IndEfficiency, eff.Totals); v.Numerator != num || v.Denominator != den {
			t.Fatalf("efficiency tile %+v", v)
		}
		br, _ := BuildBerthRevenue(in, "")
		var rev, length int64
		for _, r := range br.Records {
			rev += int64(r.Revenue)
			length += int64(r.Length)
		}
		if v := TileValueOf(IndBerthRevenuePerM, br.Totals); v.Numerator != rev || v.Denominator != length {
			t.Fatalf("berth revenue tile %+v", v)
		}
		vs := BuildVessels(in, "")
		var loa int64
		for _, r := range vs.Records {
			loa += int64(r.LOA)
		}
		if v := TileValueOf(IndVesselsAvgLOA, vs.Totals); v.Numerator != loa || v.Denominator != int64(len(vs.Records)) {
			t.Fatalf("vessels tile %+v", v)
		}

		cs := genContracts(t)
		from := genDate(t, "pf", 0, 30)
		to := from.AddDays(rapid.IntRange(0, 120).Draw(t, "pl"))
		prod, _ := BuildProduction(ProductionInput{From: from, To: to, Contracts: cs})
		var comp money.Cents
		for _, r := range prod.Records {
			comp += r.Competence
		}
		if TileValueOf(IndProduction, prod.Totals).Value.Value != int64(comp) {
			t.Fatal("production tile")
		}
		cus, _ := BuildCustomers(CustomersInput{From: from, To: to, Contracts: cs, NewOnly: rapid.Bool().Draw(t, "new")})
		var nc int64
		for _, r := range cus.Records {
			nc += r.New
		}
		if TileValueOf(IndNewCustomers, cus.Totals).Value.Value != nc {
			t.Fatal("new customers tile")
		}

		ls := genLines(t)
		basis := rapid.SampledFrom([]string{"issued", "competence"}).Draw(t, "basis")
		ch, _ := BuildCharges(ChargesInput{From: from, To: to, Basis: basis, Lines: ls})
		var gross money.Cents
		for _, r := range ch.Records {
			gross += r.Gross
		}
		if TileValueOf(IndCharged, ch.Totals).Value.Value != int64(gross) {
			t.Fatal("charged tile")
		}
		paged, _ := PageOf(ch.Records, 2, 1)
		_ = paged
		if FoldCharges(ch.Records) != ch.Totals {
			t.Fatal("charges totals are the fold of the records")
		}
	})
}

// TestCalc_DailyEqualsTile (INV-64): the daily facts of every comparable indicator sum to its tile.
func TestCalc_DailyEqualsTile(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genOccupancy(t)
		occ, f := BuildOccupancy(in)
		checkDaily(t, IndOccupancy, occ.Totals, f)
		eff, f := BuildEfficiency(in)
		checkDaily(t, IndEfficiency, eff.Totals, f)
		br, f := BuildBerthRevenue(in, "")
		checkDaily(t, IndBerthRevenuePerM, br.Totals, f)

		cs := genContracts(t)
		from := genDate(t, "pf", 0, 30)
		to := from.AddDays(rapid.IntRange(0, 120).Draw(t, "pl"))
		prod, f := BuildProduction(ProductionInput{From: from, To: to, Contracts: cs, GroupBy: "month"})
		checkDaily(t, IndProduction, prod.Totals, f)
		checkDaily(t, IndTransitAvgNight, prod.Totals, f)
		var gm money.Cents
		for _, g := range prod.Groups {
			gm += g.Totals.Competence
		}
		if gm != prod.Totals.Competence {
			t.Fatal("months must sum to the total")
		}
		cus, f := BuildCustomers(CustomersInput{From: from, To: to, Contracts: cs})
		checkDaily(t, IndNewCustomers, cus.Totals, f)
		checkDaily(t, IndRenewalRate, cus.Totals, f)
		ch, f := BuildCharges(ChargesInput{From: from, To: to, Basis: rapid.SampledFrom([]string{"issued", "competence"}).Draw(t, "b"), Lines: genLines(t)})
		checkDaily(t, IndCharged, ch.Totals, f)
	})
}

// TestAccruals_CompetencePlusDeferralEqualsAmount (INV-60 extended): for every line issued by the closing date,
// its competence up to C plus its deferral at C equals its amount.
func TestAccruals_CompetencePlusDeferralEqualsAmount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ls := genLines(t)
		c := genDate(t, "c", 0, 120)
		res := BuildAccruals(AccrualsInput{From: d("2025-01-01"), To: c, Lines: ls, Section: "deferral"})
		def := map[string]AccrualsRecord{}
		for _, r := range res.Records {
			def[r.ChargeID] = r
		}
		for i := range ls {
			l := &ls[i]
			if l.IssuedOn.After(c) {
				continue
			}
			sf, _ := l.Span()
			var upNet money.Cents
			if !sf.After(c) {
				upNet, _ = accrual.Within(l.accrualLine(l.Net), sf, c)
			}
			if upNet+def[l.ChargeID].DeferralNet != l.Net {
				t.Fatalf("line %s: competence %d + deferral %d != %d", l.ChargeID, upNet, def[l.ChargeID].DeferralNet, l.Net)
			}
		}
	})
}
