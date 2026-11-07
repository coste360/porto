package view

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/money"
)

// Converters of the M5 charges, exports, reconciliations, billing settings and opening balance imports
// (m5-plan §7.3.1; CHARGES-EXPORT).

func billingDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

// BillingBuckets converts VAT buckets.
func BillingBuckets(bs []chargeplan.VATBucket) []api.VatBucket {
	out := make([]api.VatBucket, 0, len(bs))
	for _, b := range bs {
		out = append(out, api.VatBucket{
			VatRateBp: b.RateBP, VatNature: String(b.Nature), NetCents: int64(b.Net), VatCents: int64(b.VAT), GrossCents: int64(b.Gross),
		})
	}
	return out
}

func chargeCustomer(p *charge.Party) *api.CustomerBrief {
	if p == nil {
		return nil
	}
	return &api.CustomerBrief{Id: p.ID, DisplayName: String(p.DisplayName)}
}

// ChargeRow converts a charge of a list (no lines).
func ChargeRow(r charge.Row) api.Charge {
	c := r.Charge
	overdue := r.Overdue
	out := api.Charge{
		Id: c.ID, Number: c.Number, Customer: chargeCustomer(r.Customer), OriginType: api.ChargeOriginType(c.OriginType),
		OriginId: c.OriginID, ContractId: String(c.ContractID), ContractCode: String(r.ContractCode), VesselId: String(c.VesselID),
		RightId: String(c.RightID), IssuedOn: c.IssuedOn.String(), DueOn: c.DueOn.String(), ServiceFrom: billingDate(c.ServiceFrom),
		ServiceTo: billingDate(c.ServiceTo), TotalCents: c.TotalCents, NetCents: c.NetCents, VatCents: c.VATCents,
		PaidAmountCents: c.PaidAmountCents, CreditedCents: c.CreditedCents, ResidualCents: c.ResidualCents(),
		PaymentStatus: api.ChargePaymentStatus(c.PaymentStatus), InvoicingStatus: api.ChargeInvoicingStatus(c.InvoicingStatus),
		FiscalMode: api.FiscalMode(c.FiscalMode), Overdue: &overdue, Description: c.Description,
		ExternalInvoiceNumber: String(c.ExternalInvoiceNumber), ExternalInvoiceDate: billingDate(c.ExternalInvoiceDate),
		CancelReason: String(c.CancelReason), ExportedAt: Time(c.ExportedAt), CancelledAt: Time(c.CancelledAt),
		CreatedAt: Time(c.CreatedAt), Version: c.Version,
	}
	if c.CorrectionKind != "" {
		k := api.CorrectionKind(c.CorrectionKind)
		out.CorrectionKind = &k
	}
	if c.CorrectsChargeID != "" {
		out.CorrectsCharge = &api.ChargeRef{Id: c.CorrectsChargeID}
	}
	return out
}

func chargeRef(c charge.Charge) api.ChargeRef {
	st := api.ChargePaymentStatus(c.PaymentStatus)
	total := c.TotalCents
	return api.ChargeRef{Id: c.ID, Number: c.Number, PaymentStatus: &st, TotalCents: &total}
}

// ChargeLines converts charge lines.
func ChargeLines(ls []charge.Line) []api.ChargeLine {
	out := make([]api.ChargeLine, 0, len(ls))
	for _, l := range ls {
		al := api.ChargeLine{
			Seq: l.Seq, ItemId: String(l.ItemID), ItemCode: l.ItemCode, Description: l.Description, QtyMilli: l.QtyMilli,
			UnitCents: l.UnitCents, DiscountCents: l.DiscountCents, NetCents: l.NetCents, VatCents: l.VATCents,
			GrossCents: l.GrossCents, VatRateBp: l.VATRateBP, PriceIncludesVat: l.PriceIncludesVAT,
			RevenueClass: String(l.RevenueClassCode), ExternalCode: String(l.ExternalCode), ServiceFrom: billingDate(l.ServiceFrom),
			ServiceTo: billingDate(l.ServiceTo),
		}
		if l.VATNature != "" {
			n := api.VatNature(l.VATNature)
			al.VatNature = &n
		}
		out = append(out, al)
	}
	return out
}

