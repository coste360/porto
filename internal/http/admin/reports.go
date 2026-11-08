package admin

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/money"
)

// Reports of M7 (m7-plan §3, §7, §8). Every handler parses the raw query string with report.ParseParams (the
// exact inverse of Params.Values, so a dashboard drill link reproduces the request), calls the report service
// (which authorises: another marina 404, no report permission 403) and answers JSON, or CSV/XLSX built by
// export from the very same result (INV-63). Handlers log only the report key, format, period length, record
// count and duration (no PII).

// Content types of the report downloads.
const (
	reportCSVType  = "text/csv; charset=utf-8"
	reportXLSXType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// reportRun is a report method of the service.
type reportRun[Res any] func(ctx context.Context, p *auth.Principal, marinaID string, q report.Params) (*Res, error)

// reportView is how a handler reads and encodes one report result.
type reportView[Res any] struct {
	env   func(*Res) *calc.Envelope
	count func(*Res) int
	json  func(*Res) any
	table func(*Res, string) export.Table
}

// serveReport is the shared body of the report handlers.
func serveReport[Res any](h *Handler, w http.ResponseWriter, r *http.Request, marinaID string, key calc.ReportKey, run reportRun[Res], v reportView[Res]) {
	ctx := r.Context()
	p := principal(r)
	start := h.Clock.Now()
	q, err := report.ParseParams(key, r.URL.Query())
	if err != nil {
		// Authorise first: another marina is 404 and a role without reports 403, never 422.
		if cerr := h.Reports.Check(ctx, p, marinaID); cerr != nil {
			respond.Error(w, r, cerr)
			return
		}
		respond.Error(w, r, err)
		return
	}
	q.Report = key
	if r.URL.Query().Get("format") == "" && acceptsCSV(r.Header.Get("Accept")) {
		q.Format = report.FormatCSV
	}
	if q.Lang == "" && p != nil {
		q.Lang = p.Locale
	}
	if q.Lang != "en" {
		q.Lang = "it"
	}
	res, err := run(ctx, p, marinaID, q)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	e := v.env(res)
	log := respond.Logger(ctx)
	defer func() {
		log.Info("report served", "report", string(key), "format", string(q.Format),
			"period_days", e.From.DaysUntil(e.To)+1, "records", v.count(res),
			"duration_ms", h.Clock.Now().Sub(start).Milliseconds())
	}()
	switch q.Format {
	case report.FormatCSV, report.FormatXLSX:
		if v.count(res) > report.MaxExportRows {
			respond.Error(w, r, report.ErrTooLarge)
			return
		}
		t := v.table(res, q.Lang)
		var b bytes.Buffer
		ext, ctype := "csv", reportCSVType
		if q.Format == report.FormatXLSX {
			ext, ctype = "xlsx", reportXLSXType
			err = export.WriteXLSX(&b, t)
		} else {
			err = export.WriteCSV(&b, t)
		}
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeDownload(w, t.Filename(ext), ctype, b.Bytes())
	default:
		respond.JSON(w, http.StatusOK, v.json(res))
	}
}

// acceptsCSV reports an Accept header asking for text/csv (the format parameter wins when present).
func acceptsCSV(accept string) bool {
	for part := range strings.SplitSeq(accept, ",") {
		mt, _, _ := strings.Cut(part, ";")
		if strings.EqualFold(strings.TrimSpace(mt), "text/csv") {
			return true
		}
	}
	return false
}

// resultView builds the reportView of a Result-shaped report.
func resultView[T, R, Res any](res func(*Res) *calc.Result[T, R], j func(*Res) any, t func(*Res, string) export.Table) reportView[Res] {
	return reportView[Res]{
		env:   func(x *Res) *calc.Envelope { return &res(x).Envelope },
		count: func(x *Res) int { return len(res(x).Records) },
		json:  j,
		table: t,
	}
}

// GetOccupancyReport implements api.ServerInterface (GET .../reports/occupancy).
func (h *Handler) GetOccupancyReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetOccupancyReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyOccupancy, h.Reports.Occupancy, resultView(
		func(x *calc.OccupancyResult) *calc.Result[calc.OccupancyTotals, calc.OccupancyRecord] {
			return &x.Result
		},
		func(x *calc.OccupancyResult) any { return occupancyJSON(x) }, export.Occupancy))
}

// GetEfficiencyReport implements api.ServerInterface (GET .../reports/efficiency).
func (h *Handler) GetEfficiencyReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetEfficiencyReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyEfficiency, h.Reports.Efficiency, resultView(
		func(x *calc.EfficiencyResult) *calc.Result[calc.EfficiencyTotals, calc.EfficiencyRecord] {
			return &x.Result
		},
		func(x *calc.EfficiencyResult) any { return efficiencyJSON(x) }, export.Efficiency))
}

