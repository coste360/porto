//go:build integration

package charge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/internal/xlsx"
	"github.com/coste360/porto/pkg/ext"
)

var june = ExportFilter{PeriodFrom: contracttest.D("2027-06-01"), PeriodTo: contracttest.D("2027-06-30")}

// TestCharge_ExportedIsImmutable (INV-51): once exported, amounts and lines are refused by the service and by the
// triggers on both dialects; due date and description stay editable; corrections are linked adjustments.
func TestCharge_ExportedIsImmutable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		d := e.manual(t, "GRU", 2000, 6100)
		// Editable in place while to_invoice: adjust is refused with use_edit.
		if _, err := e.svc.Adjust(ctx, e.Owner, e.MarinaID, d.Charge.ID, d.Charge.Version, AdjustInput{
			Mode: "delta", Reason: "x", Lines: []ItemLine{{ItemID: e.items["GRU"], Qty: 1000, Unit: ptr(money.Cents(100))}},
		}); !errors.Is(err, ErrUseEdit) {
			t.Fatalf("adjust of an editable charge: %v", err)
		}
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, ""); err != nil {
			t.Fatal(err)
		}
		cur := e.charge(t, d.Charge.ID)
		if cur.InvoicingStatus != store.ChargeExported {
			t.Fatalf("status %s", cur.InvoicingStatus)
		}
		u := money.Cents(1)
		if _, err := e.svc.Update(ctx, e.Owner, e.MarinaID, cur.ID, cur.Version, EditInput{
			Lines: []ItemLine{{ItemID: e.items["GRU"], Qty: 1000, Unit: &u}},
		}); !errors.Is(err, ErrImmutable) {
			t.Fatalf("edit of exported lines: %v", err)
		}
		if _, err := e.svc.Cancel(ctx, e.Owner, e.MarinaID, cur.ID, cur.Version, "x"); !errors.Is(err, ErrImmutable) {
			t.Fatalf("cancel of exported: %v", err)
		}
		due := contracttest.D("2027-08-01")
		upd, err := e.svc.Update(ctx, e.Owner, e.MarinaID, cur.ID, cur.Version, EditInput{DueOn: &due})
		if err != nil || !upd.Charge.DueOn.Equal(due) {
			t.Fatalf("due date on exported: %v", err)
		}
		// Triggers (both dialects).
		_, err = db.NewUpdate().Model((*store.Charge)(nil)).Set("total_cents = 1").Where("id = ?", cur.ID).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.ChargeImmutableMessage) {
			t.Fatalf("trigger on amount: %v", err)
		}
		_, err = db.NewInsert().Model(&store.ChargeLine{
			ID: store.NewID(), MarinaID: e.MarinaID, ChargeID: cur.ID, Seq: 9, QtyMilli: 1000, NetCents: 1, GrossCents: 1,
			VATNature: "N1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.ChargeImmutableMessage) {
			t.Fatalf("trigger on line insert: %v", err)
		}
		_, err = db.NewUpdate().Model((*store.Charge)(nil)).Set("invoicing_status = 'to_invoice'").Where("id = ?", cur.ID).Exec(context.Background())
		if !store.IsTriggerViolation(err, store.ChargeImmutableMessage) {
			t.Fatalf("trigger on status back: %v", err)
		}
		// Corrections: delta (negative, offset to the residual) and replace.
		adj, err := e.svc.Adjust(ctx, e.Owner, e.MarinaID, cur.ID, upd.Charge.Version, AdjustInput{
			Mode: "delta", Reason: "sconto", Lines: []ItemLine{{ItemID: e.items["GRU"], Qty: -1000, Unit: ptr(money.Cents(2000))}},
		})
		if err != nil || len(adj) != 1 || adj[0].Charge.TotalCents != -2000 || adj[0].Charge.CorrectsChargeID != cur.ID {
			t.Fatalf("delta %+v %v", adj, err)
		}
		after := e.charge(t, cur.ID)
		if after.CreditedCents != 2000 || after.PaymentStatus != store.ChargePartiallyPaid {
			t.Fatalf("offset not applied: %+v", after)
		}
		rep, err := e.svc.Adjust(ctx, e.Owner, e.MarinaID, cur.ID, after.Version, AdjustInput{
			Mode: "replace", Reason: "errore", Lines: []ItemLine{{ItemID: e.items["GRU"], Qty: 1000, Unit: ptr(money.Cents(5000))}},
		})
		if err != nil || len(rep) != 2 || rep[0].Charge.TotalCents != -12200 || rep[1].Charge.TotalCents != 5000 {
			t.Fatalf("replace %+v %v", rep, err)
		}
		// The reversal offset the residual (12200 − 2000) and the rest became credit.
		if e.credits.grants[rep[0].Charge.ID] != 2000 {
			t.Fatalf("credit of the reversal %d", e.credits.grants[rep[0].Charge.ID])
		}
		// Adjustments go to the next export, linked.
		exp, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, "")
		if err != nil || exp.ChargeCount != 3 {
			t.Fatalf("second export %+v %v", exp, err)
		}
		e.invariants(t)
	})
}

