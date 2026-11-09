package export

import "github.com/coste360/porto/internal/domain/report/calc"

// Table of the rental report (m11-plan §11): one column set per view; slots as integers, money in cents.

func init() {
	for k, v := range map[string][2]string{
		"unit_code":         {"Unità", "Unit"},
		"formula_code":      {"Formula", "Formula"},
		"booking_number":    {"Noleggio", "Rental"},
		"line_kind":         {"Tipo riga", "Line kind"},
		"available_slots":   {"Slot disponibili", "Available slots"},
		"rented_slots":      {"Slot noleggiati", "Rented slots"},
		"utilisation_bp":    {"Utilizzo %", "Utilisation %"},
		"net_cents":         {"Imponibile", "Net"}, //nolint:misspell // Italian
		"vat_cents":         {"IVA", "VAT"},
		"gross_cents":       {"Totale", "Gross"},
		"damage_status":     {"Stato danno", "Damage status"},
		"estimate_cents":    {"Stima", "Estimate"},
		"retained_cents":    {"Trattenuto", "Retained"},
		"deposit_mode":      {"Modalità cauzione", "Deposit mode"},
		"deposit_status":    {"Stato cauzione", "Deposit status"},
		"deposit_cents":     {"Cauzione", "Deposit"},
		"released_cents":    {"Restituito", "Released"},
		"outstanding_cents": {"In essere", "Outstanding"},
		"records":           {"Righe", "Rows"},
	} {
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
}

// Rental is the rental table of the selected view.
func Rental(r *calc.RentalResult, view, lang string) Table {
	type R = calc.RentalRecord
	type T = calc.RentalTotals
	var cols []col[R]
	var totals []tcol[T]
	switch view {
	case calc.RentalViewRevenue:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"booking_number", TypeText, "", func(x *R) Value { return Text(x.BookingNumber) }},
			{"unit_code", TypeText, "", func(x *R) Value { return Text(x.UnitCode) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"formula_code", TypeText, "", func(x *R) Value { return Text(x.FormulaCode) }},
			{"line_kind", TypeText, "", func(x *R) Value { return Text(x.LineKind) }},
			{"net_cents", TypeCents, "net_cents", func(x *R) Value { return Opt(x.Net) }},
			{"vat_cents", TypeCents, "vat_cents", func(x *R) Value { return Opt(x.VAT) }},
			{"gross_cents", TypeCents, "gross_cents", func(x *R) Value { return Opt(x.Gross) }},
		}
		totals = []tcol[T]{
			{"records", TypeInt, func(x *T) Value { return Int(x.Records) }},
			{"net_cents", TypeCents, func(x *T) Value { return Opt(x.Net) }},
			{"vat_cents", TypeCents, func(x *T) Value { return Opt(x.VAT) }},
			{"gross_cents", TypeCents, func(x *T) Value { return Opt(x.Gross) }},
		}
	case calc.RentalViewDamages:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"booking_number", TypeText, "", func(x *R) Value { return Text(x.BookingNumber) }},
			{"unit_code", TypeText, "", func(x *R) Value { return Text(x.UnitCode) }},
			{"damage_status", TypeText, "", func(x *R) Value { return Text(x.DamageStatus) }},
			{"estimate_cents", TypeCents, "estimate_cents", func(x *R) Value { return Opt(x.Estimate) }},
			{"retained_cents", TypeCents, "retained_cents", func(x *R) Value { return Opt(x.Retained) }},
		}
		totals = []tcol[T]{
			{"records", TypeInt, func(x *T) Value { return Int(x.Records) }},
			{"estimate_cents", TypeCents, func(x *T) Value { return Opt(x.Estimate) }},
			{"retained_cents", TypeCents, func(x *T) Value { return Opt(x.Retained) }},
		}
	case calc.RentalViewDeposits:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"booking_number", TypeText, "", func(x *R) Value { return Text(x.BookingNumber) }},
			{"unit_code", TypeText, "", func(x *R) Value { return Text(x.UnitCode) }},
			{"deposit_mode", TypeText, "", func(x *R) Value { return Text(x.DepositMode) }},
			{"deposit_status", TypeText, "", func(x *R) Value { return Text(x.DepositStatus) }},
			{"deposit_cents", TypeCents, "deposit_cents", func(x *R) Value { return Opt(x.Deposit) }},
			{"retained_cents", TypeCents, "retained_cents", func(x *R) Value { return Opt(x.Retained) }},
			{"released_cents", TypeCents, "released_cents", func(x *R) Value { return Opt(x.Released) }},
			{"outstanding_cents", TypeCents, "outstanding_cents", func(x *R) Value { return Opt(x.Outstanding) }},
		}
		totals = []tcol[T]{
			{"records", TypeInt, func(x *T) Value { return Int(x.Records) }},
			{"deposit_cents", TypeCents, func(x *T) Value { return Opt(x.Deposit) }},
			{"retained_cents", TypeCents, func(x *T) Value { return Opt(x.Retained) }},
			{"released_cents", TypeCents, func(x *T) Value { return Opt(x.Released) }},
			{"outstanding_cents", TypeCents, func(x *T) Value { return Opt(x.Outstanding) }},
		}
	default:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"unit_code", TypeText, "", func(x *R) Value { return Text(x.UnitCode) }},
			{"category", TypeText, "", func(x *R) Value { return Text(x.Category) }},
			{"available_slots", TypeInt, "available_slots", func(x *R) Value { return Opt(x.AvailableSlots) }},
			{"rented_slots", TypeInt, "rented_slots", func(x *R) Value { return Opt(x.RentedSlots) }},
			{"utilisation_bp", TypeBP, "utilisation_bp", func(x *R) Value { return BP(x.UtilisationBP) }},
		}
		totals = []tcol[T]{
			{"available_slots", TypeInt, func(x *T) Value { return Opt(x.AvailableSlots) }},
			{"rented_slots", TypeInt, func(x *T) Value { return Opt(x.RentedSlots) }},
			{"utilisation_bp", TypeBP, func(x *T) Value { return BP(x.UtilisationBP) }},
		}
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}