// GetAllocationWasteReport implements api.ServerInterface (GET .../reports/allocation-waste).
func (h *Handler) GetAllocationWasteReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetAllocationWasteReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyAllocationWaste, h.Reports.AllocationWaste, resultView(
		func(x *calc.WasteResult) *calc.Result[calc.WasteTotals, calc.WasteRecord] { return &x.Result },
		func(x *calc.WasteResult) any { return wasteJSON(x) }, export.Waste))
}

// GetLostDemandReport implements api.ServerInterface (GET .../reports/lost-demand).
func (h *Handler) GetLostDemandReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetLostDemandReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyLostDemand, h.Reports.LostDemand, resultView(
		func(x *calc.LostDemandResult) *calc.LostDemandResult { return x },
		func(x *calc.LostDemandResult) any { return lostDemandJSON(x) }, export.LostDemand))
}

// GetCustomersReport implements api.ServerInterface (GET .../reports/customers).
func (h *Handler) GetCustomersReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetCustomersReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyCustomers, h.Reports.Customers, resultView(
		func(x *calc.CustomersResult) *calc.CustomersResult { return x },
		func(x *calc.CustomersResult) any { return customersJSON(x) }, export.Customers))
}

// GetProductionReport implements api.ServerInterface (GET .../reports/production).
func (h *Handler) GetProductionReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetProductionReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyProduction, h.Reports.Production, resultView(
		func(x *calc.ProductionResult) *calc.ProductionResult { return x },
		func(x *calc.ProductionResult) any { return productionJSON(x) }, export.Production))
}

// GetAccrualsReport implements api.ServerInterface (GET .../reports/accruals).
func (h *Handler) GetAccrualsReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetAccrualsReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyAccruals, h.Reports.Accruals, resultView(
		func(x *calc.AccrualsResult) *calc.AccrualsResult { return x },
		func(x *calc.AccrualsResult) any { return accrualsJSON(x) }, export.Accruals))
}

// GetChargesReport implements api.ServerInterface (GET .../reports/charges).
func (h *Handler) GetChargesReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetChargesReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyCharges, h.Reports.Charges, resultView(
		func(x *calc.ChargesResult) *calc.Result[calc.ChargesTotals, calc.ChargesRecord] { return &x.Result },
		func(x *calc.ChargesResult) any { return chargesReportJSON(x) }, export.Charges))
}

// GetPaymentsReport implements api.ServerInterface (GET .../reports/payments).
func (h *Handler) GetPaymentsReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetPaymentsReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyPayments, h.Reports.Payments, resultView(
		func(x *calc.PaymentsResult) *calc.PaymentsResult { return x },
		func(x *calc.PaymentsResult) any { return paymentsReportJSON(x) }, export.Payments))
}

// GetRevenueClassesReport implements api.ServerInterface (GET .../reports/revenue-classes).
func (h *Handler) GetRevenueClassesReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetRevenueClassesReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyRevenueClasses, h.Reports.RevenueClasses, reportView[calc.RevenueClassesResult]{
		env:   func(x *calc.RevenueClassesResult) *calc.Envelope { return &x.Envelope },
		count: func(x *calc.RevenueClassesResult) int { return len(x.Records) },
		json:  func(x *calc.RevenueClassesResult) any { return revenueClassesJSON(x) },
		table: export.RevenueClasses,
	})
}

// GetReceivablesReport implements api.ServerInterface (GET .../reports/receivables).
func (h *Handler) GetReceivablesReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetReceivablesReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyReceivables, h.Reports.Receivables, resultView(
		func(x *calc.ReceivablesResult) *calc.ReceivablesResult { return x },
		func(x *calc.ReceivablesResult) any { return receivablesReportJSON(x) }, export.Receivables))
}

// GetPresencesReport implements api.ServerInterface (GET .../reports/presences).
func (h *Handler) GetPresencesReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetPresencesReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyPresences, h.Reports.Presences, resultView(
		func(x *calc.PresencesResult) *calc.PresencesResult { return x },
		func(x *calc.PresencesResult) any { return presencesReportJSON(x) }, export.Presences))
}

// GetRightsReport implements api.ServerInterface (GET .../reports/rights).
func (h *Handler) GetRightsReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetRightsReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyRights, h.Reports.Rights, resultView(
		func(x *calc.RightsResult) *calc.RightsResult { return x },
		func(x *calc.RightsResult) any { return rightsReportJSON(x) }, export.Rights))
}

// GetCashReport implements api.ServerInterface (GET .../reports/cash).
func (h *Handler) GetCashReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetCashReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyCash, h.Reports.Cash, resultView(
		func(x *calc.CashResult) *calc.CashResult { return x },
		func(x *calc.CashResult) any { return cashReportJSON(x) }, export.Cash))
}

// GetVesselsReport implements api.ServerInterface (GET .../reports/vessels).
func (h *Handler) GetVesselsReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetVesselsReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyVessels, h.Reports.Vessels, resultView(
		func(x *calc.VesselsResult) *calc.VesselsResult { return x },
		func(x *calc.VesselsResult) any { return vesselsReportJSON(x) }, export.Vessels))
}

