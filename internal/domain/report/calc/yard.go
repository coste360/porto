package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// ---- R-yard (m10-plan §12): views hours, load, margins, margins_by_work_type, lifting ----
//
// The tiles yard_open_orders, yard_hours and yard_margin are the Totals of their drill views (INV-62). Money is
// visible with reports.yard as well as reports.all (D32): the yard report is never served without one of them.

// Yard views.
const (
	YardViewHours      = "hours"
	YardViewLoad       = "load"
	YardViewMargins    = "margins"
	YardViewMarginsWT  = "margins_by_work_type"
	YardViewLifting    = "lifting"
	yardBayNone        = "none"
	yardBookingDone    = "done"
	yardBookingBooked  = "booked"
	yardOrderInvoiced  = "invoiced"
	yardOrderCancelled = "cancelled"
)

// YardViews are the views of the yard report (first = default).
var YardViews = []string{YardViewHours, YardViewLoad, YardViewMargins, YardViewMarginsWT, YardViewLifting}

// Yard indicator keys (OpenAPI TileKey).
const (
	IndYardOpenOrders = string(TileYardOpenOrders)
	IndYardHours      = string(TileYardHours)
	IndYardMargin     = string(TileYardMargin)
)

func init() {
	Indicators[IndYardOpenOrders] = Indicator{Unit: UnitCount}
	// Hours are milli-hours: the tile unit is a count of thousandths of an hour (the UI divides by 1000).
	Indicators[IndYardHours] = Indicator{Unit: UnitCount}
	Indicators[IndYardMargin] = Indicator{Unit: UnitCents}
}

// YardEntry is an approved, non-voided time entry.
type YardEntry struct {
	ID              string
	Date            calendar.Date
	EmployeeID      string
	EmployeeCode    string
	EmployeeName    string
	Department      string
	WorkOrderID     string
	WorkOrderNumber string
	WorkTypeID      string
	WorkTypeCode    string
	QualificationID string
	HoursMilli      int64
	CostCents       money.Cents
}

// YardEmployeeDay is the capacity, assigned and actual hours of an employee on a day (load view).
type YardEmployeeDay struct {
	EmployeeID     string
	EmployeeCode   string
	EmployeeName   string
	Department     string
	Qualifications []string
	Date           calendar.Date
	CapacityMilli  int64
	AssignedMilli  int64
	ActualMilli    int64
}

// YardBayDay is the assigned hours on a day of the tasks of orders sitting at a bay (no capacity, D22).
type YardBayDay struct {
	Bay           string // resource code, "none" without a position
	Date          calendar.Date
	AssignedMilli int64
}

// YardOrder is an order with its final account figures (computed by the runner with workorder/account).
type YardOrder struct {
	ID          string
	Number      string
	Status      string
	BillingMode string
	CreatedOn   calendar.Date
	InvoicedOn  calendar.Date // zero when not invoiced
	Active      bool          // has entries, movements or external costs in the period
	Revenue     money.Cents
	Cost        money.Cents
	CostLabour  money.Cents
	CostMat     money.Cents
	CostService money.Cents
	CostExt     money.Cents
	QuoteNet    money.Cents
	HoursMilli  int64
	ByWorkType  []YardWorkTypeFigures
}

// YardWorkTypeFigures are the account figures of one work type of an order.
type YardWorkTypeFigures struct {
	WorkTypeID   string
	WorkTypeCode string
	Revenue      money.Cents
	Cost         money.Cents
}

// YardBooking is a lifting booking (requested bookings have no window and are left out by the runner).
type YardBooking struct {
	ID            string
	EquipmentID   string
	EquipmentCode string
	Date          calendar.Date
	FromSlot      int
	ToSlot        int
	Purpose       string
	Status        string
	WorkOrderID   string
}

// YardInput is the input of BuildYard.
type YardInput struct {
	From, To         calendar.Date
	View             string
	GroupBy          string
	EmployeeID       string
	WorkOrderID      string
	WorkTypeID       string
	EquipmentID      string
	OpenOnly         bool
	InvoicedInPeriod bool
	Entries          []YardEntry
	Days             []YardEmployeeDay
	Bays             []YardBayDay
	Orders           []YardOrder
	Bookings         []YardBooking
	Equipment        []string // bookable equipment ids (window slots per equipment)
	WindowSlotsDay   int64    // lift_to_slot − lift_from_slot
}

// YardTotals (OpenAPI YardReportTotals).
type YardTotals struct {
	Records     int64
	HoursMilli  int64
	Capacity    Opt[int64]
	Assigned    Opt[int64]
	LoadBP      BP
	Revenue     Opt[money.Cents]
	Cost        Opt[money.Cents]
	Margin      Opt[money.Cents]
	MarginBP    BP
	QuoteNet    Opt[money.Cents]
	BookedSlots Opt[int64]
	WindowSlots Opt[int64]
}