func ptr[T any](v T) *T { return &v }

// TestChargeExport_EachChargeOnce (INV-53): overlapping periods and filters never export a charge twice as first;
// re-exports are flagged not first; cancelled charges are never exported; opening balances neither.
func TestChargeExport_EachChargeOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		a := e.manual(t, "GRU", 1000, 1000)
		b := e.manual(t, "ACQUA", 3000, 250)
		c := e.manual(t, "GRU", 1000, 500)
		if _, err := e.svc.Cancel(ctx, e.Owner, e.MarinaID, c.Charge.ID, c.Charge.Version, "errore"); err != nil {
			t.Fatal(err)
		}
		first, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, ExportFilter{
			PeriodFrom: june.PeriodFrom, PeriodTo: june.PeriodTo, OriginTypes: []string{store.ChargeOriginManual}, RevenueClass: a.Lines[0].RevenueClassCode,
		}, "")
		if err != nil || first.ChargeCount != 2 {
			t.Fatalf("first export %+v %v", first, err)
		}
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, ExportFilter{PeriodFrom: contracttest.D("2027-05-01"), PeriodTo: contracttest.D("2027-12-31")}, ""); !errors.Is(err, ErrExportEmpty) {
			t.Fatalf("overlapping export: %v", err)
		}
		re, err := e.svc.Reexport(ctx, e.Owner, e.MarinaID, ReexportInput{ExportID: first.ID})
		if err != nil || re.ChargeCount != 2 || re.Kind != "reexport" {
			t.Fatalf("reexport %+v %v", re, err)
		}
		if _, err := e.svc.Reexport(ctx, e.Owner, e.MarinaID, ReexportInput{ChargeIDs: []string{c.Charge.ID}}); err == nil {
			t.Fatal("re-export of a never exported charge accepted")
		}
		for _, id := range []string{a.Charge.ID, b.Charge.ID} {
			refs, _ := store.ChargeExportRefs(context.Background(), db.DB, e.scope, id)
			if len(refs) != 2 || !refs[0].IsFirst || refs[1].IsFirst {
				t.Fatalf("export refs of %s: %+v", id, refs)
			}
		}
		if refs, _ := store.ChargeExportRefs(context.Background(), db.DB, e.scope, c.Charge.ID); len(refs) != 0 {
			t.Fatalf("cancelled charge exported: %+v", refs)
		}
		e.invariants(t)
	})
}

// TestConcurrency_ChargeExportParallel (INV-53): 100 goroutines export the same period: every charge is in exactly
// one first export, every item's charge is exported.
func TestConcurrency_ChargeExportParallel(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		for i := range 30 {
			e.manual(t, "GRU", 1000, money.Cents(100+i))
		}
		var ok, empty atomic.Int32
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, "")
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrExportEmpty) || store.IsSerializationFailure(err):
					empty.Add(1)
				default:
					t.Errorf("export: %v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() < 1 {
			t.Fatal("no export succeeded")
		}
		var n int
		if err := db.NewRaw("SELECT COUNT(*) FROM charge_export_items WHERE is_first = ?", true).Scan(context.Background(), &n); err != nil {
			t.Fatal(err)
		}
		if n != 30 {
			t.Fatalf("%d first items, want 30", n)
		}
		e.invariants(t)
	})
}