// ChargeDetail converts a charge with its lines, matches, offsets, corrections, exports and reminders.
func ChargeDetail(d *charge.Detail) api.Charge {
	out := ChargeRow(charge.Row{Charge: d.Charge, Customer: d.Customer, ContractCode: d.ContractCode, Overdue: d.Overdue})
	lines := ChargeLines(d.Lines)
	out.Lines = &lines
	pl := make([]chargeplan.Line, 0, len(d.Lines))
	for _, l := range d.Lines {
		pl = append(pl, chargeplan.Line{
			VATRateBP: l.VATRateBP, VATNature: l.VATNature, Net: money.Cents(l.NetCents), VAT: money.Cents(l.VATCents), Gross: money.Cents(l.GrossCents),
		})
	}
	vs := BillingBuckets(chargeplan.Totals(pl))
	out.VatSummary = &vs
	if d.Corrects != nil {
		r := chargeRef(*d.Corrects)
		out.CorrectsCharge = &r
	}
	corr := make([]api.ChargeRef, 0, len(d.Corrections))
	for _, c := range d.Corrections {
		corr = append(corr, chargeRef(c))
	}
	out.Corrections = &corr
	matches := make([]api.ChargeMatchRef, 0, len(d.Matches))
	for _, m := range d.Matches {
		matches = append(matches, api.ChargeMatchRef{
			Id: m.ID, PaymentId: m.PaymentID, PaymentNumber: m.PaymentNumber, AmountCents: m.AmountCents,
			ReversesMatchId: String(m.ReversesMatchID), CreatedAt: m.CreatedAt.UTC(),
		})
	}
	out.Matches = &matches
	offs := make([]api.ChargeOffsetRef, 0, len(d.Offsets))
	for _, o := range d.Offsets {
		offs = append(offs, api.ChargeOffsetRef{NegativeChargeId: o.NegativeChargeID, TargetChargeId: o.TargetChargeID, AmountCents: o.AmountCents})
	}
	out.Offsets = &offs
	exps := make([]api.ChargeExportRef, 0, len(d.Exports))
	for _, e := range d.Exports {
		exps = append(exps, api.ChargeExportRef{ExportId: e.ExportID, Number: e.Number, First: e.IsFirst, CreatedAt: Time(e.CreatedAt)})
	}
	out.Exports = &exps
	rems := make([]api.ChargeReminderRef, 0, len(d.Reminders))
	for _, r := range d.Reminders {
		rems = append(rems, api.ChargeReminderRef{Level: api.ReminderLevel(r.Level), Status: api.ReminderItemStatus(r.Status), CreatedAt: r.CreatedAt.UTC()})
	}
	out.Reminders = &rems
	return out
}

// ChargePage converts a page of charges.
func ChargePage(rows []charge.Row, next string) api.ChargePage {
	items := make([]api.Charge, 0, len(rows))
	for _, r := range rows {
		items = append(items, ChargeRow(r))
	}
	return api.ChargePage{Items: items, NextCursor: String(next)}
}

// ExportPreview converts a preview.
func ExportPreview(p *charge.Preview) api.ExportPreview {
	out := api.ExportPreview{
		Count: p.Totals.ChargeCount, TotalCents: int64(p.Totals.ChargeTotals), TotalsByVat: BillingBuckets(p.Totals.ByVAT),
		TotalsByOrigin: []api.OriginTotal{}, Warnings: []api.ExportWarning{},
	}
	for _, o := range p.Totals.ByOrigin {
		out.TotalsByOrigin = append(out.TotalsByOrigin, api.OriginTotal{OriginType: api.ChargeOriginType(o.Key), Count: o.Count, TotalCents: int64(o.Total)})
	}
	for _, w := range p.Warnings {
		out.Warnings = append(out.Warnings, api.ExportWarning{Code: api.ExportWarningCode(w.Code), CustomerId: String(w.CustomerID), ChargeIds: w.ChargeIDs})
	}
	return out
}

