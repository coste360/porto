package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Queries of berth_groups (marina-level groups with overlapping memberships, spec 8.6).

// ListBerthGroups returns the groups of a marina by sort order.
func ListBerthGroups(ctx context.Context, db bun.IDB, s MarinaScope) ([]BerthGroup, error) {
	out := []BerthGroup{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("sort_order, code").Scan(ctx)
	return out, err
}

// GetBerthGroup returns one group of a marina.
func GetBerthGroup(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*BerthGroup, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	g := new(BerthGroup)
	if err := db.NewSelect().Model(g).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return g, nil
}

// InsertBerthGroup inserts a group; a code already taken is ErrConflict.
func InsertBerthGroup(ctx context.Context, db bun.IDB, s MarinaScope, g *BerthGroup) error {
	g.MarinaID = s.ID()
	_, err := db.NewInsert().Model(g).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateBerthGroup writes the editable fields when the stored version equals expected, and bumps the version.
func UpdateBerthGroup(ctx context.Context, db bun.IDB, s MarinaScope, g *BerthGroup, expected int) error {
	res, err := db.NewUpdate().Model(g).Column("code", "name", "kind", "sort_order", "updated_at").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", g.ID).Where("version = ?", expected).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetBerthGroup(ctx, db, s, g.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	g.Version = expected + 1
	return nil
}

// DeleteBerthGroup deletes a group (members cascade) when its version equals expected.
func DeleteBerthGroup(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := db.NewDelete().Table("berth_groups").Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetBerthGroup(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// GroupMemberCounts returns, per group id, the number of members per resource type.
func GroupMemberCounts(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]map[string]int, error) {
	var rows []struct {
		GroupID      string `bun:"group_id"`
		ResourceType string `bun:"resource_type"`
		N            int    `bun:"n"`
	}
	err := db.NewSelect().Table("berth_group_members").Column("group_id", "resource_type").ColumnExpr("COUNT(*) AS n").
		Where("marina_id = ?", s.ID()).Group("group_id", "resource_type").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]int{}
	for _, r := range rows {
		if out[r.GroupID] == nil {
			out[r.GroupID] = map[string]int{}
		}
		out[r.GroupID][r.ResourceType] = r.N
	}
	return out, nil
}
