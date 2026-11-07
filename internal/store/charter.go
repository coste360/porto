package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of charter_fleets, charter_fleet_vessels and charter_contracts (spec 13.6; m4-plan §3.5). A vessel is in
// at most one active fleet (charter_fleet_vessels_active_uidx): a second one is ErrConflict.

// InsertCharterFleet inserts a fleet.
func InsertCharterFleet(ctx context.Context, db bun.IDB, f *CharterFleet) error {
	_, err := db.NewInsert().Model(f).Exec(ctx)
	return err
}

// GetCharterFleet returns a fleet of a marina.
func GetCharterFleet(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CharterFleet, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	f := new(CharterFleet)
	if err := db.NewSelect().Model(f).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return f, nil
}

// UpdateCharterFleet writes f when its version equals expected and bumps the version.
func UpdateCharterFleet(ctx context.Context, db bun.IDB, s MarinaScope, expected int, f *CharterFleet, now time.Time) error {
	f.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(f).Column("name", "updated_at").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", f.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	f.Version = expected + 1
	return nil
}

// ListCharterFleets returns the fleets of a marina (customerID narrows), by name.
func ListCharterFleets(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]CharterFleet, error) {
	out := []CharterFleet{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if customerID != "" {
		q = q.Where("customer_id = ?", customerID)
	}
	err := q.OrderExpr("name, id").Scan(ctx)
	return out, err
}

// FleetVessels returns the vessels of fleets (all, active or not).
func FleetVessels(ctx context.Context, db bun.IDB, s MarinaScope, fleetIDs []string) ([]CharterFleetVessel, error) {
	out := []CharterFleetVessel{}
	ids := validIDs(fleetIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("fleet_id IN (?)", bun.List(ids)).
		OrderExpr("fleet_id, created_at, id").Scan(ctx)
	return out, err
}

// SetFleetVessels makes vesselIDs the active vessels of a fleet: missing rows are inserted, others deactivated.
// A vessel active in another fleet is ErrConflict.
func SetFleetVessels(ctx context.Context, db bun.IDB, s MarinaScope, fleetID string, vesselIDs []string, now time.Time) error {
	rows, err := FleetVessels(ctx, db, s, []string{fleetID})
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, v := range vesselIDs {
		want[v] = true
	}
	have := map[string]*CharterFleetVessel{}
	for i := range rows {
		have[rows[i].VesselID] = &rows[i]
	}
	for _, r := range rows {
		if r.Active && !want[r.VesselID] {
			if _, err := db.NewUpdate().Model((*CharterFleetVessel)(nil)).Set("active = ?", false).Set("updated_at = ?", UTC(now)).
				Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Exec(ctx); err != nil {
				return err
			}
		}
	}
	for _, v := range vesselIDs {
		if r := have[v]; r != nil {
			if r.Active {
				continue
			}
			_, err := db.NewUpdate().Model((*CharterFleetVessel)(nil)).Set("active = ?", true).Set("updated_at = ?", UTC(now)).
				Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Exec(ctx)
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			if err != nil {
				return err
			}
			continue
		}
		_, err := db.NewInsert().Model(&CharterFleetVessel{
			ID: NewID(), MarinaID: s.ID(), FleetID: fleetID, VesselID: v, Active: true, CreatedAt: UTC(now), UpdatedAt: UTC(now),
		}).Exec(ctx)
		if IsUniqueViolation(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// InsertCharterContract inserts the charter fields of a contract.
func InsertCharterContract(ctx context.Context, db bun.IDB, c *CharterContract) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// GetCharterContract returns the charter fields of a contract.
func GetCharterContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*CharterContract, error) {
	if !ValidID(contractID) {
		return nil, ErrNotFound
	}
	c := new(CharterContract)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}
