package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/workorder/account"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// R-yard (m10-plan §12) on the M7 machinery: one read transaction, the pure builder calc.BuildYard, the margins
// from the pure workorder/account (the same account as the order page), tiles = Totals of the drill views.

// YardResult is the yard report.
type YardResult = calc.YardResult

// Yard lift window defaults (yard_settings.lift_from_slot/lift_to_slot when never saved).
const (
	yardLiftFrom = 28
	yardLiftTo   = 76
)

// authorizeYard checks reports.all, else reports.yard (money visible with both, D32). Another marina is
// auth.ErrNotFound.
func (s *Service) authorizeYard(ctx context.Context, p *auth.Principal, marinaID string) (access, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermReportsAll, auth.PermReportsYard)
	if err != nil {
		return access{}, err
	}
	return access{Scope: scope, Full: true}, nil
}

// hasYard reports whether the principal reads the yard report on the marina of acc.
func hasYard(p *auth.Principal, acc access) bool {
	return p.Can(acc.Scope.OrgID(), acc.Scope.ID(), auth.PermReportsAll) || p.Can(acc.Scope.OrgID(), acc.Scope.ID(), auth.PermReportsYard)
}

// CheckYard authorises p for the yard report (handlers call it before answering a parse error).
func (s *Service) CheckYard(ctx context.Context, p *auth.Principal, marinaID string) error {
	_, err := s.authorizeYard(ctx, p, marinaID)
	return err
}

// Yard returns the yard report (reports.yard or reports.all).
func (s *Service) Yard(ctx context.Context, p *auth.Principal, marinaID string, q Params) (*YardResult, error) {
	acc, err := s.authorizeYard(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if q.Report != "" && q.Report != calc.KeyYard {
		return nil, filterErr("report")
	}
	q.Report = calc.KeyYard
	if err := checkPeriod(q.From, q.To); err != nil {
		return nil, err
	}
	if !calc.YardGroupAllowed(orDefault(q.View, calc.YardViewHours), q.GroupBy) {
		return nil, filterErr("group_by")
	}
	var out *YardResult
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r := s.newRun(ctx, tx, p, acc)
		var err error
		if q.From, q.To, err = r.period(q.From, q.To); err != nil {
			return err
		}
		res, _, err := r.runYard(q)
		if err != nil {
			return err
		}
		out = res
		return finish(res, q, r.today)
	})
	return out, err
}

func (r *run) runYard(q Params) (*YardResult, calc.Facts, error) {
	view := orDefault(q.View, calc.YardViewHours)
	in := calc.YardInput{
		From: q.From, To: q.To, View: view, GroupBy: q.GroupBy, EmployeeID: q.EmployeeID, WorkOrderID: q.WorkOrderID,
		WorkTypeID: q.WorkTypeID, EquipmentID: q.EquipmentID, OpenOnly: q.OpenOnly, InvoicedInPeriod: q.InvoicedInPeriod,
	}
	var err error
	switch view {
	case calc.YardViewHours:
		err = r.yardEntries(&in, q)
	case calc.YardViewLoad:
		err = r.yardLoad(&in, q)
	case calc.YardViewMargins, calc.YardViewMarginsWT:
		err = r.yardOrders(&in, q)
	case calc.YardViewLifting:
		err = r.yardBookings(&in, q)
	}
	if err != nil {
		return nil, nil, err
	}
	res, facts := calc.BuildYard(in)
	return &res, facts, nil
}

// orderNumber formats a work order number as the order side does (C<year>-NNNNNN).
func orderNumber(year int, n int64) string { return fmt.Sprintf("C%04d-%06d", year, n) }

func employeeName(first, last string) string { return strings.TrimSpace(first + " " + last) }

func (r *run) yardEntries(in *calc.YardInput, q Params) error {
	rows, err := store.ReportYardEntries(r.ctx, r.tx, r.scope(), q.From, q.To)
	if err != nil {
		return err
	}
	for _, e := range rows {
		in.Entries = append(in.Entries, calc.YardEntry{
			ID: e.ID, Date: e.WorkDate, EmployeeID: e.EmployeeID, EmployeeCode: e.EmployeeCode, EmployeeName: employeeName(e.FirstName, e.LastName),
			Department: e.Department, WorkOrderID: e.WorkOrderID, WorkOrderNumber: orderNumber(e.OrderYear, e.OrderNumber),
			WorkTypeID: e.WorkTypeID, WorkTypeCode: e.WorkTypeCode, QualificationID: e.QualificationID, HoursMilli: e.HoursMilli,
			CostCents: money.Cents(e.CostCents),
		})
	}
	return nil
}

