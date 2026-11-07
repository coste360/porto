package store

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Fiscal documents and their daily numbering (m5-plan §5.2). Numbers are YYYYMMDD-NNNN from fiscal_sequences
// (one row per marina and business day), unique per marina (fiscal_documents_number_uidx). Issued documents are
// frozen by trigger except the move to void. Owned by CASH-DRIVERS.

// NextFiscalNumber returns the next document number of day and advances the counter. It must run inside the
// transaction that inserts the document: the counter row is locked by the UPDATE until commit, so parallel
// receipts of the same day get consecutive numbers and a rollback gives the number back.
func NextFiscalNumber(ctx context.Context, tx bun.IDB, s MarinaScope, day calendar.Date) (int64, error) {
	seed := &FiscalSequence{MarinaID: s.ID(), Day: day, LastNumber: 0}
	if _, err := tx.NewInsert().Model(seed).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return 0, fmt.Errorf("fiscal sequence seed: %w", err)
	}
	var n int64
	err := tx.NewRaw("UPDATE fiscal_sequences SET last_number = last_number + 1 WHERE marina_id = ? AND day = ? RETURNING last_number",
		s.ID(), day).Scan(ctx, &n)
	if err != nil {
		return 0, fmt.Errorf("fiscal sequence next: %w", err)
	}
	return n, nil
}

// FiscalNumber formats a document number: YYYYMMDD-NNNN.
func FiscalNumber(day calendar.Date, n int64) string {
	return fmt.Sprintf("%04d%02d%02d-%04d", day.Year(), int(day.Month()), day.Day(), n)
}

// InsertFiscalDocument inserts a document.
func InsertFiscalDocument(ctx context.Context, tx bun.IDB, d *FiscalDocument) error {
	_, err := tx.NewInsert().Model(d).Exec(ctx)
	return err
}

// GetFiscalDocument returns a document of the marina.
func GetFiscalDocument(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*FiscalDocument, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(FiscalDocument)
	err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return d, notFound(err)
}

// LockFiscalDocument locks a document (FOR UPDATE on PostgreSQL).
func LockFiscalDocument(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*FiscalDocument, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(FiscalDocument)
	q := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	return d, notFound(q.Scan(ctx))
}

