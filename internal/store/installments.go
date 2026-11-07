package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of installments (spec 13.7; m4-plan §3.3). Status changes are conditional updates so two concurrent
// writers never both succeed (the issue job, bulk issue, settlement). Paid is written only by SettleInstallmentRow,
// called only by contract.SettleInstallment(s) (INV-32).

// InsertInstallments inserts the rows of a plan.
func InsertInstallments(ctx context.Context, db bun.IDB, rows []Installment) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// InstallmentsOfContracts returns the instalments of contracts ordered by contract, plan version and seq; with
// live, cancelled rows are left out.
func InstallmentsOfContracts(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string, live bool) ([]Installment, error) {
	out := []Installment{}
	ids := validIDs(contractIDs)
	if len(ids) == 0 {
		return out, nil
	}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(ids))
	if live {
		q = q.Where("status <> ?", InstallmentCancelled)
	}
	err := q.OrderExpr("contract_id, plan_version, seq").Scan(ctx)
	return out, err
}

// GetInstallment returns one instalment of a marina.
func GetInstallment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Installment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	in := new(Installment)
	if err := db.NewSelect().Model(in).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return in, nil
}

// InstallmentsByIDs returns the instalments of a marina with the given ids, ordered by id.
func InstallmentsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Installment, error) {
	out := []Installment{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).OrderExpr("id").Scan(ctx)
	return out, err
}

// IssueInstallmentRow marks one scheduled instalment issued; false when it was not scheduled any more.
func IssueInstallmentRow(ctx context.Context, db bun.IDB, s MarinaScope, id string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Installment)(nil)).
		Set("status = ?", InstallmentIssued).Set("issued_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", InstallmentScheduled).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// DueToIssue returns up to limit scheduled instalments of the marina with issue_on ≤ today, oldest first.