// GetBerthRevenueReport implements api.ServerInterface (GET .../reports/berth-revenue).
func (h *Handler) GetBerthRevenueReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetBerthRevenueReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyBerthRevenue, h.Reports.BerthRevenue, resultView(
		func(x *calc.BerthRevenueResult) *calc.BerthRevenueResult { return x },
		func(x *calc.BerthRevenueResult) any { return berthRevenueJSON(x) }, export.BerthRevenue))
}

// ---- JSON encoding helpers ----

// envelopeJSON holds the common fields of every report response.
type envelopeJSON struct {
	filters map[string]string
	groupBy *string
	ignored []string
	notes   []api.ReportNote
	page    api.ReportPageInfo
	period  api.ReportPeriod
	report  api.ReportKey
	section *string
	today   string
}

func envJSON(e *calc.Envelope) envelopeJSON {
	out := envelopeJSON{
		filters: e.Filters,
		groupBy: rpS(e.GroupBy),
		ignored: e.IgnoredFilters,
		notes:   make([]api.ReportNote, 0, len(e.Notes)),
		page:    api.ReportPageInfo{Page: e.Page.Page, PageSize: e.Page.PageSize, TotalRecords: e.Page.Total},
		period:  api.ReportPeriod{From: e.From.String(), To: e.To.String()},
		report:  api.ReportKey(e.Report),
		section: rpS(e.Section),
		today:   e.Today.String(),
	}
	if out.filters == nil {
		out.filters = map[string]string{}
	}
	if out.ignored == nil {
		out.ignored = []string{}
	}
	for _, n := range e.Notes {
		out.notes = append(out.notes, api.ReportNote{Code: n.Code, Count: n.Count})
	}
	return out
}

// rpS is a pointer to a non-empty string.
func rpS(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// rpE is a pointer to a non-empty enumeration value.
func rpE[T ~string](s string) *T {
	if s == "" {
		return nil
	}
	v := T(s)
	return &v
}

// rpD is a pointer to a non-zero date.
func rpD(d calendar.Date) *string {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

// rpI is a pointer to an integer.
func rpI[T ~int64 | ~int](n T) *T { return &n }

// rpC is a pointer to an amount in cents.
func rpC(c money.Cents) *int64 {
	n := int64(c)
	return &n
}

func refJSON(r calc.Ref) api.ReportRef {
	return api.ReportRef{Type: api.ReportRefType(r.Type), Id: r.ID, Label: rpS(r.Label)}
}

// groupsJSON maps the groups with their totals encoder (never null).
func groupsJSON[T, G any](gs []calc.Group[T], f func(calc.Group[T]) G) []G {
	out := make([]G, 0, len(gs))
	for _, g := range gs {
		out = append(out, f(g))
	}
	return out
}

// recordsJSON maps the records (never null).
func recordsJSON[R, J any](rs []R, f func(*R) J) []J {
	out := make([]J, 0, len(rs))
	for i := range rs {
		out = append(out, f(&rs[i]))
	}
	return out
}

func occupancyJSON(x *calc.OccupancyResult) api.OccupancyReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.OccupancyTotals) api.OccupancyTotals {
		return api.OccupancyTotals{
			Unit: api.ReportUnit(t.Unit), OccupiedUnits: t.OccupiedUnits, AvailableUnits: t.AvailableUnits,
			ClosedUnits: t.ClosedUnits, OutOfServiceUnits: t.OutOfServiceUnits, ReleasedUnits: t.ReleasedUnits,
			OccupancyBp: t.OccupancyBP.Ptr(),
		}
	}
	return api.OccupancyReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today,
		Variant: api.OccupancyReportVariant(x.Variant), Family: api.ReportFamily(x.Family), Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.OccupancyTotals]) api.OccupancyGroup {
			return api.OccupancyGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.OccupancyRecord) api.OccupancyRecord {
			return api.OccupancyRecord{
				Role: api.OccupancyRecordRole(r.Role), ResourceId: r.ResourceID, ResourceCode: r.ResourceCode,
				ResourceType: api.ResourceType(r.ResourceType), MapId: rpS(r.MapID), Category: rpS(r.Category),
				Kind: rpE[api.OccupancyRecordKind](r.Kind), AllocationId: rpS(r.AllocationID), ContractId: rpS(r.ContractID),
				ContractNumber: rpS(r.ContractNumber), OccupiedUnits: r.OccupiedUnits, AvailableUnits: r.AvailableUnits,
				ClosedUnits: r.ClosedUnits, OutOfServiceUnits: r.OutOfServiceUnits, Ref: refJSON(r.Ref),
			}
		}),
	}
}