// TestChargeExport_FilesMatchItems: the CSV and the workbook parse back to the export items × lines, with the
// totals of the export.
func TestChargeExport_FilesMatchItems(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		e.manual(t, "GRU", 1500, 12345)
		e.manual(t, "ACQUA", 1000, 999)
		tr := e.transit(t, e.Berths[0], "2027-06-05", "2027-06-08", true)
		exp, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, "")
		if err != nil {
			t.Fatal(err)
		}
		if exp.CSVSHA256 == "" || exp.XLSXSHA256 == "" || exp.PDFFileID == "" {
			t.Fatalf("files not built: %+v", exp)
		}
		csvB, _, err := e.svc.DownloadExport(ctx, e.Owner, e.MarinaID, exp.ID, FormatCSV)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(csvB, []byte("\ufeff")) {
			t.Fatal("CSV without BOM")
		}
		r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(csvB, []byte("\ufeff"))))
		r.Comma = ';'
		recs, err := r.ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		ids, _ := store.ExportChargeIDs(context.Background(), db.DB, e.scope, exp.ID)
		lines := e.lines(t, ids...)
		if len(recs)-1 != len(lines) || len(lines) != exp.LineCount {
			t.Fatalf("CSV rows %d, lines %d (%d)", len(recs)-1, len(lines), exp.LineCount)
		}
		var gross money.Cents
		for _, rec := range recs[1:] {
			v, ok := ParseEuros(rec[29])
			if !ok {
				t.Fatalf("total %q", rec[29])
			}
			gross += v
		}
		if want := TotalsOfExport(exp).Gross; gross != want {
			t.Fatalf("Σ CSV %d ≠ export total %d", gross, want)
		}
		xb, _, err := e.svc.DownloadExport(ctx, e.Owner, e.MarinaID, exp.ID, FormatXLSX)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := xlsx.ReadFirstSheet(bytes.NewReader(xb), int64(len(xb)), xlsx.DefaultLimits)
		if err != nil || len(rows) != len(recs) {
			t.Fatalf("xlsx rows %d %v", len(rows), err)
		}
		pdf, _, err := e.svc.DownloadExport(ctx, e.Owner, e.MarinaID, exp.ID, FormatPDF)
		if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatalf("pdf %v", err)
		}
		// Same items, same bytes: the download is the build recorded on the export.
		if sha256Hex(csvB) != exp.CSVSHA256 || sha256Hex(xb) != exp.XLSXSHA256 {
			t.Fatal("download differs from the recorded build")
		}
		_ = tr
	})
}

// TestInvoiceReconciliation_CSV: dry run reports without writing; apply sets invoiced on exported charges only.
func TestInvoiceReconciliation_CSV(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		a := e.manual(t, "GRU", 1000, 1000)
		b := e.manual(t, "GRU", 1000, 2000)
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, ""); err != nil {
			t.Fatal(err)
		}
		c := e.manual(t, "GRU", 1000, 3000)
		text := "Numero addebito;Numero fattura;Data\r\n" + a.Charge.Number + ";FT-1;15/06/2027\r\n" + b.Charge.Number +
			";FT-2;2027-06-16\r\n" + c.Charge.Number + ";FT-3;16/06/2027\r\nA2027-999999;FT-4;16/06/2027\r\n" + a.Charge.Number +
			";FT-5;16/06/2027\r\n"
		rec, rep, err := e.svc.Reconcile(ctx, e.Owner, e.MarinaID, ReconcileInput{CSV: text, DryRun: true})
		if err != nil || rec.MatchedCount != 2 || len(rep) != 5 {
			t.Fatalf("dry run %+v %+v %v", rec, rep, err)
		}
		want := []string{OutcomeOK, OutcomeOK, OutcomeNotExported, OutcomeUnknownCharge, OutcomeDuplicateRow}
		for i, r := range rep {
			if r.Outcome != want[i] {
				t.Fatalf("row %d: %s, want %s", i+1, r.Outcome, want[i])
			}
		}
		if e.charge(t, a.Charge.ID).InvoicingStatus != store.ChargeExported {
			t.Fatal("dry run wrote")
		}
		if _, _, err := e.svc.Reconcile(ctx, e.Owner, e.MarinaID, ReconcileInput{CSV: text}); err != nil {
			t.Fatal(err)
		}
		got := e.charge(t, a.Charge.ID)
		if got.InvoicingStatus != store.ChargeInvoiced || got.ExternalInvoiceNumber != "FT-1" || !got.ExternalInvoiceDate.Equal(contracttest.D("2027-06-15")) {
			t.Fatalf("invoiced %+v", got)
		}
		if e.charge(t, c.Charge.ID).InvoicingStatus != store.ChargeToInvoice {
			t.Fatal("unexported charge invoiced")
		}
		if _, err := e.svc.SetInvoice(ctx, e.Owner, e.MarinaID, c.Charge.ID, "FT-9", contracttest.D("2027-06-20")); !errors.Is(err, ErrNotExported) {
			t.Fatalf("manual invoice of to_invoice: %v", err)
		}
		e.invariants(t)
	})
}

