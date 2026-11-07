package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read models of the ledger (m5-plan §4.5): open charges, scadenzario rows, statement sources and the payment
// lens. Read-only; the domain computes balances, ageing buckets and pages.

// OpenCustomerCharges returns the open positive charges of a customer with a residual, oldest due first (auto-match
// order: due_on, number). ids narrows to some charges when not nil.
func OpenCustomerCharges(ctx context.Context, db bun.IDB, s MarinaScope, customerID string, ids []string) ([]Charge, error) {
	out := []Charge{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		Where("total_cents > 0").Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("paid_amount_cents + credited_cents < total_cents")
	if ids != nil {
		if len(ids) == 0 {
			return out, nil
		}
		q = q.Where("id IN (?)", bun.List(ids))
	}
	err := q.OrderExpr("due_on, number").Scan(ctx)
	return out, err
}

// OpenChargesFilter selects open positive charges (ageing, map lens).
type OpenChargesFilter struct {
	CustomerID  string
	ContractIDs []string
}

// OpenPositiveCharges returns the open positive charges with a residual (any customer when CustomerID is empty).
func OpenPositiveCharges(ctx context.Context, db bun.IDB, s MarinaScope, f OpenChargesFilter) ([]Charge, error) {
	out := []Charge{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("total_cents > 0").
		Where("payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
		Where("paid_amount_cents + credited_cents < total_cents")
	if f.CustomerID != "" {
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if f.ContractIDs != nil {
		if len(f.ContractIDs) == 0 {
			return out, nil
		}
		q = q.Where("contract_id IN (?)", bun.List(f.ContractIDs))
	}
	err := q.OrderExpr("due_on, number").Scan(ctx)
	return out, err
}

// CustomerCharges returns the charges of a customer that are not cancelled (statement), oldest first.
func CustomerCharges(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]Charge, error) {
	out := []Charge{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		Where("payment_status <> ?", ChargeCancelled).OrderExpr("issued_on, number").Scan(ctx)
	return out, err
}

// DepositMatch is a match of a deposit (retention) with its date (statement row).
type DepositMatch struct {
	PaymentID     string    `bun:"payment_id"`
	PaymentNumber string    `bun:"payment_number"`
	ChargeID      string    `bun:"charge_id"`
	AmountCents   int64     `bun:"amount_cents"`
	CreatedAt     time.Time `bun:"created_at"`
	MatchID       string    `bun:"id"`
}

// DepositMatchesOfCustomer returns the matches of the customer's deposits, oldest first.
func DepositMatchesOfCustomer(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]DepositMatch, error) {
	out := []DepositMatch{}
	err := db.NewSelect().TableExpr("payment_matches AS m").Join("JOIN payments AS p ON p.id = m.payment_id AND p.marina_id = m.marina_id").
		ColumnExpr("m.id, m.payment_id, p.number AS payment_number, m.charge_id, m.amount_cents, m.created_at").
		Where("m.marina_id = ?", s.ID()).Where("p.customer_id = ?", customerID).Where("p.kind = ?", PaymentKindDeposit).
		OrderExpr("m.created_at, m.id").Scan(ctx, &out)
	return out, err
}

// ReceivableFilter selects the rows of the scadenzario.
type ReceivableFilter struct {
	DueFrom, DueTo calendar.Date // inclusive, zero = open
	CustomerID     string
	ContractID     string
	GroupID        string
	OriginType     string
	Charges        bool // open charges
	Scheduled      bool // scheduled instalments (no charge yet)
}

// ReceivableRow is one row of the scadenzario: an open charge or a scheduled instalment.
type ReceivableRow struct {
	Kind           string        `bun:"kind"` // charge, scheduled_installment
	ID             string        `bun:"id"`
	Number         string        `bun:"number"`
	CustomerID     string        `bun:"customer_id"`
	ContractID     string        `bun:"contract_id"`
	ContractKind   string        `bun:"contract_kind"`
	ContractYear   int           `bun:"contract_year"`
	ContractNumber int64         `bun:"contract_number"`
	OriginType     string        `bun:"origin_type"`
	Description    string        `bun:"description"`
	DueOn          calendar.Date `bun:"due_on"`
	AmountCents    int64         `bun:"amount_cents"`
	ResidualCents  int64         `bun:"residual_cents"`
}

const groupMemberSQL = `EXISTS (SELECT 1 FROM contract_lines AS l
	JOIN berth_group_members AS g ON g.resource_id = l.resource_id AND g.marina_id = l.marina_id AND g.resource_type = 'berth'
	WHERE l.contract_id = %s AND l.marina_id = %s AND l.resource_type = 'berth' AND g.group_id = ?)`

// ReceivableRows returns the rows of the scadenzario ordered by due date, then id.
func ReceivableRows(ctx context.Context, db bun.IDB, s MarinaScope, f ReceivableFilter) ([]ReceivableRow, error) {
	out := []ReceivableRow{}
	if f.Charges {
		var rows []ReceivableRow
		q := db.NewSelect().TableExpr("charges AS ch").
			Join("LEFT JOIN contracts AS c ON c.id = ch.contract_id AND c.marina_id = ch.marina_id").
			ColumnExpr("'charge' AS kind, ch.id, ch.number, COALESCE(CAST(ch.customer_id AS TEXT), '') AS customer_id").
			ColumnExpr("COALESCE(CAST(ch.contract_id AS TEXT), '') AS contract_id, COALESCE(c.kind, '') AS contract_kind").
			ColumnExpr("COALESCE(c.year, 0) AS contract_year, COALESCE(c.number, 0) AS contract_number").
			ColumnExpr("ch.origin_type, ch.description, ch.due_on, ch.total_cents AS amount_cents").
			ColumnExpr("ch.total_cents - ch.paid_amount_cents - ch.credited_cents AS residual_cents").
			Where("ch.marina_id = ?", s.ID()).Where("ch.total_cents > 0").
			Where("ch.payment_status IN (?)", bun.List([]string{ChargeOpen, ChargePartiallyPaid})).
			Where("ch.paid_amount_cents + ch.credited_cents < ch.total_cents")
		if !f.DueFrom.IsZero() {
			q = q.Where("ch.due_on >= ?", f.DueFrom)
		}
		if !f.DueTo.IsZero() {
			q = q.Where("ch.due_on <= ?", f.DueTo)
		}
		if f.CustomerID != "" {
			q = q.Where("ch.customer_id = ?", f.CustomerID)
		}
		if f.ContractID != "" {
			q = q.Where("ch.contract_id = ?", f.ContractID)
		}
		if f.OriginType != "" {
			q = q.Where("ch.origin_type = ?", f.OriginType)
		}
		if f.GroupID != "" {
			q = q.Where(ledgerSprintf(groupMemberSQL, "ch.contract_id", "ch.marina_id"), f.GroupID)
		}
		if err := q.Scan(ctx, &rows); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	if f.Scheduled && (f.OriginType == "" || f.OriginType == ChargeOriginInstallment) {
		var rows []ReceivableRow
		q := db.NewSelect().TableExpr("installments AS i").
			Join("JOIN contracts AS c ON c.id = i.contract_id AND c.marina_id = i.marina_id").
			ColumnExpr("'scheduled_installment' AS kind, i.id, '' AS number").
			ColumnExpr("COALESCE(CAST(c.billing_customer_id AS TEXT), CAST(c.customer_id AS TEXT), '') AS customer_id").
			ColumnExpr("i.contract_id, c.kind AS contract_kind, c.year AS contract_year, c.number AS contract_number").
			ColumnExpr("'installment' AS origin_type, '' AS description, i.due_on, i.amount_cents, i.amount_cents AS residual_cents").
			Where("i.marina_id = ?", s.ID()).Where("i.status = ?", InstallmentScheduled).Where("i.amount_cents > 0")
		if !f.DueFrom.IsZero() {
			q = q.Where("i.due_on >= ?", f.DueFrom)
		}
		if !f.DueTo.IsZero() {
			q = q.Where("i.due_on <= ?", f.DueTo)
		}
		if f.CustomerID != "" {
			q = q.Where("COALESCE(c.billing_customer_id, c.customer_id) = ?", f.CustomerID)
		}
		if f.ContractID != "" {
			q = q.Where("i.contract_id = ?", f.ContractID)
		}
		if f.GroupID != "" {
			q = q.Where(ledgerSprintf(groupMemberSQL, "i.contract_id", "i.marina_id"), f.GroupID)
		}
		if err := q.Scan(ctx, &rows); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	sortReceivables(out)
	return out, nil
}

// LastReminderLevels returns the last reminder level recorded for each charge (sent or awaiting approval).
func LastReminderLevels(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ChargeID  string    `bun:"charge_id"`
		Level     string    `bun:"level"`
		CreatedAt time.Time `bun:"created_at"`
	}
	if err := db.NewSelect().TableExpr("reminder_items").Column("charge_id", "level", "created_at").
		Where("marina_id = ?", s.ID()).Where("charge_id IN (?)", bun.List(chargeIDs)).
		Where("status IN ('sent','awaiting_approval','queued')").OrderExpr("created_at, id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ChargeID] = r.Level // the latest wins
	}
	return out, nil
}

// ActiveExclusion is an active dunning exclusion (not removed, until empty or ≥ today).
type ActiveExclusion struct {
	CustomerID string `bun:"customer_id"`
	ChargeID   string `bun:"charge_id"`
	Kind       string `bun:"kind"`
}

// ActiveExclusions returns the active dunning exclusions of the marina on today.
func ActiveExclusions(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date) ([]ActiveExclusion, error) {
	out := []ActiveExclusion{}
	err := db.NewSelect().TableExpr("dunning_exclusions").
		ColumnExpr("COALESCE(CAST(customer_id AS TEXT), '') AS customer_id, COALESCE(CAST(charge_id AS TEXT), '') AS charge_id, kind").
		Where("marina_id = ?", s.ID()).Where("removed_at IS NULL").Where("(until_on IS NULL OR until_on >= ?)", today).
		Scan(ctx, &out)
	return out, err
}

// LensAllocation is an allocated resource of a map on a night with its contract.
type LensAllocation struct {
	ResourceType   string `bun:"resource_type"`
	ResourceID     string `bun:"resource_id"`
	ContractID     string `bun:"contract_id"`
	ContractStatus string `bun:"contract_status"`
	CustomerID     string `bun:"customer_id"`
}

// LensAllocations returns the active allocations of a map covering a night that belong to a contract or transit.
func LensAllocations(ctx context.Context, db bun.IDB, s MarinaScope, mapID string, night calendar.Date) ([]LensAllocation, error) {
	out := []LensAllocation{}
	err := db.NewSelect().TableExpr("allocations AS a").
		Join("JOIN contract_lines AS l ON l.id = a.contract_line_id AND l.marina_id = a.marina_id").
		Join("JOIN contracts AS c ON c.id = l.contract_id AND c.marina_id = l.marina_id").
		ColumnExpr("a.resource_type, a.resource_id, c.id AS contract_id, c.status AS contract_status").
		ColumnExpr("COALESCE(CAST(c.billing_customer_id AS TEXT), CAST(c.customer_id AS TEXT), '') AS customer_id").
		Where("a.marina_id = ?", s.ID()).Where("a.map_id = ?", mapID).Where("a.status = ?", AllocActive).
		Where("a.first_night <= ?", night).Where("a.last_night >= ?", night).
		OrderExpr("a.resource_type, a.resource_id, a.id").Scan(ctx, &out)
	return out, err
}

// IssuedInstallmentsWithoutCharge returns the issued (or partially paid) instalments of some contracts that have
// no charge yet (M4 data before the backfill), for the payment lens.
func IssuedInstallmentsWithoutCharge(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string) ([]Installment, error) {
	out := []Installment{}
	if len(contractIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(contractIDs)).
		Where("status IN (?)", bun.List([]string{InstallmentIssued, InstallmentPartiallyPaid})).
		Where("charge_id IS NULL").Where("amount_cents > 0").Scan(ctx)
	return out, err
}

func ledgerSprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// sortReceivables orders rows by due date, then id.
func sortReceivables(rows []ReceivableRow) {
	slices.SortStableFunc(rows, func(a, b ReceivableRow) int {
		if c := a.DueOn.Compare(b.DueOn); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}