func efficiencyJSON(x *calc.EfficiencyResult) api.EfficiencyReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.EfficiencyTotals) api.EfficiencyTotals {
		return api.EfficiencyTotals{
			Unit: api.ReportUnit(t.Unit), Nights: t.Nights, VesselUnits: t.VesselUnits, ResourceUnits: t.ResourceUnits,
			EfficiencyBp: t.EfficiencyBP.Ptr(),
		}
	}
	return api.EfficiencyReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today,
		Family: api.ReportFamily(x.Family), Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.EfficiencyTotals]) api.EfficiencyGroup {
			return api.EfficiencyGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.EfficiencyRecord) api.EfficiencyRecord {
			return api.EfficiencyRecord{
				ResourceId: r.ResourceID, ResourceCode: r.ResourceCode, ResourceType: api.ResourceType(r.ResourceType),
				AllocationId: r.AllocationID, Kind: api.EfficiencyRecordKind(r.Kind), ContractId: rpS(r.ContractID),
				ContractNumber: rpS(r.ContractNumber), VesselId: rpS(r.VesselID), VesselName: rpS(r.VesselName),
				LoaCm: rpI(int64(r.LOA)), BeamCm: rpI(int64(r.Beam)), Nights: r.Nights, VesselUnits: r.VesselUnits,
				ResourceUnits: r.ResourceUnits, EfficiencyBp: r.EfficiencyBP.Ptr(), Ref: refJSON(r.Ref),
			}
		}),
	}
}

func wasteJSON(x *calc.WasteResult) api.AllocationWasteReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.WasteTotals) api.WasteTotals {
		return api.WasteTotals{
			Count: t.Count, Nights: t.Nights, WasteBpNights: t.WasteBPNights, AppliedCents: t.Applied.Ptr(),
			ListCents: t.List.Ptr(), LostCents: t.Lost.Ptr(), Unpriced: t.Unpriced,
		}
	}
	briefs := func(bs []calc.ResourceBrief) []api.ReportResourceBrief {
		out := make([]api.ReportResourceBrief, 0, len(bs))
		for _, b := range bs {
			out = append(out, api.ReportResourceBrief{Id: b.ID, Code: b.Code})
		}
		return out
	}
	sugg := make([]api.WasteSuggestion, 0, len(x.Suggestions))
	for _, s := range x.Suggestions {
		ids := s.AllocationIDs
		if ids == nil {
			ids = []string{}
		}
		sugg = append(sugg, api.WasteSuggestion{
			Kind: api.WasteSuggestionKind(s.Kind), AllocationIds: ids, FromBerths: briefs(s.FromBerths),
			ToBerths: briefs(s.ToBerths), WasteBeforeBp: s.WasteBeforeBP, WasteAfterBp: s.WasteAfterBP, GainCm2Nights: s.GainCm2Nights,
		})
	}
	return api.AllocationWasteReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today,
		ThresholdBp: x.ThresholdBP, Suggestions: sugg, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.WasteTotals]) api.WasteGroup {
			return api.WasteGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.WasteRecord) api.WasteRecord {
			return api.WasteRecord{
				AllocationId: r.AllocationID, Kind: api.WasteRecordKind(r.Kind), ContractId: rpS(r.ContractID),
				ContractNumber: rpS(r.ContractNumber), BerthId: r.BerthID, BerthCode: r.BerthCode, Category: rpS(r.Category),
				VesselId: r.VesselID, VesselName: rpS(r.VesselName), LoaCm: int64(r.LOA), BeamCm: int64(r.Beam),
				BerthLengthCm: int64(r.BerthLength), BerthWidthCm: int64(r.BerthWidth), FillBp: r.FillBP, WasteBp: r.WasteBP,
				NightsInPeriod: r.NightsInPeriod, AppliedCents: r.Applied.Ptr(), ListCents: r.List.Ptr(), LostCents: r.Lost.Ptr(),
				Ref: refJSON(r.Ref),
			}
		}),
	}
}

func lostDemandJSON(x *calc.LostDemandResult) api.LostDemandReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.LostDemandTotals) api.LostDemandTotals {
		return api.LostDemandTotals{Count: t.Count, Nights: t.Nights, ValueCents: t.Value.Ptr(), Unpriced: t.Unpriced}
	}
	return api.LostDemandReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.LostDemandTotals]) api.LostDemandGroup {
			return api.LostDemandGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.LostDemandRecord) api.LostDemandRecord {
			return api.LostDemandRecord{
				InquiryId: r.InquiryID, Arrival: r.Arrival.String(), Departure: r.Departure.String(),
				NightsInPeriod: r.NightsInPeriod, Outcome: api.InquiryOutcome(r.Outcome), Purpose: api.SearchPurpose(r.Purpose),
				Term: rpE[api.ContractTerm](r.Term), Channel: api.Channel(r.Channel), Source: api.InquirySource(r.Source),
				LoaCm: int64(r.LOA), BeamCm: int64(r.Beam), LengthBand: api.ReportLengthBand(r.LengthBand),
				PricedCategory: rpS(r.PricedCategory), ValueCents: r.Value.Ptr(), Ref: refJSON(r.Ref),
			}
		}),
	}
}