// FiscalDocumentsOfPayment returns the documents of a payment, oldest first.
func FiscalDocumentsOfPayment(ctx context.Context, db bun.IDB, s MarinaScope, paymentID string) ([]FiscalDocument, error) {
	out := []FiscalDocument{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("payment_id = ?", paymentID).OrderExpr("id").Scan(ctx)
	return out, err
}

// FiscalDocumentFilter narrows ListFiscalDocuments.
type FiscalDocumentFilter struct {
	From, To  calendar.Date
	Status    string
	SessionID string // documents of the payments of a cash session
}

// ListFiscalDocuments returns documents newest first (cursor = last id).
func ListFiscalDocuments(ctx context.Context, db bun.IDB, s MarinaScope, f FiscalDocumentFilter, cursor string, limit int) (Page[FiscalDocument], error) {
	limit = ClampLimit(limit)
	rows := []FiscalDocument{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if !f.From.IsZero() {
		q = q.Where("business_date >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("business_date <= ?", f.To)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.SessionID != "" {
		q = q.Where("payment_id IN (SELECT id FROM payments WHERE marina_id = ? AND cash_session_id = ?)", s.ID(), f.SessionID)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[FiscalDocument]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[FiscalDocument]{}, err
	}
	return pageOf(rows, limit, func(d FiscalDocument) string { return d.ID }), nil
}

// PendingFiscalDocuments returns pending documents of every marina, oldest first (job fiscal.issue).
func PendingFiscalDocuments(ctx context.Context, db bun.IDB, limit int) ([]FiscalDocument, error) {
	out := []FiscalDocument{}
	err := db.NewSelect().Model(&out).Where("status = ?", FiscalPending).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// SetFiscalOutcome records the outcome of an issue attempt on a pending or failed document (conditional: a
// concurrent attempt that already issued it wins). It reports whether the row changed.
func SetFiscalOutcome(ctx context.Context, tx bun.IDB, s MarinaScope, d *FiscalDocument, now time.Time) (bool, error) {
	now = UTC(now)
	q := tx.NewUpdate().Model((*FiscalDocument)(nil)).Set("status = ?", d.Status).
		Set("external_ref = ?", nullString(d.ExternalRef)).Set("error_code = ?", nullString(d.ErrorCode)).
		Set("attempts = attempts + 1").Set("version = version + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", d.ID).Where("status IN (?)", bun.List([]string{FiscalPending, FiscalFailed}))
	if d.Status == FiscalIssued {
		q = q.Set("issued_at = ?", UTC(d.IssuedAt))
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// VoidFiscalDocument moves an issued document to void (conditional).
func VoidFiscalDocument(ctx context.Context, tx bun.IDB, s MarinaScope, id string, now time.Time) (bool, error) {
	now = UTC(now)
	res, err := tx.NewUpdate().Model((*FiscalDocument)(nil)).Set("status = ?", FiscalVoid).Set("voided_at = ?", now).
		Set("version = version + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", FiscalIssued).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// FiscalTotalsDoc is an issued document with the method of its payment (corrispettivi).
type FiscalTotalsDoc struct {
	ID           string        `bun:"id"`
	PaymentID    string        `bun:"payment_id"`
	BusinessDate calendar.Date `bun:"business_date"`
	VAT          JSON          `bun:"vat"`
	TotalCents   int64         `bun:"total_cents"`
	Method       string        `bun:"method"`
}

// FiscalDocumentsForTotals returns the issued (not void) sale documents with business date in [from, to].
func FiscalDocumentsForTotals(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]FiscalTotalsDoc, error) {
	out := []FiscalTotalsDoc{}
	err := db.NewSelect().TableExpr("fiscal_documents AS fd").Join("JOIN payments AS p ON p.id = fd.payment_id AND p.marina_id = fd.marina_id").
		ColumnExpr("fd.id, fd.payment_id, fd.business_date, fd.vat, fd.total_cents, p.method").
		Where("fd.marina_id = ?", s.ID()).Where("fd.status = ?", FiscalIssued).Where("fd.refund_id IS NULL").
		Where("fd.business_date >= ?", from).Where("fd.business_date <= ?", to).OrderExpr("fd.business_date, fd.number").
		Scan(ctx, &out)
	return out, err
}

// FiscalRefund is a succeeded refund of a payment that carries a fiscal document (corrispettivi deduct it).
type FiscalRefund struct {
	ID           string        `bun:"id"`
	PaymentID    string        `bun:"payment_id"`
	BusinessDate calendar.Date `bun:"business_date"`
	AmountCents  int64         `bun:"amount_cents"`
	Method       string        `bun:"method"`
}

// FiscalRefundsForTotals returns the succeeded refunds dated in [from, to] of payments with an issued document.
func FiscalRefundsForTotals(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]FiscalRefund, error) {
	out := []FiscalRefund{}
	err := db.NewSelect().TableExpr("refunds AS r").ColumnExpr("r.id, r.payment_id, r.business_date, r.amount_cents, r.method").
		Where("r.marina_id = ?", s.ID()).Where("r.status = ?", RefundSucceeded).Where("r.kind = ?", RefundKindRefund).
		Where("r.business_date >= ?", from).Where("r.business_date <= ?", to).
		Where("r.payment_id IN (SELECT payment_id FROM fiscal_documents WHERE marina_id = ? AND status = ? AND refund_id IS NULL)", s.ID(), FiscalIssued).
		OrderExpr("r.business_date, r.id").Scan(ctx, &out)
	return out, err
}

// FiscalVATOfPayments returns the vat buckets of the issued sale documents of the given payments (refund split).
func FiscalVATOfPayments(ctx context.Context, db bun.IDB, s MarinaScope, paymentIDs []string) (map[string]JSON, error) {
	out := map[string]JSON{}
	if len(paymentIDs) == 0 {
		return out, nil
	}
	var rows []FiscalTotalsDoc
	if err := db.NewSelect().TableExpr("fiscal_documents").ColumnExpr("payment_id, vat").
		Where("marina_id = ?", s.ID()).Where("status = ?", FiscalIssued).Where("refund_id IS NULL").
		Where("payment_id IN (?)", bun.List(paymentIDs)).OrderExpr("id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, ok := out[r.PaymentID]; !ok {
			out[r.PaymentID] = r.VAT
		}
	}
	return out, nil
}