// YardRecord (OpenAPI YardReportRecord): the fields of its view, the others unset.
type YardRecord struct {
	Date            calendar.Date
	EmployeeID      string
	EmployeeCode    string
	EmployeeName    string
	Department      string
	Bay             string
	WorkOrderID     string
	WorkOrderNumber string
	WorkOrderStatus string
	BillingMode     string
	WorkTypeID      string
	WorkTypeCode    string
	QualificationID string
	HoursMilli      Opt[int64]
	Capacity        Opt[int64]
	Assigned        Opt[int64]
	Actual          Opt[int64]
	LoadBP          BP
	Overload        *bool
	Revenue         Opt[money.Cents]
	Cost            Opt[money.Cents]
	CostLabour      Opt[money.Cents]
	CostMaterial    Opt[money.Cents]
	CostService     Opt[money.Cents]
	CostExternal    Opt[money.Cents]
	Margin          Opt[money.Cents]
	MarginBP        BP
	QuoteNet        Opt[money.Cents]
	DeltaNet        Opt[money.Cents]
	EquipmentID     string
	EquipmentCode   string
	FromSlot        *int
	ToSlot          *int
	Purpose         string
	BookingStatus   string
	Qualifications  []string // load: grouping by qualification only
	Ref             Ref
}

// YardResult is the yard report.
type YardResult = Result[YardTotals, YardRecord]

// yardGroups are the group_by values of each view.
var yardGroups = map[string][]string{
	YardViewHours:     {"employee", "order", "work_type", "qualification", "month", "week"},
	YardViewLoad:      {"employee", "department", "qualification", "bay", "week", "month"},
	YardViewMargins:   {"status", "month"},
	YardViewMarginsWT: {"work_type", "order"},
	YardViewLifting:   {"equipment", "status", "month"},
}

// YardGroupAllowed reports whether group_by is valid for the view.
func YardGroupAllowed(view, groupBy string) bool {
	return groupBy == "" || slices.Contains(yardGroups[view], groupBy)
}

// orderOpen reports an order that still takes work.
func orderOpen(status string) bool {
	switch status {
	case "planned", "in_progress", "on_hold", "completed":
		return true
	}
	return false
}

// FoldYard is the only producer of yard totals.
func FoldYard(view string, recs []YardRecord, in YardInput) YardTotals {
	t := YardTotals{Records: int64(len(recs))}
	switch view {
	case YardViewHours:
		var cost money.Cents
		for i := range recs {
			t.HoursMilli += recs[i].HoursMilli.Value
			cost += recs[i].Cost.Value
		}
		t.Cost = Some(cost)
	case YardViewLoad:
		var capacity, assigned, actual int64
		hasCap := false
		for i := range recs {
			if recs[i].Capacity.OK {
				capacity += recs[i].Capacity.Value
				hasCap = true
			}
			assigned += recs[i].Assigned.Value
			actual += recs[i].Actual.Value
		}
		t.Assigned, t.HoursMilli = Some(assigned), actual
		if hasCap {
			t.Capacity = Some(capacity)
			t.LoadBP = Ratio(assigned, capacity)
		}
	case YardViewMargins, YardViewMarginsWT:
		var rev, cost, quote money.Cents
		for i := range recs {
			rev += recs[i].Revenue.Value
			cost += recs[i].Cost.Value
			quote += recs[i].QuoteNet.Value
			t.HoursMilli += recs[i].HoursMilli.Value
		}
		t.Revenue, t.Cost, t.Margin, t.MarginBP = Some(rev), Some(cost), Some(rev-cost), Ratio(int64(rev-cost), int64(rev))
		if view == YardViewMargins {
			t.QuoteNet = Some(quote)
		}
	case YardViewLifting:
		var booked int64
		equipment := map[string]bool{}
		for i := range recs {
			booked += int64(*recs[i].ToSlot - *recs[i].FromSlot)
			equipment[recs[i].EquipmentID] = true
		}
		t.BookedSlots = Some(booked)
		days := int64(in.From.DaysUntil(in.To) + 1)
		n := int64(len(in.Equipment))
		if in.EquipmentID != "" {
			n = 1
		}
		t.WindowSlots = Some(n * days * in.WindowSlotsDay)
	}
	return t
}

