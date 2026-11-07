//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/csv"
	"reflect"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/internal/xlsx"
)

// TestAcceptance_M5_SimulatedYear (m5-plan §11.4; spec 32.1 "Esportazione", 32.3 M5): a year of moorings with
// instalments and stay services at mixed rates, transits (confirmed at the desk, shortened before and after the
// weekly export, cancelled after confirmation), manual charges, counter sales, adjustments of exported charges,
// weekly exports (some filtered by origin, one re-exported) and monthly invoice reconciliations. Assertions:
//  1. exactly once: every non-cancelled charge is in exactly one first export, the union of the first exports' CSV
//     files has each charge's lines exactly once and equals the database items; the workbooks parse to the same
//     rows;
//  2. the exported lines of each completed contract and charged transit sum per (rate, nature) to its chargeplan
//     target, and to the snapshot VAT summary when it was never repriced;
//  3. each export's stored totals equal the sums of its file rows, its grand total the sum of its charge totals;
//  4. the billing invariant queries are empty every week, the contract and occupancy ones at the end; the invoiced
//     charges are the reconciled rows.
func TestAcceptance_M5_SimulatedYear(t *testing.T) {
	if testing.Short() {
		t.Skip("simulated year")
	}
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newSimYear(t, db)
		ctx := context.Background()
		scope, err := store.ResolveMarinaScope(ctx, db.DB, y.marina)
		if err != nil {
			t.Fatal(err)
		}
		checkBilling := func(when string) {
			vs, err := store.CheckBillingInvariants(ctx, db.DB, scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range vs {
				t.Errorf("%s: %s %v %s", when, v.Query, v.IDs, v.Detail)
			}
			if len(vs) > 0 {
				t.FailNow()
			}
		}
		y.run(t, func(d calendar.Date) {
			if d.ISOWeekday() == 7 {
				checkBilling(d.String())
			}
		})
		checkBilling("end")

		// 1. Exactly once.
		var all []store.Charge
		if err := db.NewSelect().Model(&all).Where("marina_id = ?", y.marina).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		live := map[string]store.Charge{}
		for _, c := range all {
			if c.PaymentStatus != store.ChargeCancelled && c.OriginType != store.ChargeOriginOpeningBalance {
				live[c.ID] = c
			}
		}
		seenLines := map[string]bool{}
		firstOf := map[string]string{}
		rowsOfExport := map[string][][]string{}
		for _, id := range y.exports {
			rows := y.downloadCSV(t, id)
			rowsOfExport[id] = rows
			for _, r := range rows {
				chargeID, seq := r[37], r[18]
				key := chargeID + "/" + seq
				if seenLines[key] {
					t.Fatalf("line %s exported twice as first", key)
				}
				seenLines[key] = true
				if prev, ok := firstOf[chargeID]; ok && prev != id {
					t.Fatalf("charge %s in first exports %s and %s", chargeID, prev, id)
				}
				firstOf[chargeID] = id
			}
			xb := y.download(t, id, "xlsx")
			xr, err := xlsx.ReadFirstSheet(bytes.NewReader(xb), int64(len(xb)), xlsx.DefaultLimits)
			if err != nil || len(xr)-1 != len(rows) {
				t.Fatalf("export %s: workbook rows %d, CSV rows %d (%v)", id, len(xr)-1, len(rows), err)
			}
		}
		for id, c := range live {
			if firstOf[id] == "" {
				t.Fatalf("charge %s (%s, %s) never exported", c.Number, c.OriginType, c.InvoicingStatus)
			}
			if c.FirstExportID != firstOf[id] {
				t.Fatalf("charge %s first export %s, files say %s", c.Number, c.FirstExportID, firstOf[id])
			}
		}
		for id := range firstOf {
			if _, ok := live[id]; !ok {
				t.Fatalf("exported charge %s is cancelled or unknown", id)
			}
		}
		var lineCount int
		ids := make([]string, 0, len(live))
		for id := range live {
			ids = append(ids, id)
		}
		dbLines, err := store.ChargeLinesOf(ctx, db.DB, scope, ids)
		if err != nil {
			t.Fatal(err)
		}
		lineCount = len(dbLines)
		if lineCount != len(seenLines) {
			t.Fatalf("%d lines in the files, %d in the database", len(seenLines), lineCount)
		}
		if len(y.reexports) == 0 {
			t.Error("no re-export in the scenario")
		}

		// 3. Per export: stored totals = Σ file rows per (rate, nature); grand total = Σ charge totals.
		for _, id := range y.exports {
			e, err := store.GetChargeExport(ctx, db.DB, scope, id)
			if err != nil {
				t.Fatal(err)
			}
			tt := charge.TotalsOfExport(e)
			got := bucketsOfRows(t, rowsOfExport[id])
			if !reflect.DeepEqual(nonZero(got), nonZero(tt.ByVAT)) {
				t.Fatalf("export %s: totals %+v, rows %+v", e.Number, tt.ByVAT, got)
			}
			if tt.Gross != tt.ChargeTotals {
				t.Fatalf("export %s: lines %d ≠ charges %d", e.Number, tt.Gross, tt.ChargeTotals)
			}
		}

		// 2. Exported lines per contract and transit = its target per (rate, nature).
		linesOfCharge := map[string][][]string{}
		for _, rows := range rowsOfExport {
			for _, r := range rows {
				linesOfCharge[r[37]] = append(linesOfCharge[r[37]], r)
			}
		}
		targets, err := store.ContractsForTargetCheck(ctx, db.DB, scope)
		if err != nil {
			t.Fatal(err)
		}
		checked, plain := 0, 0
		for i := range targets {
			c := &targets[i]
			var rows [][]string
			for _, ch := range live {
				if ch.ContractID == c.ID && ch.OriginType != store.ChargeOriginTermination {
					rows = append(rows, linesOfCharge[ch.ID]...)
				}
			}
			target, err := y.h.app.Charges.ContractTargetOf(ctx, db.DB, scope, c)
			if err != nil {
				t.Fatalf("contract %s: %v", c.ID, err)
			}
			got, want := nonZero(bucketsOfRows(t, rows)), nonZero(chargeplan.Totals(target.Lines))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("contract %s (%s, %s): exported %+v, target %+v", c.ID, c.Kind, c.Status, got, want)
			}
			checked++
			if snap := contract.DecodeSnapshot(c.PriceSnapshot); snap != nil && len(snap.Adjustments) == 0 {
				if !reflect.DeepEqual(got, nonZero(snap.Breakdown.VATSummary)) {
					t.Fatalf("contract %s: exported %+v, snapshot %+v", c.ID, got, snap.Breakdown.VATSummary)
				}
				plain++
			}
		}
		if checked < 50 || plain == 0 {
			t.Fatalf("only %d contracts and transits checked (%d without adjustments)", checked, plain)
		}

		// 4. Invariants and reconciliation.
		vs, err := store.CheckContractInvariants(ctx, db.DB, y.marina)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("contract invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
		if vs, err = store.CheckOccupancyInvariants(ctx, db.DB, y.marina, y.today); err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("occupancy invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
		var invoiced, matched int
		if err := db.NewRaw("SELECT COUNT(*) FROM charges WHERE marina_id = ? AND invoicing_status = 'invoiced' AND origin_type <> 'opening_balance'", y.marina).Scan(ctx, &invoiced); err != nil {
			t.Fatal(err)
		}
		if err := db.NewRaw("SELECT COALESCE(SUM(matched_count), 0) FROM invoice_reconciliations WHERE marina_id = ? AND dry_run = ?", y.marina, false).Scan(ctx, &matched); err != nil {
			t.Fatal(err)
		}
		if invoiced == 0 || invoiced != matched {
			t.Fatalf("invoiced %d, reconciled %d", invoiced, matched)
		}
		t.Logf("charges %d (live %d), lines %d, exports %d, contracts and transits checked %d (%d without adjustments), invoiced %d",
			len(all), len(live), lineCount, len(y.exports), checked, plain, invoiced)
	})
}

