package export

import "github.com/coste360/porto/internal/domain/report/calc"

// Table of the sensors report, view discrepancies (m12-plan §10.3): one row per (resource, night), codes only.

func init() {
	for k, v := range map[string][2]string{
		"map_name":                   {"Mappa", "Map"},
		"resource_type":              {"Tipo risorsa", "Resource type"},
		"from_cm":                    {"Da (m)", "From (m)"},
		"to_cm":                      {"A (m)", "To (m)"},
		"contractual":                {"Stato contrattuale", "Contractual status"},
		"presence":                   {"Presenza alle 02:00", "Presence at 02:00"},
		"physical":                   {"Stato fisico", "Physical state"},
		"discrepancy_kind":           {"Discrepanza", "Discrepancy"},
		"alert_count":                {"Avvisi", "Alerts"},
		"records":                    {"Righe", "Rows"},
		"occupied_unexpected":        {"Occupati non previsti", "Unexpected occupied"},
		"absent":                     {"Assenti", "Absent"},
		"resources_with_discrepancy": {"Risorse con discrepanze", "Resources with discrepancies"},
		"nights_compared":            {"Notti confrontate", "Nights compared"},
		"coverage_bp":                {"Copertura %", "Coverage %"},
	} {
		if _, ok := labels[k]; !ok {
			labels[k] = v
		}
	}
}

func cmOpt(p *int64) Value {
	if p == nil {
		return Null()
	}
	return Opt(calc.Some(*p))
}

// Sensors is the sensors table.
func Sensors(r *calc.SensorsResult, lang string) Table {
	type R = calc.SensorsRecord
	type T = calc.SensorsTotals
	cols := []col[R]{
		{"night", TypeDate, "", func(x *R) Value { return Day(x.Night) }},
		{"map_name", TypeText, "", func(x *R) Value { return Text(x.MapName) }},
		{"resource_code", TypeText, "", func(x *R) Value { return Text(x.ResourceCode) }},
		{"resource_type", TypeText, "", func(x *R) Value { return Text(x.ResourceType) }},
		{"from_cm", TypeCm, "", func(x *R) Value { return cmOpt(x.FromCm) }},
		{"to_cm", TypeCm, "", func(x *R) Value { return cmOpt(x.ToCm) }},
		{"contractual", TypeText, "", func(x *R) Value { return Text(x.Contractual) }},
		{"presence", TypeText, "", func(x *R) Value { return Text(x.Presence) }},
		{"physical", TypeText, "", func(x *R) Value { return Text(x.Physical) }},
		{"discrepancy_kind", TypeText, "", func(x *R) Value { return Text(x.Kind) }},
		{"alert_count", TypeInt, "", func(x *R) Value { return Int(int64(len(x.AlertIDs))) }},
	}
	totals := []tcol[T]{
		{"records", TypeInt, func(x *T) Value { return Int(x.Records) }},
		{"occupied_unexpected", TypeInt, func(x *T) Value { return Opt(x.OccupiedUnexpected) }},
		{"absent", TypeInt, func(x *T) Value { return Opt(x.Absent) }},
		{"resources_with_discrepancy", TypeInt, func(x *T) Value { return Opt(x.ResourcesWithDiscrepancy) }},
		{"nights_compared", TypeInt, func(x *T) Value { return Opt(x.NightsCompared) }},
		{"coverage_bp", TypeBP, func(x *T) Value { return BP(x.CoverageBP) }},
	}
	return build(r.Envelope, lang, spec[T, R]{cols: cols, totals: totals}, r.Totals, r.Groups, r.Records)
}
