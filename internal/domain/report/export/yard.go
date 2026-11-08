package export

import "github.com/coste360/porto/internal/domain/report/calc"

// Table of the yard report (m10-plan §12): one column set per view; hours in milli (TypeMilli), money in cents.

func init() {
	for k, v := range map[string][2]string{
		"employee_code":       {"Dipendente", "Employee"},
		"employee_name":       {"Nome", "Name"},
		"department":          {"Reparto", "Department"},
		"work_order_number":   {"Commessa", "Work order"},
		"work_order_status":   {"Stato commessa", "Order status"},
		"billing_mode":        {"Modalità", "Billing mode"},
		"work_type_code":      {"Lavorazione", "Work type"},
		"qualification_id":    {"Qualifica", "Qualification"},
		"capacity_milli":      {"Capacità (ore)", "Capacity (hours)"},
		"assigned_milli":      {"Assegnate (ore)", "Assigned (hours)"},
		"actual_milli":        {"Consuntivate (ore)", "Actual (hours)"},
		"load_bp":             {"Carico %", "Load %"},
		"overload":            {"Sovraccarico", "Overload"},
		"bay":                 {"Postazione", "Bay"},
		"revenue_cents":       {"Ricavo", "Revenue"},
		"cost_cents":          {"Costo", "Cost"},
		"cost_labour_cents":   {"Costo manodopera", "Labour cost"},
		"cost_material_cents": {"Costo materiali", "Material cost"},
		"cost_service_cents":  {"Costo servizi", "Service cost"},
		"cost_external_cents": {"Costi esterni", "External costs"},
		"margin_cents":        {"Margine", "Margin"},
		"margin_bp":           {"Margine %", "Margin %"},
		"quote_net_cents":     {"Imponibile preventivo", "Quote net"}, //nolint:misspell // Italian
		"delta_net_cents":     {"Scostamento", "Delta"},
		"equipment_code":      {"Mezzo", "Equipment"},
		"from_slot":           {"Dallo slot", "From slot"},
		"to_slot":             {"Allo slot", "To slot"},
		"booking_purpose":     {"Scopo", "Purpose"},
		"booking_status":      {"Stato", "Status"},
		"records":             {"Righe", "Rows"},
		"booked_slots":        {"Slot prenotati", "Booked slots"},
		"window_slots":        {"Slot disponibili", "Window slots"},
	} {
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
}

func intPtr(p *int) Value {
	if p == nil {
		return Null()
	}
	return Int(int64(*p))
}

// Yard is the yard table of the selected view.
func Yard(r *calc.YardResult, view, lang string) Table {
	type R = calc.YardRecord
	type T = calc.YardTotals
	var cols []col[R]
	var totals []tcol[T]
	switch view {
	case calc.YardViewLoad:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"employee_code", TypeText, "", func(x *R) Value { return Text(x.EmployeeCode) }},
			{"employee_name", TypeText, "", func(x *R) Value { return Text(x.EmployeeName) }},
			{"department", TypeText, "", func(x *R) Value { return Text(x.Department) }},
			{"bay", TypeText, "", func(x *R) Value { return Text(x.Bay) }},
			{"capacity_milli", TypeMilli, "capacity_milli", func(x *R) Value { return Opt(x.Capacity) }},
			{"assigned_milli", TypeMilli, "assigned_milli", func(x *R) Value { return Opt(x.Assigned) }},
			{"actual_milli", TypeMilli, "hours_milli", func(x *R) Value { return Opt(x.Actual) }},
			{"load_bp", TypeBP, "load_bp", func(x *R) Value { return BP(x.LoadBP) }},
			{"overload", TypeText, "", func(x *R) Value {
				if x.Overload == nil {
					return Null()
				}
				return yesNo(*x.Overload, lang)
			}},
		}
		totals = []tcol[T]{
			{"capacity_milli", TypeMilli, func(x *T) Value { return Opt(x.Capacity) }},
			{"assigned_milli", TypeMilli, func(x *T) Value { return Opt(x.Assigned) }},
			{"hours_milli", TypeMilli, func(x *T) Value { return Int(x.HoursMilli) }},
			{"load_bp", TypeBP, func(x *T) Value { return BP(x.LoadBP) }},
		}
	case calc.YardViewMargins, calc.YardViewMarginsWT:
		cols = []col[R]{
			{"work_order_number", TypeText, "", func(x *R) Value { return Text(x.WorkOrderNumber) }},
			{"work_order_status", TypeText, "", func(x *R) Value { return Text(x.WorkOrderStatus) }},
			{"billing_mode", TypeText, "", func(x *R) Value { return Text(x.BillingMode) }},
			{"work_type_code", TypeText, "", func(x *R) Value { return Text(x.WorkTypeCode) }},
			{"revenue_cents", TypeCents, "revenue_cents", func(x *R) Value { return Opt(x.Revenue) }},
			{"cost_cents", TypeCents, "cost_cents", func(x *R) Value { return Opt(x.Cost) }},
			{"cost_labour_cents", TypeCents, "", func(x *R) Value { return Opt(x.CostLabour) }},
			{"cost_material_cents", TypeCents, "", func(x *R) Value { return Opt(x.CostMaterial) }},
			{"cost_service_cents", TypeCents, "", func(x *R) Value { return Opt(x.CostService) }},
			{"cost_external_cents", TypeCents, "", func(x *R) Value { return Opt(x.CostExternal) }},
			{"margin_cents", TypeCents, "margin_cents", func(x *R) Value { return Opt(x.Margin) }},
			{"margin_bp", TypeBP, "margin_bp", func(x *R) Value { return BP(x.MarginBP) }},
			{"quote_net_cents", TypeCents, "quote_net_cents", func(x *R) Value { return Opt(x.QuoteNet) }},
			{"delta_net_cents", TypeCents, "", func(x *R) Value { return Opt(x.DeltaNet) }},
		}
		totals = []tcol[T]{
			{"records", TypeInt, func(x *T) Value { return Int(x.Records) }},
			{"revenue_cents", TypeCents, func(x *T) Value { return Opt(x.Revenue) }},
			{"cost_cents", TypeCents, func(x *T) Value { return Opt(x.Cost) }},
			{"margin_cents", TypeCents, func(x *T) Value { return Opt(x.Margin) }},
			{"margin_bp", TypeBP, func(x *T) Value { return BP(x.MarginBP) }},
			{"quote_net_cents", TypeCents, func(x *T) Value { return Opt(x.QuoteNet) }},
		}
	case calc.YardViewLifting:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"equipment_code", TypeText, "", func(x *R) Value { return Text(x.EquipmentCode) }},
			{"from_slot", TypeInt, "", func(x *R) Value { return intPtr(x.FromSlot) }},
			{"to_slot", TypeInt, "", func(x *R) Value { return intPtr(x.ToSlot) }},
			{"booking_purpose", TypeText, "", func(x *R) Value { return Text(x.Purpose) }},
			{"booking_status", TypeText, "", func(x *R) Value { return Text(x.BookingStatus) }},
		}
		totals = []tcol[T]{
			{"booked_slots", TypeInt, func(x *T) Value { return Opt(x.BookedSlots) }},
			{"window_slots", TypeInt, func(x *T) Value { return Opt(x.WindowSlots) }},
		}
	default:
		cols = []col[R]{
			{"date", TypeDate, "", func(x *R) Value { return Day(x.Date) }},
			{"employee_code", TypeText, "", func(x *R) Value { return Text(x.EmployeeCode) }},
			{"employee_name", TypeText, "", func(x *R) Value { return Text(x.EmployeeName) }},
			{"work_order_number", TypeText, "", func(x *R) Value { return Text(x.WorkOrderNumber) }},
			{"work_type_code", TypeText, "", func(x *R) Value { return Text(x.WorkTypeCode) }},
			{"qualification_id", TypeText, "", func(x *R) Value { return Text(x.QualificationID) }},
			{"hours_milli", TypeMilli, "hours_milli", func(x *R) Value { return Opt(x.HoursMilli) }},
			{"cost_cents", TypeCents, "cost_cents", func(x *R) Value { return Opt(x.Cost) }},
		}
		totals = []tcol[T]{
			{"hours_milli", TypeMilli, func(x *T) Value { return Int(x.HoursMilli) }},
			{"cost_cents", TypeCents, func(x *T) Value { return Opt(x.Cost) }},
		}
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}
