package calc

import (
	"testing"

	"github.com/coste360/porto/internal/calendar"
)

func yd(s string) calendar.Date { return calendar.MustParse(s) }

func TestYard_HoursAndGroups(t *testing.T) {
	in := YardInput{
		From: yd("2027-06-01"), To: yd("2027-06-30"), View: YardViewHours, GroupBy: "employee",
		Entries: []YardEntry{
			{ID: "1", Date: yd("2027-06-02"), EmployeeID: "e1", EmployeeCode: "LUC", WorkOrderID: "o1", WorkOrderNumber: "C2027-000001", HoursMilli: 5000, CostCents: 11000},
			{ID: "2", Date: yd("2027-06-03"), EmployeeID: "e2", EmployeeCode: "MAR", WorkOrderID: "o1", WorkOrderNumber: "C2027-000001", HoursMilli: 4000, CostCents: 9600},
			{ID: "3", Date: yd("2027-07-01"), EmployeeID: "e2", EmployeeCode: "MAR", WorkOrderID: "o1", HoursMilli: 1000, CostCents: 2400},
		},
	}
	res, facts := BuildYard(in)
	if len(res.Records) != 2 || res.Totals.HoursMilli != 9000 || res.Totals.Cost.Value != 20600 || len(res.Groups) != 2 {
		t.Fatalf("hours %+v", res.Totals)
	}
	var sum int64
	for _, f := range facts[IndYardHours] {
		sum += f.Num
	}
	if sum != 9000 || TileValueOf(IndYardHours, res.Totals).Value.Value != 9000 {
		t.Fatalf("tile = Σ drill: %d", sum)
	}
	in.EmployeeID = "e1"
	if res, _ = BuildYard(in); res.Totals.HoursMilli != 5000 {
		t.Fatalf("filter %+v", res.Totals)
	}
}

// TestYard_LoadRatioOfSums: aggregate load = Σ assigned / Σ capacity, never the mean of ratios (INV-64).
func TestYard_LoadRatioOfSums(t *testing.T) {
	in := YardInput{
		From: yd("2027-06-07"), To: yd("2027-06-08"), View: YardViewLoad, GroupBy: "department",
		Days: []YardEmployeeDay{
			{EmployeeID: "e1", EmployeeCode: "A", Department: "verniciatura", Date: yd("2027-06-07"), CapacityMilli: 8000, AssignedMilli: 8000},
			{EmployeeID: "e1", EmployeeCode: "A", Department: "verniciatura", Date: yd("2027-06-08"), CapacityMilli: 2000, AssignedMilli: 0},
			{EmployeeID: "e2", EmployeeCode: "B", Department: "meccanica", Date: yd("2027-06-07"), CapacityMilli: 0, AssignedMilli: 1000},
		},
	}
	res, _ := BuildYard(in)
	if res.Totals.Capacity.Value != 10000 || res.Totals.Assigned.Value != 9000 || res.Totals.LoadBP.Value != 9000 {
		t.Fatalf("totals %+v", res.Totals)
	}
	if !*res.Records[2].Overload || res.Records[2].LoadBP.OK {
		t.Fatalf("zero capacity overload %+v", res.Records[2])
	}
	if YardCapacityMilli(480, 0) != 8000 || YardCapacityMilli(480, 600) != 0 || YardCapacityMilli(50, 0) != 833 {
		t.Fatal("capacity")
	}
}

func TestYard_MarginsAndTiles(t *testing.T) {
	orders := []YardOrder{
		{
			ID: "o1", Number: "C2027-000001", Status: "invoiced", InvoicedOn: yd("2027-06-20"), Revenue: 190090, Cost: 84510, QuoteNet: 170260,
			ByWorkType: []YardWorkTypeFigures{{WorkTypeID: "a", WorkTypeCode: "ANTIFOULING", Revenue: 172090, Cost: 69510}, {WorkTypeID: "p", WorkTypeCode: "PROPULSION", Revenue: 18000, Cost: 15000}},
		},
		{ID: "o2", Number: "C2027-000002", Status: "in_progress", Active: true, Revenue: 10000, Cost: 4000, CreatedOn: yd("2027-06-01")},
		{ID: "o3", Number: "C2027-000003", Status: "planned", CreatedOn: yd("2027-07-15")},
		{ID: "o4", Number: "C2027-000004", Status: "cancelled", Active: true},
	}
	base := YardInput{From: yd("2027-06-01"), To: yd("2027-06-30"), View: YardViewMargins, Orders: orders}
	res, _ := BuildYard(base)
	if len(res.Records) != 2 || res.Totals.Margin.Value != 105580+6000 {
		t.Fatalf("margins %+v", res.Totals)
	}
	inv := base
	inv.InvoicedInPeriod = true
	res, facts := BuildYard(inv)
	if len(res.Records) != 1 || TileValueOf(IndYardMargin, res.Totals).Value.Value != 105580 || res.Totals.MarginBP.Value != 5554 {
		t.Fatalf("invoiced %+v", res.Totals)
	}
	if facts[IndYardMargin][yd("2027-06-20")].Num != 105580 {
		t.Fatal("margin fact")
	}
	open := base
	open.OpenOnly = true
	if res, _ = BuildYard(open); TileValueOf(IndYardOpenOrders, res.Totals).Value.Value != 1 {
		t.Fatalf("open orders %+v", res.Records)
	}
	wt := base
	wt.View, wt.GroupBy = YardViewMarginsWT, "work_type"
	res, _ = BuildYard(wt)
	if len(res.Records) != 2 || len(res.Groups) != 2 || res.Totals.Revenue.Value != 190090 {
		t.Fatalf("by work type %+v", res)
	}
}

func TestYard_Lifting(t *testing.T) {
	in := YardInput{
		From: yd("2027-06-01"), To: yd("2027-06-02"), View: YardViewLifting, Equipment: []string{"tl", "cr"}, WindowSlotsDay: 48,
		Bookings: []YardBooking{
			{ID: "b1", EquipmentID: "tl", EquipmentCode: "TL", Date: yd("2027-06-01"), FromSlot: 32, ToSlot: 36, Status: "booked"},
			{ID: "b2", EquipmentID: "tl", EquipmentCode: "TL", Date: yd("2027-06-02"), FromSlot: 40, ToSlot: 42, Status: "done"},
			{ID: "b3", EquipmentID: "cr", EquipmentCode: "CR", Date: yd("2027-06-02"), FromSlot: 40, ToSlot: 42, Status: "cancelled"},
		},
	}
	res, _ := BuildYard(in)
	if len(res.Records) != 2 || res.Totals.BookedSlots.Value != 6 || res.Totals.WindowSlots.Value != 2*2*48 {
		t.Fatalf("lifting %+v", res.Totals)
	}
	if YardGroupAllowed(YardViewHours, "equipment") || !YardGroupAllowed(YardViewLifting, "equipment") {
		t.Fatal("group_by per view")
	}
}