// ChargeExport converts an export.
func ChargeExport(e *charge.Export) api.ChargeExport {
	t := charge.TotalsOfExport(e)
	vat := BillingBuckets(t.ByVAT)
	out := api.ChargeExport{
		Id: e.ID, Number: e.Number, Kind: api.ChargeExportKind(e.Kind), ReexportOf: String(e.ReexportOf),
		PeriodFrom: billingDate(e.PeriodFrom), PeriodTo: billingDate(e.PeriodTo), Delivery: api.ChargeExportDelivery(e.Delivery),
		ExporterCode: String(e.ExporterCode), DeliveryStatus: api.ChargeExportDeliveryStatus(e.DeliveryStatus),
		DeliveryError: String(e.DeliveryError), ChargeCount: e.ChargeCount, LineCount: e.LineCount, TotalCents: int64(t.Gross),
		TotalsByVat: &vat, CreatedAt: e.CreatedAt.UTC(), Version: e.Version,
	}
	if !e.PeriodFrom.IsZero() {
		f := charge.ExportFiltersOf(e)
		af := api.ExportFilter{PeriodFrom: f.PeriodFrom.String(), PeriodTo: f.PeriodTo.String(), CustomerId: String(f.CustomerID), RevenueClass: String(f.RevenueClass)}
		if len(f.OriginTypes) > 0 {
			ots := make([]api.ChargeOriginType, 0, len(f.OriginTypes))
			for _, o := range f.OriginTypes {
				ots = append(ots, api.ChargeOriginType(o))
			}
			af.OriginTypes = &ots
		}
		out.Filters = &af
	}
	files := []api.ChargeExportFiles{api.ChargeExportFilesCsv, api.ChargeExportFilesXlsx, api.ChargeExportFilesPdf}
	out.Files = &files
	return out
}

// ChargeExportPage converts a page of exports.
func ChargeExportPage(p charge.Page[charge.Export]) api.ChargeExportPage {
	items := make([]api.ChargeExport, 0, len(p.Items))
	for i := range p.Items {
		items = append(items, ChargeExport(&p.Items[i]))
	}
	return api.ChargeExportPage{Items: items, NextCursor: String(p.NextCursor)}
}

// ChargeExportDetail converts an export with a page of items.
func ChargeExportDetail(d *charge.ExportDetail) api.ChargeExportDetail {
	items := make([]api.ChargeExportItem, 0, len(d.Items))
	for _, it := range d.Items {
		ai := api.ChargeExportItem{ChargeId: it.ChargeID, Number: it.Number, First: it.IsFirst, TotalCents: it.TotalCents}
		if it.CustomerID != "" {
			ai.Customer = &api.CustomerBrief{Id: it.CustomerID, DisplayName: String(d.Names[it.CustomerID])}
		}
		items = append(items, ai)
	}
	return api.ChargeExportDetail{Export: ChargeExport(&d.Export), Items: items, NextCursor: String(d.NextCursor)}
}

// InvoiceReconciliation converts a reconciliation.
func InvoiceReconciliation(r *charge.Reconciliation, report []charge.ReportRow) api.InvoiceReconciliation {
	if report == nil {
		report = charge.ReportOf(r)
	}
	rows := make([]api.InvoiceReconciliationRow, 0, len(report))
	for _, x := range report {
		rows = append(rows, api.InvoiceReconciliationRow{Row: x.Row, ChargeNumber: String(x.ChargeNumber), Outcome: api.InvoiceReconciliationRowOutcome(x.Outcome)})
	}
	return api.InvoiceReconciliation{
		Id: r.ID, Source: api.InvoiceReconciliationSource(r.Source), DryRun: r.DryRun, RowCount: r.RowCount,
		MatchedCount: r.MatchedCount, Report: rows, CreatedAt: r.CreatedAt.UTC(),
	}
}

// InvoiceReconciliationPage converts a page of reconciliations.
func InvoiceReconciliationPage(p charge.Page[charge.Reconciliation]) api.InvoiceReconciliationPage {
	items := make([]api.InvoiceReconciliation, 0, len(p.Items))
	for i := range p.Items {
		items = append(items, InvoiceReconciliation(&p.Items[i], nil))
	}
	return api.InvoiceReconciliationPage{Items: items, NextCursor: String(p.NextCursor)}
}