func DueToIssue(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date, limit int) ([]Installment, error) {
	out := []Installment{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", InstallmentScheduled).
		Where("issue_on <= ?", today).OrderExpr("issue_on, id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// OverdueToNotify returns up to limit issued instalments with due_on < today never notified as overdue.
func OverdueToNotify(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date, limit int) ([]Installment, error) {
	out := []Installment{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", InstallmentIssued).
		Where("due_on < ?", today).Where("overdue_notified_at IS NULL").OrderExpr("due_on, id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// MarkOverdueNotified records the overdue notice of an instalment once; false when already recorded.
func MarkOverdueNotified(ctx context.Context, db bun.IDB, s MarinaScope, id string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*Installment)(nil)).Set("overdue_notified_at = ?", UTC(now)).
		Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("overdue_notified_at IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// Settlement is what SettleInstallmentRow records.
type Settlement struct {
	Source    string // manual (M4), payment (M5)
	Method    string
	RefEnc    string
	SettledOn calendar.Date
	At        time.Time
	UserID    string
}

// SettleInstallmentRow marks an issued or scheduled instalment paid when its version equals expected (0 = any);
// false when the row was not settleable any more (status or version). It is the only writer of paid (INV-32).
func SettleInstallmentRow(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int, st Settlement) (bool, error) {
	q := db.NewUpdate().Model((*Installment)(nil)).
		Set("status = ?", InstallmentPaid).Set("settlement_source = ?", st.Source).Set("settlement_method = ?", st.Method).
		Set("settlement_ref_enc = ?", installmentNull(st.RefEnc)).Set("settled_on = ?", st.SettledOn).Set("settled_at = ?", UTC(st.At)).
		Set("settled_by_user_id = ?", installmentNull(st.UserID)).Set("updated_at = ?", UTC(st.At)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("status IN (?)", bun.List([]string{InstallmentIssued, InstallmentScheduled}))
	if expected > 0 {
		q = q.Where("version = ?", expected)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// CancelInstallments cancels the live instalments of a contract in the given statuses, recording the plan that
// replaced them (nil when none); it returns the cancelled ids.
func CancelInstallments(ctx context.Context, db bun.IDB, s MarinaScope, contractID string, statuses []string, replacedBy *int, now time.Time) ([]string, error) {
	var ids []string
	if err := db.NewSelect().Model((*Installment)(nil)).Column("id").Where("marina_id = ?", s.ID()).
		Where("contract_id = ?", contractID).Where("status IN (?)", bun.List(statuses)).OrderExpr("id").
		Scan(ctx, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return ids, nil
	}
	res, err := db.NewUpdate().Model((*Installment)(nil)).Set("status = ?", InstallmentCancelled).
		Set("replaced_by_plan = ?", replacedBy).Set("cancelled_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Where("status IN (?)", bun.List(statuses)).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected(res) != int64(len(ids)) {
		// A row left the statuses between the select and the update: the returned ids would be wrong.
		return nil, ErrVersionMismatch
	}
	return ids, nil
}

// InstallmentFilter narrows ListInstallmentRows (the scadenzario).
type InstallmentFilter struct {
	From, To   *calendar.Date // due_on range, inclusive
	Status     string         // a status, or overdue (issued and due_on < Today)
	Today      calendar.Date
	CustomerID string
	ContractID string
	GroupID    string // berth group of an allocated berth of the contract
}

// ListInstallmentRows returns a page of instalments ordered by due date, then id; the cursor is the id of the
// last row.
func ListInstallmentRows(ctx context.Context, db bun.IDB, s MarinaScope, f InstallmentFilter, cursor string, limit int) (Page[Installment], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Installment]{}, err
	}
	rows := []Installment{}
	q := installmentQuery(db, s, f).Model(&rows)
	if q == nil {
		return Page[Installment]{Items: rows}, nil
	}
	if after != "" {
		last, err := GetInstallment(ctx, db, s, after)
		if err != nil {
			return Page[Installment]{}, ErrInvalidCursor
		}
		q = q.Where("(i.due_on > ? OR (i.due_on = ? AND i.id > ?))", last.DueOn, last.DueOn, last.ID)
	}
	if err := q.OrderExpr("i.due_on, i.id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Installment]{}, err
	}
	return pageOf(rows, limit, func(i Installment) string { return i.ID }), nil
}

// AllInstallmentRows returns every instalment of the filter (CSV export), at most max rows.
func AllInstallmentRows(ctx context.Context, db bun.IDB, s MarinaScope, f InstallmentFilter, maxRows int) ([]Installment, error) {
	rows := []Installment{}
	q := installmentQuery(db, s, f)
	if q == nil {
		return rows, nil
	}
	err := q.Model(&rows).OrderExpr("i.due_on, i.id").Limit(int64(maxRows)).Scan(ctx)
	return rows, err
}

func installmentQuery(db bun.IDB, s MarinaScope, f InstallmentFilter) *bun.SelectQuery {
	q := db.NewSelect().ModelTableExpr("installments AS i").ColumnExpr("i.*").Where("i.marina_id = ?", s.ID())
	if f.From != nil {
		q = q.Where("i.due_on >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("i.due_on <= ?", *f.To)
	}
	switch f.Status {
	case "":
		q = q.Where("i.status <> ?", InstallmentCancelled)
	case "overdue":
		q = q.Where("i.status = ?", InstallmentIssued).Where("i.due_on < ?", f.Today)
	default:
		q = q.Where("i.status = ?", f.Status)
	}
	if f.ContractID != "" {
		if !ValidID(f.ContractID) {
			return nil
		}
		q = q.Where("i.contract_id = ?", f.ContractID)
	}
	if f.CustomerID != "" {
		if !ValidID(f.CustomerID) {
			return nil
		}
		q = q.Where("EXISTS (SELECT 1 FROM contracts AS c WHERE c.id = i.contract_id AND c.marina_id = i.marina_id AND (c.customer_id = ? OR c.billing_customer_id = ?))", f.CustomerID, f.CustomerID)
	}
	if f.GroupID != "" {
		if !ValidID(f.GroupID) {
			return nil
		}
		q = q.Where(`EXISTS (SELECT 1 FROM contract_lines AS l
			JOIN berth_group_members AS g ON g.resource_id = l.resource_id AND g.marina_id = l.marina_id AND g.resource_type = 'berth'
			WHERE l.contract_id = i.contract_id AND l.marina_id = i.marina_id AND l.resource_type = 'berth' AND g.group_id = ?)`, f.GroupID)
	}
	return q
}

// OverdueInstallmentExists reports whether a contract of the customer has an issued instalment due before
// dueBefore (renewal exclusion arrears, m4-plan §15 D5). contractID narrows to one contract when set.
func OverdueInstallmentExists(ctx context.Context, db bun.IDB, s MarinaScope, customerID, contractID string, dueBefore calendar.Date) (bool, error) {
	q := db.NewSelect().TableExpr("installments AS i").ColumnExpr("1").Where("i.marina_id = ?", s.ID()).
		Where("i.status = ?", InstallmentIssued).Where("i.due_on < ?", dueBefore)
	if contractID != "" {
		q = q.Where("i.contract_id = ?", contractID)
	} else {
		q = q.Where("EXISTS (SELECT 1 FROM contracts AS c WHERE c.id = i.contract_id AND c.marina_id = i.marina_id AND c.customer_id = ?)", customerID)
	}
	return q.Exists(ctx)
}

func installmentNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SetContractPlan writes the plan columns of a contract (policy with the resolved payment clause, total, price
// snapshot, plan version) without bumping its version: they are written in the transaction of the state change
// that planned them (creation, replan, addendum), which bumps the version itself when it is a client change.
func SetContractPlan(ctx context.Context, db bun.IDB, s MarinaScope, c *Contract, now time.Time) error {
	c.UpdatedAt = UTC(now)
	_, err := db.NewUpdate().Model(c).Column("policy", "total_cents", "price_snapshot", "installment_plan_version", "updated_at").
		Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Exec(ctx)
	return err
}

// Lookups of the contracts CSV import (m4-plan §3.7).

// CustomerIDsByFiscalCode maps fiscal codes (upper case) to customer ids of a marina.
func CustomerIDsByFiscalCode(ctx context.Context, db bun.IDB, s MarinaScope, codes []string) (map[string]string, error) {
	out := map[string]string{}
	if len(codes) == 0 {
		return out, nil
	}
	var rows []struct {
		ID         string `bun:"id"`
		FiscalCode string `bun:"fiscal_code"`
	}
	err := db.NewSelect().TableExpr("customers").ColumnExpr("id, UPPER(fiscal_code) AS fiscal_code").Where("marina_id = ?", s.ID()).
		Where("UPPER(fiscal_code) IN (?)", bun.List(codes)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.FiscalCode] = r.ID
	}
	return out, err
}

// VesselIDsByRegistration maps registrations (upper case) to vessel ids of a marina.
func VesselIDsByRegistration(ctx context.Context, db bun.IDB, s MarinaScope, regs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(regs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID           string `bun:"id"`
		Registration string `bun:"registration"`
	}
	err := db.NewSelect().TableExpr("vessels").ColumnExpr("id, UPPER(registration) AS registration").Where("marina_id = ?", s.ID()).
		Where("UPPER(registration) IN (?)", bun.List(regs)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.Registration] = r.ID
	}
	return out, err
}

// BerthIDsByCode maps berth codes to berth ids of a marina (every map).
func BerthIDsByCode(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	var rows []struct {
		ID   string `bun:"id"`
		Code string `bun:"code"`
	}
	err := db.NewSelect().TableExpr("berths").ColumnExpr("id, code").Where("marina_id = ?", s.ID()).Scan(ctx, &rows)
	out := map[string]string{}
	for _, r := range rows {
		out[r.Code] = r.ID
	}
	return out, err
}