func (r *run) yardLoad(in *calc.YardInput, q Params) error {
	if q.GroupBy == "bay" {
		rows, err := store.ReportYardBayAssigned(r.ctx, r.tx, r.scope(), q.From, q.To)
		if err != nil {
			return err
		}
		for _, b := range rows {
			bay := b.Key
			if bay == "" {
				bay = "none"
			}
			in.Bays = append(in.Bays, calc.YardBayDay{Bay: bay, Date: b.WorkDate, AssignedMilli: b.HoursMilli})
		}
		return nil
	}
	staff, err := store.ReportYardStaffOf(r.ctx, r.tx, r.scope(), q.From, q.To)
	if err != nil {
		return err
	}
	assigned, err := store.ReportYardAssigned(r.ctx, r.tx, r.scope(), q.From, q.To)
	if err != nil {
		return err
	}
	actual, err := store.ReportYardActual(r.ctx, r.tx, r.scope(), q.From, q.To)
	if err != nil {
		return err
	}
	type key struct {
		emp string
		day calendar.Date
	}
	asg, act := map[key]int64{}, map[key]int64{}
	for _, a := range assigned {
		asg[key{a.Key, a.WorkDate}] = a.HoursMilli
	}
	for _, a := range actual {
		act[key{a.Key, a.WorkDate}] = a.HoursMilli
	}
	cal := map[string]map[int]int64{}
	for _, c := range staff.Calendars {
		if cal[c.EmployeeID] == nil {
			cal[c.EmployeeID] = map[int]int64{}
		}
		cal[c.EmployeeID][c.Weekday] = int64(c.Minutes)
	}
	quals := map[string][]string{}
	for _, eq := range staff.Qualifications {
		quals[eq.EmployeeID] = append(quals[eq.EmployeeID], eq.QualificationID)
	}
	for _, e := range staff.Employees {
		for _, d := range calendar.Range(q.From, q.To) {
			minutes := cal[e.ID][d.ISOWeekday()]
			var absent int64
			for _, a := range staff.Absences {
				if a.EmployeeID != e.ID || d.Before(a.FromOn) || d.After(a.ToOn) {
					continue
				}
				if a.MinutesPerDay == nil {
					absent = minutes
				} else {
					absent += int64(*a.MinutesPerDay)
				}
			}
			in.Days = append(in.Days, calc.YardEmployeeDay{
				EmployeeID: e.ID, EmployeeCode: e.Code, EmployeeName: employeeName(e.FirstName, e.LastName), Department: e.Department,
				Qualifications: quals[e.ID], Date: d, CapacityMilli: calc.YardCapacityMilli(minutes, absent),
				AssignedMilli: asg[key{e.ID, d}], ActualMilli: act[key{e.ID, d}],
			})
		}
	}
	return nil
}

type rateJSON struct {
	Units       map[string]money.Cents `json:"units"`
	LabourItems map[string]string      `json:"labour_items"`
	VAT         map[string]account.VAT `json:"vat"`
}