func customersJSON(x *calc.CustomersResult) api.CustomersReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.CustomersTotals) api.CustomersTotals {
		return api.CustomersTotals{
			Customers: t.Customers, NewCustomers: t.NewCustomers, Contracts: t.Contracts, ValueCents: int64(t.Value),
			ArrearsCents: int64(t.Arrears), RenewalEligible: t.RenewalEligible, RenewalRenewed: t.RenewalRenewed,
			RenewalRateBp: t.RenewalRateBP.Ptr(),
		}
	}
	return api.CustomersReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.CustomersTotals]) api.CustomersGroup {
			return api.CustomersGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.CustomersRecord) api.CustomersRecord {
			out := api.CustomersRecord{
				Section: api.CustomersRecordSection(r.Section), CustomerId: r.CustomerID, CustomerName: rpS(r.CustomerName),
				Ref: refJSON(r.Ref),
			}
			if r.Section == "renewals" {
				out.ContractId, out.ContractNumber = rpS(r.ContractID), rpS(r.ContractNumber)
				out.Term, out.LastNight = rpE[api.ContractTerm](r.Term), rpD(r.LastNight)
				out.Renewed, out.NewContractId = rpI(r.Renewed), rpS(r.NewContractID)
				return out
			}
			out.Category, out.Nationality = rpS(r.Category), rpS(r.Nationality)
			out.FirstContractOn, out.AcquisitionChannel = rpD(r.FirstContractOn), rpE[api.Channel](r.AcquisitionChannel)
			out.SeniorityYears, out.SeniorityBand = rpI(r.SeniorityYears), rpE[api.CustomersRecordSeniorityBand](r.SeniorityBand)
			out.New, out.ValueCents, out.ArrearsCents = rpI(r.New), rpC(r.Value), rpC(r.Arrears)
			out.Contracts = rpI(r.Contracts)
			return out
		}),
	}
}

func productionJSON(x *calc.ProductionResult) api.ProductionReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.ProductionTotals) api.ProductionTotals {
		return api.ProductionTotals{
			Contracts: t.Contracts, Nights: t.Nights, CompetenceCents: int64(t.Competence), Transits: t.Transits,
			TransitNights: t.TransitNights, TransitCompetenceCents: int64(t.TransitCompetence),
			TransitAvgNightCents: t.TransitAvgNight.Ptr(),
		}
	}
	return api.ProductionReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.ProductionTotals]) api.ProductionGroup {
			return api.ProductionGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.ProductionRecord) api.ProductionRecord {
			return api.ProductionRecord{
				ContractId: r.ContractID, Number: r.Number, Kind: api.ContractKind(r.Kind), Term: rpE[api.ContractTerm](r.Term),
				Status: api.ContractStatus(r.Status), Channel: rpE[api.Channel](r.Channel), CustomerId: rpS(r.CustomerID),
				CustomerName: rpS(r.CustomerName), Category: rpS(r.Category), StartsOn: r.StartsOn.String(),
				EndsOn: r.EndsOn.String(), TotalCents: int64(r.Total), NightsInPeriod: r.NightsInPeriod,
				CompetenceCents: int64(r.Competence), Ref: refJSON(r.Ref),
			}
		}),
	}
}

func accrualsJSON(x *calc.AccrualsResult) api.AccrualsReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.AccrualsTotals) api.AccrualsTotals {
		return api.AccrualsTotals{
			ClosingDate: t.ClosingDate.String(), CompetenceNetCents: int64(t.CompetenceNet),
			CompetenceGrossCents: int64(t.CompetenceGross), DeferralNetCents: int64(t.DeferralNet),
			DeferralGrossCents: int64(t.DeferralGross), AccrualCents: int64(t.Accrual),
		}
	}
	return api.AccrualsReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.AccrualsTotals]) api.AccrualsGroup {
			return api.AccrualsGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.AccrualsRecord) api.AccrualsRecord {
			out := api.AccrualsRecord{
				Section: api.AccrualsRecordSection(r.Section), Date: r.Date.String(), ContractId: rpS(r.ContractID),
				ContractNumber: rpS(r.ContractNumber), CustomerId: rpS(r.CustomerID), CustomerName: rpS(r.CustomerName),
				Ref: refJSON(r.Ref),
			}
			if r.Section == "accrual" {
				out.MaturedCents, out.ChargedCents, out.AccrualCents = rpC(r.Matured), rpC(r.Charged), rpC(r.Accrual)
				return out
			}
			out.ChargeId, out.ChargeNumber, out.LineSeq = rpS(r.ChargeID), rpS(r.ChargeNumber), rpI(r.LineSeq)
			out.Description, out.ServiceFrom, out.ServiceTo = rpS(r.Description), rpD(r.ServiceFrom), rpD(r.ServiceTo)
			out.AmountNetCents, out.AmountGrossCents = rpC(r.AmountNet), rpC(r.AmountGross)
			if r.Section == "deferral" {
				out.DeferralNetCents, out.DeferralGrossCents = rpC(r.DeferralNet), rpC(r.DeferralGross)
			} else {
				out.CompetenceNetCents, out.CompetenceGrossCents = rpC(r.CompetenceNet), rpC(r.CompetenceGross)
			}
			return out
		}),
	}
}

