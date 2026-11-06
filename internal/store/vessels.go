package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of vessels and vessel_parties (m1-plan §3.1, §4.4). Every function is scoped by the marina.

// VesselFilter narrows ListVessels. Search is a normalised term matched against vessels.search_text.
type VesselFilter struct {
	Search     string
	CustomerID string // vessels linked to this customer by any party role
	HullType   string
	Archived   bool // true = only archived vessels, false = only active ones
}

// InsertVessel inserts a vessel.
func InsertVessel(ctx context.Context, db bun.IDB, v *Vessel) error {
	_, err := db.NewInsert().Model(v).Exec(ctx)
	return err
}

// GetVessel returns one vessel of a marina.
func GetVessel(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Vessel, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	v := new(Vessel)
	if err := db.NewSelect().Model(v).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return v, nil
}

// VesselsByIDs returns the vessels of a marina with the given ids, ordered by id.
func VesselsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Vessel, error) {
	out := []Vessel{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).
		OrderExpr("id").Scan(ctx)
	return out, err
}

var vesselColumns = []string{
	"name", "registration", "registry_office", "flag", "home_port", "hull_type", "model", "builder", "year_built",
	"hull_color", "hull_material", "loa_cm", "lwl_cm", "beam_cm", "draft_cm", "air_draft_cm", "displacement_kg",
	"measured_loa_cm", "measured_beam_cm", "measured_draft_cm", "measured_air_draft_cm", "measured_at",
	"measured_by_user_id", "dimensions_source", "engines", "fuel", "power_required_a", "notes", "search_text",
	"archived_at", "updated_at",
}

// UpdateVessel writes every editable column when the stored version equals ifMatch and bumps the version.
func UpdateVessel(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, v *Vessel) error {
	res, err := db.NewUpdate().Model(v).Column(vesselColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", v.ID).Where("version = ?", ifMatch).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetVessel(ctx, db, s, v.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	v.Version = ifMatch + 1
	return nil
}

// ListVessels returns a page of vessels, newest first.
func ListVessels(ctx context.Context, db bun.IDB, s MarinaScope, f VesselFilter, cursor string, limit int) (Page[Vessel], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Vessel]{}, err
	}
	rows := []Vessel{}
	q := db.NewSelect().Model(&rows).Where("vessel.marina_id = ?", s.ID())
	if f.Archived {
		q = q.Where("vessel.archived_at IS NOT NULL")
	} else {
		q = q.Where("vessel.archived_at IS NULL")
	}
	if f.HullType != "" {
		q = q.Where("vessel.hull_type = ?", f.HullType)
	}
	if f.Search != "" {
		q = q.Where("vessel.search_text LIKE ? ESCAPE '!'", likePattern(f.Search))
	}
	if f.CustomerID != "" {
		if !ValidID(f.CustomerID) {
			return Page[Vessel]{Items: []Vessel{}}, nil
		}
		q = q.Where("EXISTS (SELECT 1 FROM vessel_parties AS vp WHERE vp.vessel_id = vessel.id AND vp.marina_id = ? AND vp.customer_id = ?)",
			s.ID(), f.CustomerID)
	}
	if after != "" {
		q = q.Where("vessel.id < ?", after)
	}
	if err := q.OrderExpr("vessel.id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Vessel]{}, err
	}
	return pageOf(rows, limit, func(v Vessel) string { return v.ID }), nil
}

// PartiesOfVessels returns the parties of the given vessels, ordered by vessel, role and creation.
func PartiesOfVessels(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string) ([]VesselParty, error) {
	out := []VesselParty{}
	vesselIDs = validIDs(vesselIDs)
	if len(vesselIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("vessel_id IN (?)", bun.List(vesselIDs)).
		OrderExpr("vessel_id, role, id").Scan(ctx)
	return out, err
}

// PartiesOfCustomers returns the parties of the given customers, ordered by customer, vessel and role.
func PartiesOfCustomers(ctx context.Context, db bun.IDB, s MarinaScope, customerIDs []string) ([]VesselParty, error) {
	out := []VesselParty{}
	customerIDs = validIDs(customerIDs)
	if len(customerIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id IN (?)", bun.List(customerIDs)).
		OrderExpr("customer_id, vessel_id, role").Scan(ctx)
	return out, err
}

// ReplaceVesselParties deletes the parties of a vessel and inserts ps.
func ReplaceVesselParties(ctx context.Context, db bun.IDB, s MarinaScope, vesselID string, ps []VesselParty) error {
	if _, err := db.NewDelete().Table("vessel_parties").Where("marina_id = ?", s.ID()).Where("vessel_id = ?", vesselID).
		Exec(ctx); err != nil {
		return err
	}
	if len(ps) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&ps).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// DeleteVesselParty removes one party row.
func DeleteVesselParty(ctx context.Context, db bun.IDB, s MarinaScope, id string) error {
	res, err := db.NewDelete().Table("vessel_parties").Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// UpdateVesselParty re-points a party to another customer and sets its share (merge).
func UpdateVesselParty(ctx context.Context, db bun.IDB, s MarinaScope, id, customerID string, shareMillis int64, at time.Time) error {
	var share any
	if shareMillis > 0 {
		share = shareMillis
	}
	res, err := db.NewUpdate().Table("vessel_parties").Set("customer_id = ?", customerID).Set("share_millis = ?", share).
		Set("updated_at = ?", at).Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// DeletePartiesOfCustomer removes every party link of a customer (anonymisation).
func DeletePartiesOfCustomer(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) error {
	_, err := db.NewDelete().Table("vessel_parties").Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		Exec(ctx)
	return err
}

// BumpVesselVersion increments the version of a vessel (party changes that do not go through UpdateVessel).
func BumpVesselVersion(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) error {
	res, err := db.NewUpdate().Table("vessels").Set("version = version + 1").Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}