// BuildYard builds the yard report for the view of in. Facts: yard_hours (by work date) on the hours view,
// yard_margin (by invoice date) on the margins view.
func BuildYard(in YardInput) (YardResult, Facts) {
	view := in.View
	if view == "" {
		view = YardViewHours
	}
	var recs []YardRecord
	facts := Facts{}
	switch view {
	case YardViewHours:
		d := Daily{}
		for _, e := range in.Entries {
			if !inPeriod(e.Date, in.From, in.To) || (in.EmployeeID != "" && e.EmployeeID != in.EmployeeID) ||
				(in.WorkOrderID != "" && e.WorkOrderID != in.WorkOrderID) || (in.WorkTypeID != "" && e.WorkTypeID != in.WorkTypeID) {
				continue
			}
			recs = append(recs, YardRecord{
				Date: e.Date, EmployeeID: e.EmployeeID, EmployeeCode: e.EmployeeCode, EmployeeName: e.EmployeeName,
				Department: e.Department, WorkOrderID: e.WorkOrderID, WorkOrderNumber: e.WorkOrderNumber, WorkTypeID: e.WorkTypeID,
				WorkTypeCode: e.WorkTypeCode, QualificationID: e.QualificationID, HoursMilli: Some(e.HoursMilli), Cost: Some(e.CostCents),
				Ref: Ref{Type: "work_order", ID: e.WorkOrderID, Label: e.WorkOrderNumber},
			})
			d.Add(e.Date, e.HoursMilli, 0)
		}
		slices.SortStableFunc(recs, func(a, b YardRecord) int {
			if c := cmpDate(a.Date, b.Date); c != 0 {
				return c
			}
			return compareKeys([]string{a.EmployeeCode, a.WorkOrderNumber}, []string{b.EmployeeCode, b.WorkOrderNumber})
		})
		facts[IndYardHours] = d
	case YardViewLoad:
		if in.GroupBy == "bay" {
			for _, b := range in.Bays {
				if !inPeriod(b.Date, in.From, in.To) {
					continue
				}
				recs = append(recs, YardRecord{Date: b.Date, Bay: b.Bay, Assigned: Some(b.AssignedMilli), Ref: Ref{Type: "work_order", Label: b.Bay}})
			}
			slices.SortStableFunc(recs, func(a, b YardRecord) int {
				if c := compareKeys([]string{a.Bay}, []string{b.Bay}); c != 0 {
					return c
				}
				return cmpDate(a.Date, b.Date)
			})
			break
		}
		for _, d := range in.Days {
			if !inPeriod(d.Date, in.From, in.To) || (in.EmployeeID != "" && d.EmployeeID != in.EmployeeID) {
				continue
			}
			over := d.AssignedMilli > d.CapacityMilli
			r := YardRecord{
				Date: d.Date, EmployeeID: d.EmployeeID, EmployeeCode: d.EmployeeCode, EmployeeName: d.EmployeeName,
				Department: d.Department, Capacity: Some(d.CapacityMilli), Assigned: Some(d.AssignedMilli), Actual: Some(d.ActualMilli),
				Overload: &over, Qualifications: d.Qualifications, Ref: Ref{Type: "employee", ID: d.EmployeeID, Label: d.EmployeeCode},
			}
			if d.CapacityMilli > 0 {
				r.LoadBP = Ratio(d.AssignedMilli, d.CapacityMilli)
			}
			recs = append(recs, r)
		}
		slices.SortStableFunc(recs, func(a, b YardRecord) int {
			if c := compareKeys([]string{a.EmployeeCode}, []string{b.EmployeeCode}); c != 0 {
				return c
			}
			return cmpDate(a.Date, b.Date)
		})
	case YardViewMargins, YardViewMarginsWT:
		d := Daily{}
		for _, o := range in.Orders {
			if o.Status == yardOrderCancelled || (in.WorkOrderID != "" && o.ID != in.WorkOrderID) {
				continue
			}
			invoicedIn := !o.InvoicedOn.IsZero() && inPeriod(o.InvoicedOn, in.From, in.To)
			switch {
			case in.OpenOnly:
				if !orderOpen(o.Status) || o.CreatedOn.After(in.To) {
					continue
				}
			case in.InvoicedInPeriod:
				if !invoicedIn {
					continue
				}
			default:
				if !o.Active && !invoicedIn {
					continue
				}
			}
			ref := Ref{Type: "work_order", ID: o.ID, Label: o.Number}
			if view == YardViewMargins {
				m := o.Revenue - o.Cost
				recs = append(recs, YardRecord{
					WorkOrderID: o.ID, WorkOrderNumber: o.Number, WorkOrderStatus: o.Status, BillingMode: o.BillingMode,
					HoursMilli: Some(o.HoursMilli), Revenue: Some(o.Revenue), Cost: Some(o.Cost), CostLabour: Some(o.CostLabour),
					CostMaterial: Some(o.CostMat), CostService: Some(o.CostService), CostExternal: Some(o.CostExt), Margin: Some(m),
					MarginBP: Ratio(int64(m), int64(o.Revenue)), QuoteNet: Some(o.QuoteNet), DeltaNet: Some(o.Revenue - o.QuoteNet),
					Date: o.InvoicedOn, Ref: ref,
				})
				if invoicedIn {
					d.Add(o.InvoicedOn, int64(m), 0)
				}
				continue
			}
			for _, w := range o.ByWorkType {
				if in.WorkTypeID != "" && w.WorkTypeID != in.WorkTypeID {
					continue
				}
				m := w.Revenue - w.Cost
				recs = append(recs, YardRecord{
					WorkOrderID: o.ID, WorkOrderNumber: o.Number, WorkOrderStatus: o.Status, BillingMode: o.BillingMode,
					WorkTypeID: w.WorkTypeID, WorkTypeCode: w.WorkTypeCode, Revenue: Some(w.Revenue), Cost: Some(w.Cost), Margin: Some(m),
					MarginBP: Ratio(int64(m), int64(w.Revenue)), Ref: ref,
				})
			}
		}
		slices.SortStableFunc(recs, func(a, b YardRecord) int {
			return compareKeys([]string{a.WorkOrderNumber, a.WorkTypeCode}, []string{b.WorkOrderNumber, b.WorkTypeCode})
		})
		if view == YardViewMargins && in.InvoicedInPeriod {
			facts[IndYardMargin] = d
		}
	case YardViewLifting:
		for _, b := range in.Bookings {
			if !inPeriod(b.Date, in.From, in.To) || (in.EquipmentID != "" && b.EquipmentID != in.EquipmentID) ||
				(b.Status != yardBookingBooked && b.Status != yardBookingDone) {
				continue
			}
			from, to := b.FromSlot, b.ToSlot
			recs = append(recs, YardRecord{
				Date: b.Date, EquipmentID: b.EquipmentID, EquipmentCode: b.EquipmentCode, FromSlot: &from, ToSlot: &to,
				Purpose: b.Purpose, BookingStatus: b.Status, WorkOrderID: b.WorkOrderID,
				Ref: Ref{Type: "equipment_booking", ID: b.ID, Label: b.EquipmentCode},
			})
		}
		slices.SortStableFunc(recs, func(a, b YardRecord) int {
			if c := cmpDate(a.Date, b.Date); c != 0 {
				return c
			}
			if c := compareKeys([]string{a.EquipmentCode}, []string{b.EquipmentCode}); c != 0 {
				return c
			}
			return *a.FromSlot - *b.FromSlot
		})
	}
	if recs == nil {
		recs = []YardRecord{}
	}
	fold := func(rs []YardRecord) YardTotals { return FoldYard(view, rs, in) }
	res := YardResult{Records: recs, Totals: fold(recs)}
	key := map[string]func(YardRecord) []string{
		"employee":      func(r YardRecord) []string { return []string{orNone(r.EmployeeCode)} },
		"department":    func(r YardRecord) []string { return []string{orNone(r.Department)} },
		"order":         func(r YardRecord) []string { return []string{orNone(r.WorkOrderNumber)} },
		"work_type":     func(r YardRecord) []string { return []string{orNone(r.WorkTypeCode)} },
		"status":        func(r YardRecord) []string { return []string{orNone(r.WorkOrderStatus + r.BookingStatus)} },
		"equipment":     func(r YardRecord) []string { return []string{orNone(r.EquipmentCode)} },
		"bay":           func(r YardRecord) []string { return []string{orNone(r.Bay)} },
		"week":          func(r YardRecord) []string { return dateKey(r.Date, WeekKey) },
		"month":         func(r YardRecord) []string { return dateKey(r.Date, MonthKey) },
		"qualification": yardQualKeys,
	}[in.GroupBy]
	if key != nil && YardGroupAllowed(view, in.GroupBy) {
		res.Groups = GroupRecords(recs, key, fold)
	}
	return res, facts
}

func dateKey(d calendar.Date, f func(calendar.Date) string) []string {
	if d.IsZero() {
		return []string{yardBayNone}
	}
	return []string{f(d)}
}

func yardQualKeys(r YardRecord) []string {
	if r.QualificationID != "" {
		return []string{r.QualificationID}
	}
	if len(r.Qualifications) > 0 {
		return r.Qualifications
	}
	return []string{yardBayNone}
}

// YardCapacityMilli is the capacity of an employee on a day: calendar minutes of the weekday minus absence minutes
// (≥ 0), as milli-hours (minutes × 1000 / 60 half-even, D21; marina closures do not apply).
func YardCapacityMilli(calendarMinutes, absenceMinutes int64) int64 {
	m := max(calendarMinutes-absenceMinutes, 0)
	return money.DivRoundHalfEven(m*1000, 60)
}
