package export

import "github.com/coste360/porto/internal/domain/report/calc"

// Tables of the M9 reports (m9-plan §11). Surfaces are exported in cm²-nights with TypeCm2 (m² with two
// decimals), quantities in milli with TypeMilli.

func init() {
	for k, v := range map[string][2]string{
		"yes":                   {"Sì", "Yes"},
		"no":                    {"No", "No"},
		"area_code":             {"Area", "Area"},
		"map_kind":              {"Tipo mappa", "Map kind"},
		"contract_code":         {"Contratto", "Contract"},
		"hull_m2_nights":        {"Scafo m² × notti", "Hull m² × nights"},
		"footprint_m2_nights":   {"Ingombro m² × notti", "Footprint m² × nights"},
		"available_m2_nights":   {"Disponibile m² × notti", "Available m² × nights"},
		"occupied_m2_nights":    {"Occupato m² × notti", "Occupied m² × nights"}, //nolint:misspell // Italian
		"occupancy_bp":          {"Occupazione %", "Occupancy %"},
		"exit_on":               {"Uscita", "Exit"},
		"exit_blocked":          {"Uscita bloccata", "Exit blocked"},
		"exits_blocked":         {"Uscite bloccate", "Blocked exits"},
		"production_cents":      {"Produzione", "Production"},
		"boats":                 {"Imbarcazioni", "Boats"},
		"element_type":          {"Tipo elemento", "Element type"},
		"element_code":          {"Elemento", "Element"},
		"intervention":          {"Intervento", "Intervention"}, //nolint:misspell // Italian
		"priority":              {"Priorità", "Priority"},
		"done_on":               {"Eseguito il", "Done on"},
		"overdue_days":          {"Giorni di ritardo", "Days overdue"},
		"internal_minutes":      {"Minuti interni", "Internal minutes"},
		"external_cost_cents":   {"Costi esterni", "External costs"},
		"tasks":                 {"Interventi", "Tasks"},
		"tasks_open":            {"Aperti", "Open"},
		"tasks_scheduled":       {"Pianificati", "Scheduled"},
		"tasks_in_progress":     {"In corso", "In progress"},
		"tasks_done":            {"Eseguiti", "Done"},
		"tasks_cancelled":       {"Annullati", "Cancelled"},
		"meter_code":            {"Contatore", "Meter"},
		"first_night":           {"Dalla notte", "From night"},
		"qty_milli":             {"Quantità", "Quantity"},
		"power_milli":           {"Energia (kWh)", "Power (kWh)"},
		"water_milli":           {"Acqua (m³)", "Water (m³)"},
		"hours_milli":           {"Ore", "Hours"},
		"cycles_milli":          {"Cicli", "Cycles"},
		"pending_milli":         {"Da addebitare", "To charge"},
		"unassigned_milli":      {"Non assegnato", "Unassigned"},
		"charged_milli":         {"Addebitato (quantità)", "Charged (quantity)"},
		"pending_value_cents":   {"Valore da addebitare", "Value to charge"},
		"utility_charged_cents": {"Importo addebitato", "Charged amount"},
	} {
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
}

// yesNo is a localised boolean cell.
func yesNo(b bool, lang string) Value {
	if b {
		return Text(label("yes", lang))
	}
	return Text(label("no", lang))
}

// Storage is the storage table.
func Storage(r *calc.StorageResult, lang string) Table {
	type R = calc.StorageRecord
	type T = calc.StorageTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"area_code", TypeText, "", func(x *R) Value { return Text(x.AreaCode) }},
			{"map_kind", TypeText, "", func(x *R) Value { return Text(x.MapKind) }},
			{"contract_code", TypeText, "", func(x *R) Value { return Text(x.ContractCode) }},
			{"contract_kind", TypeText, "", func(x *R) Value { return Text(x.ContractKind) }},
			{"loa_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.LOA)) }},
			{"beam_cm", TypeCm, "", func(x *R) Value { return Int(int64(x.Beam)) }},
			{"nights", TypeInt, "", func(x *R) Value { return Int(x.Nights) }},
			{"hull_m2_nights", TypeCm2, "hull_m2_nights", func(x *R) Value { return Int(x.HullCm2Nights) }},
			{"footprint_m2_nights", TypeCm2, "occupied_m2_nights", func(x *R) Value { return Int(x.FootprintCm2Nights) }},
			{"exit_on", TypeDate, "", func(x *R) Value { return Day(x.ExitOn) }},
			{"exit_blocked", TypeText, "", func(x *R) Value { return yesNo(x.ExitBlocked, lang) }},
			{"production_cents", TypeCents, "production_cents", func(x *R) Value { return Opt(x.Production) }},
		},
		totals: []tcol[T]{
			{"available_m2_nights", TypeCm2, func(x *T) Value { return Int(x.AvailableCm2Nights) }},
			{"occupied_m2_nights", TypeCm2, func(x *T) Value { return Int(x.OccupiedCm2Nights) }},
			{"hull_m2_nights", TypeCm2, func(x *T) Value { return Int(x.HullCm2Nights) }},
			{"occupancy_bp", TypeBP, func(x *T) Value { return BP(x.OccupancyBP) }},
			{"boats", TypeInt, func(x *T) Value { return Int(x.Boats) }},
			{"exits_blocked", TypeInt, func(x *T) Value { return Int(x.ExitsBlocked) }},
			{"production_cents", TypeCents, func(x *T) Value { return Opt(x.Production) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Maintenance is the maintenance table.
func Maintenance(r *calc.MaintenanceResult, lang string) Table {
	type R = calc.MaintenanceRecord
	type T = calc.MaintenanceTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"element_type", TypeText, "", func(x *R) Value { return Text(x.ElementType) }},
			{"element_code", TypeText, "", func(x *R) Value { return Text(x.ElementCode) }},
			{"intervention", TypeText, "", func(x *R) Value { return Text(x.Intervention) }},
			{"source", TypeText, "", func(x *R) Value { return Text(x.Source) }},
			{"priority", TypeText, "", func(x *R) Value { return Text(x.Priority) }},
			{"status", TypeText, "tasks", func(x *R) Value { return Text(x.Status) }},
			{"due_on", TypeDate, "", func(x *R) Value { return Day(x.DueOn) }},
			{"done_on", TypeDate, "", func(x *R) Value { return Day(x.DoneOn) }},
			{"overdue_days", TypeInt, "overdue", func(x *R) Value { return Int(x.OverdueDays) }},
			{"internal_minutes", TypeInt, "internal_minutes", func(x *R) Value { return Int(x.InternalMinutes) }},
			{"external_cost_cents", TypeCents, "external_cost_cents", func(x *R) Value { return Opt(x.ExternalCost) }},
		},
		totals: []tcol[T]{
			{"tasks", TypeInt, func(x *T) Value { return Int(x.Tasks) }},
			{"tasks_open", TypeInt, func(x *T) Value { return Int(x.Open) }},
			{"tasks_scheduled", TypeInt, func(x *T) Value { return Int(x.Scheduled) }},
			{"tasks_in_progress", TypeInt, func(x *T) Value { return Int(x.InProgress) }},
			{"tasks_done", TypeInt, func(x *T) Value { return Int(x.Done) }},
			{"tasks_cancelled", TypeInt, func(x *T) Value { return Int(x.Cancelled) }},
			{"overdue", TypeInt, func(x *T) Value { return Int(x.Overdue) }},
			{"internal_minutes", TypeInt, func(x *T) Value { return Int(x.InternalMinutes) }},
			{"external_cost_cents", TypeCents, func(x *T) Value { return Opt(x.ExternalCost) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}

// Utilities is the utilities table.
func Utilities(r *calc.UtilitiesResult, lang string) Table {
	type R = calc.UtilitiesRecord
	type T = calc.UtilitiesTotals
	sp := spec[T, R]{
		cols: []col[R]{
			{"meter_code", TypeText, "", func(x *R) Value { return Text(x.MeterCode) }},
			{"kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
			{"resource_type", TypeText, "", func(x *R) Value { return Text(x.ResourceType) }},
			{"contract_code", TypeText, "", func(x *R) Value { return Text(x.ContractCode) }},
			{"first_night", TypeDate, "", func(x *R) Value { return Day(x.First) }},
			{"last_night", TypeDate, "", func(x *R) Value { return Day(x.Last) }},
			{"nights", TypeInt, "", func(x *R) Value { return Int(x.Nights) }},
			{"qty_milli", TypeMilli, "", func(x *R) Value { return Int(x.QtyMilli) }},
			{"status", TypeText, "", func(x *R) Value { return Text(x.Status) }},
			{"utility_charged_cents", TypeCents, "utility_charged_cents", func(x *R) Value { return Opt(x.Charged) }},
			{"pending_value_cents", TypeCents, "pending_value_cents", func(x *R) Value { return Opt(x.PendingValue) }},
		},
		totals: []tcol[T]{
			{"power_milli", TypeMilli, func(x *T) Value { return Int(x.PowerMilli) }},
			{"water_milli", TypeMilli, func(x *T) Value { return Int(x.WaterMilli) }},
			{"hours_milli", TypeMilli, func(x *T) Value { return Int(x.HoursMilli) }},
			{"cycles_milli", TypeMilli, func(x *T) Value { return Int(x.CyclesMilli) }},
			{"pending_milli", TypeMilli, func(x *T) Value { return Int(x.PendingMilli) }},
			{"unassigned_milli", TypeMilli, func(x *T) Value { return Int(x.UnassignedMilli) }},
			{"charged_milli", TypeMilli, func(x *T) Value { return Int(x.ChargedMilli) }},
			{"utility_charged_cents", TypeCents, func(x *T) Value { return Opt(x.Charged) }},
			{"pending_value_cents", TypeCents, func(x *T) Value { return Opt(x.PendingValue) }},
		},
	}
	return build(r.Envelope, lang, sp, r.Totals, r.Groups, r.Records)
}
