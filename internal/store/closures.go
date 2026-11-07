package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of closures (spec 10.8; m3-plan §13 D18).

// Closure scopes.
const (
	ClosureMarina    = "marina"
	ClosureMap       = "map"
	ClosureStructure = "structure"
	ClosureGroup     = "group"
	ClosureResource  = "resource"
)

// InsertClosure inserts a closure of the marina.
func InsertClosure(ctx context.Context, tx bun.IDB, s MarinaScope, c *Closure) error {
	c.MarinaID = s.ID()
	if c.Version == 0 {
		c.Version = 1
	}
	_, err := tx.NewInsert().Model(c).Exec(ctx)
	return err
}

// GetClosure returns a closure of the marina.
func GetClosure(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Closure, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(Closure)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// DeleteClosure deletes a closure when its version equals expected.
func DeleteClosure(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int) error {
	res, err := tx.NewDelete().Model((*Closure)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetClosure(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListClosures returns the closures of the marina intersecting [from, to] (zero = unbounded), ordered by date.
func ListClosures(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]Closure, error) {
	out := []Closure{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if !from.IsZero() {
		q = q.Where("to_date >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("from_date <= ?", to)
	}
	err := q.OrderExpr("from_date, id").Scan(ctx)
	return out, err
}

// ClosureScopeExists reports whether the scope of a closure (map, structure, group or resource of resourceType)
// belongs to the marina.
func ClosureScopeExists(ctx context.Context, db bun.IDB, s MarinaScope, scopeType, scopeID, resourceType string) (bool, error) {
	if scopeType == ClosureResource {
		return ResourceExists(ctx, db, s, resourceType, scopeID)
	}
	table := map[string]string{ClosureMap: "maps", ClosureStructure: "harbor_structures", ClosureGroup: "berth_groups"}[scopeType]
	if table == "" || !ValidID(scopeID) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", s.ID()).Where("id = ?", scopeID).Count(ctx)
	return n > 0, err
}
