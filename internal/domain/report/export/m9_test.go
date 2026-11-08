package export

import (
	"bytes"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/money"
)

func m9Results() []any {
	d := calendar.MustParse("2026-03-05")
	st := &calc.StorageResult{Totals: calc.StorageTotals{
		AvailableCm2Nights: 1_000_000, OccupiedCm2Nights: 250_000, Boats: 1,
		OccupancyBP: calc.BP{Value: 2500, OK: true}, Production: calc.Some(money.Cents(100)),
	}}
	st.Envelope = env(calc.KeyStorage, "")
	st.Records = []calc.StorageRecord{{AreaCode: "SHED", MapKind: "shed", Nights: 10, FootprintCm2Nights: 250_000, ExitOn: d, ExitBlocked: true}}
	st.Groups = []calc.Group[calc.StorageTotals]{{Key: "a1", Label: "SHED", Totals: st.Totals}}
	mt := &calc.MaintenanceResult{Totals: calc.MaintenanceTotals{Tasks: 1, Open: 1, Overdue: 1}}
	mt.Envelope = env(calc.KeyMaintenance, "")
	mt.Records = []calc.MaintenanceRecord{{TaskID: "t1", ElementCode: "A1", Status: "open", DueOn: d, OverdueDays: 3}}
	ut := &calc.UtilitiesResult{Totals: calc.UtilitiesTotals{PowerMilli: 12_345, PendingMilli: 12_345}}
	ut.Envelope = env(calc.KeyUtilities, "")
	ut.Records = []calc.UtilitiesRecord{{MeterCode: "P01", Kind: "power", First: d, Last: d, Nights: 1, QtyMilli: 12_345, Status: "pending"}}
	return []any{st, mt, ut}
}

func TestExport_M9Reports(t *testing.T) {
	for _, res := range m9Results() {
		for _, lang := range []string{"it", "en"} {
			tab, err := Of(res, lang)
			if err != nil {
				t.Fatal(err)
			}
			name := string(tab.Report) + "/" + lang
			if len(tab.Columns) == 0 || len(tab.Rows) != 1 || len(tab.Total) != len(tab.Columns) {
				t.Fatalf("%s: table %+v", name, tab)
			}
			for _, c := range tab.Columns {
				if _, ok := labels[c.Key]; !ok {
					t.Errorf("%s: column %s has no label", name, c.Key)
				}
			}
			for _, s := range tab.Summaries {
				for _, c := range s.Columns {
					if _, ok := labels[c.Key]; !ok {
						t.Errorf("%s: summary column %s has no label", name, c.Key)
					}
				}
			}
			var b bytes.Buffer
			if err := WriteCSV(&b, tab); err != nil {
				t.Errorf("%s: csv %v", name, err)
			}
			if err := WriteXLSX(&b, tab); err != nil {
				t.Errorf("%s: xlsx %v", name, err)
			}
		}
	}
	tab := Storage(m9Results()[0].(*calc.StorageResult), "it")
	for i, c := range tab.Columns {
		if c.Key == "footprint_m2_nights" && tab.Rows[0][i].String() != "250000" { // cm²-nights, written as m² by the writers
			t.Errorf("footprint m² × nights = %q", tab.Rows[0][i].String())
		}
		if c.Key == "exit_blocked" && tab.Rows[0][i].String() != "Sì" {
			t.Errorf("exit blocked = %q", tab.Rows[0][i].String())
		}
	}
}
