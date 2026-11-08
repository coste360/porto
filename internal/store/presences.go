package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of presences and vessel_presence (spec 10.6, 9.5; m3-plan §4.4), plus the allocation reads of the
// movements of the day. Every function is scoped by the marina.

// PresenceFilter narrows ListPresences.
type PresenceFilter struct {
	Night    *calendar.Date
	VesselID string
}

// InsertPresence inserts a presence event.
func InsertPresence(ctx context.Context, db bun.IDB, p *Presence) error {
	_, err := db.NewInsert().Model(p).Exec(ctx)
	return err
}

// GetPresence returns one presence of a marina.
func GetPresence(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Presence, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	p := new(Presence)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// PresenceByIdempotencyKey returns the presence recorded with an Idempotency-Key (M6b domain dedupe).
func PresenceByIdempotencyKey(ctx context.Context, db bun.IDB, s MarinaScope, key string) (*Presence, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	p := new(Presence)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("idempotency_key = ?", key).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// SetPresencePhoto links a stored file to a presence.
func SetPresencePhoto(ctx context.Context, db bun.IDB, s MarinaScope, id, fileID string, now time.Time) error {
	res, err := db.NewUpdate().Model((*Presence)(nil)).Set("photo_file_id = ?", fileID).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// ListPresences returns a page of presences, newest first.
func ListPresences(ctx context.Context, db bun.IDB, s MarinaScope, f PresenceFilter, cursor string, limit int) (Page[Presence], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Presence]{}, err
	}
	rows := []Presence{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Night != nil {
		q = q.Where("night = ?", *f.Night)
	}
	if f.VesselID != "" {
		if !ValidID(f.VesselID) {
			return Page[Presence]{Items: []Presence{}}, nil
		}
		q = q.Where("vessel_id = ?", f.VesselID)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Presence]{}, err
	}
	return pageOf(rows, limit, func(p Presence) string { return p.ID }), nil
}

// PresencesOfContracts returns the presences linked to the given contracts, oldest first.
func PresencesOfContracts(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string) ([]Presence, error) {
	out := []Presence{}
	contractIDs = validIDs(contractIDs)
	if len(contractIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(contractIDs)).
		OrderExpr("at, id").Scan(ctx)
	return out, err
}

// PresencesOn returns the presences of a night, oldest first.
func PresencesOn(ctx context.Context, db bun.IDB, s MarinaScope, night calendar.Date) ([]Presence, error) {
	out := []Presence{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("night = ?", night).OrderExpr("at, id").Scan(ctx)
	return out, err
}

// GetVesselPresence returns the presence state of a vessel (ErrNotFound when none was ever recorded).
func GetVesselPresence(ctx context.Context, db bun.IDB, s MarinaScope, vesselID string) (*VesselPresence, error) {
	if !ValidID(vesselID) {
		return nil, ErrNotFound
	}
	vp := new(VesselPresence)
	if err := db.NewSelect().Model(vp).Where("marina_id = ?", s.ID()).Where("vessel_id = ?", vesselID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return vp, nil
}

// VesselPresences returns the presence states of the given vessels keyed by vessel id.
func VesselPresences(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string) (map[string]VesselPresence, error) {
	out := map[string]VesselPresence{}
	vesselIDs = validIDs(vesselIDs)
	if len(vesselIDs) == 0 {
		return out, nil
	}
	var rows []VesselPresence
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("vessel_id IN (?)", bun.List(vesselIDs)).
		Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.VesselID] = r
	}
	return out, nil
}

// UpsertVesselPresence writes the presence state of a vessel.
func UpsertVesselPresence(ctx context.Context, db bun.IDB, vp *VesselPresence) error {
	_, err := db.NewInsert().Model(vp).
		On("CONFLICT (vessel_id) DO UPDATE").
		Set("state = EXCLUDED.state").Set("since = EXCLUDED.since").Set("allocation_id = EXCLUDED.allocation_id").
		Set("last_presence_id = EXCLUDED.last_presence_id").Set("expected_return_on = EXCLUDED.expected_return_on").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// VesselsInState returns the presence rows of a marina in a state.
func VesselsInState(ctx context.Context, db bun.IDB, s MarinaScope, state string) ([]VesselPresence, error) {
	out := []VesselPresence{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("state = ?", state).OrderExpr("since, vessel_id").Scan(ctx)
	return out, err
}

// VesselAllocationOn returns the active allocation of a vessel covering a night (contract, transit or hold;
// contracts and transits first), or ErrNotFound.
func VesselAllocationOn(ctx context.Context, db bun.IDB, s MarinaScope, vesselID string, night calendar.Date) (*Allocation, error) {
	if !ValidID(vesselID) {
		return nil, ErrNotFound
	}
	var rows []Allocation
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("vessel_id = ?", vesselID).
		Where("status = ?", AllocActive).Where("kind IN (?)", bun.List([]string{AllocKindContract, AllocKindTransit, AllocKindHold})).
		Where("first_night <= ?", night).Where("last_night >= ?", night).
		OrderExpr("CASE kind WHEN 'hold' THEN 1 ELSE 0 END, first_night DESC, id").Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// MovementAllocations returns the active vessel allocations (not blocks) of a marina that start on night
// (startsOn true) or whose last night is night (startsOn false), optionally on one map, with their contract.
func MovementAllocations(ctx context.Context, db bun.IDB, s MarinaScope, night calendar.Date, startsOn bool, mapID string) ([]ContractAllocation, error) {
	out := []ContractAllocation{}
	q := db.NewSelect().Model(&out).ModelTableExpr("allocations AS allocation").ColumnExpr("allocation.*").ColumnExpr("cl.contract_id").
		Join("LEFT JOIN contract_lines AS cl ON cl.id = allocation.contract_line_id AND cl.marina_id = allocation.marina_id").
		Where("allocation.marina_id = ?", s.ID()).Where("allocation.status = ?", AllocActive).
		Where("allocation.vessel_id IS NOT NULL").Where("allocation.contract_line_id IS NOT NULL")
	if startsOn {
		q = q.Where("allocation.first_night = ?", night)
	} else {
		q = q.Where("allocation.last_night = ?", night)
	}
	if mapID != "" {
		q = q.Where("allocation.map_id = ?", mapID)
	}
	err := q.OrderExpr("allocation.resource_id, allocation.id").Scan(ctx)
	return out, err
}

// VesselAllocationsCovering returns the active vessel allocations of the given vessels covering a night, with
// their contract.
func VesselAllocationsCovering(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string, night calendar.Date) ([]ContractAllocation, error) {
	out := []ContractAllocation{}
	vesselIDs = validIDs(vesselIDs)
	if len(vesselIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).ModelTableExpr("allocations AS allocation").ColumnExpr("allocation.*").ColumnExpr("cl.contract_id").
		Join("LEFT JOIN contract_lines AS cl ON cl.id = allocation.contract_line_id AND cl.marina_id = allocation.marina_id").
		Where("allocation.marina_id = ?", s.ID()).Where("allocation.status = ?", AllocActive).
		Where("allocation.vessel_id IN (?)", bun.List(vesselIDs)).
		Where("allocation.first_night <= ?", night).Where("allocation.last_night >= ?", night).
		OrderExpr("allocation.vessel_id, allocation.id").Scan(ctx)
	return out, err
}