// download fetches a file of an export.
func (y *simYear) download(t *testing.T, exportID, format string) []byte {
	t.Helper()
	r := y.owner.do(t, "GET", y.M+"/charge-exports/"+exportID+"/file?format="+format, nil)
	if r.status != 200 {
		t.Fatalf("download %s %s: %d %s", exportID, format, r.status, r.body)
	}
	return r.body
}

// downloadCSV returns the data rows of an export's CSV.
func (y *simYear) downloadCSV(t *testing.T, exportID string) [][]string {
	t.Helper()
	b := bytes.TrimPrefix(y.download(t, exportID, "csv"), []byte("\ufeff"))
	r := csv.NewReader(bytes.NewReader(b))
	r.Comma = ';'
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 || recs[0][0] != "Numero addebito" || len(recs[0]) != len(charge.ExportColumns) {
		t.Fatalf("CSV header %v", recs)
	}
	return recs[1:]
}

// bucketsOfRows sums CSV rows per (rate, nature) (columns Imponibile, Aliquota IVA, Natura, IVA, Totale riga).
func bucketsOfRows(t *testing.T, rows [][]string) []chargeplan.VATBucket {
	t.Helper()
	var lines []chargeplan.Line
	for _, r := range rows {
		net, ok1 := charge.ParseEuros(r[25])
		vat, ok2 := charge.ParseEuros(r[28])
		gross, ok3 := charge.ParseEuros(r[29])
		rate, ok4 := charge.ParseEuros(r[26]) // percent: "22" → 2200 cents = basis points
		if !ok1 || !ok2 || !ok3 || !ok4 || net+vat != gross {
			t.Fatalf("row %v", r)
		}
		lines = append(lines, chargeplan.Line{VATRateBP: int64(rate), VATNature: r[27], Net: net, VAT: vat, Gross: gross})
	}
	return chargeplan.Totals(lines)
}

func nonZero(b []chargeplan.VATBucket) []chargeplan.VATBucket {
	out := []chargeplan.VATBucket{}
	for _, x := range b {
		if x.Gross != 0 || x.VAT != 0 || x.Net != money.Cents(0) {
			out = append(out, x)
		}
	}
	return out
}
