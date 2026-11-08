package calc

import (
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/money"
)

func storageFixture() StorageInput {
	from, to := d("2026-03-01"), d("2026-03-10")
	in := StorageInput{
		From: from, To: to, Full: true,
		Areas: []StorageArea{
			{ID: "s1", Code: "SHED-A", MapKind: "shed", CellCm: 100, UsableCells: 100},
			{ID: "y1", Code: "YARD-B", MapKind: "yard", CellCm: 200, UsableCells: 10},
		},
		Allocations: map[string]*StorageAllocation{
			"a1": {
				ID: "a1", AreaID: "s1", Kind: KindContract, ContractID: "c1", ContractCode: "CTR-1", ContractKind: ContractStorage,
				VesselID: "v1", LOA: 800, Beam: 300, ExitOn: d("2026-03-05"), ExitBlocked: true, Production: Some(money.Cents(5000)),
			},
			"a2": {
				ID: "a2", AreaID: "s1", Kind: KindTransit, ContractID: "t1", ContractCode: "TRN-1", ContractKind: ContractTransit,
				VesselID: "v2", LOA: 600, Beam: 250, ExitOn: d("2026-04-01"),
			},
			"a3": {
				ID: "a3", AreaID: "y1", Kind: KindContract, ContractID: "c3", ContractCode: "CTR-3", ContractKind: ContractStorage,
				VesselID: "v3", LOA: 1000, Beam: 350, ExitOn: d("2026-03-20"), ExitBlocked: true,
			},
		},
	}
	for night := from; !night.After(to); night = night.AddDays(1) {
		in.Rows = append(in.Rows, NightRow{ResourceID: "s1", Night: night, AllocationID: "a1", Kind: KindContract, Cells: 30})
		in.Rows = append(in.Rows, NightRow{ResourceID: "y1", Night: night, AllocationID: "a3", Kind: KindContract, Cells: 6})
		if night.Before(d("2026-03-04")) {
			in.Rows = append(in.Rows, NightRow{ResourceID: "s1", Night: night, AllocationID: "a2", Kind: KindTransit, Cells: 20})
		}
		if night.Equal(d("2026-03-02")) {
			in.Rows = append(in.Rows, NightRow{ResourceID: "s1", Night: night, AllocationID: "b1", Kind: KindBlock, Cells: 10})
			in.Rows = append(in.Rows, NightRow{ResourceID: "s1", Night: night, AllocationID: "h1", Kind: "hold", Cells: 5})
		}
	}
	return in
}

