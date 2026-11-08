package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// ReportCashSessionRow is a cash session with its register code.
type ReportCashSessionRow struct {
	ID              string        `bun:"id"`
	RegisterCode    string        `bun:"register_code"`
	Status          string        `bun:"status"`
	BusinessDate    calendar.Date `bun:"business_date"`
	FloatCents      int64         `bun:"float_cents"`
	Expected        JSON          `bun:"expected"`
	Counted         JSON          `bun:"counted"`
	DifferenceCents int64         `bun:"difference_cents"`
}

// ReportCashSessions returns the cash sessions with business date in the period.
func ReportCashSessions(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportCashSessionRow, error) {
	out := []ReportCashSessionRow{}
	err := db.NewSelect().TableExpr("cash_sessions AS s").
		ColumnExpr("s.id, r.code AS register_code, s.status, s.business_date, s.float_cents, s.expected, s.counted, s.difference_cents").
		Join("JOIN cash_registers AS r ON r.id = s.register_id").
		Where("s.marina_id = ?", s.ID()).Where("s.business_date >= ?", f.From).Where("s.business_date <= ?", f.To).
		OrderExpr("s.business_date, r.code, s.id").Scan(ctx, &out)
	return out, err
}

// ReportFiscalDocumentRow is an issued or void fiscal document with the method of its payment.
type ReportFiscalDocumentRow struct {
	ID           string        `bun:"id"`
	Number       string        `bun:"number"`
	Status       string        `bun:"status"`
	BusinessDate calendar.Date `bun:"business_date"`
	Method       string        `bun:"method"`
	TotalCents   int64         `bun:"total_cents"`
	VAT          JSON          `bun:"vat"`
}

// ReportFiscalDocuments returns the issued and void fiscal documents with business date in the period.
func ReportFiscalDocuments(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportFiscalDocumentRow, error) {
	out := []ReportFiscalDocumentRow{}
	err := db.NewSelect().TableExpr("fiscal_documents AS d").
		ColumnExpr("d.id, d.number, d.status, d.business_date, COALESCE(p.method, '') AS method, d.total_cents, d.vat").
		Join("LEFT JOIN payments AS p ON p.id = d.payment_id").
		Where("d.marina_id = ?", s.ID()).Where("d.status IN (?)", bun.List([]string{"issued", "void"})).
		Where("d.business_date >= ?", f.From).Where("d.business_date <= ?", f.To).
		OrderExpr("d.business_date, d.number").Scan(ctx, &out)
	return out, err
}