// fakeExporter is a ChargeExporter of the tests.
type fakeExporter struct {
	mu    sync.Mutex
	sent  []ext.ChargeBatch
	refs  []ext.InvoiceRef
	fails bool
}

func (f *fakeExporter) Code() string { return "fake" }

func (f *fakeExporter) Export(_ context.Context, b ext.ChargeBatch) (*ext.ExportReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails {
		return nil, errors.New("down")
	}
	f.sent = append(f.sent, b)
	for i, c := range b.Charges {
		f.refs = append(f.refs, ext.InvoiceRef{ChargeID: c.ChargeID, Number: "EXT-" + string(rune('A'+i)), Date: "2027-06-30"})
	}
	return &ext.ExportReceipt{Ref: "batch-1"}, nil
}

func (f *fakeExporter) Poll(context.Context, ext.ExportReceipt) ([]ext.InvoiceRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refs, nil
}

// TestChargeExporter_FakeRoundTrip: an export with an exporter code is delivered by the job after commit, then
// polled: the charges become invoiced with the external numbers. Unknown codes are refused.
func TestChargeExporter_FakeRoundTrip(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		a := e.manual(t, "GRU", 1000, 1000)
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, "fake"); !errors.Is(err, ErrExporterUnknown) {
			t.Fatalf("unknown exporter: %v", err)
		}
		fx := &fakeExporter{}
		e.svc.SetExporters([]ext.ChargeExporter{fx})
		exp, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, "fake")
		if err != nil || exp.DeliveryStatus != "pending" {
			t.Fatalf("export %+v %v", exp, err)
		}
		if err := e.svc.RunExporter(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(fx.sent) != 1 || len(fx.sent[0].Charges) != 1 || fx.sent[0].Charges[0].Customer.Name == "" {
			t.Fatalf("batch %+v", fx.sent)
		}
		got, _ := store.GetChargeExport(context.Background(), db.DB, e.scope, exp.ID)
		if got.DeliveryStatus != "sent" || got.DeliveryRef != "batch-1" {
			t.Fatalf("delivery %+v", got)
		}
		if err := e.svc.RunExporter(context.Background()); err != nil {
			t.Fatal(err)
		}
		ch := e.charge(t, a.Charge.ID)
		if ch.InvoicingStatus != store.ChargeInvoiced || ch.ExternalInvoiceNumber != "EXT-A" {
			t.Fatalf("not invoiced %+v", ch)
		}
		e.invariants(t)
	})
}