func TestCalc_StorageReport(t *testing.T) {
	in := storageFixture()
	res, facts := BuildStorage(in)
	if len(res.Records) != 3 {
		t.Fatalf("records = %d", len(res.Records))
	}
	// Available: shed 100 cells × 1 m² × 10 nights − 10 blocked cells × 1 night; yard 10 cells × 4 m² × 10.
	wantAvail := int64(100*10_000*10-10*10_000) + 10*40_000*10
	if res.Totals.AvailableCm2Nights != wantAvail {
		t.Errorf("available = %d, want %d", res.Totals.AvailableCm2Nights, wantAvail)
	}
	wantOcc := int64(30*10_000*10+20*10_000*3) + 6*40_000*10
	if res.Totals.OccupiedCm2Nights != wantOcc {
		t.Errorf("occupied = %d, want %d", res.Totals.OccupiedCm2Nights, wantOcc)
	}
	if res.Totals.Boats != 3 || res.Totals.ExitsBlocked != 1 {
		t.Errorf("boats %d, exits blocked %d (a3 exits after the period)", res.Totals.Boats, res.Totals.ExitsBlocked)
	}
	if !res.Totals.Production.OK || res.Totals.Production.Value != 5000 {
		t.Errorf("production = %+v", res.Totals.Production)
	}
	if got := Ratio(CmToM2Milli(wantOcc), CmToM2Milli(wantAvail)); res.Totals.OccupancyBP != got {
		t.Errorf("occupancy = %+v, want %+v", res.Totals.OccupancyBP, got)
	}
	for _, r := range res.Records {
		if r.AllocationID == "a2" && (r.Nights != 3 || r.HullCm2Nights != 600*250*3 || r.Ref.Type != "transit") {
			t.Errorf("a2 = %+v", r)
		}
	}
	// Facts sum to the totals (INV-62: the tile is the fold of its records).
	s := facts[IndStorageOccupancy].Sum()
	if s.Num != res.Totals.OccupiedCm2Nights || s.Den != res.Totals.AvailableCm2Nights {
		t.Errorf("facts %+v vs totals %+v", s, res.Totals)
	}
	if v := TileValueOf(IndStorageOccupancy, res.Totals); v.Value.Value != res.Totals.OccupancyBP.Value || v.Value.OK != res.Totals.OccupancyBP.OK {
		t.Errorf("tile %+v vs %+v", v, res.Totals.OccupancyBP)
	}
	if v := TileValueOf(IndStorageExitsBlocked, res.Totals); v.Value.Value != facts[IndStorageExitsBlocked].Sum().Num {
		t.Errorf("exits tile %+v", v)
	}
	// Without reports.all: no production.
	in.Full = false
	res, _ = BuildStorage(in)
	if res.Totals.Production.OK || res.Records[0].Production.OK {
		t.Error("production visible without reports.all")
	}
}

func TestCalc_StorageGroups(t *testing.T) {
	for _, gb := range []string{"area", "map_kind", "month", "contract_kind"} {
		in := storageFixture()
		in.GroupBy = gb
		in.To = d("2026-04-05") // two months
		res, _ := BuildStorage(in)
		if len(res.Groups) == 0 {
			t.Fatalf("%s: no groups", gb)
		}
		var occ, boats, hull int64
		var avail int64
		for _, g := range res.Groups {
			occ += g.Totals.OccupiedCm2Nights
			hull += g.Totals.HullCm2Nights
			boats += g.Totals.Boats
			avail += g.Totals.AvailableCm2Nights
		}
		if occ != res.Totals.OccupiedCm2Nights || hull != res.Totals.HullCm2Nights {
			t.Errorf("%s: Σ groups occ %d hull %d, totals %+v", gb, occ, hull, res.Totals)
		}
		if gb != "contract_kind" && avail != res.Totals.AvailableCm2Nights {
			t.Errorf("%s: Σ available %d, total %d", gb, avail, res.Totals.AvailableCm2Nights)
		}
		if gb != "month" && boats != res.Totals.Boats {
			t.Errorf("%s: Σ boats %d", gb, boats)
		}
	}
}

func TestCalc_StorageExitsBlockedOnly(t *testing.T) {
	in := storageFixture()
	in.ExitsBlockedOnly = true
	res, facts := BuildStorage(in)
	if len(res.Records) != 1 || res.Records[0].AllocationID != "a1" {
		t.Fatalf("records = %+v", res.Records)
	}
	if res.Totals.ExitsBlocked != 1 || facts[IndStorageExitsBlocked].Sum().Num != 1 {
		t.Errorf("totals %+v", res.Totals)
	}
	if s := facts[IndStorageOccupancy].Sum(); s.Num != res.Totals.OccupiedCm2Nights {
		t.Errorf("facts %d vs %d", s.Num, res.Totals.OccupiedCm2Nights)
	}
}

func TestCalc_CmToM2Milli(t *testing.T) {
	for cm2, want := range map[int64]int64{10_000: 1000, 15: 2, 25: 2, 35: 4, 0: 0} {
		if got := CmToM2Milli(cm2); got != want {
			t.Errorf("CmToM2Milli(%d) = %d, want %d", cm2, got, want)
		}
	}
}

