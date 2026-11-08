package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Report R-yard (M10, m10-plan §12): views hours, load, margins, margins by work type, lifting, on the shared
// serveReport body. Owned by ORDERS.

// GetYardReport implements api.ServerInterface (GET .../reports/yard).
func (h *Handler) GetYardReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetYardReportParams) {
	q, err := report.ParseParams(calc.KeyYard, r.URL.Query())
	if err != nil {
		// Authorise first with the yard permissions: another marina is 404, a role without them 403, never 422.
		if cerr := h.Reports.CheckYard(r.Context(), principal(r), marinaID); cerr != nil {
			respond.Error(w, r, cerr)
			return
		}
		respond.Error(w, r, err)
		return
	}
	viewName := q.View
	if viewName == "" {
		viewName = calc.YardViewHours
	}
	serveReport(h, w, r, marinaID, calc.KeyYard, h.Reports.Yard, resultView(
		func(x *calc.YardResult) *calc.YardResult { return x },
		func(x *calc.YardResult) any { return yardReportJSON(x, viewName) },
		func(x *calc.YardResult, lang string) export.Table { return export.Yard(x, viewName, lang) }))
}

func yardReportJSON(x *calc.YardResult, viewName string) api.YardReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.YardTotals) api.YardReportTotals {
		return api.YardReportTotals{
			Records: t.Records, HoursMilli: t.HoursMilli, CapacityMilli: t.Capacity.Ptr(), AssignedMilli: t.Assigned.Ptr(),
			LoadBp: t.LoadBP.Ptr(), RevenueCents: t.Revenue.Ptr(), CostCents: t.Cost.Ptr(), MarginCents: t.Margin.Ptr(),
			MarginBp: t.MarginBP.Ptr(), QuoteNetCents: t.QuoteNet.Ptr(), BookedSlots: t.BookedSlots.Ptr(), WindowSlots: t.WindowSlots.Ptr(),
		}
	}
	return api.YardReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals), View: api.YardReportView(viewName),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.YardTotals]) api.YardReportGroup {
			return api.YardReportGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.YardRecord) api.YardReportRecord {
			return api.YardReportRecord{
				Date: rpD(r.Date), EmployeeId: rpS(r.EmployeeID), EmployeeCode: rpS(r.EmployeeCode), EmployeeName: rpS(r.EmployeeName),
				Department: rpS(r.Department), Bay: rpS(r.Bay), WorkOrderId: rpS(r.WorkOrderID), WorkOrderNumber: rpS(r.WorkOrderNumber),
				WorkOrderStatus: rpE[api.WorkOrderStatus](r.WorkOrderStatus), BillingMode: rpE[api.YardBillingMode](r.BillingMode),
				WorkTypeId: rpS(r.WorkTypeID), WorkTypeCode: rpS(r.WorkTypeCode), QualificationId: rpS(r.QualificationID),
				HoursMilli: r.HoursMilli.Ptr(), CapacityMilli: r.Capacity.Ptr(), AssignedMilli: r.Assigned.Ptr(), ActualMilli: r.Actual.Ptr(),
				LoadBp: r.LoadBP.Ptr(), Overload: r.Overload, RevenueCents: r.Revenue.Ptr(), CostCents: r.Cost.Ptr(),
				CostLabourCents: r.CostLabour.Ptr(), CostMaterialCents: r.CostMaterial.Ptr(), CostServiceCents: r.CostService.Ptr(),
				CostExternalCents: r.CostExternal.Ptr(), MarginCents: r.Margin.Ptr(), MarginBp: r.MarginBP.Ptr(),
				QuoteNetCents: r.QuoteNet.Ptr(), DeltaNetCents: r.DeltaNet.Ptr(), EquipmentId: rpS(r.EquipmentID),
				EquipmentCode: rpS(r.EquipmentCode), FromSlot: r.FromSlot, ToSlot: r.ToSlot,
				Purpose: rpE[api.EquipmentBookingPurpose](r.Purpose), BookingStatus: rpE[api.EquipmentBookingStatus](r.BookingStatus),
				Ref: refJSON(r.Ref),
			}
		}),
	}
}
