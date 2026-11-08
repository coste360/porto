package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// ReportChargeLineRow is a line of a non-cancelled charge with the charge fields (service period of the line,
// else of the charge).
type ReportChargeLineRow struct {
	ChargeID        string        `bun:"charge_id"`
	Number          string        `bun:"number"`
	Seq             int           `bun:"seq"`
	IssuedOn        calendar.Date `bun:"issued_on"`
	DueOn           calendar.Date `bun:"due_on"`
	OriginType      string        `bun:"origin_type"`
	CustomerID      string        `bun:"customer_id"`
	ContractID      string        `bun:"contract_id"`
	ContractKind    string        `bun:"contract_kind"`
	ContractYear    int           `bun:"contract_year"`
	ContractNumber  int64         `bun:"contract_number"`
	ItemID          string        `bun:"item_id"`
	ItemCode        string        `bun:"item_code"`
	Description     string        `bun:"description"`
	RevenueClass    string        `bun:"revenue_class_code"`
	VATRateBP       int64         `bun:"vat_rate_bp"`
	VATNature       string        `bun:"vat_nature"`
	ServiceFrom     calendar.Date `bun:"service_from"`
	ServiceTo       calendar.Date `bun:"service_to"`
	NetCents        int64         `bun:"net_cents"`
	VATCents        int64         `bun:"vat_cents"`
	GrossCents      int64         `bun:"gross_cents"`
	PaymentStatus   string        `bun:"payment_status"`
	InvoicingStatus string        `bun:"invoicing_status"`
	FiscalMode      string        `bun:"fiscal_mode"`
}

// ReportChargeLines returns the lines of the non-cancelled charges issued in the period, or whose service period
// intersects it, or issued by its end with service after it (deferrals at the closing date To).
func ReportChargeLines(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportChargeLineRow, error) {
	out := []ReportChargeLineRow{}
	const sf = "COALESCE(l.service_from, ch.service_from, ch.issued_on)"
	const st = "COALESCE(l.service_to, ch.service_to, ch.issued_on)"
	err := db.NewSelect().TableExpr("charge_lines AS l").
		ColumnExpr("l.charge_id, ch.number, l.seq, ch.issued_on, ch.due_on, ch.origin_type").
		ColumnExpr("ch.customer_id AS customer_id, ch.contract_id AS contract_id").
		ColumnExpr("COALESCE(c.kind, '') AS contract_kind, COALESCE(c.year, 0) AS contract_year, COALESCE(c.number, 0) AS contract_number").
		ColumnExpr("l.item_id AS item_id, l.item_code, l.description, l.revenue_class_code, l.vat_rate_bp").
		ColumnExpr("COALESCE(l.vat_nature, '') AS vat_nature").
		ColumnExpr("COALESCE(l.service_from, ch.service_from) AS service_from, COALESCE(l.service_to, ch.service_to) AS service_to").
		ColumnExpr("l.net_cents, l.vat_cents, l.gross_cents, ch.payment_status, ch.invoicing_status, ch.fiscal_mode").
		Join("JOIN charges AS ch ON ch.id = l.charge_id").
		Join("LEFT JOIN contracts AS c ON c.id = ch.contract_id").
		Where("ch.marina_id = ?", s.ID()).Where("l.marina_id = ?", s.ID()).Where("ch.payment_status <> ?", ChargeCancelled).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("ch.issued_on >= ? AND ch.issued_on <= ?", f.From, f.To).
				WhereOr(sf+" <= ? AND "+st+" >= ?", f.To, f.From).
				WhereOr("ch.issued_on <= ? AND "+st+" > ?", f.To, f.To)
		}).
		OrderExpr("ch.issued_on, ch.number, l.seq").Scan(ctx, &out)
	return out, err
}

// ReportContractChargeRow is a non-cancelled charge of a contract (gross, for the accruals).
type ReportContractChargeRow struct {
	ContractID string        `bun:"contract_id"`
	IssuedOn   calendar.Date `bun:"issued_on"`
	TotalCents int64         `bun:"total_cents"`
}

// ReportContractCharges returns the non-cancelled charges of contracts issued by the date upTo.
func ReportContractCharges(ctx context.Context, db bun.IDB, s MarinaScope, upTo calendar.Date) ([]ReportContractChargeRow, error) {
	out := []ReportContractChargeRow{}
	err := db.NewSelect().TableExpr("charges").ColumnExpr("contract_id, issued_on, total_cents").
		Where("marina_id = ?", s.ID()).Where("contract_id IS NOT NULL").Where("payment_status <> ?", ChargeCancelled).
		Where("issued_on <= ?", upTo).OrderExpr("contract_id, issued_on, number").Scan(ctx, &out)
	return out, err
}