// TestOpeningBalances_ImportUndo: a CSV and an Excel file of balances (dry run, apply) create invoiced charges
// (never exported); a negative balance is credit; undo cancels while nothing is paid.
func TestOpeningBalances_ImportUndo(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		cust := e.AddCustomer(t, "Bianchi")
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewUpdate().Model((*store.Customer)(nil)).Set("fiscal_code = 'BNCMRA80A01H501Z'").Where("id = ?", cust).Exec(ctx)
			return err
		})
		text := "cf;importo;scadenza;rif\nBNCMRA80A01H501Z;1.234,56;31/07/2027;FT-77\nXXXXXX;10,00;31/07/2027;\nBNCMRA80A01H501Z;0;31/07/2027;\n"
		m := OpeningMapping{CustomerMatch: "fiscal_code", CustomerColumn: 0, AmountColumn: 1, DueOnColumn: 2, DescriptionColumn: -1, ReferenceColumn: 3}
		dry, err := e.svc.ImportOpeningBalances(ctx, e.Owner, e.MarinaID, OpeningInput{
			Format: "csv", FileBase64: base64.StdEncoding.EncodeToString([]byte(text)), HasHeader: true, Mapping: m, DryRun: true,
		})
		if err != nil || dry.Status != "dry_run" {
			t.Fatalf("dry run %+v %v", dry, err)
		}
		rep := ReportOfImport(dry)
		if rep.ValidRows != 1 || len(rep.Issues) != 2 || rep.Issues[0].Code != "unknown_customer" || rep.Issues[1].Code != "zero_amount" || rep.TotalCents != 123456 {
			t.Fatalf("report %+v", rep)
		}
		applied, err := e.svc.ImportOpeningBalances(ctx, e.Owner, e.MarinaID, OpeningInput{
			Format: "csv", File: []byte(text), HasHeader: true, Mapping: m,
		})
		if err != nil || applied.ChargeCount != 1 {
			t.Fatalf("apply %+v %v", applied, err)
		}
		cs, _ := store.ChargesOfImport(context.Background(), db.DB, e.scope, applied.ID)
		if len(cs) != 1 || cs[0].InvoicingStatus != store.ChargeInvoiced || cs[0].ExternalInvoiceNumber != "FT-77" || cs[0].TotalCents != 123456 {
			t.Fatalf("charges %+v", cs)
		}
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, ""); !errors.Is(err, ErrExportEmpty) {
			t.Fatalf("opening balances exported: %v", err)
		}
		// Excel with a credit.
		var xb bytes.Buffer
		if err := xlsx.Write(&xb, []xlsx.Sheet{{Name: "Saldi", Header: []string{"cf", "importo", "scadenza"}, Rows: [][]xlsx.Cell{
			{xlsx.Str("BNCMRA80A01H501Z"), xlsx.Cents(-5000), xlsx.Date(calendar.MustParse("2027-07-31"))},
		}}}); err != nil {
			t.Fatal(err)
		}
		cr, err := e.svc.ImportOpeningBalances(ctx, e.Owner, e.MarinaID, OpeningInput{
			Format: "xlsx", File: xb.Bytes(), HasHeader: true, Mapping: OpeningMapping{CustomerMatch: "fiscal_code", AmountColumn: 1, DueOnColumn: 2, DescriptionColumn: -1, ReferenceColumn: -1},
		})
		if err != nil || cr.ChargeCount != 1 {
			t.Fatalf("xlsx %+v %v", cr, err)
		}
		ccs, _ := store.ChargesOfImport(context.Background(), db.DB, e.scope, cr.ID)
		if e.credits.grants[ccs[0].ID] != 5000 {
			t.Fatalf("credit %d", e.credits.grants[ccs[0].ID])
		}
		if _, err := e.svc.UndoOpeningBalances(ctx, e.Owner, e.MarinaID, cr.ID, cr.Version); !errors.Is(err, ErrImportNotUndoable) {
			t.Fatalf("undo of a credit import: %v", err)
		}
		undone, err := e.svc.UndoOpeningBalances(ctx, e.Owner, e.MarinaID, applied.ID, applied.Version)
		if err != nil || undone.Status != "undone" {
			t.Fatalf("undo %+v %v", undone, err)
		}
		if e.charge(t, cs[0].ID).PaymentStatus != store.ChargeCancelled {
			t.Fatal("not cancelled")
		}
		if _, err := e.svc.UndoOpeningBalances(ctx, e.Owner, e.MarinaID, applied.ID, undone.Version); !errors.Is(err, ErrImportNotUndoable) {
			t.Fatalf("second undo: %v", err)
		}
		e.invariants(t)
	})
}

// TestCharge_AnonymisedCustomerKeepsSnapshot: the fiscal snapshot is refreshed at the first export from the live
// customer, but not when the customer was anonymised (the creation snapshot stays).
func TestCharge_AnonymisedCustomerKeepsSnapshot(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		a := e.manual(t, "GRU", 1000, 1000)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewUpdate().Model((*store.Customer)(nil)).Set("last_name = 'Anonimo'").Set("anonymized_at = ?", time.Now()).
				Where("id = ?", e.Customer).Exec(ctx)
			return err
		})
		if _, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, ""); err != nil {
			t.Fatal(err)
		}
		f, err := e.svc.DecryptSnapshot(e.charge(t, a.Charge.ID))
		if err != nil || !strings.Contains(f.Name, "Rossi") {
			t.Fatalf("snapshot %+v %v", f, err)
		}
	})
}

