//go:build integration

package backup

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
)

// seedM5Rows adds at least one row of every backed-up M5 table (m5-plan §6.4): billing settings, an origin item, a
// sequence, an exported and paid charge with its lines (frozen by the triggers), a cancelled charge, a negative
// adjustment correcting the exported charge (deferred self reference) with its offset, an export with its first
// item, a reconciliation, an opening balance import, a register with a closed session (frozen) and a movement, a
// fiscal sequence, a payment with a match and its reversal, a refund, an issued fiscal document, provider settings,
// an intent with its charge, a mandate, a payment request with its charge, a reminder run and item, an exclusion
// and a worklist state. The frozen rows exercise the late status pass of internal/backup.
func seedM5Rows(t *testing.T, db *store.DB, marinaID, userID string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Now())
	d := calendar.MustParse
	var customer store.Customer
	if err := db.NewSelect().Model(&customer).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("customer: %v", err)
	}
	var item store.Item
	if err := db.NewSelect().Model(&item).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("item: %v", err)
	}
	var contract store.Contract
	if err := db.NewSelect().Model(&contract).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("contract: %v", err)
	}

	charge := &store.Charge{
		ID: store.NewID(), MarinaID: marinaID, Number: "A2027-900001", CustomerID: customer.ID,
		OriginType: store.ChargeOriginManual, ContractID: contract.ID, IssuedOn: d("2027-01-10"), DueOn: d("2027-02-10"),
		ServiceFrom: d("2027-01-01"), ServiceTo: d("2027-01-31"), TotalCents: 12200, NetCents: 10000, VATCents: 2200,
		PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice, FiscalMode: "invoice",
		Description: "Servizi", FiscalSnapshotEnc: "enc", CreatedByUserID: userID, CreatedByActor: "user",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	charge.OriginID = charge.ID
	cancelled := &store.Charge{
		ID: store.NewID(), MarinaID: marinaID, Number: "A2027-900002", CustomerID: customer.ID,
		OriginType: store.ChargeOriginManual, IssuedOn: d("2027-01-11"), DueOn: d("2027-02-11"), TotalCents: 500,
		NetCents: 410, VATCents: 90, PaymentStatus: store.ChargeOpen, InvoicingStatus: store.ChargeToInvoice,
		FiscalMode: "invoice", CreatedByActor: "user", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	cancelled.OriginID = cancelled.ID
	adjustment := &store.Charge{
		ID: store.NewID(), MarinaID: marinaID, Number: "A2027-900003", CustomerID: customer.ID,
		OriginType: store.ChargeOriginAdjustment, OriginID: charge.ID, IssuedOn: d("2027-01-20"), DueOn: d("2027-01-20"),
		TotalCents: -1220, NetCents: -1000, VATCents: -220, CreditedCents: -1220, PaymentStatus: store.ChargePaid,
		InvoicingStatus: store.ChargeToInvoice, FiscalMode: "invoice", CorrectsChargeID: charge.ID, CorrectionKind: "delta",
		CreatedByActor: "user", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	line := func(chargeID string, net, vat int64) *store.ChargeLine {
		return &store.ChargeLine{
			ID: store.NewID(), MarinaID: marinaID, ChargeID: chargeID, Seq: 1, ItemID: item.ID, ItemCode: item.Code,
			Description: "Riga", QtyMilli: 1000, UnitCents: net + vat, NetCents: net, VATCents: vat, GrossCents: net + vat,
			VATRateBP: 2200, PriceIncludesVAT: true, PlanKey: "snap:1", CreatedAt: now, UpdatedAt: now,
		}
	}
	export := &store.ChargeExport{
		ID: store.NewID(), MarinaID: marinaID, Number: "E2027-9001", Kind: "export", PeriodFrom: d("2027-01-01"),
		PeriodTo: d("2027-01-31"), Filters: store.JSON(`{}`), Delivery: "file", DeliveryStatus: "none", ChargeCount: 1,
		LineCount: 1, Totals: store.JSON(`{"total_cents":12200}`), CreatedByUserID: userID, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	register := &store.CashRegister{ID: store.NewID(), MarinaID: marinaID, Code: "backup", Name: "Cassa backup", Active: true, IsDefault: false, Version: 1, CreatedAt: now, UpdatedAt: now}
	session := &store.CashSession{
		ID: store.NewID(), MarinaID: marinaID, RegisterID: register.ID, Status: store.CashSessionOpen, OpenedByUserID: userID,
		OpenedAt: now, BusinessDate: d("2027-01-12"), FloatCents: 10000, Expected: store.JSON(`{}`), Counted: store.JSON(`{}`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	payment := &store.Payment{
		ID: store.NewID(), MarinaID: marinaID, Number: "P2027-900001", CustomerID: customer.ID, Kind: store.PaymentKindPayment,
		Method: store.PaymentMethodCash, Source: store.PaymentSourceManual, AmountCents: 12200, AllocatedCents: 0, RefundedCents: 200,
		Currency: "EUR", ReceivedOn: d("2027-01-12"), CashSessionID: session.ID, VATSplit: store.JSON(`[]`),
		IdempotencyKey: "seed-m5", RecordedByUserID: userID, RecordedByActor: "user", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	match := &store.PaymentMatch{ID: store.NewID(), MarinaID: marinaID, PaymentID: payment.ID, ChargeID: charge.ID, AmountCents: 2000, CreatedByActor: "user", CreatedAt: now}
	reversal := &store.PaymentMatch{
		ID: store.NewID(), MarinaID: marinaID, PaymentID: payment.ID, ChargeID: charge.ID, AmountCents: -2000,
		ReversesMatchID: match.ID, CreatedByActor: "user", CreatedAt: now,
	}
	paid := &store.PaymentMatch{ID: store.NewID(), MarinaID: marinaID, PaymentID: payment.ID, ChargeID: charge.ID, AmountCents: 10980, CreatedByActor: "user", CreatedAt: now}
	refund := &store.Refund{
		ID: store.NewID(), MarinaID: marinaID, Number: "R2027-900001", PaymentID: payment.ID, Kind: store.RefundKindRefund,
		Method: store.PaymentMethodCash, Source: "manual", Status: store.RefundSucceeded, AmountCents: 200, Reason: "resto",
		BusinessDate: d("2027-01-12"), CashSessionID: session.ID, CustomerID: customer.ID, CompletedAt: now,
		RecordedByActor: "user", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	intent := &store.PaymentIntent{
		ID: store.NewID(), MarinaID: marinaID, Purpose: store.IntentPurposePaymentRequest, ProviderCode: "stripe",
		CustomerID: customer.ID, AmountCents: 12200, Currency: "EUR", Status: store.IntentExpired, Attempt: 1,
		IdempotencyKey: "payment_request:seed:1", CreatedByActor: "system", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	request := &store.PaymentRequest{
		ID: store.NewID(), MarinaID: marinaID, CustomerID: customer.ID, TokenHash: "hash-m5", AmountCents: 12200,
		Currency: "EUR", Status: "paid", Source: "auto", SentAt: now, SentCount: 1, PaidAt: now, CreatedByActor: "system",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	run := &store.ReminderRun{ID: store.NewID(), MarinaID: marinaID, RunOn: d("2027-02-20"), Status: "done", Counts: store.JSON(`{"sent":1}`), StartedAt: now, FinishedAt: now, CreatedByActor: "system", CreatedAt: now, UpdatedAt: now}
	rows := []any{
		&store.BillingSettings{
			MarinaID: marinaID, DefaultDueDays: 30, AutoPaymentRequest: true, ReminderPolicy: store.JSON(`{}`), ReminderHour: 9,
			ArrearsPolicy: store.JSON(`{"enabled":false}`), ExportReminderDays: 30, FiscalDriver: "none", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.ChargeOriginItem{ID: store.NewID(), MarinaID: marinaID, OriginKey: "opening_balance", ItemID: item.ID, CreatedAt: now, UpdatedAt: now},
		&store.BillingSequence{MarinaID: marinaID, Series: "charge", Period: "2027", NextNumber: 4},
		charge, cancelled, line(charge.ID, 10000, 2200), line(cancelled.ID, 410, 90), adjustment, line(adjustment.ID, -1000, -220),
		&store.ChargeOffset{ID: store.NewID(), MarinaID: marinaID, NegativeChargeID: adjustment.ID, TargetChargeID: charge.ID, AmountCents: 1220, CreatedAt: now},
		export,
		&store.ChargeExportItem{ID: store.NewID(), MarinaID: marinaID, ExportID: export.ID, ChargeID: charge.ID, IsFirst: true, CreatedAt: now},
		&store.InvoiceReconciliation{
			ID: store.NewID(), MarinaID: marinaID, Source: "csv", FileSHA256: "f", RowCount: 1, MatchedCount: 1,
			Report: store.JSON(`[{"number":"A2027-900001","outcome":"ok"}]`), CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now,
		},
		&store.ChargeImport{
			ID: store.NewID(), MarinaID: marinaID, Status: "dry_run", FileSHA256: "f", Format: "xlsx", Mapping: store.JSON(`{}`),
			Report: store.JSON(`{"valid":1}`), CreatedByUserID: userID, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		register, session,
		&store.CashMovement{ID: store.NewID(), MarinaID: marinaID, SessionID: session.ID, Kind: "in", AmountCents: 500, Reason: "fondo", CreatedByUserID: userID, CreatedAt: now},
		&store.FiscalSequence{MarinaID: marinaID, Day: d("2027-01-12"), LastNumber: 1},
		payment, match, reversal, paid, refund,
		&store.FiscalDocument{
			ID: store.NewID(), MarinaID: marinaID, PaymentID: payment.ID, ChargeID: charge.ID, Driver: "none", Number: "20270112-0001",
			BusinessDate: d("2027-01-12"), Status: store.FiscalIssued, Lines: store.JSON(`[]`), VAT: store.JSON(`[]`), TotalCents: 12200,
			IssuedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.PaymentProviderSettings{
			ID: store.NewID(), MarinaID: marinaID, ProviderCode: "stripe", Mode: "test", CredentialsEnc: "c", WebhookSecretEnc: "w",
			PublishableKeyLast4: "4242", Methods: store.JSON(`["card"]`), Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		intent,
		&store.PaymentIntentCharge{ID: store.NewID(), MarinaID: marinaID, IntentID: intent.ID, ChargeID: charge.ID, AmountCents: 12200, CreatedAt: now, UpdatedAt: now},
		&store.SEPAMandate{
			ID: store.NewID(), MarinaID: marinaID, CustomerID: customer.ID, ProviderCode: "stripe", ProviderCustomerRefEnc: "c",
			PaymentMethodRefEnc: "p", MandateRef: "m", IBANLast4: "1234", Status: "active", ActivatedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		request,
		&store.PaymentRequestCharge{ID: store.NewID(), MarinaID: marinaID, RequestID: request.ID, ChargeID: charge.ID, AmountCents: 12200, CreatedAt: now},
		run,
		&store.ReminderItem{
			ID: store.NewID(), MarinaID: marinaID, RunID: run.ID, ChargeID: charge.ID, CustomerID: customer.ID, Level: store.ReminderCourteous,
			Status: "sent", RequestID: request.ID, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.DunningExclusion{ID: store.NewID(), MarinaID: marinaID, ChargeID: charge.ID, Kind: "disputed", UntilOn: d("2027-06-30"), Note: "n", CreatedByUserID: userID, CreatedAt: now, UpdatedAt: now},
		&store.WorkItemState{ID: store.NewID(), MarinaID: marinaID, SourceType: "charge_to_export", SourceID: charge.ID, AssigneeUserID: userID, Status: "in_progress", Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		q := db.NewInsert().Model(row)
		switch row.(type) {
		case *store.BillingSettings, *store.ChargeOriginItem, *store.BillingSequence, *store.FiscalSequence:
			// The demo seed already creates these per-marina singletons; keep its rows.
			q = q.On("CONFLICT DO NOTHING")
		}
		if _, err := q.Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", row, err)
		}
	}
	// Final states that the triggers then freeze: the charge is paid and exported, the other one cancelled, the
	// session closed; the payment's allocation follows its matches.
	updates := []struct {
		table, set string
		args       []any
		id         string
	}{
		{"payments", "allocated_cents = ?", []any{int64(10980)}, payment.ID},
		{"charges", "paid_amount_cents = ?, credited_cents = ?, payment_status = ?", []any{int64(10980), int64(1220), store.ChargePaid}, charge.ID},
		{"charges", "invoicing_status = ?, first_export_id = ?, exported_at = ?", []any{store.ChargeExported, export.ID, now}, charge.ID},
		{"charges", "payment_status = ?, cancel_reason = ?, cancelled_at = ?", []any{store.ChargeCancelled, "errore", now}, cancelled.ID},
		{"charges", "invoicing_status = ?", []any{store.ChargeExported}, adjustment.ID},
		{"cash_sessions", "status = ?, closed_at = ?, closed_by_user_id = ?", []any{store.CashSessionClosed, now, userID}, session.ID},
	}
	for _, u := range updates {
		if _, err := db.NewUpdate().Table(u.table).Set(u.set, u.args...).Where("id = ?", u.id).Exec(ctx); err != nil {
			t.Fatalf("update %s: %v", u.table, err)
		}
	}
}
