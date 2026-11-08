package export

import (
	"fmt"

	"github.com/coste360/porto/internal/domain/report/calc"
)

// col is a record column: get reads the cell from a record; total names the totals column whose value fills
// the Totale row under it ("" leaves it empty).
type col[R any] struct {
	key   string
	typ   Type
	total string
	get   func(*R) Value
}

// tcol is a totals column (Totale row and the Riepilogo sheet of the groups).
type tcol[T any] struct {
	key string
	typ Type
	get func(*T) Value
}

// spec is the table definition of one report section.
type spec[T, R any] struct {
	cols   []col[R]
	totals []tcol[T]
}

// build turns records and totals into a Table with the Riepilogo sheet of the groups.
func build[T, R any](env calc.Envelope, lang string, sp spec[T, R], totals T, groups []calc.Group[T], records []R) Table {
	t := Table{Report: env.Report, From: env.From, To: env.To, Section: env.Section}
	t.Columns = make([]Column, len(sp.cols))
	for i, c := range sp.cols {
		t.Columns[i] = Column{Key: c.key, Label: label(c.key, lang), Type: c.typ}
	}
	t.Rows = make([][]Value, len(records))
	for r := range records {
		row := make([]Value, len(sp.cols))
		for i, c := range sp.cols {
			row[i] = c.get(&records[r])
		}
		t.Rows[r] = row
	}
	byKey := make(map[string]Value, len(sp.totals))
	for _, tc := range sp.totals {
		byKey[tc.key] = tc.get(&totals)
	}
	t.Total = make([]Value, len(sp.cols))
	for i, c := range sp.cols {
		if c.total != "" {
			t.Total[i] = byKey[c.total]
		}
	}
	if len(t.Total) > 0 && t.Total[0].IsNull() {
		t.Total[0] = Text(label("total_row", lang))
	}
	t.Summaries = []Sheet{groupSheet(lang, sp.totals, groups)}
	return t
}

// groupSheet is the Riepilogo: one row per group with its recomputed totals.
func groupSheet[T any](lang string, tcols []tcol[T], groups []calc.Group[T]) Sheet {
	s := Sheet{Name: SheetSummary, Columns: []Column{{Key: "group", Label: label("group", lang), Type: TypeText}}}
	for _, tc := range tcols {
		s.Columns = append(s.Columns, Column{Key: tc.key, Label: label(tc.key, lang), Type: tc.typ})
	}
	for g := range groups {
		name := groups[g].Label
		if name == "" {
			name = groups[g].Key
		}
		row := []Value{Text(name)}
		for _, tc := range tcols {
			row = append(row, tc.get(&groups[g].Totals))
		}
		s.Rows = append(s.Rows, row)
	}
	return s
}

// Of returns the table of any report result or of the comparison (pointer to a calc result type).
func Of(v any, lang string) (Table, error) {
	switch r := v.(type) {
	case *calc.OccupancyResult:
		return Occupancy(r, lang), nil
	case *calc.EfficiencyResult:
		return Efficiency(r, lang), nil
	case *calc.WasteResult:
		return Waste(r, lang), nil
	case *calc.LostDemandResult:
		return LostDemand(r, lang), nil
	case *calc.CustomersResult:
		return Customers(r, lang), nil
	case *calc.ProductionResult:
		return Production(r, lang), nil
	case *calc.AccrualsResult:
		return Accruals(r, lang), nil
	case *calc.ChargesResult:
		return Charges(r, lang), nil
	case *calc.PaymentsResult:
		return Payments(r, lang), nil
	case *calc.RevenueClassesResult:
		return RevenueClasses(r, lang), nil
	case *calc.ReceivablesResult:
		return Receivables(r, lang), nil
	case *calc.PresencesResult:
		return Presences(r, lang), nil
	case *calc.RightsResult:
		return Rights(r, lang), nil
	case *calc.CashResult:
		return Cash(r, lang), nil
	case *calc.VesselsResult:
		return Vessels(r, lang), nil
	case *calc.BerthRevenueResult:
		return BerthRevenue(r, lang), nil
	case *calc.StorageResult:
		return Storage(r, lang), nil
	case *calc.MaintenanceResult:
		return Maintenance(r, lang), nil
	case *calc.UtilitiesResult:
		return Utilities(r, lang), nil
	case *calc.Comparison:
		return Comparison(r, lang), nil
	}
	return Table{}, fmt.Errorf("export: unsupported result %T", v)
}

// unitType is the column type of a value in unit u.
func unitType(u calc.Unit) Type {
	switch u {
	case calc.UnitCents, calc.UnitCentsPerM, calc.UnitCentsPerNight:
		return TypeCents
	case calc.UnitBP:
		return TypeBP
	case calc.UnitCm, calc.UnitCmNight:
		return TypeCm
	case calc.UnitCm2, calc.UnitCm2Night:
		return TypeCm2
	default:
		return TypeInt
	}
}

// joinCodes lists the berth codes of a suggestion.
func joinCodes(rs []calc.ResourceBrief) string {
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += ", "
		}
		out += r.Code
	}
	return out
}
