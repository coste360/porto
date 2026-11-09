package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Report R-rental (M11, m11-plan §11): views utilisation, revenue, damages, deposits, on the shared serveReport
// body. Owned by FLEET.

// GetRentalReport implements api.ServerInterface (GET .../reports/rental).
func (h *Handler) GetRentalReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetRentalReportParams) {
	q, err := report.ParseParams(calc.KeyRental, r.URL.Query())
	if err != nil {
		// Authorise first: another marina is 404, a role without reports.rental 403, never 422.
		if cerr := h.Reports.CheckRental(r.Context(), principal(r), marinaID); cerr != nil {
			respond.Error(w, r, cerr)
			return
		}
		respond.Error(w, r, err)
		return
	}
	viewName := q.View
	if viewName == "" {
		viewName = calc.RentalViewUtilisation
	}
	serveReport(h, w, r, marinaID, calc.KeyRental, h.Reports.Rental, resultView(
		func(x *calc.RentalResult) *calc.RentalResult { return x },
		func(x *calc.RentalResult) any { return rentalReportJSON(x, viewName) },
		func(x *calc.RentalResult, lang string) export.Table { return export.Rental(x, viewName, lang) }))
}

func rentalReportJSON(x *calc.RentalResult, viewName string) api.RentalReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.RentalTotals) api.RentalReportTotals {
		return api.RentalReportTotals{
			Records: t.Records, AvailableSlots: t.AvailableSlots.Ptr(), RentedSlots: t.RentedSlots.Ptr(), UtilisationBp: t.UtilisationBP.Ptr(),
			NetCents: t.Net.Ptr(), VatCents: t.VAT.Ptr(), GrossCents: t.Gross.Ptr(), EstimateCents: t.Estimate.Ptr(),
			RetainedCents: t.Retained.Ptr(), DepositCents: t.Deposit.Ptr(), ReleasedCents: t.Released.Ptr(), OutstandingCents: t.Outstanding.Ptr(),
		}
	}
	return api.RentalReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals), View: api.RentalReportView(viewName),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.RentalTotals]) api.RentalReportGroup {
			return api.RentalReportGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.RentalRecord) api.RentalReportRecord {
			return api.RentalReportRecord{
				Date: rpD(r.Date), UnitId: rpS(r.UnitID), UnitCode: rpS(r.UnitCode), Category: rpE[api.RentalCategory](r.Category),
				FormulaId: rpS(r.FormulaID), FormulaCode: rpS(r.FormulaCode), BookingId: rpS(r.BookingID), BookingNumber: rpS(r.BookingNumber),
				LineKind: rpS(r.LineKind), ChargeId: rpS(r.ChargeID), AvailableSlots: r.AvailableSlots.Ptr(), RentedSlots: r.RentedSlots.Ptr(),
				UtilisationBp: r.UtilisationBP.Ptr(), NetCents: r.Net.Ptr(), VatCents: r.VAT.Ptr(), GrossCents: r.Gross.Ptr(),
				DamageId: rpS(r.DamageID), DamageStatus: rpE[api.RentalDamageStatus](r.DamageStatus), EstimateCents: r.Estimate.Ptr(),
				RetainedCents: r.Retained.Ptr(), DepositMode: rpE[api.RentalDepositMode](r.DepositMode),
				DepositStatus: rpE[api.RentalDepositStatus](r.DepositStatus), DepositCents: r.Deposit.Ptr(), ReleasedCents: r.Released.Ptr(),
				OutstandingCents: r.Outstanding.Ptr(), Ref: refJSON(r.Ref),
			}
		}),
	}
}
