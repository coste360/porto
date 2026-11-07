package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of charges and their lines (spec 16.1; m5-plan §3). Every state change is a conditional UPDATE whose
// affected count the caller checks; the triggers of 00010_charges are the backstop of INV-51. Only
// internal/domain/charge calls the writers of this file, except the money columns paid_amount_cents and
// payment_status, which only internal/domain/payment changes (payments.go, m5-plan §14.3). credited_cents changes
// only through ApplyOffset.

// Billing sequence series (billing_sequences.series).
const (
	SeriesCharge  = "charge"
	SeriesPayment = "payment"
	SeriesRefund  = "refund"
	SeriesExport  = "export"
	SeriesRequest = "request"
)

// NextBillingNumber returns the next number of a billing series in period (the year, YYYY) for the marina of s and
// advances the counter (gap-free: it must run in the transaction that inserts the numbered row, m5-plan §16 D17).
// On PostgreSQL the UPDATE locks the counter row until commit, so two transactions never get the same number.
func NextBillingNumber(ctx context.Context, tx bun.IDB, s MarinaScope, series string, year int) (int64, error) {
	period := fmt.Sprintf("%04d", year)
	seed := &BillingSequence{MarinaID: s.ID(), Series: series, Period: period, NextNumber: 1}
	if _, err := tx.NewInsert().Model(seed).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return 0, fmt.Errorf("billing sequence seed: %w", err)
	}
	var n int64
	err := tx.NewRaw(
		"UPDATE billing_sequences SET next_number = next_number + 1 WHERE marina_id = ? AND series = ? AND period = ? RETURNING next_number - 1",
		s.ID(), series, period,
	).Scan(ctx, &n)
	if err != nil {
		return 0, fmt.Errorf("billing sequence next: %w", err)
	}
	return n, nil
}

// ErrOriginCharged is returned by InsertCharge when the origin already has its live primary charge (the partial
// unique index charges_origin_primary_uidx, INV-54).
var ErrOriginCharged = errors.New("store: origin already charged")

// InsertCharge inserts a charge and its lines. A violation of the live primary index is ErrOriginCharged; the
// caller's transaction must then be retried or abandoned (PostgreSQL aborts it).
func InsertCharge(ctx context.Context, db bun.IDB, c *Charge, lines []ChargeLine) error {
	if _, err := db.NewInsert().Model(c).Exec(ctx); err != nil {
		if IsUniqueViolation(err) && strings.Contains(err.Error(), "charges_origin_primary_uidx") {
			return ErrOriginCharged
		}
		if IsUniqueViolation(err) && isOriginPrimaryViolation(err) {
			return ErrOriginCharged
		}
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&lines).Exec(ctx)
	return err
}

// isOriginPrimaryViolation recognises the SQLite message of the partial unique index (which names the columns,
// not the index).
func isOriginPrimaryViolation(err error) bool {
	m := err.Error()
	return strings.Contains(m, "charges.origin_type") && strings.Contains(m, "charges.origin_id")
}

