package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Row type of the M12 public marina profile (internal/migrations/*/00027_marina_profile.sql, m12-plan §12.1).
// PUBLIC owns the writes (marina_profile.go) and the public read helpers (public_profile.go).

// Profile enums (CHECKs of 00027 and m12-plan §12.1).
const (
	ProfileSeabedSand  = "sand"
	ProfileSeabedMud   = "mud"
	ProfileSeabedRock  = "rock"
	ProfileSeabedWeed  = "weed"
	ProfileSeabedMixed = "mixed"
	ProfileSeabedOther = "other"
)

// MarinaProfile is the row of marina_profiles (one per marina). JSON columns: Locales []string, Address
// {street, postal_code, city, province, country}, VHFChannels []int, OfficeHours and DockHours [{weekday, from, to}],
// I18n texts {it, en}, Shelter []string, AccessImages and Photos [{file_id, alt, sort}], Services (typed object),
// OccupancyCategories []string, POI [{kind, x_cm, y_cm}].
type MarinaProfile struct {
	bun.BaseModel `bun:"table:marina_profiles"`

	MarinaID              string     `bun:"marina_id,pk"`
	Published             bool       `bun:"published"`
	PublishedAt           *time.Time `bun:"published_at"`
	Locales               JSON       `bun:"locales"`
	EntranceLatE7         *int64     `bun:"entrance_lat_e7"`
	EntranceLonE7         *int64     `bun:"entrance_lon_e7"`
	Address               JSON       `bun:"address"`
	Phone                 string     `bun:"phone"`
	Email                 string     `bun:"email"`
	Website               string     `bun:"website"`
	VHFChannels           JSON       `bun:"vhf_channels"`
	OfficeHours           JSON       `bun:"office_hours"`
	DockHours             JSON       `bun:"dock_hours"`
	HoursNote             JSON       `bun:"hours_note"`
	BerthsTotalOverride   *int       `bun:"berths_total_override"`
	TransitBerthsOverride *int       `bun:"transit_berths_override"`
	MaxLoaCm              *int64     `bun:"max_loa_cm"`
	DepthEntranceCm       *int64     `bun:"depth_entrance_cm"`
	DepthQuayCm           *int64     `bun:"depth_quay_cm"`
	DepthMinCm            *int64     `bun:"depth_min_cm"`
	DepthMaxCm            *int64     `bun:"depth_max_cm"`
	Seabed                string     `bun:"seabed,nullzero"`
	Shelter               JSON       `bun:"shelter"`
	AccessNotes           JSON       `bun:"access_notes"`
	AccessImages          JSON       `bun:"access_images"`
	Lights                JSON       `bun:"lights"`
	Services              JSON       `bun:"services"`
	Photos                JSON       `bun:"photos"`
	ShowMap               bool       `bun:"show_map"`
	MapID                 string     `bun:"map_id,nullzero"`
	ShowPrices            bool       `bun:"show_prices"`
	ShowOccupancy         bool       `bun:"show_occupancy"`
	OccupancyCategories   JSON       `bun:"occupancy_categories"`
	POI                   JSON       `bun:"poi"`
	Description           JSON       `bun:"description"`
	Version               int        `bun:"version"`
	CreatedAt             time.Time  `bun:"created_at"`
	UpdatedAt             time.Time  `bun:"updated_at"`
}

// GetMarinaProfile returns the saved profile of the marina (ErrNotFound when never saved: the service then builds
// the defaults from the marina, version 0).
func GetMarinaProfile(ctx context.Context, db bun.IDB, s MarinaScope) (*MarinaProfile, error) {
	row := new(MarinaProfile)
	if err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// LockMarinaProfile is GetMarinaProfile with FOR UPDATE on PostgreSQL (publish, photo delete).
func LockMarinaProfile(ctx context.Context, db bun.IDB, s MarinaScope) (*MarinaProfile, error) {
	row := new(MarinaProfile)
	q := db.NewSelect().Model(row).Where("marina_id = ?", s.ID())
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}
