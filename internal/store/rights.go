package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the M4 berth rights (m4-plan §4.1, §4.2): rights, holders, authorised vessels, owner allocation
// links and transfers. Every query is scoped by the marina.

// lockRow takes a row lock on PostgreSQL (SQLite write transactions are already exclusive).
func lockRow(ctx context.Context, tx bun.IDB, table string, s MarinaScope, id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	q := "SELECT id FROM " + table + " WHERE marina_id = ? AND id = ?"
	if IsPostgres(tx) {
		q += " FOR UPDATE"
	}
	var got string
	err := tx.NewRaw(q, s.ID(), id).Scan(ctx, &got)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// versionedUpdate writes columns of a row when its version equals expected and bumps the version; a missing row
// is ErrNotFound and a stale version ErrVersionMismatch.
func versionedUpdate(ctx context.Context, db bun.IDB, s MarinaScope, model any, table, id string, expected int, columns ...string) error {
	res, err := db.NewUpdate().Model(model).Column(columns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		n, err := db.NewSelect().Table(table).Where("marina_id = ?", s.ID()).Where("id = ?", id).Count(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return ErrVersionMismatch
	}
	return nil
}

// ---- berth_rights ----

// RightFilter narrows ListBerthRights.
type RightFilter struct {
	BerthID    string
	CustomerID string
	Status     string
	ActiveOn   calendar.Date
}

// InsertBerthRight inserts a right.
func InsertBerthRight(ctx context.Context, db bun.IDB, s MarinaScope, r *BerthRight) error {
	r.MarinaID = s.ID()
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// GetBerthRight returns a right of the marina.
func GetBerthRight(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*BerthRight, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(BerthRight)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// LockBerthRight locks a right row (PostgreSQL) and returns it: the per-right lock that serialises mandates,
// transfers and holder changes of one right.
func LockBerthRight(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*BerthRight, error) {
	if err := lockRow(ctx, tx, "berth_rights", s, id); err != nil {
		return nil, err
	}
	return GetBerthRight(ctx, tx, s, id)
}

// UpdateBerthRight writes the mutable fields of a right when its version equals expected and bumps the version.
func UpdateBerthRight(ctx context.Context, db bun.IDB, s MarinaScope, r *BerthRight, expected int) error {
	if err := versionedUpdate(ctx, db, s, r, "berth_rights", r.ID, expected, "to_on", "title", "status", "end_reason", "updated_at"); err != nil {
		return err
	}
	r.Version = expected + 1
	return nil
}

// ListBerthRights returns a page of rights by id (creation order).
func ListBerthRights(ctx context.Context, db bun.IDB, s MarinaScope, f RightFilter, cursor string, limit int) (Page[BerthRight], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[BerthRight]{}, err
	}
	limit = ClampLimit(limit)
	var rows []BerthRight
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.BerthID != "" {
		q = q.Where("berth_id = ?", f.BerthID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if !f.ActiveOn.IsZero() {
		q = q.Where("status = 'active' AND from_on <= ? AND to_on >= ?", f.ActiveOn, f.ActiveOn)
	}
	if f.CustomerID != "" {
		q = q.Where("id IN (SELECT right_id FROM berth_right_holders WHERE marina_id = ? AND customer_id = ?)", s.ID(), f.CustomerID)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[BerthRight]{}, err
	}
	return pageOf(rows, limit, func(r BerthRight) string { return r.ID }), nil
}

// RightsOfBerth returns every right of a berth by start date (the holder chain).
func RightsOfBerth(ctx context.Context, db bun.IDB, s MarinaScope, berthID string) ([]BerthRight, error) {
	out := []BerthRight{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("berth_id = ?", berthID).OrderExpr("from_on, id").Scan(ctx)
	return out, err
}

// RightsActiveOn returns the rights active on a date, optionally limited to some berths.
func RightsActiveOn(ctx context.Context, db bun.IDB, s MarinaScope, on calendar.Date, berthIDs []string) ([]BerthRight, error) {
	out := []BerthRight{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("status IN ('active','transferred','ended') AND from_on <= ? AND to_on >= ?", on, on)
	if berthIDs != nil {
		if len(berthIDs) == 0 {
			return out, nil
		}
		q = q.Where("berth_id IN (?)", bun.List(berthIDs))
	}
	err := q.OrderExpr("berth_id, from_on").Scan(ctx)
	return out, err
}

// ActiveRights returns every active right of the marina.
func ActiveRights(ctx context.Context, db bun.IDB, s MarinaScope) ([]BerthRight, error) {
	out := []BerthRight{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = 'active'").OrderExpr("id").Scan(ctx)
	return out, err
}

// CountBerthRights returns how many rights a marina has.
func CountBerthRights(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*BerthRight)(nil)).Where("marina_id = ?", s.ID()).Count(ctx))
}

// ---- berth_right_holders ----

// ReplaceRightHolders replaces the holders of a right.
func ReplaceRightHolders(ctx context.Context, db bun.IDB, s MarinaScope, rightID string, hs []BerthRightHolder) error {
	if _, err := db.NewDelete().Model((*BerthRightHolder)(nil)).Where("marina_id = ?", s.ID()).Where("right_id = ?", rightID).Exec(ctx); err != nil {
		return err
	}
	if len(hs) == 0 {
		return nil
	}
	for i := range hs {
		hs[i].MarinaID, hs[i].RightID = s.ID(), rightID
	}
	_, err := db.NewInsert().Model(&hs).Exec(ctx)
	return err
}

// HoldersOfRights returns the holders of rights, by right and share (largest first).
func HoldersOfRights(ctx context.Context, db bun.IDB, s MarinaScope, rightIDs []string) ([]BerthRightHolder, error) {
	out := []BerthRightHolder{}
	ids := validIDs(rightIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("right_id IN (?)", bun.List(ids)).
		OrderExpr("right_id, share_millis DESC, id").Scan(ctx)
	return out, err
}

// ---- berth_right_vessels ----

// ReplaceRightVessels replaces the authorised vessels of a right.
func ReplaceRightVessels(ctx context.Context, db bun.IDB, s MarinaScope, rightID string, vs []BerthRightVessel) error {
	if _, err := db.NewDelete().Model((*BerthRightVessel)(nil)).Where("marina_id = ?", s.ID()).Where("right_id = ?", rightID).Exec(ctx); err != nil {
		return err
	}
	if len(vs) == 0 {
		return nil
	}
	for i := range vs {
		vs[i].MarinaID, vs[i].RightID = s.ID(), rightID
	}
	_, err := db.NewInsert().Model(&vs).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// VesselsOfRights returns the authorised vessels of rights (primary first).
func VesselsOfRights(ctx context.Context, db bun.IDB, s MarinaScope, rightIDs []string) ([]BerthRightVessel, error) {
	out := []BerthRightVessel{}
	ids := validIDs(rightIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("right_id IN (?)", bun.List(ids)).
		OrderExpr("right_id, is_primary DESC, id").Scan(ctx)
	return out, err
}

// ---- berth_right_allocations ----

// LinkRightAllocation records that an owner allocation belongs to a right.
func LinkRightAllocation(ctx context.Context, db bun.IDB, s MarinaScope, rightID, allocationID string, at time.Time) error {
	_, err := db.NewInsert().Model(&BerthRightAllocation{AllocationID: allocationID, MarinaID: s.ID(), RightID: rightID, CreatedAt: at}).Exec(ctx)
	return err
}

// AllocationsOfRights returns the owner allocations of rights (activeOnly: only the active ones), by first night.
func AllocationsOfRights(ctx context.Context, db bun.IDB, s MarinaScope, rightIDs []string, activeOnly bool) (map[string][]Allocation, error) {
	out := map[string][]Allocation{}
	ids := validIDs(rightIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		Allocation
		RightID string `bun:"right_id"`
	}
	q := db.NewSelect().TableExpr("allocations AS a").ColumnExpr("a.*, l.right_id").
		Join("JOIN berth_right_allocations AS l ON l.allocation_id = a.id").
		Where("l.marina_id = ?", s.ID()).Where("l.right_id IN (?)", bun.List(ids))
	if activeOnly {
		q = q.Where("a.status = 'active'")
	}
	if err := q.OrderExpr("a.first_night, a.id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RightID] = append(out[r.RightID], r.Allocation)
	}
	return out, nil
}

// RightOfAllocations returns, per owner allocation id, its right id.
func RightOfAllocations(ctx context.Context, db bun.IDB, s MarinaScope, allocIDs []string) (map[string]string, error) {
	out := map[string]string{}
	ids := validIDs(allocIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []BerthRightAllocation
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("allocation_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.AllocationID] = r.RightID
	}
	return out, nil
}

// ---- right_transfers ----

// TransferFilter narrows ListRightTransfers.
type TransferFilter struct {
	RightID string
	Status  string
}

// InsertRightTransfer inserts a transfer.
func InsertRightTransfer(ctx context.Context, db bun.IDB, s MarinaScope, t *RightTransfer) error {
	t.MarinaID = s.ID()
	_, err := db.NewInsert().Model(t).Exec(ctx)
	return err
}

// GetRightTransfer returns a transfer of the marina.
func GetRightTransfer(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RightTransfer, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(RightTransfer)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// UpdateRightTransfer writes the mutable fields of a transfer when its version equals expected.
func UpdateRightTransfer(ctx context.Context, db bun.IDB, s MarinaScope, t *RightTransfer, expected int) error {
	if err := versionedUpdate(ctx, db, s, t, "right_transfers", t.ID, expected, "status", "requirement_issues", "new_right_id",
		"document_id", "signature_request_id", "completed_at", "cancelled_at", "updated_at"); err != nil {
		return err
	}
	t.Version = expected + 1
	return nil
}

// ListRightTransfers returns a page of transfers by id.
func ListRightTransfers(ctx context.Context, db bun.IDB, s MarinaScope, f TransferFilter, cursor string, limit int) (Page[RightTransfer], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[RightTransfer]{}, err
	}
	limit = ClampLimit(limit)
	var rows []RightTransfer
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.RightID != "" {
		q = q.Where("right_id = ?", f.RightID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[RightTransfer]{}, err
	}
	return pageOf(rows, limit, func(r RightTransfer) string { return r.ID }), nil
}

// CompletedTransfersOfRights returns, per new right id, the transfer that created it.
func CompletedTransfersOfRights(ctx context.Context, db bun.IDB, s MarinaScope, rightIDs []string) (map[string]string, error) {
	out := map[string]string{}
	ids := validIDs(rightIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []RightTransfer
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("status = 'completed'").
		Where("new_right_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.NewRightID] = r.ID
	}
	return out, nil
}

// ItemBaseRuleBodies returns the bodies of the active base rules of an item in the active price lists valid on a
// date (the takeover fee of a transfer is the flat amount of the first one).
func ItemBaseRuleBodies(ctx context.Context, db bun.IDB, s MarinaScope, itemID string, on calendar.Date) ([]JSON, error) {
	var out []JSON
	if !ValidID(itemID) {
		return nil, ErrNotFound
	}
	err := db.NewRaw(`SELECT r.body FROM price_rules AS r JOIN price_lists AS l ON l.id = r.price_list_id
		WHERE r.marina_id = ? AND r.item_id = ? AND r.stage = 'base' AND r.active = ? AND l.status = 'active'
			AND l.valid_from <= ? AND l.valid_to >= ? ORDER BY l.valid_from DESC, r.seq`, s.ID(), itemID, true, on, on).Scan(ctx, &out)
	return out, err
}

// GetItemOfMarina returns ErrNotFound unless the item belongs to the marina.
func GetItemOfMarina(ctx context.Context, db bun.IDB, s MarinaScope, itemID string) error {
	_, err := GetItem(ctx, db, s, itemID)
	return err
}

// CustomersByTaxIDs returns the active customers of the marina whose fiscal code or VAT number (upper case) is
// among ids, by that id (rights import).
func CustomersByTaxIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]Customer, error) {
	out := map[string]Customer{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Customer
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("merged_into_id IS NULL").
		Where("(UPPER(fiscal_code) IN (?) OR UPPER(vat_number) IN (?))", bun.List(ids), bun.List(ids)).OrderExpr("id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, c := range rows {
		for _, k := range []string{c.FiscalCode, c.VATNumber} {
			if k == "" {
				continue
			}
			up := upper(k)
			if _, ok := out[up]; !ok {
				out[up] = c
			}
		}
	}
	return out, nil
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// RightDependents counts the rows that make a right not undoable by an import undo: mandates, transfers, fee
// lines, holder instalments and allocations other than its owner ones.
func RightDependents(ctx context.Context, db bun.IDB, s MarinaScope, rightID string) (int, error) {
	total := 0
	for _, q := range []struct{ table, col string }{
		{"management_mandates", "right_id"},
		{"right_transfers", "right_id"},
		{"management_fee_lines", "right_id"},
		{"holder_installments", "right_id"},
		{"common_charge_allocations", "right_id"},
	} {
		n, err := db.NewSelect().Table(q.table).Where("marina_id = ?", s.ID()).Where(q.col+" = ?", rightID).Count(ctx)
		if err != nil {
			return 0, err
		}
		total += int(n)
	}
	return total, nil
}

// DeleteBerthRight deletes a right (holders, vessels and allocation links cascade).
func DeleteBerthRight(ctx context.Context, db bun.IDB, s MarinaScope, id string) error {
	return expectOne(db.NewDelete().Model((*BerthRight)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx))
}