func chargesRecordJSON(r *calc.ChargesRecord) api.ChargesRecord {
	out := api.ChargesRecord{
		ChargeId: r.ChargeID, Number: r.Number, Seq: r.Seq, IssuedOn: r.IssuedOn.String(), DueOn: rpD(r.DueOn),
		Month: rpS(r.Month), OriginType: api.ChargeOriginType(r.OriginType), CustomerId: rpS(r.CustomerID),
		CustomerName: rpS(r.CustomerName), ItemId: rpS(r.ItemID), ItemCode: rpS(r.ItemCode),
		Description: rpS(r.Description), RevenueClass: rpS(r.RevenueClass), VatNature: rpS(r.VATNature),
		ServiceFrom: rpD(r.ServiceFrom), ServiceTo: rpD(r.ServiceTo), NetCents: int64(r.Net), VatCents: int64(r.VAT),
		GrossCents: int64(r.Gross), PaymentStatus: api.ChargePaymentStatus(r.PaymentStatus),
		InvoicingStatus: api.ChargeInvoicingStatus(r.InvoicingStatus), FiscalMode: rpE[api.FiscalMode](r.FiscalMode),
		Ref: refJSON(r.Ref),
	}
	if r.VATRateBP != 0 || r.VATNature == "" {
		out.VatRateBp = rpI(r.VATRateBP)
	}
	return out
}

func chargesReportJSON(x *calc.ChargesResult) api.ChargesReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.ChargesTotals) api.ChargesTotals {
		return api.ChargesTotals{
			Charges: t.Charges, Lines: t.Lines, NetCents: int64(t.Net), VatCents: int64(t.VAT), GrossCents: int64(t.Gross),
			ChargedCents: int64(t.Charged),
		}
	}
	return api.ChargesReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Basis: api.ChargesReportBasis(x.Basis), Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.ChargesTotals]) api.ChargesGroup {
			return api.ChargesGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, chargesRecordJSON),
	}
}

func paymentsReportJSON(x *calc.PaymentsResult) api.PaymentsReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.PaymentsTotals) api.PaymentsTotals {
		return api.PaymentsTotals{
			Count: t.Count, CollectedCents: int64(t.Collected), PaymentsCents: int64(t.Payments),
			RefundsCents: int64(t.Refunds), FeesCents: int64(t.Fees), PayoutsCents: int64(t.Payouts),
		}
	}
	return api.PaymentsReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.PaymentsTotals]) api.PaymentsGroup {
			return api.PaymentsGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.PaymentsRecord) api.PaymentsRecord {
			return api.PaymentsRecord{
				Section: api.PaymentsRecordSection(r.Section), Kind: api.PaymentsRecordKind(r.Kind), Id: r.ID,
				Number: r.Number, Date: r.Date.String(), Method: api.PaymentMethod(r.Method), Source: rpS(r.Source),
				ProviderCode: rpS(r.ProviderCode), CustomerId: rpS(r.CustomerID), CustomerName: rpS(r.CustomerName),
				AmountCents: int64(r.Amount), FeeCents: rpC(r.Fee), Ref: refJSON(r.Ref),
			}
		}),
	}
}

func revenueClassesJSON(x *calc.RevenueClassesResult) api.RevenueClassesReport {
	e := envJSON(&x.Envelope)
	classes := make([]api.RevenueClassGroup, 0, len(x.Classes))
	for _, c := range x.Classes {
		months := make([]api.RevenueClassMonth, 0, len(c.Months))
		for _, m := range c.Months {
			months = append(months, api.RevenueClassMonth{Month: m.Month, NetCents: int64(m.Net)})
		}
		classes = append(classes, api.RevenueClassGroup{
			Key: c.Key, Label: c.Label, ParentKey: rpS(c.ParentKey), NetCents: int64(c.Net), ShareBp: c.ShareBP.Ptr(),
			Months: months,
		})
	}
	return api.RevenueClassesReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Basis: api.RevenueClassesReportBasis(x.Basis),
		Totals: api.RevenueClassesTotals{
			Lines: x.Totals.Lines, NetCents: int64(x.Totals.Net), UnclassifiedCents: int64(x.Totals.Unclassified),
		},
		Groups:  classes,
		Records: recordsJSON(x.Records, chargesRecordJSON),
	}
}