// TestChargeExport_FilesFrozenAfterPayment (m5-plan §16 D27): a payment recorded, a vessel renamed and a due date
// moved after the
// export do not change its files: a later download is the same bytes, with the sha256 recorded at the first build;
// a re-export made afterwards shows the new payment status.
func TestChargeExport_FilesFrozenAfterPayment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		m := e.manual(t, "GRU", 1000, 5000)
		e.transit(t, e.Berths[0], "2027-06-05", "2027-06-08", true)
		exp, err := e.svc.CreateExport(ctx, e.Owner, e.MarinaID, june, "")
		if err != nil {
			t.Fatal(err)
		}
		download := func(id string) ([]byte, []byte) {
			t.Helper()
			c, _, err := e.svc.DownloadExport(ctx, e.Owner, e.MarinaID, id, FormatCSV)
			if err != nil {
				t.Fatal(err)
			}
			x, _, err := e.svc.DownloadExport(ctx, e.Owner, e.MarinaID, id, FormatXLSX)
			if err != nil {
				t.Fatal(err)
			}
			return c, x
		}
		csv1, xlsx1 := download(exp.ID)
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewUpdate().Model((*store.Charge)(nil)).Set("paid_amount_cents = total_cents").
				Set("payment_status = ?", store.ChargePaid).Where("id = ?", m.Charge.ID).Exec(ctx); err != nil {
				return err
			}
			_, err := tx.NewUpdate().Model((*store.Vessel)(nil)).Set("name = 'Rinominata'").Where("id = ?", e.Vessel).Exec(ctx)
			return err
		})
		// The due date stays editable on an exported charge (INV-51): moving it must not change the frozen files.
		cur := e.charge(t, m.Charge.ID)
		due := cur.DueOn.AddDays(45)
		if _, err := e.svc.Update(ctx, e.Owner, e.MarinaID, cur.ID, cur.Version, EditInput{DueOn: &due}); err != nil {
			t.Fatal(err)
		}
		csv2, xlsx2 := download(exp.ID)
		if !bytes.Equal(csv1, csv2) || !bytes.Equal(xlsx1, xlsx2) {
			t.Fatal("export files changed after a payment or a due date edit")
		}
		if sha256Hex(csv2) != exp.CSVSHA256 || sha256Hex(xlsx2) != exp.XLSXSHA256 {
			t.Fatal("download differs from the recorded build")
		}
		re, err := e.svc.Reexport(ctx, e.Owner, e.MarinaID, ReexportInput{ExportID: exp.ID})
		if err != nil {
			t.Fatal(err)
		}
		csv3, _ := download(re.ID)
		if !strings.Contains(string(csv3), "Rinominata") || !strings.Contains(string(csv3), ";Pagato;") {
			t.Fatalf("re-export does not show the current data:\n%s", csv3)
		}
	})
}

// TestChargeExport_ReexportRefusesNeverExported (INV-53): opening balances (born invoiced, never exported) and a
// cancelled one cannot be re-exported by charge id.
func TestChargeExport_ReexportRefusesNeverExported(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newCenv(t, db)
		ctx := fixture.Ctx()
		e.inTx(t, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewUpdate().Model((*store.Customer)(nil)).Set("fiscal_code = 'BNCMRA80A01H501Z'").Where("id = ?", e.Customer).Exec(ctx)
			return err
		})
		text := "cf;importo;scadenza\nBNCMRA80A01H501Z;100,00;31/07/2027\n"
		m := OpeningMapping{CustomerMatch: "fiscal_code", CustomerColumn: 0, AmountColumn: 1, DueOnColumn: 2, DescriptionColumn: -1, ReferenceColumn: -1}
		imp, err := e.svc.ImportOpeningBalances(ctx, e.Owner, e.MarinaID, OpeningInput{Format: "csv", File: []byte(text), HasHeader: true, Mapping: m})
		if err != nil {
			t.Fatal(err)
		}
		cs, _ := store.ChargesOfImport(context.Background(), db.DB, e.scope, imp.ID)
		if len(cs) != 1 {
			t.Fatalf("charges %+v", cs)
		}
		if _, err := e.svc.Reexport(ctx, e.Owner, e.MarinaID, ReexportInput{ChargeIDs: []string{cs[0].ID}}); err == nil {
			t.Fatal("re-export of a never exported opening balance accepted")
		}
		if _, err := e.svc.UndoOpeningBalances(ctx, e.Owner, e.MarinaID, imp.ID, imp.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Reexport(ctx, e.Owner, e.MarinaID, ReexportInput{ChargeIDs: []string{cs[0].ID}}); err == nil {
			t.Fatal("re-export of a cancelled opening balance accepted")
		}
		if refs, _ := store.ChargeExportRefs(context.Background(), db.DB, e.scope, cs[0].ID); len(refs) != 0 {
			t.Fatalf("opening balance exported: %+v", refs)
		}
	})
}