// BillingSettings converts the billing settings.
func BillingSettings(s *charge.Settings, fiscalDrivers []string) api.BillingSettings {
	levels := make([]api.ReminderLevelPolicy, 0, len(s.Reminder.Levels))
	for _, l := range s.Reminder.Levels {
		levels = append(levels, api.ReminderLevelPolicy{Level: api.ReminderLevel(l.Level), Days: l.Days, Enabled: l.Enabled})
	}
	items := make([]api.OriginItem, 0, len(s.OriginItems))
	for _, k := range charge.OriginItemKeys {
		if id, ok := s.OriginItems[k]; ok {
			items = append(items, api.OriginItem{OriginKey: api.OriginItemKey(k), ItemId: id})
		}
	}
	if fiscalDrivers == nil {
		fiscalDrivers = []string{}
	}
	r := s.Row
	return api.BillingSettings{
		DefaultDueDays: r.DefaultDueDays, AutoPaymentRequest: r.AutoPaymentRequest, ReminderPolicy: api.ReminderPolicy{Levels: levels},
		ReminderHour: r.ReminderHour, ArrearsPolicy: api.ArrearsPolicy{
			Enabled: s.Arrears.Enabled, DaysOverdue: s.Arrears.DaysOverdue, MinAmountCents: int64(s.Arrears.MinAmountCents),
		},
		ExportReminderDays: r.ExportReminderDays, FiscalDriver: r.FiscalDriver, ReceiptFooter: r.ReceiptFooter,
		SepaLeadDays: r.SEPALeadDays, OriginItems: items, FiscalDrivers: fiscalDrivers, Version: r.Version,
	}
}

// BillingSettingsIn converts the settings input.
func BillingSettingsIn(in api.BillingSettingsInput, fiscalDrivers []string) charge.SettingsInput {
	out := charge.SettingsInput{
		DefaultDueDays: in.DefaultDueDays, AutoPaymentRequest: in.AutoPaymentRequest, ReminderHour: in.ReminderHour,
		Arrears: charge.ArrearsPolicy{
			Enabled: in.ArrearsPolicy.Enabled, DaysOverdue: in.ArrearsPolicy.DaysOverdue, MinAmountCents: money.Cents(in.ArrearsPolicy.MinAmountCents),
		},
		ExportReminderDays: in.ExportReminderDays, FiscalDriver: in.FiscalDriver, ReceiptFooter: in.ReceiptFooter,
		SEPALeadDays: in.SepaLeadDays, OriginItems: map[string]string{}, FiscalDrivers: fiscalDrivers,
	}
	for _, l := range in.ReminderPolicy.Levels {
		out.Reminder.Levels = append(out.Reminder.Levels, charge.ReminderLevel{Level: string(l.Level), Days: l.Days, Enabled: l.Enabled})
	}
	for _, it := range in.OriginItems {
		out.OriginItems[string(it.OriginKey)] = it.ItemId
	}
	return out
}

// ChargeImport converts an opening balance import run.
func ChargeImport(r *charge.Import) api.ChargeImport {
	rep := charge.ReportOfImport(r)
	issues := make([]api.ChargeImportRowIssue, 0, len(rep.Issues))
	for _, i := range rep.Issues {
		issues = append(issues, api.ChargeImportRowIssue{Row: i.Row, Code: api.ChargeImportRowIssueCode(i.Code)})
	}
	total := int64(rep.TotalCents)
	return api.ChargeImport{
		Id: r.ID, Status: api.ChargeImportStatus(r.Status), Format: api.ChargeImportFormat(r.Format), ChargeCount: r.ChargeCount,
		ValidRows: rep.ValidRows, TotalCents: &total, Issues: issues, CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
}

// ChargeImportPage converts a page of import runs.
func ChargeImportPage(p charge.Page[charge.Import]) api.ChargeImportPage {
	items := make([]api.ChargeImport, 0, len(p.Items))
	for i := range p.Items {
		items = append(items, ChargeImport(&p.Items[i]))
	}
	return api.ChargeImportPage{Items: items, NextCursor: String(p.NextCursor)}
}