// ReportPayments returns the payments received in the period (every kind; the report keeps payments and
// deposits).
func ReportPayments(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]Payment, error) {
	out := []Payment{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("received_on >= ?", f.From).
		Where("received_on <= ?", f.To).OrderExpr("received_on, number").Scan(ctx)
	return out, err
}

// ReportRefunds returns the refunds and payouts with business date in the period.
func ReportRefunds(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]Refund, error) {
	out := []Refund{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("business_date >= ?", f.From).
		Where("business_date <= ?", f.To).OrderExpr("business_date, number").Scan(ctx)
	return out, err
}

// ReportOpenChargeRow is a positive open charge with its residual components.
type ReportOpenChargeRow struct {
	ID              string        `bun:"id"`
	Number          string        `bun:"number"`
	CustomerID      string        `bun:"customer_id"`
	OriginType      string        `bun:"origin_type"`
	IssuedOn        calendar.Date `bun:"issued_on"`
	DueOn           calendar.Date `bun:"due_on"`
	TotalCents      int64         `bun:"total_cents"`
	PaidAmountCents int64         `bun:"paid_amount_cents"`
	CreditedCents   int64         `bun:"credited_cents"`
}

// ReportOpenCharges returns the positive open or partially paid charges with a residual issued by asOf.
func ReportOpenCharges(ctx context.Context, db bun.IDB, s MarinaScope, asOf calendar.Date) ([]ReportOpenChargeRow, error) {
	out := []ReportOpenChargeRow{}
	err := db.NewSelect().TableExpr("charges").
		ColumnExpr("id, number, customer_id AS customer_id, origin_type, issued_on, due_on").
		ColumnExpr("total_cents, paid_amount_cents, credited_cents").
		Where("marina_id = ?", s.ID()).Where("total_cents > 0").
		Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("paid_amount_cents + credited_cents < total_cents").Where("issued_on <= ?", asOf).
		OrderExpr("due_on, number").Scan(ctx, &out)
	return out, err
}

// ReportInstallmentRow is a scheduled contract or holder instalment.
type ReportInstallmentRow struct {
	Kind           string        `bun:"kind"` // installment, holder_installment
	ID             string        `bun:"id"`
	CustomerID     string        `bun:"customer_id"`
	ContractKind   string        `bun:"contract_kind"`
	ContractYear   int           `bun:"contract_year"`
	ContractNumber int64         `bun:"contract_number"`
	Seq            int           `bun:"seq"`
	DueOn          calendar.Date `bun:"due_on"`
	AmountCents    int64         `bun:"amount_cents"`
}

// ReportScheduledInstallments returns the scheduled contract and holder instalments due by dueBy (2 queries).
func ReportScheduledInstallments(ctx context.Context, db bun.IDB, s MarinaScope, dueBy calendar.Date) ([]ReportInstallmentRow, error) {
	out := []ReportInstallmentRow{}
	err := db.NewSelect().TableExpr("installments AS i").
		ColumnExpr("'installment' AS kind, i.id, COALESCE(c.billing_customer_id, c.customer_id) AS customer_id").
		ColumnExpr("c.kind AS contract_kind, c.year AS contract_year, c.number AS contract_number, i.seq, i.due_on, i.amount_cents").
		Join("JOIN contracts AS c ON c.id = i.contract_id").
		Where("i.marina_id = ?", s.ID()).Where("i.status = ?", "scheduled").Where("i.due_on <= ?", dueBy).
		Where("i.amount_cents > 0").OrderExpr("i.due_on, i.id").Scan(ctx, &out)
	if err != nil {
		return nil, err
	}
	var hs []ReportInstallmentRow
	err = db.NewSelect().TableExpr("holder_installments").
		ColumnExpr("'holder_installment' AS kind, id, customer_id, '' AS contract_kind, 0 AS contract_year, 0 AS contract_number").
		ColumnExpr("seq, due_on, amount_cents").
		Where("marina_id = ?", s.ID()).Where("status = ?", "scheduled").Where("due_on <= ?", dueBy).
		Where("kind = ?", "installment").Where("amount_cents > 0").OrderExpr("due_on, id").Scan(ctx, &hs)
	return append(out, hs...), err
}

// ReportReminderRow is a sent reminder of a charge.
type ReportReminderRow struct {
	ID           string    `bun:"id"`
	ChargeID     string    `bun:"charge_id"`
	ChargeNumber string    `bun:"charge_number"`
	CustomerID   string    `bun:"customer_id"`
	Level        string    `bun:"level"`
	UpdatedAt    time.Time `bun:"updated_at"`
}

// ReportReminders returns the reminders sent (status sent) in [from, to) (instants).
func ReportReminders(ctx context.Context, db bun.IDB, s MarinaScope, from, to time.Time) ([]ReportReminderRow, error) {
	out := []ReportReminderRow{}
	err := db.NewSelect().TableExpr("reminder_items AS r").
		ColumnExpr("r.id, r.charge_id, ch.number AS charge_number, r.customer_id, r.level, r.updated_at").
		Join("JOIN charges AS ch ON ch.id = r.charge_id").
		Where("r.marina_id = ?", s.ID()).Where("r.status = ?", "sent").
		Where("r.updated_at >= ?", UTC(from)).Where("r.updated_at < ?", UTC(to)).
		OrderExpr("r.updated_at, r.id").Scan(ctx, &out)
	return out, err
}

// ReportRevenueClassRow is a revenue class with its parent code.
type ReportRevenueClassRow struct {
	Code       string `bun:"code"`
	Name       JSON   `bun:"name"`
	ParentCode string `bun:"parent_code"`
}

// ReportRevenueClasses returns the revenue classes of the marina.
func ReportRevenueClasses(ctx context.Context, db bun.IDB, s MarinaScope) ([]ReportRevenueClassRow, error) {
	out := []ReportRevenueClassRow{}
	err := db.NewSelect().TableExpr("revenue_classes AS r").
		ColumnExpr("r.code, r.name, COALESCE(p.code, '') AS parent_code").
		Join("LEFT JOIN revenue_classes AS p ON p.id = r.parent_id").
		Where("r.marina_id = ?", s.ID()).OrderExpr("r.sort_order, r.code").Scan(ctx, &out)
	return out, err
}