// GetCharge returns a charge of the marina.
func GetCharge(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Charge, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(Charge)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// LockCharge takes a row lock on one charge (PostgreSQL; SQLite has a single writer) and returns it.
func LockCharge(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Charge, error) {
	rows, err := LockChargesSorted(ctx, db, s, []string{id})
	if err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// ChargeByNumber returns a charge of the marina by its number.
func ChargeByNumber(ctx context.Context, db bun.IDB, s MarinaScope, number string) (*Charge, error) {
	c := new(Charge)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("number = ?", number).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// ChargesByIDs returns the charges of the marina with the given ids, ordered by number.
func ChargesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Charge, error) {
	out := []Charge{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var part []Charge
		if err := db.NewSelect().Model(&part).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids[start:end])).
			Scan(ctx); err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	sortCharges(out)
	return out, nil
}

// ChargesByNumbers returns the charges of the marina with the given numbers.
func ChargesByNumbers(ctx context.Context, db bun.IDB, s MarinaScope, numbers []string) ([]Charge, error) {
	out := []Charge{}
	for start := 0; start < len(numbers); start += 500 {
		end := min(start+500, len(numbers))
		var part []Charge
		if err := db.NewSelect().Model(&part).Where("marina_id = ?", s.ID()).Where("number IN (?)", bun.List(numbers[start:end])).
			Scan(ctx); err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

func sortCharges(cs []Charge) {
	// Numbers are A{yyyy}-{n:06}: lexical order is creation order within the marina.
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Number < cs[j-1].Number; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// ChargeLinesOf returns the lines of charges ordered by charge and seq.
func ChargeLinesOf(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) ([]ChargeLine, error) {
	out := []ChargeLine{}
	ids := validIDs(chargeIDs)
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var part []ChargeLine
		if err := db.NewSelect().Model(&part).Where("marina_id = ?", s.ID()).Where("charge_id IN (?)", bun.List(ids[start:end])).
			OrderExpr("charge_id, seq").Scan(ctx); err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

// ChargesOfOrigin returns the charges of an origin: the charges with (origin_type, origin_id) and, transitively,
// every adjustment of them (origin adjustment / origin_id = an included charge). With live, cancelled charges are
// left out (their adjustments too). Ordered by number.
func ChargesOfOrigin(ctx context.Context, db bun.IDB, s MarinaScope, originType, originID string, live bool) ([]Charge, error) {
	var out []Charge
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("origin_type = ?", originType).Where("origin_id = ?", originID)
	if live {
		q = q.Where("payment_status <> ?", ChargeCancelled)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	frontier := make([]string, 0, len(out))
	for _, c := range out {
		seen[c.ID] = true
		frontier = append(frontier, c.ID)
	}
	for len(frontier) > 0 {
		var next []Charge
		q := db.NewSelect().Model(&next).Where("marina_id = ?", s.ID()).Where("origin_type = ?", ChargeOriginAdjustment).
			Where("origin_id IN (?)", bun.List(frontier))
		if live {
			q = q.Where("payment_status <> ?", ChargeCancelled)
		}
		if err := q.Scan(ctx); err != nil {
			return nil, err
		}
		frontier = frontier[:0]
		for _, c := range next {
			if !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
				frontier = append(frontier, c.ID)
			}
		}
	}
	if out == nil {
		out = []Charge{}
	}
	sortCharges(out)
	return out, nil
}

// LivePrimaryCharge returns the live primary charge of an origin (non-cancelled, no corrects_charge_id), or
// ErrNotFound.
func LivePrimaryCharge(ctx context.Context, db bun.IDB, s MarinaScope, originType, originID string) (*Charge, error) {
	c := new(Charge)
	err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("origin_type = ?", originType).
		Where("origin_id = ?", originID).Where("corrects_charge_id IS NULL").Where("payment_status <> ?", ChargeCancelled).
		OrderExpr("number DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// ChargesOfContract returns the charges of a contract (any origin) ordered by number.
func ChargesOfContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) ([]Charge, error) {
	out := []Charge{}
	if !ValidID(contractID) {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).OrderExpr("number").Scan(ctx)
	return out, err
}

// ChargeEdit is an in-place edit of a to_invoice charge (customer, due date, description and, optionally, lines
// with the new totals).
type ChargeEdit struct {
	CustomerID     *string
	DueOn          *calendar.Date
	Description    *string
	FiscalSnapshot *string
	Lines          []ChargeLine // nil = lines unchanged
	Total, Net     int64
	VAT            int64
	ServiceFrom    calendar.Date
	ServiceTo      calendar.Date
}

// EditCharge applies an edit to a charge at version expected (0 = any). With lines, the charge must still be
// to_invoice, unpaid and not offset (else ErrImmutable); without, due date and description can change on
// exported charges too. ErrVersionMismatch when the version moved.
func EditCharge(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int, e ChargeEdit, now time.Time) error {
	q := db.NewUpdate().Model((*Charge)(nil)).Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("payment_status <> ?", ChargeCancelled)
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	if e.DueOn != nil {
		q = q.Set("due_on = ?", *e.DueOn)
	}
	if e.Description != nil {
		q = q.Set("description = ?", *e.Description)
	}
	structural := e.CustomerID != nil || e.Lines != nil || e.FiscalSnapshot != nil
	if e.CustomerID != nil {
		q = q.Set("customer_id = ?", nullStr(*e.CustomerID))
	}
	if e.FiscalSnapshot != nil {
		q = q.Set("fiscal_snapshot_enc = ?", nullStr(*e.FiscalSnapshot))
	}
	if e.Lines != nil {
		q = q.Set("total_cents = ?", e.Total).Set("net_cents = ?", e.Net).Set("vat_cents = ?", e.VAT).
			Set("service_from = ?", nullDate(e.ServiceFrom)).Set("service_to = ?", nullDate(e.ServiceTo))
	}
	if structural {
		q = q.Where("invoicing_status = ?", ChargeToInvoice)
		if e.Lines != nil || e.CustomerID != nil {
			q = q.Where("paid_amount_cents = 0").Where("credited_cents = 0")
		}
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return mapChargeErr(err)
	}
	if affected(res) == 0 {
		cur, gerr := GetCharge(ctx, db, s, id)
		if gerr != nil {
			return gerr
		}
		if expected > 0 && cur.Version != expected {
			return ErrVersionMismatch
		}
		return ErrImmutable
	}
	if e.Lines == nil {
		return nil
	}
	if _, err := db.NewDelete().Model((*ChargeLine)(nil)).Where("marina_id = ?", s.ID()).Where("charge_id = ?", id).Exec(ctx); err != nil {
		return mapChargeErr(err)
	}
	if len(e.Lines) == 0 {
		return nil
	}
	_, err = db.NewInsert().Model(&e.Lines).Exec(ctx)
	return mapChargeErr(err)
}

// CancelChargeRow cancels an open, unexported, unpaid and not offset charge at version expected (0 = any); false
// when it was not cancellable any more.
func CancelChargeRow(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int, reason string, now time.Time) (bool, error) {
	q := db.NewUpdate().Model((*Charge)(nil)).Set("payment_status = ?", ChargeCancelled).Set("cancel_reason = ?", reason).
		Set("cancelled_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("payment_status = ?", ChargeOpen).
		Where("invoicing_status = ?", ChargeToInvoice).Where("paid_amount_cents = 0").Where("credited_cents = 0")
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, mapChargeErr(err)
	}
	return affected(res) == 1, nil
}

// CancelImportedCharge cancels an opening balance charge of an import (born invoiced) while it has no match and no
// offset. The immutability trigger allows a payment status change on an invoiced charge.
func CancelImportedCharge(ctx context.Context, db bun.IDB, s MarinaScope, id, reason string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Charge)(nil)).Set("payment_status = ?", ChargeCancelled).Set("cancel_reason = ?", reason).
		Set("cancelled_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("origin_type = ?", ChargeOriginOpeningBalance).
		Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePaid})).
		Where("paid_amount_cents = 0").Where("(credited_cents = 0 OR total_cents < 0)").Exec(ctx)
	if err != nil {
		return false, mapChargeErr(err)
	}
	return affected(res) == 1, nil
}

// ApplyOffset applies amount of a negative charge to the charge it corrects (m5-plan §3.5): it inserts the
// insert-only charge_offsets row and raises the target's credited_cents with a conditional UPDATE that re-checks
// the limit (paid + credited + amount ≤ total) and recomputes the payment status. false when the target cannot take
// the amount (cancelled, negative or not enough residual). It is the only writer of credited_cents of positive
// charges.
func ApplyOffset(ctx context.Context, db bun.IDB, s MarinaScope, negativeID, targetID string, amount int64, now time.Time) (bool, error) {
	if amount <= 0 {
		return false, fmt.Errorf("store: offset amount must be positive")
	}
	res, err := db.NewUpdate().Model((*Charge)(nil)).
		Set("credited_cents = credited_cents + ?", amount).
		Set("payment_status = CASE WHEN paid_amount_cents + credited_cents + ? >= total_cents THEN ? ELSE ? END",
			amount, ChargePaid, ChargePartiallyPaid).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", targetID).Where("total_cents > 0").
		Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("paid_amount_cents + credited_cents + ? <= total_cents", amount).Exec(ctx)
	if err != nil {
		return false, mapChargeErr(err)
	}
	if affected(res) != 1 {
		return false, nil
	}
	off := &ChargeOffset{
		ID: NewID(), MarinaID: s.ID(), NegativeChargeID: negativeID, TargetChargeID: targetID, AmountCents: amount,
		CreatedAt: UTC(now),
	}
	if _, err := db.NewInsert().Model(off).Exec(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// OffsetsOf returns the offsets where the charges are the negative or the target side.
func OffsetsOf(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) ([]ChargeOffset, error) {
	out := []ChargeOffset{}
	ids := validIDs(chargeIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("negative_charge_id IN (?)", bun.List(ids)).WhereOr("target_charge_id IN (?)", bun.List(ids))
		}).OrderExpr("created_at, id").Scan(ctx)
	return out, err
}

// CorrectionsOf returns the charges that correct the given charge (adjustments and replacements), by number.
func CorrectionsOf(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) ([]Charge, error) {
	out := []Charge{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("corrects_charge_id = ?", chargeID).
		OrderExpr("number").Scan(ctx)
	return out, err
}

// HasMatches reports whether a charge has payment matches (payment_matches is written by internal/domain/payment).
func HasMatches(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) (bool, error) {
	n, err := db.NewSelect().Table("payment_matches").Where("marina_id = ?", s.ID()).Where("charge_id = ?", chargeID).Count(ctx)
	return n > 0, err
}

// ChargeMatchRow is a payment match of a charge with the payment number (charge detail).
type ChargeMatchRow struct {
	ID              string    `bun:"id"`
	PaymentID       string    `bun:"payment_id"`
	PaymentNumber   string    `bun:"payment_number"`
	AmountCents     int64     `bun:"amount_cents"`
	ReversesMatchID string    `bun:"reverses_match_id"`
	CreatedAt       time.Time `bun:"created_at"`
}

// ChargeMatches returns the matches of a charge (oldest first).
func ChargeMatches(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) ([]ChargeMatchRow, error) {
	out := []ChargeMatchRow{}
	err := db.NewSelect().TableExpr("payment_matches AS m").Join("JOIN payments AS p ON p.id = m.payment_id").
		ColumnExpr("m.id, m.payment_id, p.number AS payment_number, m.amount_cents, COALESCE(CAST(m.reverses_match_id AS TEXT), '') AS reverses_match_id, m.created_at").
		Where("m.marina_id = ?", s.ID()).Where("m.charge_id = ?", chargeID).OrderExpr("m.created_at, m.id").Scan(ctx, &out)
	return out, err
}

// ChargeReminderRow is a reminder of a charge (charge detail).
type ChargeReminderRow struct {
	Level     string    `bun:"level"`
	Status    string    `bun:"status"`
	CreatedAt time.Time `bun:"created_at"`
}

// ChargeReminders returns the reminder items of a charge (oldest first).
func ChargeReminders(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) ([]ChargeReminderRow, error) {
	out := []ChargeReminderRow{}
	err := db.NewSelect().Table("reminder_items").ColumnExpr("level, status, created_at").Where("marina_id = ?", s.ID()).
		Where("charge_id = ?", chargeID).OrderExpr("created_at").Scan(ctx, &out)
	return out, err
}

// ChargeExportRefRow is an export item of a charge with the export number.
type ChargeExportRefRow struct {
	ExportID  string    `bun:"export_id"`
	Number    string    `bun:"number"`
	IsFirst   bool      `bun:"is_first"`
	CreatedAt time.Time `bun:"created_at"`
}

// ChargeExportRefs returns the exports of a charge (oldest first).
func ChargeExportRefs(ctx context.Context, db bun.IDB, s MarinaScope, chargeID string) ([]ChargeExportRefRow, error) {
	out := []ChargeExportRefRow{}
	err := db.NewSelect().TableExpr("charge_export_items AS i").Join("JOIN charge_exports AS e ON e.id = i.export_id").
		ColumnExpr("i.export_id, e.number, i.is_first, e.created_at").Where("i.marina_id = ?", s.ID()).
		Where("i.charge_id = ?", chargeID).OrderExpr("e.created_at, e.number").Scan(ctx, &out)
	return out, err
}

// ChargeFilter selects charges of a list (m5-plan §7.3.1).
type ChargeFilter struct {
	PaymentStatus   string
	InvoicingStatus string
	OriginType      string
	CustomerID      string
	ContractID      string
	IssuedFrom      calendar.Date
	IssuedTo        calendar.Date
	DueFrom         calendar.Date
	DueTo           calendar.Date
	OverdueOn       calendar.Date // non-zero: open with residual and due_on < this date
	RevenueClass    string
	Number          string // prefix of the number
}

func (f ChargeFilter) apply(q *bun.SelectQuery, s MarinaScope) *bun.SelectQuery {
	q = q.Where("c.marina_id = ?", s.ID())
	if f.PaymentStatus != "" {
		q = q.Where("c.payment_status = ?", f.PaymentStatus)
	}
	if f.InvoicingStatus != "" {
		q = q.Where("c.invoicing_status = ?", f.InvoicingStatus)
	}
	if f.OriginType != "" {
		q = q.Where("c.origin_type = ?", f.OriginType)
	}
	if f.CustomerID != "" {
		q = q.Where("c.customer_id = ?", f.CustomerID)
	}
	if f.ContractID != "" {
		q = q.Where("c.contract_id = ?", f.ContractID)
	}
	if !f.IssuedFrom.IsZero() {
		q = q.Where("c.issued_on >= ?", f.IssuedFrom)
	}
	if !f.IssuedTo.IsZero() {
		q = q.Where("c.issued_on <= ?", f.IssuedTo)
	}
	if !f.DueFrom.IsZero() {
		q = q.Where("c.due_on >= ?", f.DueFrom)
	}
	if !f.DueTo.IsZero() {
		q = q.Where("c.due_on <= ?", f.DueTo)
	}
	if !f.OverdueOn.IsZero() {
		q = q.Where("c.payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
			Where("c.total_cents > 0").Where("c.due_on < ?", f.OverdueOn)
	}
	if f.RevenueClass != "" {
		q = q.Where("EXISTS (SELECT 1 FROM charge_lines AS l WHERE l.charge_id = c.id AND l.revenue_class_code = ?)", f.RevenueClass)
	}
	if f.Number != "" {
		q = q.Where("c.number LIKE ?", likePrefix(f.Number))
	}
	return q
}

func likePrefix(s string) string {
	r := strings.NewReplacer(`%`, ``, `_`, ``)
	return r.Replace(strings.ToUpper(strings.TrimSpace(s))) + "%"
}

// ListCharges returns a page of charges, newest number first (cursor = id of the last row).
func ListCharges(ctx context.Context, db bun.IDB, s MarinaScope, f ChargeFilter, cursor string, limit int) (Page[Charge], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Charge]{}, err
	}
	var rows []Charge
	q := f.apply(db.NewSelect().Model(&rows).ModelTableExpr("charges AS c").ColumnExpr("c.*"), s)
	if after != "" {
		q = q.Where("c.number < (SELECT number FROM charges WHERE id = ?)", after)
	}
	if err := q.OrderExpr("c.number DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Charge]{}, err
	}
	return pageOf(rows, limit, func(c Charge) string { return c.ID }), nil
}

// AllCharges returns every charge of a filter (CSV/XLSX downloads), bounded by max (ErrConflict above it).
func AllCharges(ctx context.Context, db bun.IDB, s MarinaScope, f ChargeFilter, maxRows int) ([]Charge, error) {
	var rows []Charge
	q := f.apply(db.NewSelect().Model(&rows).ModelTableExpr("charges AS c").ColumnExpr("c.*"), s)
	if err := q.OrderExpr("c.number").Limit(int64(maxRows + 1)).Scan(ctx); err != nil {
		return nil, err
	}
	if len(rows) > maxRows {
		return nil, ErrConflict
	}
	if rows == nil {
		rows = []Charge{}
	}
	return rows, nil
}

// SetInstallmentCharge links an instalment to its charge once (charge_id IS NULL); false when already linked.
func SetInstallmentCharge(ctx context.Context, db bun.IDB, s MarinaScope, installmentID, chargeID string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Installment)(nil)).Set("charge_id = ?", chargeID).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", installmentID).Where("charge_id IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SetHolderInstallmentCharge links a holder instalment to its charge once; false when already linked.
func SetHolderInstallmentCharge(ctx context.Context, db bun.IDB, s MarinaScope, id, chargeID string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*HolderInstallment)(nil)).Set("charge_id = ?", chargeID).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("charge_id IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// InstallmentsToBackfill returns up to limit issued, partially paid or paid instalments without a charge, oldest
// first (m5-plan §3.7).
func InstallmentsToBackfill(ctx context.Context, db bun.IDB, s MarinaScope, after string, limit int) ([]Installment, error) {
	out := []Installment{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("charge_id IS NULL").
		Where("status IN (?)", bun.List([]string{InstallmentIssued, "partially_paid", InstallmentPaid}))
	if after != "" {
		q = q.Where("id > ?", after)
	}
	err := q.OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// HolderInstallmentsToBackfill is InstallmentsToBackfill for holder instalments.
func HolderInstallmentsToBackfill(ctx context.Context, db bun.IDB, s MarinaScope, after string, limit int) ([]HolderInstallment, error) {
	out := []HolderInstallment{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("charge_id IS NULL").
		Where("status IN (?)", bun.List([]string{InstallmentIssued, "partially_paid", InstallmentPaid}))
	if after != "" {
		q = q.Where("id > ?", after)
	}
	err := q.OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// CountCharges returns the number of charges of the marina (seed idempotency).
func CountCharges(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*Charge)(nil)).Where("marina_id = ?", s.ID()).Count(ctx))
}

// MarinasWithBillingWork returns the ids of the active marinas (jobs iterate over them).
func MarinasWithBillingWork(ctx context.Context, db bun.IDB) ([]string, error) {
	var ids []string
	err := db.NewSelect().Table("marinas").Column("id").Where("archived_at IS NULL").OrderExpr("id").Scan(ctx, &ids)
	return ids, err
}

// mapChargeErr maps the M5 immutability trigger to ErrImmutable.
func mapChargeErr(err error) error {
	if err != nil && IsTriggerViolation(err, ChargeImmutableMessage) {
		return fmt.Errorf("%w: %w", ErrImmutable, err)
	}
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullDate(d calendar.Date) any {
	if d.IsZero() {
		return nil
	}
	return d
}

// SetChargeInvoice records the external invoice reference of an exported or invoiced charge (conditional on the
// invoicing status); false when the charge is not exported.
func SetChargeInvoice(ctx context.Context, db bun.IDB, s MarinaScope, id, number string, date calendar.Date, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Charge)(nil)).Set("invoicing_status = ?", ChargeInvoiced).
		Set("external_invoice_number = ?", number).Set("external_invoice_date = ?", date).Set("invoiced_at = ?", UTC(now)).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("invoicing_status IN (?)", bun.List([]string{ChargeExported, ChargeInvoiced})).Exec(ctx)
	if err != nil {
		return false, mapChargeErr(err)
	}
	return affected(res) == 1, nil
}

// ActiveBaseRulesOfItem returns the active base rules of an item in the active price lists valid on day, by
// priority then recency (highest first).
func ActiveBaseRulesOfItem(ctx context.Context, db bun.IDB, s MarinaScope, itemID string, day calendar.Date) ([]PriceRule, error) {
	out := []PriceRule{}
	if !ValidID(itemID) {
		return out, nil
	}
	err := db.NewSelect().Model(&out).ModelTableExpr("price_rules AS r").ColumnExpr("r.*").
		Join("JOIN price_lists AS l ON l.id = r.price_list_id").
		Where("r.marina_id = ?", s.ID()).Where("r.item_id = ?", itemID).Where("r.stage = 'base'").Where("r.active = ?", true).
		Where("l.status = ?", PriceListActive).Where("l.valid_from <= ?", day).Where("l.valid_to >= ?", day).
		OrderExpr("r.priority DESC, r.seq DESC").Scan(ctx)
	return out, err
}

// MarkChargeInvoicedAtCreation moves a charge just inserted as to_invoice to invoiced with an external reference
// (opening balances are born invoiced, m5-plan §3.8; lines cannot be inserted on an invoiced charge, so the flip
// happens after them in the same transaction).
func MarkChargeInvoicedAtCreation(ctx context.Context, db bun.IDB, s MarinaScope, id, number string, date calendar.Date, now time.Time) error {
	res, err := db.NewUpdate().Model((*Charge)(nil)).Set("invoicing_status = ?", ChargeInvoiced).
		Set("external_invoice_number = ?", number).Set("external_invoice_date = ?", date).Set("invoiced_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("invoicing_status = ?", ChargeToInvoice).Exec(ctx)
	if err != nil {
		return mapChargeErr(err)
	}
	if affected(res) != 1 {
		return ErrConflict
	}
	return nil
}