func receivablesReportJSON(x *calc.ReceivablesResult) api.ReceivablesReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.ReceivablesTotals) api.ReceivablesTotals {
		b := t.Buckets
		return api.ReceivablesTotals{
			AsOf: t.AsOf.String(), Count: t.Count, OpenCents: int64(t.Open), OverdueCents: int64(t.Overdue),
			Buckets: api.AgeingAmounts{
				NotDueCents: int64(b.NotDue), D030Cents: int64(b.D0_30), D3160Cents: int64(b.D31_60),
				D6190Cents: int64(b.D61_90), D90PlusCents: int64(b.D90Plus), TotalCents: int64(b.Total),
			},
			FutureCents: int64(t.Future), RemindersSent: t.RemindersSent,
		}
	}
	return api.ReceivablesReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.ReceivablesTotals]) api.ReceivablesGroup {
			return api.ReceivablesGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.ReceivablesRecord) api.ReceivablesRecord {
			out := api.ReceivablesRecord{
				Section: api.ReceivablesRecordSection(r.Section), Kind: api.ReceivablesRecordKind(r.Kind), Id: r.ID,
				Number: rpS(r.Number), CustomerId: rpS(r.CustomerID), CustomerName: rpS(r.CustomerName),
				DueOn: rpD(r.DueOn), Ref: refJSON(r.Ref),
			}
			switch r.Section {
			case "reminders":
				out.ReminderLevel, out.SentOn = rpE[api.ReminderLevel](r.ReminderLevel), rpD(r.SentOn)
			case "future":
				out.TotalCents, out.ResidualCents = rpC(r.Total), rpC(r.Residual)
			default:
				out.OriginType, out.IssuedOn = rpE[api.ChargeOriginType](r.OriginType), rpD(r.IssuedOn)
				out.TotalCents, out.PaidCents, out.CreditedCents = rpC(r.Total), rpC(r.Paid), rpC(r.Credited)
				out.ResidualCents, out.Overdue, out.Bucket = rpC(r.Residual), &r.Overdue, rpE[api.AgeingBucket](r.Bucket)
			}
			return out
		}),
	}
}

func presencesReportJSON(x *calc.PresencesResult) api.PresencesReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.PresencesTotals) api.PresencesTotals {
		return api.PresencesTotals{
			Arrivals: t.Arrivals, Departures: t.Departures, AbsenceNights: t.AbsenceNights, Releases: t.Releases,
			ReleasedNights: t.ReleasedNights, ResoldNights: t.ResoldNights, ReleaseCreditCents: t.ReleaseCredit.Ptr(),
			ReleaseRevenueCents: t.ReleaseRevenue.Ptr(),
		}
	}
	return api.PresencesReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.PresencesTotals]) api.PresencesGroup {
			return api.PresencesGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.PresencesRecord) api.PresencesRecord {
			out := api.PresencesRecord{
				Section: api.PresencesRecordSection(r.Section), ContractId: rpS(r.ContractID),
				ContractNumber: rpS(r.ContractNumber), ContractKind: rpE[api.ContractKind](r.ContractKind),
				VesselId: rpS(r.VesselID), VesselName: rpS(r.VesselName), BerthCode: rpS(r.BerthCode), Ref: refJSON(r.Ref),
			}
			switch r.Section {
			case "absences":
				out.AbsenceNights = rpI(r.AbsenceNights)
			case "releases":
				out.ReleaseId, out.ReleaseFrom, out.ReleaseTo = rpS(r.ReleaseID), rpD(r.ReleaseFrom), rpD(r.ReleaseTo)
				out.ReleasedNights, out.ResoldNights = rpI(r.ReleasedNights), rpI(r.ResoldNights)
				out.CreditCents, out.RevenueCents = r.Credit.Ptr(), r.Revenue.Ptr()
			default:
				out.PresenceId, out.Event = rpS(r.PresenceID), rpE[api.PresencesRecordEvent](r.Event)
				out.Night = rpD(r.Night)
			}
			return out
		}),
	}
}

func rightsReportJSON(x *calc.RightsResult) api.RightsReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.RightsTotals) api.RightsTotals {
		return api.RightsTotals{
			Rights: t.Rights, Holders: t.Holders, Berths: t.Berths, ChargesIssuedCents: int64(t.ChargesIssued),
			ChargesCollectedCents: int64(t.ChargesCollected), ChargesResidualCents: int64(t.ChargesResidual),
			AdvanceCents: int64(t.Advance), BalanceCents: int64(t.Balance), MarinaShareCents: int64(t.MarinaShare),
			MandateRevenueCents: int64(t.MandateRevenue), MandatePayoutCents: int64(t.MandatePayout),
		}
	}
	return api.RightsReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.RightsTotals]) api.RightsGroup {
			return api.RightsGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.RightsRecord) api.RightsRecord {
			out := api.RightsRecord{
				Section: api.RightsRecordSection(r.Section), RightId: rpS(r.RightID), RightNumber: rpS(r.RightNumber),
				BerthId: rpS(r.BerthID), BerthCode: rpS(r.BerthCode), HolderId: rpS(r.HolderID),
				HolderName: rpS(r.HolderName), Ref: refJSON(r.Ref),
			}
			switch r.Section {
			case "charges":
				out.ChargeId, out.Number, out.IssuedOn = rpS(r.ChargeID), rpS(r.Number), rpD(r.IssuedOn)
				out.IssuedCents, out.PaidCents, out.ResidualCents = rpC(r.Issued), rpC(r.Paid), rpC(r.Residual)
			case "allotments":
				out.RunId, out.RunKind, out.ItemCode = rpS(r.RunID), rpE[api.CommonRunKind](r.RunKind), rpS(r.ItemCode)
				out.Date, out.AmountCents = rpD(r.Date), rpC(r.Amount)
			case "mandates":
				out.MandateId, out.Kind, out.Number = rpS(r.MandateID), rpE[api.RightsRecordKind](r.Kind), rpS(r.Number)
				out.Date, out.AmountCents = rpD(r.Date), rpC(r.Amount)
			default:
				out.ShareMillis = rpI(r.ShareMillis)
			}
			return out
		}),
	}
}

