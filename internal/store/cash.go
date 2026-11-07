package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Cash registers, sessions and movements (m5-plan §5.1). INV-55: cash_sessions_open_uidx keeps at most one open
// session per register; closed sessions are frozen by trigger. Owned by CASH-DRIVERS.

// DefaultCashRegisterCode is the code of the register created lazily.
const DefaultCashRegisterCode = "main"

// EnsureDefaultCashRegister returns the default register of the marina, creating "main" when the marina has none
// (a concurrent creation loses on the unique indexes and reads the winner).
func EnsureDefaultCashRegister(ctx context.Context, tx bun.IDB, s MarinaScope, name string, now time.Time) (*CashRegister, error) {
	if r, err := DefaultCashRegister(ctx, tx, s); err == nil {
		return r, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now = UTC(now)
	row := &CashRegister{
		ID: NewID(), MarinaID: s.ID(), Code: DefaultCashRegisterCode, Name: name, Active: true, IsDefault: true,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.NewInsert().Model(row).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return nil, fmt.Errorf("default cash register: %w", err)
	}
	return DefaultCashRegister(ctx, tx, s)
}

// DefaultCashRegister returns the default register (ErrNotFound when none).
func DefaultCashRegister(ctx context.Context, db bun.IDB, s MarinaScope) (*CashRegister, error) {
	r := new(CashRegister)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("is_default = ?", true).Limit(1).Scan(ctx)
	return r, notFound(err)
}

// GetCashRegister returns a register of the marina.
func GetCashRegister(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CashRegister, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(CashRegister)
	err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return r, notFound(err)
}

// ListCashRegisters returns the registers of the marina, default first then by code.
func ListCashRegisters(ctx context.Context, db bun.IDB, s MarinaScope) ([]CashRegister, error) {
	out := []CashRegister{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("is_default DESC, code").Scan(ctx)
	return out, err
}

// InsertCashRegister inserts a register; a taken code is ErrConflict.
func InsertCashRegister(ctx context.Context, tx bun.IDB, r *CashRegister) error {
	if _, err := tx.NewInsert().Model(r).Exec(ctx); err != nil {
		if IsUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// UpdateCashRegister saves name, active and is_default when the version equals expected.
func UpdateCashRegister(ctx context.Context, tx bun.IDB, s MarinaScope, r *CashRegister, expected int, now time.Time) error {
	res, err := tx.NewUpdate().Model((*CashRegister)(nil)).
		Set("name = ?", r.Name).Set("active = ?", r.Active).Set("is_default = ?", r.IsDefault).
		Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		if IsUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	r.Version = expected + 1
	return nil
}

// ClearDefaultCashRegister removes the default flag from every register of the marina but keep.
func ClearDefaultCashRegister(ctx context.Context, tx bun.IDB, s MarinaScope, keep string, now time.Time) error {
	_, err := tx.NewUpdate().Model((*CashRegister)(nil)).Set("is_default = ?", false).
		Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("is_default = ?", true).Where("id <> ?", keep).Exec(ctx)
	return err
}

// InsertCashSession opens a session; a second open session of the register is ErrConflict (INV-55).
func InsertCashSession(ctx context.Context, tx bun.IDB, c *CashSession) error {
	if _, err := tx.NewInsert().Model(c).Exec(ctx); err != nil {
		if IsUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// GetCashSession returns a session of the marina.
func GetCashSession(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CashSession, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(CashSession)
	err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx)
	return c, notFound(err)
}

// OpenCashSessionOf returns the open session of a register (ErrNotFound when none).
func OpenCashSessionOf(ctx context.Context, db bun.IDB, s MarinaScope, registerID string) (*CashSession, error) {
	c := new(CashSession)
	err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("register_id = ?", registerID).
		Where("status = ?", CashSessionOpen).Limit(1).Scan(ctx)
	return c, notFound(err)
}

// OpenCashSessionIDs maps register ids to their open session ids.
func OpenCashSessionIDs(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]string, error) {
	var rows []CashSession
	if err := db.NewSelect().Model(&rows).Column("id", "register_id").Where("marina_id = ?", s.ID()).
		Where("status = ?", CashSessionOpen).Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.RegisterID] = r.ID
	}
	return out, nil
}

// CashSessionFilter narrows ListCashSessions.
type CashSessionFilter struct {
	RegisterID string
	Status     string
	From, To   calendar.Date // business date range (zero = open)
}

// ListCashSessions returns sessions newest first (cursor = last id).
func ListCashSessions(ctx context.Context, db bun.IDB, s MarinaScope, f CashSessionFilter, cursor string, limit int) (Page[CashSession], error) {
	limit = ClampLimit(limit)
	rows := []CashSession{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.RegisterID != "" {
		q = q.Where("register_id = ?", f.RegisterID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if !f.From.IsZero() {
		q = q.Where("business_date >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("business_date <= ?", f.To)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[CashSession]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[CashSession]{}, err
	}
	return pageOf(rows, limit, func(c CashSession) string { return c.ID }), nil
}

// TouchOpenCashSession re-checks under the lock that a session is still open with a no-op conditional UPDATE, so a
// payment either lands before a close or fails (m5-plan §5.1). It reports whether the session is open.
func TouchOpenCashSession(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (bool, error) {
	res, err := tx.NewUpdate().Model((*CashSession)(nil)).Set("updated_at = updated_at").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("status = ?", CashSessionOpen).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// CloseCashSession freezes a session with its summary when it is still open at version expected.
func CloseCashSession(ctx context.Context, tx bun.IDB, s MarinaScope, c *CashSession, expected int, now time.Time) error {
	now = UTC(now)
	res, err := tx.NewUpdate().Model((*CashSession)(nil)).
		Set("status = ?", CashSessionClosed).Set("closed_by_user_id = ?", nullString(c.ClosedByUserID)).
		Set("closed_at = ?", now).Set("expected = ?", c.Expected).Set("counted = ?", c.Counted).
		Set("difference_cents = ?", c.DifferenceCents).Set("notes = ?", c.Notes).
		Set("version = version + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("status = ?", CashSessionOpen).
		Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	c.Status, c.ClosedAt, c.Version, c.UpdatedAt = CashSessionClosed, now, expected+1, now
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// InsertCashMovement inserts a movement (insert-only).
func InsertCashMovement(ctx context.Context, tx bun.IDB, m *CashMovement) error {
	_, err := tx.NewInsert().Model(m).Exec(ctx)
	return err
}

// ListCashMovements returns the movements of a session in creation order.
func ListCashMovements(ctx context.Context, db bun.IDB, s MarinaScope, sessionID string) ([]CashMovement, error) {
	out := []CashMovement{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("session_id = ?", sessionID).OrderExpr("id").Scan(ctx)
	return out, err
}

// CashTotals are the sums of a session: payments and succeeded refunds per method, movements in and out.
type CashTotals struct {
	Payments     map[string]int64
	Refunds      map[string]int64
	MovementsIn  int64
	MovementsOut int64
}

type methodSum struct {
	Method string `bun:"method"`
	Sum    int64  `bun:"sum"`
}

// SessionCashTotals sums what landed in a session. Call it after LockCashSession when the result is frozen.
func SessionCashTotals(ctx context.Context, db bun.IDB, s MarinaScope, sessionID string) (CashTotals, error) {
	t := CashTotals{Payments: map[string]int64{}, Refunds: map[string]int64{}}
	var pays []methodSum
	if err := db.NewSelect().TableExpr("payments").ColumnExpr("method, SUM(amount_cents) AS sum").
		Where("marina_id = ?", s.ID()).Where("cash_session_id = ?", sessionID).GroupExpr("method").Scan(ctx, &pays); err != nil {
		return t, fmt.Errorf("session payments: %w", err)
	}
	for _, p := range pays {
		t.Payments[p.Method] = p.Sum
	}
	var refs []methodSum
	if err := db.NewSelect().TableExpr("refunds").ColumnExpr("method, SUM(amount_cents) AS sum").
		Where("marina_id = ?", s.ID()).Where("cash_session_id = ?", sessionID).Where("status = ?", RefundSucceeded).
		GroupExpr("method").Scan(ctx, &refs); err != nil {
		return t, fmt.Errorf("session refunds: %w", err)
	}
	for _, r := range refs {
		t.Refunds[r.Method] = r.Sum
	}
	var movs []struct {
		Kind string `bun:"kind"`
		Sum  int64  `bun:"sum"`
	}
	if err := db.NewSelect().TableExpr("cash_movements").ColumnExpr("kind, SUM(amount_cents) AS sum").
		Where("marina_id = ?", s.ID()).Where("session_id = ?", sessionID).GroupExpr("kind").Scan(ctx, &movs); err != nil {
		return t, fmt.Errorf("session movements: %w", err)
	}
	for _, m := range movs {
		if m.Kind == "in" {
			t.MovementsIn = m.Sum
		} else {
			t.MovementsOut = m.Sum
		}
	}
	return t, nil
}

// SessionPayments returns the payments of a session (Z report VAT totals), oldest first.
func SessionPayments(ctx context.Context, db bun.IDB, s MarinaScope, sessionID string) ([]Payment, error) {
	out := []Payment{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("cash_session_id = ?", sessionID).OrderExpr("id").Scan(ctx)
	return out, err
}

// SessionRefunds returns the succeeded refunds of a session.
func SessionRefunds(ctx context.Context, db bun.IDB, s MarinaScope, sessionID string) ([]Refund, error) {
	out := []Refund{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("cash_session_id = ?", sessionID).
		Where("status = ?", RefundSucceeded).OrderExpr("id").Scan(ctx)
	return out, err
}

// UserNames maps user ids to display names (Z report operators; staff names are not customer data).
func UserNames(ctx context.Context, db bun.IDB, ids []string) (map[string]string, error) {
	out := map[string]string{}
	var want []string
	for _, id := range ids {
		if ValidID(id) {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	var rows []User
	if err := db.NewSelect().Model(&rows).Column("id", "name").Where("id IN (?)", bun.List(want)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, u := range rows {
		out[u.ID] = u.Name
	}
	return out, nil
}
