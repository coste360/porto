package calc

import (
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }

func berth(id, code, mapID string) Resource {
	return Resource{
		ID: id, Code: code, Type: TypeBerth, Family: FamilyBerth, MapID: mapID, MapKind: "water",
		Length: 1000, Width: 400, MaxLOA: 1000, MaxBeam: 400, HoldsBoat: true,
	}
}

func rows(res, alloc, kind string, from, to string) []NightRow {
	var out []NightRow
	for x := d(from); !x.After(d(to)); x = x.AddDays(1) {
		out = append(out, NightRow{ResourceID: res, Night: x, AllocationID: alloc, Kind: kind, Cells: 1})
	}
	return out
}

func cat(parts ...[]NightRow) []NightRow {
	var out []NightRow
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestBP_HalfEvenAndNil(t *testing.T) {
	cases := []struct {
		num, den int64
		want     int64
		ok       bool
	}{
		{1, 3, 3333, true},
		{2, 3, 6667, true},
		{1, 8, 1250, true},
		{1, 20000, 0, true}, // 0.5 bp → half-even 0
		{3, 20000, 2, true}, // 1.5 bp → 2
		{5, 0, 0, false},
		{0, 7, 0, true},
		{-1, 3, -3333, true},
	}
	for _, c := range cases {
		got := Ratio(c.num, c.den)
		if got.OK != c.ok || got.Value != c.want {
			t.Errorf("Ratio(%d, %d) = %+v, want %d %v", c.num, c.den, got, c.want, c.ok)
		}
	}
	if got := Ratio(5, 0).Ptr(); got != nil {
		t.Error("nil expected for a zero denominator")
	}
	if v := Scaled(25000, 1000, ScalePerM); !v.OK || v.Value != 2500 {
		t.Errorf("per metre %+v", v)
	}
}

func TestOccupancy_Table(t *testing.T) {
	ra, rb, rc := berth("b1", "A01", "m1"), berth("b2", "A02", "m1"), berth("b3", "B01", "m2")
	allocs := []Allocation{
		{ID: "al1", Kind: KindContract, ResourceID: "b1", ContractID: "c1", ContractNumber: "ORM-2026-0001", ContractKind: "mooring"},
		{ID: "al2", Kind: KindTransit, ResourceID: "b2", ContractID: "t1", ContractNumber: "TRN-2026-0001", ContractKind: "transit"},
		{ID: "hold", Kind: "hold", ResourceID: "b3"},
		{ID: "blk", Kind: KindBlock, ResourceID: "b3"},
		{ID: "resale", Kind: KindTransit, ResourceID: "b1", ContractID: "t2", ContractKind: "transit"},
	}
	base := OccupancyInput{
		From: d("2026-06-01"), To: d("2026-06-10"), Resources: []Resource{ra, rb, rc},
		Allocations: allocs,
		Rows: cat(
			rows("b1", "al1", KindContract, "2026-06-01", "2026-06-04"), // 4 nights, 06-05..06-07 released
			rows("b1", "resale", KindTransit, "2026-06-06", "2026-06-06"),
			rows("b1", "al1", KindContract, "2026-06-08", "2026-06-10"),
			rows("b2", "al2", KindTransit, "2026-06-01", "2026-06-10"),
			rows("b3", "hold", "hold", "2026-06-01", "2026-06-05"),
			rows("b3", "blk", KindBlock, "2026-06-06", "2026-06-07"),
		),
		Releases: []Release{{
			ID: "r1", AllocationID: "al1", ResourceID: "b1", ResoldAllocationID: "resale", ContractID: "c1",
			From: d("2026-06-05"), To: d("2026-06-07"), Cells: 1,
		}},
	}
	type want struct {
		occ, avail, closed, oos, released int64
		bp                                int64
	}
	cases := []struct {
		name string
		mod  func(*OccupancyInput)
		want want
	}{
		{
			"physical: holds free, block out of service, unsold release free", func(*OccupancyInput) {},
			want{occ: 4 + 1 + 3 + 10, avail: 30 - 2, oos: 2, bp: Ratio(18, 28).Value},
		},
		{
			"contractual adds the unsold released nights only once", func(in *OccupancyInput) { in.Variant = "contractual" },
			want{occ: 18 + 2, avail: 28, oos: 2, released: 2, bp: Ratio(20, 28).Value},
		},
		{"marina closure", func(in *OccupancyInput) {
			in.Closures = []Closure{{ScopeType: "marina", From: d("2026-06-01"), To: d("2026-06-02")}}
		}, want{occ: 18 - 2 - 2, avail: 28 - 6, closed: 6, oos: 2, bp: Ratio(14, 22).Value}},
		{"map closure", func(in *OccupancyInput) {
			in.Closures = []Closure{{ScopeType: "map", ScopeID: "m2", From: d("2026-06-01"), To: d("2026-06-10")}}
		}, want{occ: 18, avail: 20, closed: 10, bp: Ratio(18, 20).Value}},
		{"resource closure", func(in *OccupancyInput) {
			in.Closures = []Closure{{ScopeType: "resource", ScopeID: "b2", From: d("2026-06-10"), To: d("2026-06-30")}}
		}, want{occ: 17, avail: 27, closed: 1, oos: 2, bp: Ratio(17, 27).Value}},
		{"group closure and filter", func(in *OccupancyInput) {
			in.Resources[0].GroupIDs = []string{"g1"}
			in.Dims.GroupID = "g1"
			in.Closures = []Closure{{ScopeType: "group", ScopeID: "g1", From: d("2026-06-01"), To: d("2026-06-01")}}
		}, want{occ: 7, avail: 9, closed: 1, bp: Ratio(7, 9).Value}},
		{
			"inactive resources are not passed: map filter", func(in *OccupancyInput) { in.Dims.MapID = "m2" },
			want{occ: 0, avail: 8, oos: 2, bp: 0},
		},
	}
	for _, c := range cases {
		in := base
		in.Resources = []Resource{ra, rb, rc}
		c.mod(&in)
		res, facts := BuildOccupancy(in)
		tt := res.Totals
		got := want{tt.OccupiedUnits, tt.AvailableUnits, tt.ClosedUnits, tt.OutOfServiceUnits, tt.ReleasedUnits, tt.OccupancyBP.Value}
		if got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
		if s := facts[IndOccupancy].Sum(); s.Num != tt.OccupiedUnits || s.Den != tt.AvailableUnits {
			t.Errorf("%s: daily %+v != totals %+v", c.name, s, tt)
		}
		if tt.Unit != UnitBerthNight {
			t.Errorf("%s: unit %s", c.name, tt.Unit)
		}
	}
}

func TestOccupancy_Areas(t *testing.T) {
	lin := Resource{ID: "a1", Code: "L1", Type: TypeArea, Family: FamilyLinear, MapID: "m", CellCm: 50, UsableCells: 20, HoldsBoat: true}
	sur := Resource{ID: "a2", Code: "S1", Type: TypeArea, Family: FamilySurface, MapID: "m", CellCm: 100, UsableCells: 10, HoldsBoat: true}
	in := OccupancyInput{
		From: d("2026-07-01"), To: d("2026-07-02"), Family: FamilyLinear, Resources: []Resource{lin, sur},
		Allocations: []Allocation{{ID: "x", Kind: KindTransit, ResourceID: "a1", HasVessel: true, LOA: 1000, Beam: 350, Footprint: 400}},
		Rows: []NightRow{
			{ResourceID: "a1", Night: d("2026-07-01"), AllocationID: "x", Kind: KindTransit, Cells: 8},
			{ResourceID: "a1", Night: d("2026-07-02"), AllocationID: "x", Kind: KindTransit, Cells: 8},
			{ResourceID: "a1", Night: d("2026-07-02"), AllocationID: "m", Kind: KindMaintenance, Cells: 4},
		},
	}
	res, _ := BuildOccupancy(in)
	// available: 20 cells × 50 cm on 07-01, (20 − 4) × 50 on 07-02.
	if res.Totals.Unit != UnitCmNight || res.Totals.OccupiedUnits != 16*50 || res.Totals.AvailableUnits != 36*50 || res.Totals.OutOfServiceUnits != 4*50 {
		t.Fatalf("linear %+v", res.Totals)
	}
	eff, _ := BuildEfficiency(in)
	if eff.Totals.VesselUnits != 2*400 || eff.Totals.ResourceUnits != 16*50 || eff.Totals.EfficiencyBP.Value != 10000 {
		t.Fatalf("linear efficiency %+v", eff.Totals)
	}
	in.Family = FamilySurface
	in.Allocations[0].ResourceID = "a2"
	in.Rows = []NightRow{{ResourceID: "a2", Night: d("2026-07-01"), AllocationID: "x", Kind: KindTransit, Cells: 5}}
	res, _ = BuildOccupancy(in)
	if res.Totals.Unit != UnitCm2Night || res.Totals.OccupiedUnits != 5*10000 || res.Totals.AvailableUnits != 2*10*10000 {
		t.Fatalf("surface %+v", res.Totals)
	}
	eff, _ = BuildEfficiency(in)
	if eff.Totals.VesselUnits != 1000*350 || eff.Totals.ResourceUnits != 5*10000 {
		t.Fatalf("surface efficiency %+v", eff.Totals)
	}
}

func TestEfficiency_Table(t *testing.T) {
	b := berth("b1", "A01", "m")
	in := OccupancyInput{
		From: d("2026-06-01"), To: d("2026-06-03"), Resources: []Resource{b},
		Allocations: []Allocation{
			{ID: "a", Kind: KindContract, ResourceID: "b1", HasVessel: true, LOA: 800, Beam: 300},
			{ID: "nv", Kind: KindOwner, ResourceID: "b1"},
		},
		Rows: cat(rows("b1", "a", KindContract, "2026-06-01", "2026-06-02"), rows("b1", "nv", KindOwner, "2026-06-03", "2026-06-03")),
	}
	res, facts := BuildEfficiency(in)
	if len(res.Records) != 1 || res.Totals.Nights != 2 || res.Totals.VesselUnits != 2*800*300 || res.Totals.ResourceUnits != 2*1000*400 {
		t.Fatalf("%+v", res.Totals)
	}
	if res.Totals.EfficiencyBP.Value != 6000 || len(res.Notes) != 1 || res.Notes[0].Code != "no_vessel" {
		t.Fatalf("bp %+v notes %+v", res.Totals.EfficiencyBP, res.Notes)
	}
	if s := facts[IndEfficiency].Sum(); s.Num != res.Totals.VesselUnits || s.Den != res.Totals.ResourceUnits {
		t.Fatal("daily")
	}
	in.GroupBy = "night"
	res, _ = BuildEfficiency(in)
	if len(res.Groups) != 3 || res.Groups[2].Totals.EfficiencyBP.OK {
		t.Fatalf("night groups %+v", res.Groups)
	}
}

func TestVessels_Table(t *testing.T) {
	b1, b2 := berth("b1", "A01", "m"), berth("b2", "A02", "m")
	in := OccupancyInput{
		From: d("2026-06-01"), To: d("2026-06-05"), Resources: []Resource{b1, b2},
		Allocations: []Allocation{
			{ID: "a", Kind: KindContract, ResourceID: "b1", VesselID: "v1", VesselName: "Alfa", HasVessel: true, LOA: 1250, Beam: 400, HullType: "monohull_sail", Flag: "IT"},
			{ID: "b", Kind: KindTransit, ResourceID: "b2", VesselID: "v1", VesselName: "Alfa", HasVessel: true, LOA: 1250, Beam: 400},
			{ID: "c", Kind: KindTransit, ResourceID: "b2", VesselID: "v2", VesselName: "Beta", HasVessel: true, LOA: 799, Beam: 300},
		},
		Rows: cat(rows("b1", "a", KindContract, "2026-06-01", "2026-06-03"), rows("b2", "b", KindTransit, "2026-06-03", "2026-06-03"),
			rows("b2", "c", KindTransit, "2026-06-04", "2026-06-05")),
	}
	res := BuildVessels(in, "length_band")
	if res.Totals.Vessels != 2 || res.Totals.Nights != 3+2 || res.Totals.LOASum != 2049 || res.Totals.AvgLOA.Value != 1024 {
		t.Fatalf("%+v", res.Totals)
	}
	if res.Records[0].Kind != KindContract || res.Records[1].LengthBand != "lt8" || len(res.Groups) != 2 {
		t.Fatalf("%+v %+v", res.Records, res.Groups)
	}
}

func TestBerthRevenue_Table(t *testing.T) {
	b1 := berth("b1", "A01", "m")
	b2 := berth("b2", "A02", "m")
	b2.Length = 1500
	in := OccupancyInput{
		From: d("2026-06-01"), To: d("2026-06-30"), Resources: []Resource{b1, b2},
		Allocations: []Allocation{
			{
				ID: "a", Kind: KindContract, ResourceID: "b1", Qualifying: true,
				Contract: ContractValue{Total: 36500, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01")},
			},
			{ID: "o", Kind: KindOwner, ResourceID: "b2"},
		},
		Rows: cat(rows("b1", "a", KindContract, "2026-06-01", "2026-06-30"), rows("b2", "o", KindOwner, "2026-06-01", "2026-06-30")),
	}
	res, facts := BuildBerthRevenue(in, "")
	if res.Totals.Revenue != 30*100 || res.Totals.Length != 2500 || res.Totals.RevenuePerM.Value != 120 {
		t.Fatalf("%+v", res.Totals)
	}
	if res.Records[1].OccupiedNights != 30 || res.Records[1].Revenue != 0 || res.Records[0].RevenuePerM.Value != 300 {
		t.Fatalf("%+v", res.Records)
	}
	if facts[IndBerthRevenuePerM].Sum().Num != int64(res.Totals.Revenue) {
		t.Fatal("daily")
	}
	_ = geo.Cm(0)
	_ = money.Cents(0)
}