func cashReportJSON(x *calc.CashResult) api.CashReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.CashTotals) api.CashTotals {
		vat := make([]api.CashVatTotal, 0, len(t.VAT))
		for _, v := range t.VAT {
			vat = append(vat, api.CashVatTotal{
				RateBp: v.RateBP, Nature: rpS(v.Nature), NetCents: int64(v.Net), VatCents: int64(v.VAT), GrossCents: int64(v.Gross),
			})
		}
		methods := make([]api.CashMethodTotal, 0, len(t.Methods))
		for _, m := range t.Methods {
			methods = append(methods, api.CashMethodTotal{Method: api.PaymentMethod(m.Method), TotalCents: int64(m.Total)})
		}
		return api.CashTotals{
			Sessions: t.Sessions, DifferenceCents: int64(t.Difference), Documents: t.Documents,
			FiscalTotalCents: int64(t.FiscalTotal), FiscalVoidCents: int64(t.FiscalVoid), Vat: vat, Methods: methods,
		}
	}
	return api.CashReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.CashTotals]) api.CashGroup {
			return api.CashGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.CashRecord) api.CashRecord {
			out := api.CashRecord{
				Section: api.CashRecordSection(r.Section), BusinessDate: r.BusinessDate.String(), Ref: refJSON(r.Ref),
			}
			if r.Section == "fiscal" {
				out.DocumentId, out.Number, out.Method = rpS(r.DocumentID), rpS(r.Number), rpE[api.PaymentMethod](r.Method)
				out.TotalCents, out.VoidCents = rpC(r.Total), rpC(r.Void)
				return out
			}
			out.SessionId, out.RegisterCode, out.Status = rpS(r.SessionID), rpS(r.RegisterCode), rpS(r.Status)
			out.FloatCents, out.ExpectedCents, out.CountedCents = rpC(r.Float), rpC(r.Expected), rpC(r.Counted)
			out.DifferenceCents = rpC(r.Difference)
			return out
		}),
	}
}

func vesselsReportJSON(x *calc.VesselsResult) api.VesselsReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.VesselsTotals) api.VesselsTotals {
		return api.VesselsTotals{Vessels: t.Vessels, Nights: t.Nights, LoaSumCm: int64(t.LOASum), AvgLoaCm: t.AvgLOA.Ptr()}
	}
	return api.VesselsReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.VesselsTotals]) api.VesselsGroup {
			return api.VesselsGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.VesselsRecord) api.VesselsRecord {
			return api.VesselsRecord{
				VesselId: r.VesselID, Name: r.Name, Nights: r.Nights, LoaCm: int64(r.LOA), BeamCm: int64(r.Beam),
				HullType: rpE[api.HullType](r.HullType), Flag: rpS(r.Flag), LengthBand: api.ReportLengthBand(r.LengthBand),
				Kind: rpE[api.VesselsRecordKind](r.Kind), Ref: refJSON(r.Ref),
			}
		}),
	}
}

func berthRevenueJSON(x *calc.BerthRevenueResult) api.BerthRevenueReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.BerthRevenueTotals) api.BerthRevenueTotals {
		return api.BerthRevenueTotals{
			Berths: t.Berths, LengthCm: int64(t.Length), RevenueCents: int64(t.Revenue), RevenuePerMCents: t.RevenuePerM.Ptr(),
		}
	}
	return api.BerthRevenueReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.BerthRevenueTotals]) api.BerthRevenueGroup {
			return api.BerthRevenueGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.BerthRevenueRecord) api.BerthRevenueRecord {
			return api.BerthRevenueRecord{
				BerthId: r.BerthID, BerthCode: r.BerthCode, Category: rpS(r.Category), LengthCm: int64(r.Length),
				OccupiedNights: r.OccupiedNights, RevenueCents: int64(r.Revenue), RevenuePerMCents: r.RevenuePerM.Ptr(),
				Ref: refJSON(r.Ref),
			}
		}),
	}
}
