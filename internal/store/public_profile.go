package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read helpers of the public marina profile (M12, m12-plan §11, §12): the berth figures of the panel, the inputs
// of the aggregate occupancy. They return ids only for the computation in the service: no id, code or position
// leaves the public responses (INV-103).

// PublicBerthRow is an active berth as the profile needs it.
type PublicBerthRow struct {
	ID             string  `bun:"id"`
	MapID          string  `bun:"map_id"`
	StructureID    string  `bun:"structure_id"`
	PriceClass     string  `bun:"price_class"`
	OnlineSellable bool    `bun:"online_sellable"`
	Attributes     Strings `bun:"attributes"`
}

// PublicActiveBerths returns the active berths of the marina.
func PublicActiveBerths(ctx context.Context, db bun.IDB, s MarinaScope) ([]PublicBerthRow, error) {
	out := []PublicBerthRow{}
	err := db.NewSelect().Table("berths").
		Column("id", "map_id", "online_sellable", "attributes").
		ColumnExpr("COALESCE(CAST(structure_id AS TEXT), '') AS structure_id").
		ColumnExpr("COALESCE(price_class, '') AS price_class").
		Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("id").Scan(ctx, &out)
	return out, err
}

// PublicOccupiedBerths returns the berths with an occupancy row on night that still holds at now (expired holds
// never block, INV-05).
func PublicOccupiedBerths(ctx context.Context, db bun.IDB, s MarinaScope, night calendar.Date, now time.Time) (map[string]bool, error) {
	var ids []string
	err := db.NewSelect().Table("berth_occupancies").Column("berth_id").
		Where("marina_id = ?", s.ID()).Where("night = ?", night).
		Where("(expires_at IS NULL OR expires_at > ?)", now).Scan(ctx, &ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// PublicBerthGroups returns the berth groups of every berth member (closures scoped to a group).
func PublicBerthGroups(ctx context.Context, db bun.IDB, s MarinaScope) (map[string][]string, error) {
	var rows []BerthGroupMember
	err := db.NewSelect().Model(&rows).Column("group_id", "resource_id").
		Where("marina_id = ?", s.ID()).Where("resource_type = ?", "berth").Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		out[r.ResourceID] = append(out[r.ResourceID], r.GroupID)
	}
	return out, nil
}

// PublishedProfileMarinaIDs returns the ids of the marinas with a published profile among ids (getPublicMarina).
func PublishedProfileMarinaIDs(ctx context.Context, db bun.IDB, ids []string) (map[string]bool, error) {
	ids = validIDs(ids)
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var got []string
	err := db.NewSelect().Table("marina_profiles").Column("marina_id").
		Where("published = ?", true).Where("marina_id IN (?)", bun.List(ids)).Scan(ctx, &got)
	for _, id := range got {
		out[id] = true
	}
	return out, err
}

// ProfilePhotoUploaded reports whether fileID was uploaded through the public profile panel: the upload writes an
// audit row (entity profile_photo, insert-only), so other stored files of the marina (documents, handover photos)
// can never be referenced, served or deleted through the profile.
func ProfilePhotoUploaded(ctx context.Context, db bun.IDB, s MarinaScope, fileID string) (bool, error) {
	if !ValidID(fileID) {
		return false, nil
	}
	return db.NewSelect().Table("audit_log").
		Where("entity_type = ?", "profile_photo").Where("entity_id = ?", fileID).
		Where("marina_id = ?", s.ID()).Where("action = ?", "marina_profile.photo_upload").Exists(ctx)
}