func TestCalc_Competence(t *testing.T) {
	// 100 € over 10 nights: 3 nights in the period.
	got := Competence(10_000, d("2026-03-01"), d("2026-03-11"), d("2026-03-08"), d("2026-03-31"))
	if got != 3000 {
		t.Errorf("competence = %d", got)
	}
	if Competence(10_000, d("2026-03-01"), d("2026-03-11"), d("2026-04-01"), d("2026-04-30")) != 0 {
		t.Error("competence outside the period")
	}
}

func TestRapid_StorageFactsSumToTotals(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := StorageInput{From: genDate(t, "from", 0, 30), Full: rapid.Bool().Draw(t, "full"), Allocations: map[string]*StorageAllocation{}}
		in.To = in.From.AddDays(rapid.IntRange(0, 60).Draw(t, "len"))
		in.GroupBy = rapid.SampledFrom([]string{"", "area", "map_kind", "month", "contract_kind"}).Draw(t, "gb")
		in.ExitsBlockedOnly = rapid.Bool().Draw(t, "ebo")
		na := rapid.IntRange(1, 3).Draw(t, "areas")
		for i := range na {
			in.Areas = append(in.Areas, StorageArea{
				ID: "s" + strconv.Itoa(i), Code: "S" + strconv.Itoa(i), MapKind: rapid.SampledFrom([]string{"yard", "shed"}).Draw(t, "mk"),
				CellCm: geoCm(int64(rapid.IntRange(33, 200).Draw(t, "cell"))), UsableCells: int64(rapid.IntRange(10, 400).Draw(t, "usable")),
			})
		}
		kinds := []string{KindContract, KindTransit, KindOwner, "hold", KindBlock, KindMaintenance}
		for a := range rapid.IntRange(0, 8).Draw(t, "allocs") {
			id := "a" + strconv.Itoa(a)
			area := "s" + strconv.Itoa(rapid.IntRange(0, na-1).Draw(t, "area"+id))
			kind := rapid.SampledFrom(kinds).Draw(t, "kind"+id)
			first := in.From.AddDays(rapid.IntRange(-5, 40).Draw(t, "first"+id))
			last := first.AddDays(rapid.IntRange(0, 30).Draw(t, "n"+id))
			cells := int64(rapid.IntRange(1, 9).Draw(t, "cells"+id))
			if Occupied(kind) {
				in.Allocations[id] = &StorageAllocation{
					ID: id, AreaID: area, Kind: kind, ContractKind: rapid.SampledFrom([]string{ContractStorage, ContractTransit, ""}).Draw(t, "ck"+id),
					LOA: 700, Beam: 250, ExitOn: last.AddDays(1), ExitBlocked: rapid.Bool().Draw(t, "blk"+id), Production: Some(money.Cents(100)),
				}
			}
			for n := first; !n.After(last); n = n.AddDays(1) {
				in.Rows = append(in.Rows, NightRow{ResourceID: area, Night: n, AllocationID: id, Kind: kind, Cells: cells})
			}
		}
		res, facts := BuildStorage(in)
		s := facts[IndStorageOccupancy].Sum()
		if s.Num != res.Totals.OccupiedCm2Nights || s.Den != res.Totals.AvailableCm2Nights {
			t.Fatalf("facts %+v vs totals %+v", s, res.Totals)
		}
		if e := facts[IndStorageExitsBlocked].Sum().Num; e != res.Totals.ExitsBlocked {
			t.Fatalf("exits facts %d vs %d", e, res.Totals.ExitsBlocked)
		}
		var occ, milli int64
		for _, r := range res.Records {
			occ += r.FootprintCm2Nights
			milli += r.FootprintM2Milli
		}
		if milli != res.Totals.OccupiedM2Milli || TileValueOf(IndStorageOccupancy, res.Totals).Numerator != milli {
			t.Fatalf("Σ record milli %d vs %+v", milli, res.Totals)
		}
		if occ != res.Totals.OccupiedCm2Nights || int64(len(res.Records)) != res.Totals.Boats {
			t.Fatalf("Σ records %d vs %+v", occ, res.Totals)
		}
		if in.GroupBy == "area" || in.GroupBy == "map_kind" || in.GroupBy == "month" {
			var gocc, gav int64
			for _, g := range res.Groups {
				gocc += g.Totals.OccupiedCm2Nights
				gav += g.Totals.AvailableCm2Nights
			}
			if gocc != res.Totals.OccupiedCm2Nights || gav != res.Totals.AvailableCm2Nights {
				t.Fatalf("%s: Σ groups %d/%d vs %+v", in.GroupBy, gocc, gav, res.Totals)
			}
		}
	})
}