func (r *run) yardOrders(in *calc.YardInput, q Params) error {
	data, active, err := store.ReportYardOrders(r.ctx, r.tx, r.scope(), q.From, q.To, r.localDayStart(q.From), r.localDayStart(q.To.AddDays(1)))
	if err != nil {
		return err
	}
	itemType, itemVAT, wtCode := map[string]string{}, map[string]account.VAT{}, map[string]string{}
	for _, it := range data.Items {
		itemType[it.ID] = it.ItemType
		itemVAT[it.ID] = account.VAT{RateBP: it.VATRateBP, Nature: it.VATNature, PriceIncludesVAT: it.PriceIncludesVAT}
	}
	for _, w := range data.WorkTypes {
		wtCode[w.ID] = w.Code
	}
	costs := map[string]money.Cents{}
	for id, c := range data.ItemCosts {
		costs[id] = money.Cents(c)
	}
	empQuals := map[string][]string{}
	for _, eq := range data.EmployeeQuals {
		empQuals[eq.EmployeeID] = append(empQuals[eq.EmployeeID], eq.QualificationID)
	}
	fixedIncl := data.HasSettings && data.Settings.FixedIncludesExternal
	src := map[string]*account.Source{}
	for _, o := range data.Orders {
		var rj rateJSON
		_ = json.Unmarshal(o.RateSnapshot, &rj)
		src[o.ID] = &account.Source{
			Mode: o.BillingMode, FixedIncludesExternal: fixedIncl, OrderWorkType: o.WorkTypeID, Units: rj.Units,
			LabourItems: rj.LabourItems, SnapshotVAT: rj.VAT, ItemType: itemType, ItemVAT: itemVAT, ItemCost: costs,
			TaskWorkType: map[string]string{}, EmployeeQuals: empQuals,
		}
	}
	for _, l := range data.Lines {
		s := src[l.WorkOrderID]
		s.Lines = append(s.Lines, account.BudgetLine{
			LineID: l.ID, ItemID: l.ItemID, WorkTypeID: l.WorkTypeID, QualificationID: l.QualificationID, Qty: qty.Milli(l.QtyMilli),
			Unit: money.Cents(l.UnitCents), Discount: money.Cents(l.DiscountCents), Net: money.Cents(l.NetCents), VAT: money.Cents(l.VATCents),
			Gross: money.Cents(l.GrossCents), UnitCost: money.Cents(l.UnitCostCents), ConsumedQty: (*qty.Milli)(l.ConsumedQtyMilli),
		})
	}
	for _, t := range data.Tasks {
		src[t.WorkOrderID].TaskWorkType[t.ID] = t.WorkTypeID
	}
	for _, e := range data.Entries {
		s := src[e.WorkOrderID]
		s.Entries = append(s.Entries, account.SourceEntry{
			ID: e.ID, EmployeeID: e.EmployeeID, TaskID: e.TaskID, QualificationID: e.QualificationID, WorkTypeID: e.WorkTypeID,
			Hours: qty.Milli(e.HoursMilli), CostCents: money.Cents(e.CostCents), Late: e.Late,
		})
	}
	for _, m := range data.Movements {
		s := src[m.WorkOrderID]
		s.Movements = append(s.Movements, account.SourceMovement{
			ID: m.ID, TaskID: m.TaskID, ItemID: m.ItemID, Return: m.Kind == store.StockReturn, Qty: qty.Milli(m.QtyMilli),
			UnitCost: money.Cents(m.UnitCostCents), UnitPrice: money.Cents(m.UnitPriceCents), Late: m.Late,
		})
	}
	for _, x := range data.External {
		s := src[x.WorkOrderID]
		s.External = append(s.External, account.ExternalCost{
			ID: x.ID, ItemID: x.ItemID, WorkTypeID: x.WorkTypeID, Cost: money.Cents(x.CostCents), Billable: x.Billable,
			Price: money.Cents(x.PriceCents), Late: x.Late,
		})
	}
	for _, o := range data.Orders {
		a, err := account.Build(account.Assemble(*src[o.ID]))
		if errors.Is(err, account.ErrInvalidMode) {
			continue
		}
		if err != nil {
			return err
		}
		t := a.Totals
		yo := calc.YardOrder{
			ID: o.ID, Number: orderNumber(o.Year, o.Number), Status: o.Status, BillingMode: o.BillingMode,
			CreatedOn: calendar.In(o.CreatedAt, r.loc), Active: active[o.ID], Revenue: t.Revenue, Cost: t.Cost,
			CostLabour: t.CostLabour, CostMat: t.CostMaterial, CostService: t.CostService, CostExt: t.CostExt, QuoteNet: a.QuoteNet,
			HoursMilli: int64(t.HoursLabour),
		}
		if !o.InvoicedAt.IsZero() {
			yo.InvoicedOn = calendar.In(o.InvoicedAt, r.loc)
		}
		for _, w := range a.ByWorkType {
			yo.ByWorkType = append(yo.ByWorkType, calc.YardWorkTypeFigures{
				WorkTypeID: w.WorkTypeID, WorkTypeCode: wtCode[w.WorkTypeID], Revenue: w.Revenue, Cost: w.Cost,
			})
		}
		in.Orders = append(in.Orders, yo)
	}
	return nil
}

func (r *run) yardBookings(in *calc.YardInput, q Params) error {
	rows, eq, err := store.ReportYardBookings(r.ctx, r.tx, r.scope(), q.From, q.To)
	if err != nil {
		return err
	}
	for _, b := range rows {
		in.Bookings = append(in.Bookings, calc.YardBooking{
			ID: b.ID, EquipmentID: b.EquipmentID, EquipmentCode: b.EquipmentCode, Date: b.Date, FromSlot: b.FromSlot, ToSlot: b.ToSlot,
			Purpose: b.Purpose, Status: b.Status, WorkOrderID: b.WorkOrderID,
		})
	}
	in.Equipment = eq
	from, to := yardLiftFrom, yardLiftTo
	if set, ok, err := store.WorkOrderYardSettings(r.ctx, r.tx, r.scope()); err != nil {
		return err
	} else if ok && set.LiftToSlot > set.LiftFromSlot {
		from, to = set.LiftFromSlot, set.LiftToSlot
	}
	in.WindowSlotsDay = int64(to - from)
	return nil
}
