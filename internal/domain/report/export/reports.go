package export

import (
	"slices"
	"strings"

	"github.com/coste360/porto/internal/domain/report/calc"
)

// One table per report (m7-plan §3, §7): the columns are the JSON record fields of the selected section in the
// JSON order of interest; integers keep their unit (the column type formats them).

// Occupancy is the R1 table (units by the result unit: berth-nights, cm-nights or cm²-nights).
func Occupancy(r *calc.OccupancyResult, lang string) Table {
	ut := unitType(r.Totals.Unit)
	type R = calc.OccupancyRecord
	type T = calc.OccupancyTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"role", TypeText, "", func(x *R) Value { return Text(x.Role) }},
			{"resource_code", TypeText, "", func(x *R) Value { return Text(x.ResourceCode) }},
			{"resource_type", TypeText, "", func(x *R) Value { return Text(x.ResourceType) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"contract_number", TypeText, "", func(x *R) Value { return Text(x.ContractNumber) }},
			{"occupied_units", ut, "occupied_units", func(x *R) Value { return Int(x.OccupiedUnits) }},
			{"available_units", ut, "available_units", func(x *R) Value { return Int(x.AvailableUnits) }},
			{"closed_units", ut, "closed_units", func(x *R) Value { return Int(x.ClosedUnits) }},
			{"out_of_service_units", ut, "out_of_service_units", func(x *R) Value { return Int(x.OutOfServiceUnits) }},
		},
		totals: []tcol[T]{
			{"occupied_units", ut, func(x *T) Value { return Int(x.OccupiedUnits) }},
			{"available_units", ut, func(x *T) Value { return Int(x.AvailableUnits) }},
			{"closed_units", ut, func(x *T) Value { return Int(x.ClosedUnits) }},
			{"out_of_service_units", ut, func(x *T) Value { return Int(x.OutOfServiceUnits) }},
			{"released_units", ut, func(x *T) Value { return Int(x.ReleasedUnits) }},
			{"occupancy_bp", TypeBP, func(x *T) Value { return BP(x.OccupancyBP) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Efficiency is the R2 table.
func Efficiency(r *calc.EfficiencyResult, lang string) Table {
	ut := unitType(r.Totals.Unit)
	type R = calc.EfficiencyRecord
	type T = calc.EfficiencyTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"resource_code", TypeText, "", func(x *R) Value { return Text(x.ResourceCode) }},
			{"resource_type", TypeText, "", func(x *R) Value { return Text(x.ResourceType) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"contract_number", TypeText, "", func(x *R) Value { return Text(x.ContractNumber) }},
			{"vessel_name", TypeText, "", func(x *R) Value { return Text(x.VesselName) }},
			{"loa_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.LOA)) }},
			{"beam_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.Beam)) }},
			{"nights", TypeInt, "nights", func(x *R) Value { return Int(x.Nights) }},
			{"vessel_units", ut, "vessel_units", func(x *R) Value { return Int(x.VesselUnits) }},
			{"resource_units", ut, "resource_units", func(x *R) Value { return Int(x.ResourceUnits) }},
			{"efficiency_bp", TypeBP, "efficiency_bp", func(x *R) Value { return BP(x.EfficiencyBP) }},
		},
		totals: []tcol[T]{
			{"nights", TypeInt, func(x *T) Value { return Int(x.Nights) }},
			{"vessel_units", ut, func(x *T) Value { return Int(x.VesselUnits) }},
			{"resource_units", ut, func(x *T) Value { return Int(x.ResourceUnits) }},
			{"efficiency_bp", TypeBP, func(x *T) Value { return BP(x.EfficiencyBP) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Waste is the R3 table, with the suggestions sheet.
func Waste(r *calc.WasteResult, lang string) Table {
	type R = calc.WasteRecord
	type T = calc.WasteTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"berth_code", TypeText, "", func(x *R) Value { return Text(x.BerthCode) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"contract_number", TypeText, "", func(x *R) Value { return Text(x.ContractNumber) }},
			{"vessel_name", TypeText, "", func(x *R) Value { return Text(x.VesselName) }},
			{"loa_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.LOA)) }},
			{"beam_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.Beam)) }},
			{"berth_length_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.BerthLength)) }},
			{"berth_width_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.BerthWidth)) }},
			{"fill_bp", TypeBP, "", func(x *R) Value { return Int(x.FillBP) }},
			{"waste_bp", TypeBP, "", func(x *R) Value { return Int(x.WasteBP) }},
			{"nights_in_period", TypeInt, "nights", func(x *R) Value { return Int(x.NightsInPeriod) }},
			{"applied_cents", TypeCents, "applied_cents", func(x *R) Value { return Opt(x.Applied) }},
			{"list_cents", TypeCents, "list_cents", func(x *R) Value { return Opt(x.List) }},
			{"lost_cents", TypeCents, "lost_cents", func(x *R) Value { return Opt(x.Lost) }},
		},
		totals: []tcol[T]{
			{"count", TypeInt, func(x *T) Value { return Int(x.Count) }},
			{"nights", TypeInt, func(x *T) Value { return Int(x.Nights) }},
			{"waste_bp_nights", TypeInt, func(x *T) Value { return Int(x.WasteBPNights) }},
			{"applied_cents", TypeCents, func(x *T) Value { return Opt(x.Applied) }},
			{"list_cents", TypeCents, func(x *T) Value { return Opt(x.List) }},
			{"lost_cents", TypeCents, func(x *T) Value { return Opt(x.Lost) }},
			{"unpriced", TypeInt, func(x *T) Value { return Int(x.Unpriced) }},
		},
	}
	t := build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
	s := Sheet{Name: "Suggerimenti", Columns: columns(lang,
		Column{Key: "suggestion_kind", Type: TypeText}, Column{Key: "allocation_ids", Type: TypeText},
		Column{Key: "from_berths", Type: TypeText}, Column{Key: "to_berths", Type: TypeText},
		Column{Key: "waste_before_bp", Type: TypeBP}, Column{Key: "waste_after_bp", Type: TypeBP},
		Column{Key: "gain_cm2_nights", Type: TypeCm2})}
	for _, g := range r.Suggestions {
		s.Rows = append(s.Rows, []Value{
			Text(g.Kind), Text(strings.Join(g.AllocationIDs, ", ")), Text(joinCodes(g.FromBerths)),
			Text(joinCodes(g.ToBerths)), Int(g.WasteBeforeBP), Int(g.WasteAfterBP), Int(g.GainCm2Nights),
		})
	}
	t.Summaries = append(t.Summaries, s)
	return t
}

// LostDemand is the R4 table.
func LostDemand(r *calc.LostDemandResult, lang string) Table {
	type R = calc.LostDemandRecord
	type T = calc.LostDemandTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"arrival", TypeDate, "", func(x *R) Value { return Day(x.Arrival) }},
			{"departure", TypeDate, "", func(x *R) Value { return Day(x.Departure) }},
			{"nights_in_period", TypeInt, "nights", func(x *R) Value { return Int(x.NightsInPeriod) }},
			{"outcome", TypeText, "", func(x *R) Value { return Text(x.Outcome) }},
			{"purpose", TypeText, "", func(x *R) Value { return Text(x.Purpose) }},
			{"term", TypeText, "", func(x *R) Value { return Text(x.Term) }},
			{"channel", TypeText, "", func(x *R) Value { return Text(x.Channel) }},
			{"source", TypeText, "", func(x *R) Value { return Text(x.Source) }},
			{"loa_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.LOA)) }},
			{"beam_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.Beam)) }},
			{"length_band", TypeText, "", func(x *R) Value { return Text(x.LengthBand) }},
			{"priced_category", TypeText, "", func(x *R) Value { return Text(x.PricedCategory) }},
			{"value_cents", TypeCents, "value_cents", func(x *R) Value { return Opt(x.Value) }},
		},
		totals: []tcol[T]{
			{"count", TypeInt, func(x *T) Value { return Int(x.Count) }},
			{"nights", TypeInt, func(x *T) Value { return Int(x.Nights) }},
			{"value_cents", TypeCents, func(x *T) Value { return Opt(x.Value) }},
			{"unpriced", TypeInt, func(x *T) Value { return Int(x.Unpriced) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Customers is the R5 table (section customers or renewals).
func Customers(r *calc.CustomersResult, lang string) Table {
	type R = calc.CustomersRecord
	type T = calc.CustomersTotals
	totals := []tcol[T]{
		{"customers", TypeInt, func(x *T) Value { return Int(x.Customers) }},
		{"new_customers", TypeInt, func(x *T) Value { return Int(x.NewCustomers) }},
		{"contracts", TypeInt, func(x *T) Value { return Int(x.Contracts) }},
		{"value_cents", TypeCents, func(x *T) Value { return Int(int64(x.Value)) }},
		{"arrears_cents", TypeCents, func(x *T) Value { return Int(int64(x.Arrears)) }},
		{"renewal_eligible", TypeInt, func(x *T) Value { return Int(x.RenewalEligible) }},
		{"renewal_renewed", TypeInt, func(x *T) Value { return Int(x.RenewalRenewed) }},
		{"renewal_rate_bp", TypeBP, func(x *T) Value { return BP(x.RenewalRateBP) }},
	}
	var cols []col[R]
	if r.Section == "renewals" {
		cols = []col[R]{
			{"contract_number", TypeText, "", func(x *R) Value { return Text(x.ContractNumber) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"term", TypeText, "", func(x *R) Value { return Text(x.Term) }},
			{"last_night", TypeDate, "", func(x *R) Value { return Day(x.LastNight) }},
			{"renewed", TypeInt, "renewal_renewed", func(x *R) Value { return Int(x.Renewed) }},
		}
	} else {
		cols = []col[R]{
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"nationality", TypeText, "", func(x *R) Value { return Text(x.Nationality) }},
			{"first_contract_on", TypeDate, "", func(x *R) Value { return Day(x.FirstContractOn) }},
			{"acquisition_channel", TypeText, "", func(x *R) Value { return Text(x.AcquisitionChannel) }},
			{"seniority_years", TypeInt, "", func(x *R) Value { return Int(x.SeniorityYears) }},
			{"seniority_band", TypeText, "", func(x *R) Value { return Text(x.SeniorityBand) }},
			{"new", TypeInt, "new_customers", func(x *R) Value { return Int(x.New) }},
			{"value_cents", TypeCents, "value_cents", func(x *R) Value { return Int(int64(x.Value)) }},
			{"arrears_cents", TypeCents, "arrears_cents", func(x *R) Value { return Int(int64(x.Arrears)) }},
			{"contracts", TypeInt, "contracts", func(x *R) Value { return Int(x.Contracts) }},
		}
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}

// Production is the R6 table.
func Production(r *calc.ProductionResult, lang string) Table {
	type R = calc.ProductionRecord
	type T = calc.ProductionTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"term", TypeText, "", func(x *R) Value { return Text(x.Term) }},
			{"status", TypeText, "", func(x *R) Value { return Text(x.Status) }},
			{"channel", TypeText, "", func(x *R) Value { return Text(x.Channel) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"starts_on", TypeDate, "", func(x *R) Value { return Day(x.StartsOn) }},
			{"ends_on", TypeDate, "", func(x *R) Value { return Day(x.EndsOn) }},
			{"total_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Total)) }},
			{"nights_in_period", TypeInt, "nights", func(x *R) Value { return Int(x.NightsInPeriod) }},
			{"competence_cents", TypeCents, "competence_cents", func(x *R) Value { return Int(int64(x.Competence)) }},
		},
		totals: []tcol[T]{
			{"contracts", TypeInt, func(x *T) Value { return Int(x.Contracts) }},
			{"nights", TypeInt, func(x *T) Value { return Int(x.Nights) }},
			{"competence_cents", TypeCents, func(x *T) Value { return Int(int64(x.Competence)) }},
			{"transits", TypeInt, func(x *T) Value { return Int(x.Transits) }},
			{"transit_nights", TypeInt, func(x *T) Value { return Int(x.TransitNights) }},
			{"transit_competence_cents", TypeCents, func(x *T) Value { return Int(int64(x.TransitCompetence)) }},
			{"transit_avg_night_cents", TypeCents, func(x *T) Value { return Opt(x.TransitAvgNight) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Accruals is the R7 table (section competence, deferral or accrual).
func Accruals(r *calc.AccrualsResult, lang string) Table {
	type R = calc.AccrualsRecord
	type T = calc.AccrualsTotals
	totals := []tcol[T]{
		{"closing_date", TypeDate, func(x *T) Value { return Day(x.ClosingDate) }},
		{"competence_net_cents", TypeCents, func(x *T) Value { return Int(int64(x.CompetenceNet)) }},
		{"competence_gross_cents", TypeCents, func(x *T) Value { return Int(int64(x.CompetenceGross)) }},
		{"deferral_net_cents", TypeCents, func(x *T) Value { return Int(int64(x.DeferralNet)) }},
		{"deferral_gross_cents", TypeCents, func(x *T) Value { return Int(int64(x.DeferralGross)) }},
		{"accrual_cents", TypeCents, func(x *T) Value { return Int(int64(x.Accrual)) }},
	}
	line := []col[R]{
		{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
		{"charge_number", TypeText, "", func(x *R) Value { return Text(x.ChargeNumber) }},
		{"line_seq", TypeInt, "", func(x *R) Value { return Int(int64(x.LineSeq)) }},
		{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
		{"description", TypeText, "", func(x *R) Value { return Text(x.Description) }},
		{"service_from", TypeDate, "", func(x *R) Value { return Day(x.ServiceFrom) }},
		{"service_to", TypeDate, "", func(x *R) Value { return Day(x.ServiceTo) }},
		{"amount_net_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.AmountNet)) }},
		{"amount_gross_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.AmountGross)) }},
	}
	var cols []col[R]
	switch r.Section {
	case "deferral":
		cols = slices.Concat(line, []col[R]{
			{"deferral_net_cents", TypeCents, "deferral_net_cents", func(x *R) Value { return Int(int64(x.DeferralNet)) }},
			{"deferral_gross_cents", TypeCents, "deferral_gross_cents", func(x *R) Value { return Int(int64(x.DeferralGross)) }},
		})
	case "accrual":
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"contract_number", TypeText, "", func(x *R) Value { return Text(x.ContractNumber) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"matured_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Matured)) }},
			{"charged_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Charged)) }},
			{"accrual_cents", TypeCents, "accrual_cents", func(x *R) Value { return Int(int64(x.Accrual)) }},
		}
	default:
		cols = slices.Concat(line, []col[R]{
			{"competence_net_cents", TypeCents, "competence_net_cents", func(x *R) Value { return Int(int64(x.CompetenceNet)) }},
			{"competence_gross_cents", TypeCents, "competence_gross_cents", func(x *R) Value { return Int(int64(x.CompetenceGross)) }},
		})
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}

// chargeCols are the columns of a charge line (R8, and R10 with the month first).
func chargeCols(withMonth bool, totalNet string) []col[calc.ChargesRecord] {
	type R = calc.ChargesRecord
	var cols []col[R]
	if withMonth {
		cols = append(cols, col[R]{"month", TypeMonth, "", func(x *R) Value { return Text(x.Month) }})
	}
	cols = append(cols, []col[R]{
		{"issued_on", TypeDate, "", func(x *R) Value { return Day(x.IssuedOn) }},
		{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
		{"seq", TypeInt, "", func(x *R) Value { return Int(int64(x.Seq)) }},
		{"due_on", TypeDate, "", func(x *R) Value { return Day(x.DueOn) }},
		{"origin_type", TypeText, "", func(x *R) Value { return Text(x.OriginType) }},
		{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
		{"item_code", TypeText, "", func(x *R) Value { return Text(x.ItemCode) }},
		{"description", TypeText, "", func(x *R) Value { return Text(x.Description) }},
		{"revenue_class", TypeText, "", func(x *R) Value { return Text(x.RevenueClass) }},
		{"vat_rate_bp", TypeBP, "", func(x *R) Value { return Int(x.VATRateBP) }},
		{"vat_nature", TypeText, "", func(x *R) Value { return Text(x.VATNature) }},
		{"service_from", TypeDate, "", func(x *R) Value { return Day(x.ServiceFrom) }},
		{"service_to", TypeDate, "", func(x *R) Value { return Day(x.ServiceTo) }},
		{"net_cents", TypeCents, totalNet, func(x *R) Value { return Int(int64(x.Net)) }},
	}...)
	if withMonth {
		return cols
	}
	return append(cols, []col[R]{
		{"vat_cents", TypeCents, "vat_cents", func(x *R) Value { return Int(int64(x.VAT)) }},
		{"gross_cents", TypeCents, "gross_cents", func(x *R) Value { return Int(int64(x.Gross)) }},
		{"payment_status", TypeText, "", func(x *R) Value { return Text(x.PaymentStatus) }},
		{"invoicing_status", TypeText, "", func(x *R) Value { return Text(x.InvoicingStatus) }},
		{"fiscal_mode", TypeText, "", func(x *R) Value { return Text(x.FiscalMode) }},
	}...)
}

// Charges is the R8 table.
func Charges(r *calc.ChargesResult, lang string) Table {
	type T = calc.ChargesTotals
	sp := spec[T, calc.ChargesRecord]{
		cols: chargeCols(false, "net_cents"),
		totals: []tcol[T]{
			{"charges", TypeInt, func(x *T) Value { return Int(x.Charges) }},
			{"lines", TypeInt, func(x *T) Value { return Int(x.Lines) }},
			{"net_cents", TypeCents, func(x *T) Value { return Int(int64(x.Net)) }},
			{"vat_cents", TypeCents, func(x *T) Value { return Int(int64(x.VAT)) }},
			{"gross_cents", TypeCents, func(x *T) Value { return Int(int64(x.Gross)) }},
			{"charged_cents", TypeCents, func(x *T) Value { return Int(int64(x.Charged)) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// RevenueClasses is the R10 table: the charge line contributions by month, the classes (Riepilogo) and the
// classes by month (Mesi).
func RevenueClasses(r *calc.RevenueClassesResult, lang string) Table {
	type T = calc.RevenueClassesTotals
	sp := spec[T, calc.ChargesRecord]{
		cols: chargeCols(true, "net_cents"),
		totals: []tcol[T]{
			{"lines", TypeInt, func(x *T) Value { return Int(x.Lines) }},
			{"net_cents", TypeCents, func(x *T) Value { return Int(int64(x.Net)) }},
			{"unclassified_cents", TypeCents, func(x *T) Value { return Int(int64(x.Unclassified)) }},
		},
	}
	t := build(r.Envelope, lang, sp, r.Totals, nil, r.Records)
	classes := Sheet{Name: SheetSummary, Columns: columns(lang,
		Column{Key: "revenue_class", Type: TypeText}, Column{Key: "class_label", Type: TypeText},
		Column{Key: "parent_key", Type: TypeText}, Column{Key: "net_cents", Type: TypeCents},
		Column{Key: "share_bp", Type: TypeBP})}
	months := Sheet{Name: "Mesi", Columns: columns(lang,
		Column{Key: "revenue_class", Type: TypeText}, Column{Key: "month", Type: TypeMonth},
		Column{Key: "net_cents", Type: TypeCents})}
	for _, c := range r.Classes {
		classes.Rows = append(classes.Rows, []Value{Text(c.Key), Text(c.Label), Text(c.ParentKey), Int(int64(c.Net)), BP(c.ShareBP)})
		for _, m := range c.Months {
			months.Rows = append(months.Rows, []Value{Text(c.Key), Text(m.Month), Int(int64(m.Net))})
		}
	}
	t.Summaries = []Sheet{classes, months}
	return t
}

// Payments is the R9 table (section main or payouts).
func Payments(r *calc.PaymentsResult, lang string) Table {
	type R = calc.PaymentsRecord
	type T = calc.PaymentsTotals
	amountTotal := "collected_cents"
	feeTotal := "fees_cents"
	if r.Section == "payouts" {
		amountTotal, feeTotal = "payouts_cents", ""
	}
	sp := spec[T, R]{
		cols: []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"method", TypeText, "", func(x *R) Value { return Text(x.Method) }},
			{"source", TypeText, "", func(x *R) Value { return Text(x.Source) }},
			{"provider_code", TypeText, "", func(x *R) Value { return Text(x.ProviderCode) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"amount_cents", TypeCents, amountTotal, func(x *R) Value { return Int(int64(x.Amount)) }},
			{"fee_cents", TypeCents, feeTotal, func(x *R) Value { return Int(int64(x.Fee)) }},
		},
		totals: []tcol[T]{
			{"count", TypeInt, func(x *T) Value { return Int(x.Count) }},
			{"collected_cents", TypeCents, func(x *T) Value { return Int(int64(x.Collected)) }},
			{"payments_cents", TypeCents, func(x *T) Value { return Int(int64(x.Payments)) }},
			{"refunds_cents", TypeCents, func(x *T) Value { return Int(int64(x.Refunds)) }},
			{"fees_cents", TypeCents, func(x *T) Value { return Int(int64(x.Fees)) }},
			{"payouts_cents", TypeCents, func(x *T) Value { return Int(int64(x.Payouts)) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Receivables is the R12 table (section open, future or reminders).
func Receivables(r *calc.ReceivablesResult, lang string) Table {
	type R = calc.ReceivablesRecord
	type T = calc.ReceivablesTotals
	totals := []tcol[T]{
		{"as_of", TypeDate, func(x *T) Value { return Day(x.AsOf) }},
		{"count", TypeInt, func(x *T) Value { return Int(x.Count) }},
		{"open_cents", TypeCents, func(x *T) Value { return Int(int64(x.Open)) }},
		{"overdue_cents", TypeCents, func(x *T) Value { return Int(int64(x.Overdue)) }},
		{"not_due_cents", TypeCents, func(x *T) Value { return Int(int64(x.Buckets.NotDue)) }},
		{"d0_30_cents", TypeCents, func(x *T) Value { return Int(int64(x.Buckets.D0_30)) }},
		{"d31_60_cents", TypeCents, func(x *T) Value { return Int(int64(x.Buckets.D31_60)) }},
		{"d61_90_cents", TypeCents, func(x *T) Value { return Int(int64(x.Buckets.D61_90)) }},
		{"d90_plus_cents", TypeCents, func(x *T) Value { return Int(int64(x.Buckets.D90Plus)) }},
		{"future_cents", TypeCents, func(x *T) Value { return Int(int64(x.Future)) }},
		{"reminders_sent", TypeInt, func(x *T) Value { return Int(x.RemindersSent) }},
	}
	var cols []col[R]
	switch r.Section {
	case "future":
		cols = []col[R]{
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"due_on", TypeDate, "", func(x *R) Value { return Day(x.DueOn) }},
			{"residual_cents", TypeCents, "future_cents", func(x *R) Value { return Int(int64(x.Residual)) }},
		}
	case "reminders":
		cols = []col[R]{
			{"sent_on", TypeDate, "", func(x *R) Value { return Day(x.SentOn) }},
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"reminder_level", TypeText, "", func(x *R) Value { return Text(x.ReminderLevel) }},
		}
	default:
		cols = []col[R]{
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"customer_name", TypeText, "", func(x *R) Value { return Text(x.CustomerName) }},
			{"origin_type", TypeText, "", func(x *R) Value { return Text(x.OriginType) }},
			{"issued_on", TypeDate, "", func(x *R) Value { return Day(x.IssuedOn) }},
			{"due_on", TypeDate, "", func(x *R) Value { return Day(x.DueOn) }},
			{"total_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Total)) }},
			{"paid_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Paid)) }},
			{"credited_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Credited)) }},
			{"residual_cents", TypeCents, "open_cents", func(x *R) Value { return Int(int64(x.Residual)) }},
			{"overdue", TypeInt, "", func(x *R) Value { return flag(x.Overdue) }},
			{"bucket", TypeText, "", func(x *R) Value { return Text(x.Bucket) }},
		}
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}

// Presences is the R13 table (section movements, absences or releases).
func Presences(r *calc.PresencesResult, lang string) Table {
	type R = calc.PresencesRecord
	type T = calc.PresencesTotals
	totals := []tcol[T]{
		{"arrivals", TypeInt, func(x *T) Value { return Int(x.Arrivals) }},
		{"departures", TypeInt, func(x *T) Value { return Int(x.Departures) }},
		{"absence_nights", TypeInt, func(x *T) Value { return Int(x.AbsenceNights) }},
		{"releases", TypeInt, func(x *T) Value { return Int(x.Releases) }},
		{"released_nights", TypeInt, func(x *T) Value { return Int(x.ReleasedNights) }},
		{"resold_nights", TypeInt, func(x *T) Value { return Int(x.ResoldNights) }},
		{"release_credit_cents", TypeCents, func(x *T) Value { return Opt(x.ReleaseCredit) }},
		{"release_revenue_cents", TypeCents, func(x *T) Value { return Opt(x.ReleaseRevenue) }},
	}
	contract := []col[R]{
		{"contract_number", TypeText, "", func(x *R) Value { return Text(x.ContractNumber) }},
		{"contract_kind", TypeText, "", func(x *R) Value { return Text(x.ContractKind) }},
		{"vessel_name", TypeText, "", func(x *R) Value { return Text(x.VesselName) }},
		{"berth_code", TypeText, "", func(x *R) Value { return Text(x.BerthCode) }},
	}
	var cols []col[R]
	switch r.Section {
	case "absences":
		cols = slices.Concat(contract, []col[R]{
			{"absence_nights", TypeInt, "absence_nights", func(x *R) Value { return Int(x.AbsenceNights) }},
		})
	case "releases":
		cols = slices.Concat(contract, []col[R]{
			{"release_from", TypeDate, "", func(x *R) Value { return Day(x.ReleaseFrom) }},
			{"release_to", TypeDate, "", func(x *R) Value { return Day(x.ReleaseTo) }},
			{"released_nights", TypeInt, "released_nights", func(x *R) Value { return Int(x.ReleasedNights) }},
			{"resold_nights", TypeInt, "resold_nights", func(x *R) Value { return Int(x.ResoldNights) }},
			{"credit_cents", TypeCents, "release_credit_cents", func(x *R) Value { return Opt(x.Credit) }},
			{"revenue_cents", TypeCents, "release_revenue_cents", func(x *R) Value { return Opt(x.Revenue) }},
		})
	default:
		cols = append([]col[R]{
			{"night", TypeDate, "", func(x *R) Value { return Day(x.Night) }},
			{"event", TypeText, "", func(x *R) Value { return Text(x.Event) }},
		}, contract...)
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}

// Rights is the R14 table (section holders, charges, allotments or mandates).
func Rights(r *calc.RightsResult, lang string) Table {
	type R = calc.RightsRecord
	type T = calc.RightsTotals
	totals := []tcol[T]{
		{"rights", TypeInt, func(x *T) Value { return Int(x.Rights) }},
		{"holders", TypeInt, func(x *T) Value { return Int(x.Holders) }},
		{"berths", TypeInt, func(x *T) Value { return Int(x.Berths) }},
		{"charges_issued_cents", TypeCents, func(x *T) Value { return Int(int64(x.ChargesIssued)) }},
		{"charges_collected_cents", TypeCents, func(x *T) Value { return Int(int64(x.ChargesCollected)) }},
		{"charges_residual_cents", TypeCents, func(x *T) Value { return Int(int64(x.ChargesResidual)) }},
		{"advance_cents", TypeCents, func(x *T) Value { return Int(int64(x.Advance)) }},
		{"balance_cents", TypeCents, func(x *T) Value { return Int(int64(x.Balance)) }},
		{"marina_share_cents", TypeCents, func(x *T) Value { return Int(int64(x.MarinaShare)) }},
		{"mandate_revenue_cents", TypeCents, func(x *T) Value { return Int(int64(x.MandateRevenue)) }},
		{"mandate_payout_cents", TypeCents, func(x *T) Value { return Int(int64(x.MandatePayout)) }},
	}
	var cols []col[R]
	switch r.Section {
	case "charges":
		cols = []col[R]{
			{"issued_on", TypeDate, "", func(x *R) Value { return Day(x.IssuedOn) }},
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"right_number", TypeText, "", func(x *R) Value { return Text(x.RightNumber) }},
			{"holder_name", TypeText, "", func(x *R) Value { return Text(x.HolderName) }},
			{"issued_cents", TypeCents, "charges_issued_cents", func(x *R) Value { return Int(int64(x.Issued)) }},
			{"paid_cents", TypeCents, "charges_collected_cents", func(x *R) Value { return Int(int64(x.Paid)) }},
			{"residual_cents", TypeCents, "charges_residual_cents", func(x *R) Value { return Int(int64(x.Residual)) }},
		}
	case "allotments":
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"run_kind", TypeText, "", func(x *R) Value { return Text(x.RunKind) }},
			{"item_code", TypeText, "", func(x *R) Value { return Text(x.ItemCode) }},
			{"right_number", TypeText, "", func(x *R) Value { return Text(x.RightNumber) }},
			{"holder_name", TypeText, "", func(x *R) Value { return Text(x.HolderName) }},
			{"amount_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Amount)) }},
		}
	case "mandates":
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"right_number", TypeText, "", func(x *R) Value { return Text(x.RightNumber) }},
			{"holder_name", TypeText, "", func(x *R) Value { return Text(x.HolderName) }},
			{"amount_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Amount)) }},
		}
	default:
		cols = []col[R]{
			{"right_number", TypeText, "", func(x *R) Value { return Text(x.RightNumber) }},
			{"berth_code", TypeText, "", func(x *R) Value { return Text(x.BerthCode) }},
			{"holder_name", TypeText, "", func(x *R) Value { return Text(x.HolderName) }},
			{"share_millis", TypeMilli, "", func(x *R) Value { return Int(x.ShareMillis) }},
		}
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}

// Cash is the R15 table (section sessions or fiscal) with the VAT and method sheets.
func Cash(r *calc.CashResult, lang string) Table {
	type R = calc.CashRecord
	type T = calc.CashTotals
	totals := []tcol[T]{
		{"sessions", TypeInt, func(x *T) Value { return Int(x.Sessions) }},
		{"difference_cents", TypeCents, func(x *T) Value { return Int(int64(x.Difference)) }},
		{"documents", TypeInt, func(x *T) Value { return Int(x.Documents) }},
		{"fiscal_total_cents", TypeCents, func(x *T) Value { return Int(int64(x.FiscalTotal)) }},
		{"fiscal_void_cents", TypeCents, func(x *T) Value { return Int(int64(x.FiscalVoid)) }},
	}
	var cols []col[R]
	if r.Section == "fiscal" {
		cols = []col[R]{
			{"business_date", TypeDate, "", func(x *R) Value { return Day(x.BusinessDate) }},
			{"number", TypeText, "", func(x *R) Value { return Text(x.Number) }},
			{"method", TypeText, "", func(x *R) Value { return Text(x.Method) }},
			{"total_cents", TypeCents, "fiscal_total_cents", func(x *R) Value { return Int(int64(x.Total)) }},
			{"void_cents", TypeCents, "fiscal_void_cents", func(x *R) Value { return Int(int64(x.Void)) }},
		}
	} else {
		cols = []col[R]{
			{"business_date", TypeDate, "", func(x *R) Value { return Day(x.BusinessDate) }},
			{"register_code", TypeText, "", func(x *R) Value { return Text(x.RegisterCode) }},
			{"status", TypeText, "", func(x *R) Value { return Text(x.Status) }},
			{"float_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Float)) }},
			{"expected_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Expected)) }},
			{"counted_cents", TypeCents, "", func(x *R) Value { return Int(int64(x.Counted)) }},
			{"difference_cents", TypeCents, "difference_cents", func(x *R) Value { return Int(int64(x.Difference)) }},
		}
	}
	t := build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
	vat := Sheet{Name: "IVA", Columns: columns(lang,
		Column{Key: "vat_rate_bp", Type: TypeBP}, Column{Key: "vat_nature", Type: TypeText},
		Column{Key: "net_cents", Type: TypeCents}, Column{Key: "vat_cents", Type: TypeCents},
		Column{Key: "gross_cents", Type: TypeCents})}
	for _, v := range r.Totals.VAT {
		vat.Rows = append(vat.Rows, []Value{Int(v.RateBP), Text(v.Nature), Int(int64(v.Net)), Int(int64(v.VAT)), Int(int64(v.Gross))})
	}
	methods := Sheet{Name: "Metodi", Columns: columns(lang,
		Column{Key: "method", Type: TypeText}, Column{Key: "total_cents", Type: TypeCents})}
	for _, m := range r.Totals.Methods {
		methods.Rows = append(methods.Rows, []Value{Text(m.Method), Int(int64(m.Total))})
	}
	t.Summaries = append(t.Summaries, vat, methods)
	return t
}

// Vessels is the R16 table.
func Vessels(r *calc.VesselsResult, lang string) Table {
	type R = calc.VesselsRecord
	type T = calc.VesselsTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"name", TypeText, "", func(x *R) Value { return Text(x.Name) }},
			{"nights", TypeInt, "nights", func(x *R) Value { return Int(x.Nights) }},
			{"loa_cm", TypeCm, "loa_sum_cm", func(x *R) Value { return Int(int64(x.LOA)) }},
			{"beam_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.Beam)) }},
			{"hull_type", TypeText, "", func(x *R) Value { return Text(x.HullType) }},
			{"flag", TypeText, "", func(x *R) Value { return Text(x.Flag) }},
			{"length_band", TypeText, "", func(x *R) Value { return Text(x.LengthBand) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
		},
		totals: []tcol[T]{
			{"vessels", TypeInt, func(x *T) Value { return Int(x.Vessels) }},
			{"nights", TypeInt, func(x *T) Value { return Int(x.Nights) }},
			{"loa_sum_cm", TypeCm, func(x *T) Value { return Int(int64(x.LOASum)) }},
			{"avg_loa_cm", TypeCm, func(x *T) Value { return Opt(x.AvgLOA) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// BerthRevenue is the R17 table.
func BerthRevenue(r *calc.BerthRevenueResult, lang string) Table {
	type R = calc.BerthRevenueRecord
	type T = calc.BerthRevenueTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"berth_code", TypeText, "", func(x *R) Value { return Text(x.BerthCode) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"length_cm", TypeCm, "length_cm", func(x *R) Value { return Int(int64(x.Length)) }},
			{"occupied_nights", TypeInt, "", func(x *R) Value { return Int(x.OccupiedNights) }},
			{"revenue_cents", TypeCents, "revenue_cents", func(x *R) Value { return Int(int64(x.Revenue)) }},
			{"revenue_per_m_cents", TypeCents, "revenue_per_m_cents", func(x *R) Value { return Opt(x.RevenuePerM) }},
		},
		totals: []tcol[T]{
			{"berths", TypeInt, func(x *T) Value { return Int(x.Berths) }},
			{"length_cm", TypeCm, func(x *T) Value { return Int(int64(x.Length)) }},
			{"revenue_cents", TypeCents, func(x *T) Value { return Int(int64(x.Revenue)) }},
			{"revenue_per_m_cents", TypeCents, func(x *T) Value { return Opt(x.RevenuePerM) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Comparison is the R11 table: one row per bucket of the current period with the aligned previous values
// (numerator and denominator columns for ratios); the Totale row carries the two period values.
func Comparison(c *calc.Comparison, lang string) Table {
	vt := unitType(c.Unit)
	t := Table{Report: calc.KeyComparison, From: c.Current.From, To: c.Current.To}
	cols := []Column{{Key: "bucket", Type: TypeDate}, {Key: "current", Type: vt}, {Key: "previous", Type: vt}}
	frac := c.Current.HasFraction || c.Previous.HasFraction
	if frac {
		cols = append(cols, Column{Key: "current_numerator", Type: TypeInt}, Column{Key: "current_denominator", Type: TypeInt},
			Column{Key: "previous_numerator", Type: TypeInt}, Column{Key: "previous_denominator", Type: TypeInt})
	}
	t.Columns = columns(lang, cols...)
	for _, p := range c.Series {
		row := []Value{Day(p.Bucket), Opt(p.Current), Opt(p.Previous)}
		if frac {
			row = append(row, Int(p.CurrentNumerator), Int(p.CurrentDenominator), Int(p.PreviousNumerator), Int(p.PreviousDenominator))
		}
		t.Rows = append(t.Rows, row)
	}
	t.Total = []Value{Text(label("total_row", lang)), Opt(c.Current.Value), Opt(c.Previous.Value)}
	if frac {
		t.Total = append(t.Total, Int(c.Current.Numerator), Int(c.Current.Denominator), Int(c.Previous.Numerator), Int(c.Previous.Denominator))
	}
	return t
}

// columns labels cs in lang.
func columns(lang string, cs ...Column) []Column {
	for i := range cs {
		cs[i].Label = label(cs[i].Key, lang)
	}
	return cs
}

// flag is 1 for true, 0 for false.
func flag(b bool) Value {
	if b {
		return Int(1)
	}
	return Int(0)
}