func TestCalc_MaintenanceReport(t *testing.T) {
	today := d("2026-05-15")
	tasks := []MaintenanceTask{
		{
			ID: "t1", ElementType: "berth", ElementID: "b1", ElementCode: "A1", Intervention: "chain_check", Source: "plan",
			Priority: "normal", Status: TaskOpen, DueOn: d("2026-05-10"), CreatedOn: d("2026-04-01"),
		},
		{
			ID: "t2", ElementType: "pedestal", ElementID: "p1", ElementCode: "P1", Source: "report", Priority: "urgent",
			Status: TaskDone, DueOn: d("2026-05-01"), DoneOn: d("2026-05-04"), CreatedOn: d("2026-04-20"),
			InternalMinutes: 90, ExternalCost: 12_000,
		},
		{
			ID: "t3", ElementType: "equipment", ElementID: "e1", ElementCode: "TL1", Source: "office", Priority: "low",
			Status: TaskCancelled, CreatedOn: d("2026-01-01"), ClosedOn: d("2026-01-05"),
		},
		{
			ID: "t4", ElementType: "berth", ElementID: "b2", ElementCode: "A2", Source: "office", Priority: "high",
			Status: TaskScheduled, DueOn: d("2026-06-01"), CreatedOn: d("2026-05-01"),
		},
		{ID: "t5", Source: "report", Priority: "normal", Status: TaskOpen, CreatedOn: d("2026-06-01")}, // after the period
	}
	in := MaintenanceInput{From: d("2026-05-01"), To: d("2026-05-31"), Today: today, Full: true, Tasks: tasks}
	res, facts := BuildMaintenance(in)
	if len(res.Records) != 3 {
		t.Fatalf("records = %+v", res.Records)
	}
	if res.Records[0].TaskID != "t1" || !res.Records[0].Overdue || res.Records[0].OverdueDays != 5 {
		t.Errorf("first = %+v", res.Records[0])
	}
	tot := res.Totals
	if tot.Tasks != 3 || tot.Open != 1 || tot.Done != 1 || tot.Scheduled != 1 || tot.Overdue != 1 ||
		tot.InternalMinutes != 90 || tot.ExternalCost.Value != 12_000 {
		t.Errorf("totals = %+v", tot)
	}
	for _, r := range res.Records {
		if r.TaskID == "t2" && (r.OverdueDays != 3 || r.Overdue) {
			t.Errorf("done late = %+v", r)
		}
	}
	if facts[IndMaintenanceOverdue].Sum().Num != TileValueOf(IndMaintenanceOverdue, tot).Value.Value {
		t.Error("overdue facts ≠ tile")
	}
	in.OverdueOnly, in.Full = true, false
	res, _ = BuildMaintenance(in)
	if len(res.Records) != 1 || res.Totals.ExternalCost.OK {
		t.Errorf("overdue only = %+v", res)
	}
	in.OverdueOnly, in.GroupBy = false, "status"
	in.Status = TaskDone
	res, _ = BuildMaintenance(in)
	if len(res.Records) != 1 || len(res.Groups) != 1 || res.Groups[0].Key != TaskDone {
		t.Errorf("status filter = %+v", res)
	}
	in.Status, in.ElementType, in.GroupBy = "", "berth", "element_type"
	res, _ = BuildMaintenance(in)
	if len(res.Records) != 2 || res.Groups[0].Totals.Tasks != 2 {
		t.Errorf("element filter = %+v", res)
	}
	// January: only the cancelled task closed then.
	res, _ = BuildMaintenance(MaintenanceInput{From: d("2026-01-01"), To: d("2026-01-31"), Today: today, Tasks: tasks})
	if len(res.Records) != 1 || res.Records[0].TaskID != "t3" {
		t.Errorf("january = %+v", res.Records)
	}
}

