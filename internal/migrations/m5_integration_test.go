//go:build integration

package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestMigrations_M5Constraints: the M5 guarantees that are database constraints hold on both dialects: one live
// primary charge per origin (INV-54), one first export per charge (INV-53), one open cash session per register
// (INV-55), each (charge, level) reminded once (INV-58), the payment allocation and charge limit CHECKs (INV-52) and
// the immutability triggers of exported and cancelled charges and their lines (INV-51), matches, closed sessions
// and issued fiscal documents.
func TestMigrations_M5Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		d := calendar.MustParse
		n := 0
		charge := func(origin, originID string, total int64) *store.Charge {
			n++
			c := &store.Charge{
				ID: store.NewID(), MarinaID: f.marinaID, Number: "A2027-" + string(rune('A'+n)), CustomerID: f.customerID,
				OriginType: origin, OriginID: originID, IssuedOn: d("2027-01-10"), DueOn: d("2027-02-10"), TotalCents: total,
				NetCents: total, VATCents: 0, PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice,
				FiscalMode: "invoice", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if originID == "" {
				c.OriginID = c.ID
			}
			return c
		}
		line := func(c *store.Charge) *store.ChargeLine {
			return &store.ChargeLine{
				ID: store.NewID(), MarinaID: f.marinaID, ChargeID: c.ID, Seq: 1, QtyMilli: 1000, NetCents: c.TotalCents,
				GrossCents: c.TotalCents, VATNature: "N1", PriceIncludesVAT: true, CreatedAt: now, UpdatedAt: now,
			}
		}

		// INV-54: one live primary charge per instalment origin; cancelled and corrections do not count.
		inst := store.NewID()
		first := charge(store.ChargeOriginInstallment, inst, 10000)
		mustInsert(t, db, first, line(first))
		wantUnique(t, "second primary charge", insertRow(t, db, charge(store.ChargeOriginInstallment, inst, 10000)))
		corr := charge(store.ChargeOriginInstallment, inst, 500)
		corr.CorrectsChargeID, corr.CorrectionKind = first.ID, "replace"
		mustInsert(t, db, corr)
		manual := charge(store.ChargeOriginManual, "", 300)
		mustInsert(t, db, manual)

		// Charge CHECKs.
		if err := insertRow(t, db, charge(store.ChargeOriginManual, "", 0)); err == nil {
			t.Fatal("zero charge accepted")
		}
		over := charge(store.ChargeOriginManual, "", 1000)
		over.PaidAmountCents = 1001
		if err := insertRow(t, db, over); err == nil {
			t.Fatal("paid above total accepted")
		}
		walkIn := charge(store.ChargeOriginCounterSale, "", 1000)
		walkIn.CustomerID = ""
		if err := insertRow(t, db, walkIn); err == nil {
			t.Fatal("walk-in invoice charge accepted")
		}
		walkIn.FiscalMode = "receipt"
		mustInsert(t, db, walkIn)
		bad := line(manual)
		bad.VATCents = 1
		if err := insertRow(t, db, bad); err == nil {
			t.Fatal("line with net + vat != gross accepted")
		}

		// INV-53: one first export per charge.
		export := func() *store.ChargeExport {
			n++
			return &store.ChargeExport{
				ID: store.NewID(), MarinaID: f.marinaID, Number: "E2027-" + string(rune('A'+n)), Kind: "export",
				Filters: store.JSON(`{}`), Delivery: "file", DeliveryStatus: "none", Totals: store.JSON(`{}`), Version: 1,
				CreatedAt: now, UpdatedAt: now,
			}
		}
		e1, e2 := export(), export()
		mustInsert(t, db, e1, e2)
		item := func(e *store.ChargeExport, first bool) *store.ChargeExportItem {
			return &store.ChargeExportItem{ID: store.NewID(), MarinaID: f.marinaID, ExportID: e.ID, ChargeID: manual.ID, IsFirst: first, CreatedAt: now}
		}
		it := item(e1, true)
		mustInsert(t, db, it, item(e2, false))
		wantUnique(t, "second first export", insertRow(t, db, &store.ChargeExportItem{
			ID: store.NewID(), MarinaID: f.marinaID, ExportID: export2(t, db, export), ChargeID: manual.ID, IsFirst: true, CreatedAt: now,
		}))
		_, err := db.NewDelete().Table("charge_export_items").Where("id = ?", it.ID).Exec(ctx)
		wantTrigger(t, "export item delete", store.InsertOnlyMessage, err)

		// INV-51: an exported charge keeps amounts and lines; due date stays editable.
		if _, err := db.NewUpdate().Table("charges").Set("invoicing_status = ?", store.ChargeExported).Where("id = ?", first.ID).Exec(ctx); err != nil {
			t.Fatalf("export: %v", err)
		}
		_, err = db.NewUpdate().Table("charges").Set("total_cents = 9000").Where("id = ?", first.ID).Exec(ctx)
		wantTrigger(t, "exported amount edit", store.ChargeImmutableMessage, err)
		_, err = db.NewUpdate().Table("charges").Set("invoicing_status = ?", store.ChargeToInvoice).Where("id = ?", first.ID).Exec(ctx)
		wantTrigger(t, "back to to_invoice", store.ChargeImmutableMessage, err)
		l2 := line(first)
		l2.Seq = 2
		wantTrigger(t, "line on exported charge", store.ChargeImmutableMessage, insertRow(t, db, l2))
		_, err = db.NewUpdate().Table("charge_lines").Set("description = 'x'").Where("charge_id = ?", first.ID).Exec(ctx)
		wantTrigger(t, "line edit on exported charge", store.ChargeImmutableMessage, err)
		if _, err := db.NewUpdate().Table("charges").Set("due_on = ?", d("2027-03-01")).Set("invoicing_status = ?", store.ChargeInvoiced).
			Where("id = ?", first.ID).Exec(ctx); err != nil {
			t.Fatalf("due date and invoiced on an exported charge: %v", err)
		}
		// A cancelled charge never changes again.
		if _, err := db.NewUpdate().Table("charges").Set("payment_status = ?", store.ChargeCancelled).Where("id = ?", manual.ID).Exec(ctx); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		_, err = db.NewUpdate().Table("charges").Set("payment_status = ?", store.ChargeOpen).Where("id = ?", manual.ID).Exec(ctx)
		wantTrigger(t, "reopen cancelled", store.ChargeImmutableMessage, err)
		// INV-51: the lines of a cancelled to_invoice charge are frozen too.
		wantTrigger(t, "line on cancelled charge", store.ChargeImmutableMessage, insertRow(t, db, line(manual)))
		wl := line(walkIn)
		mustInsert(t, db, wl)
		if _, err := db.NewUpdate().Table("charges").Set("payment_status = ?", store.ChargeCancelled).Where("id = ?", walkIn.ID).Exec(ctx); err != nil {
			t.Fatalf("cancel walk-in: %v", err)
		}
		_, err = db.NewUpdate().Table("charge_lines").Set("description = 'x'").Where("id = ?", wl.ID).Exec(ctx)
		wantTrigger(t, "line edit on cancelled charge", store.ChargeImmutableMessage, err)
		_, err = db.NewDelete().Table("charge_lines").Where("id = ?", wl.ID).Exec(ctx)
		wantTrigger(t, "line delete on cancelled charge", store.ChargeImmutableMessage, err)

		// INV-55: one open session per register; closed sessions are frozen.
		reg := &store.CashRegister{ID: store.NewID(), MarinaID: f.marinaID, Code: "main", Active: true, IsDefault: true, Version: 1, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, reg)
		session := func() *store.CashSession {
			return &store.CashSession{
				ID: store.NewID(), MarinaID: f.marinaID, RegisterID: reg.ID, Status: store.CashSessionOpen, OpenedAt: now,
				BusinessDate: d("2027-01-10"), Expected: store.JSON(`{}`), Counted: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		s1 := session()
		mustInsert(t, db, s1)
		wantUnique(t, "second open session", insertRow(t, db, session()))
		if _, err := db.NewUpdate().Table("cash_sessions").Set("status = ?", store.CashSessionClosed).Set("closed_at = ?", now).
			Where("id = ?", s1.ID).Exec(ctx); err != nil {
			t.Fatalf("close: %v", err)
		}
		_, err = db.NewUpdate().Table("cash_sessions").Set("difference_cents = 5").Where("id = ?", s1.ID).Exec(ctx)
		wantTrigger(t, "closed session edit", store.CashSessionClosedMessage, err)
		mustInsert(t, db, session())

		// INV-52: allocation CHECK; matches are insert-only.
		pay := &store.Payment{
			ID: store.NewID(), MarinaID: f.marinaID, Number: "P2027-000001", CustomerID: f.customerID, Kind: "payment",
			Method: "bank_transfer", Source: "manual", AmountCents: 1000, Currency: "EUR", ReceivedOn: d("2027-01-10"),
			VATSplit: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, pay)
		if _, err := db.NewUpdate().Table("payments").Set("allocated_cents = 800, refunded_cents = 201").Where("id = ?", pay.ID).Exec(ctx); err == nil {
			t.Fatal("allocated + refunded above amount accepted")
		}
		cashNoSession := *pay
		cashNoSession.ID, cashNoSession.Number, cashNoSession.Method = store.NewID(), "P2027-000002", "cash"
		if err := insertRow(t, db, &cashNoSession); err == nil {
			t.Fatal("manual cash payment without session accepted")
		}
		m := &store.PaymentMatch{ID: store.NewID(), MarinaID: f.marinaID, PaymentID: pay.ID, ChargeID: corr.ID, AmountCents: 100, CreatedAt: now}
		mustInsert(t, db, m)
		_, err = db.NewUpdate().Table("payment_matches").Set("amount_cents = 200").Where("id = ?", m.ID).Exec(ctx)
		wantTrigger(t, "match edit", store.InsertOnlyMessage, err)
		rev := &store.PaymentMatch{ID: store.NewID(), MarinaID: f.marinaID, PaymentID: pay.ID, ChargeID: corr.ID, AmountCents: -100, ReversesMatchID: m.ID, CreatedAt: now}
		mustInsert(t, db, rev)
		rev2 := *rev
		rev2.ID = store.NewID()
		wantUnique(t, "second reversal", insertRow(t, db, &rev2))

		// Fiscal documents: issued → void only.
		fd := &store.FiscalDocument{
			ID: store.NewID(), MarinaID: f.marinaID, PaymentID: pay.ID, ChargeID: corr.ID, Driver: "none", Number: "20270110-0001",
			BusinessDate: d("2027-01-10"), Status: store.FiscalIssued, Lines: store.JSON(`[]`), VAT: store.JSON(`[]`), TotalCents: 100,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, fd)
		_, err = db.NewUpdate().Table("fiscal_documents").Set("total_cents = 200").Where("id = ?", fd.ID).Exec(ctx)
		wantTrigger(t, "issued document edit", store.FiscalDocumentImmutableMessage, err)
		if _, err := db.NewUpdate().Table("fiscal_documents").Set("status = ?", store.FiscalVoid).Set("voided_at = ?", now).
			Where("id = ?", fd.ID).Exec(ctx); err != nil {
			t.Fatalf("void: %v", err)
		}

		// INV-58: each (charge, level) once; one run per day.
		run := &store.ReminderRun{ID: store.NewID(), MarinaID: f.marinaID, RunOn: d("2027-02-20"), Status: "done", Counts: store.JSON(`{}`), StartedAt: now, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, run)
		run2 := *run
		run2.ID = store.NewID()
		wantUnique(t, "second run of the day", insertRow(t, db, &run2))
		ri := func() *store.ReminderItem {
			return &store.ReminderItem{
				ID: store.NewID(), MarinaID: f.marinaID, RunID: run.ID, ChargeID: corr.ID, CustomerID: f.customerID,
				Level: store.ReminderCourteous, Status: "sent", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		mustInsert(t, db, ri())
		wantUnique(t, "same level twice", insertRow(t, db, ri()))

		// One open intent per charge (§16 D16) and exclusions with exactly one target.
		intent := func() *store.PaymentIntent {
			n++
			return &store.PaymentIntent{
				ID: store.NewID(), MarinaID: f.marinaID, Purpose: "sepa_collect", ProviderCode: "stripe", CustomerID: f.customerID,
				AmountCents: 100, Currency: "EUR", Status: "open", Attempt: 1, IdempotencyKey: "k" + string(rune('A'+n)),
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		i1, i2 := intent(), intent()
		mustInsert(t, db, i1, i2, &store.PaymentIntentCharge{ID: store.NewID(), MarinaID: f.marinaID, IntentID: i1.ID, ChargeID: corr.ID, OpenFlag: true, CreatedAt: now, UpdatedAt: now})
		wantUnique(t, "second open intent for a charge", insertRow(t, db, &store.PaymentIntentCharge{
			ID: store.NewID(), MarinaID: f.marinaID, IntentID: i2.ID, ChargeID: corr.ID, OpenFlag: true, CreatedAt: now, UpdatedAt: now,
		}))
		if err := insertRow(t, db, &store.DunningExclusion{
			ID: store.NewID(), MarinaID: f.marinaID, CustomerID: f.customerID, ChargeID: corr.ID, Kind: "other", CreatedAt: now, UpdatedAt: now,
		}); err == nil {
			t.Fatal("exclusion with two targets accepted")
		}
	})
}

// export2 inserts one more export and returns its id.
func export2(t *testing.T, db *store.DB, mk func() *store.ChargeExport) string {
	t.Helper()
	e := mk()
	mustInsert(t, db, e)
	return e.ID
}
