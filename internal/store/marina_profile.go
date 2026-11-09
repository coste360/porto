package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Writes of marina_profiles (M12, m12-plan §12.1). The row is created by the first save (If-Match 0) and then
// changed with optimistic locking on version: a stale version changes nothing and is ErrConflict, as a second
// first save racing on the primary key.

// profileColumns are the columns a save writes (everything but the keys, publication and timestamps of creation).
var profileColumns = []string{
	"locales", "entrance_lat_e7", "entrance_lon_e7", "address", "phone", "email", "website", "vhf_channels",
	"office_hours", "dock_hours", "hours_note", "berths_total_override", "transit_berths_override", "max_loa_cm",
	"depth_entrance_cm", "depth_quay_cm", "depth_min_cm", "depth_max_cm", "seabed", "shelter", "access_notes",
	"access_images", "lights", "services", "photos", "show_map", "map_id", "show_prices", "show_occupancy",
	"occupancy_categories", "poi", "description", "published", "published_at", "updated_at",
}

// SaveMarinaProfile writes row when the stored version equals ifMatch (0 = never saved: the row is inserted with
// version 1) and sets row.Version to the new version. A stale version or a concurrent first save is ErrConflict.
func SaveMarinaProfile(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, row *MarinaProfile, now time.Time) error {
	row.MarinaID = s.ID()
	row.UpdatedAt = now
	if ifMatch == 0 {
		row.Version = 1
		row.CreatedAt = now
		_, err := db.NewInsert().Model(row).Exec(ctx)
		if IsUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	res, err := db.NewUpdate().Model(row).Column(profileColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("version = ?", ifMatch).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrConflict
	}
	row.Version = ifMatch + 1
	return nil
}