func TestCalc_UtilitiesReport(t *testing.T) {
	cs := []UtilityConsumption{
		{
			ID: "u1", MeterID: "m1", MeterCode: "P01", Kind: MeterPower, ContractID: "c1", ContractCode: "ORM-1",
			First: d("2026-06-01"), Last: d("2026-06-30"), QtyMilli: 12_345, Status: ConsumptionCharged, Charged: Some(money.Cents(370)),
		},
		{
			ID: "u2", MeterID: "m1", MeterCode: "P01", Kind: MeterPower, ContractID: "c1", ContractCode: "ORM-1",
			First: d("2026-07-01"), Last: d("2026-07-10"), QtyMilli: 2_000, Status: ConsumptionPending, PendingValue: Some(money.Cents(60)),
		},
		{
			ID: "u3", MeterID: "m2", MeterCode: "W01", Kind: MeterWater, First: d("2026-07-01"), Last: d("2026-07-10"),
			QtyMilli: 1_500, Status: ConsumptionUnassigned,
		},
		{
			ID: "u4", MeterID: "m2", MeterCode: "W01", Kind: MeterWater, First: d("2026-07-01"), Last: d("2026-07-10"),
			QtyMilli: 700, Status: ConsumptionExcluded,
		},
		{
			ID: "u5", MeterID: "m3", MeterCode: "W02", Kind: MeterWater, ContractID: "c2", First: d("2026-07-01"),
			Last: d("2026-07-02"), QtyMilli: 100, Status: ConsumptionPending,
		},
		{
			ID: "u6", MeterID: "m1", MeterCode: "P01", Kind: MeterPower, First: d("2025-01-01"), Last: d("2025-01-31"),
			QtyMilli: 9, Status: ConsumptionPending,
		}, // outside
	}
	in := UtilitiesInput{From: d("2026-06-01"), To: d("2026-07-31"), Full: true, GroupBy: "kind", Consumptions: cs}
	res, facts := BuildUtilities(in)
	if len(res.Records) != 5 {
		t.Fatalf("records = %d", len(res.Records))
	}
	tot := res.Totals
	if tot.PowerMilli != 14_345 || tot.WaterMilli != 1_600 || tot.PendingMilli != 2_100 || tot.UnassignedMilli != 1_500 ||
		tot.ChargedMilli != 12_345 || tot.Charged.Value != 370 || tot.PendingValue.Value != 60 {
		t.Errorf("totals = %+v", tot)
	}
	if len(res.Notes) != 1 || res.Notes[0].Code != NoteNoPrice || res.Notes[0].Count != 1 {
		t.Errorf("notes = %+v", res.Notes)
	}
	if facts[IndUtilityUnbilled].Sum().Num != TileValueOf(IndUtilityUnbilled, tot).Value.Value {
		t.Error("unbilled facts ≠ tile")
	}
	var power int64
	for _, g := range res.Groups {
		power += g.Totals.PowerMilli
	}
	if power != tot.PowerMilli {
		t.Errorf("Σ groups power %d", power)
	}
	in.Full, in.Status, in.GroupBy = false, ConsumptionPending, "meter"
	res, _ = BuildUtilities(in)
	if len(res.Records) != 2 || res.Totals.PendingValue.OK || res.Records[0].PendingValue.OK || res.Groups[0].Label != "P01" {
		t.Errorf("pending, operational = %+v", res)
	}
}
