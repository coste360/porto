package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of storage contracts and surface areas (m9-plan §3, §4). Every function is scoped by the marina.

// InsertStorageDetails inserts the details of a storage contract.
func InsertStorageDetails(ctx context.Context, tx bun.IDB, d *StorageContractDetails) error {
	d.CreatedAt, d.UpdatedAt = UTC(d.CreatedAt), UTC(d.UpdatedAt)
	_, err := tx.NewInsert().Model(d).Exec(ctx)
	return err
}

// GetStorageDetails returns the details of a storage contract.
func GetStorageDetails(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*StorageContractDetails, error) {
	if !ValidID(contractID) {
		return nil, ErrNotFound
	}
	d := new(StorageContractDetails)
	if err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// LockStorageDetails returns the details of a storage contract, locked for update on PostgreSQL.
func LockStorageDetails(ctx context.Context, tx bun.IDB, s MarinaScope, contractID string) (*StorageContractDetails, error) {
	if !ValidID(contractID) {
		return nil, ErrNotFound
	}
	d := new(StorageContractDetails)
	q := tx.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// UpdateStorageDetails writes the mutable columns when the version equals expected and bumps it
// (ErrVersionMismatch otherwise).
func UpdateStorageDetails(ctx context.Context, tx bun.IDB, s MarinaScope, d *StorageContractDetails, expected int, now time.Time) error {
	d.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(d).Column("support", "support_owner", "haul_out_on", "launch_on", "notes_handling", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("contract_id = ?", d.ContractID).
		Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	d.Version = expected + 1
	return nil
}

// StorageDetailsOf returns the details of the given storage contracts by contract id.
func StorageDetailsOf(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string) (map[string]StorageContractDetails, error) {
	out := map[string]StorageContractDetails{}
	ids := validIDs(contractIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []StorageContractDetails
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ContractID] = r
	}
	return out, nil
}

// LockDynamicArea returns a dynamic area locked for update on PostgreSQL (SQLite write transactions are already
// serialised): automatic storage placements on an area run one after the other.
func LockDynamicArea(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*DynamicArea, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	a := new(DynamicArea)
	q := tx.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// StorageArea is a surface dynamic area of a yard or shed map.
type StorageArea struct {
	DynamicArea `bun:",extend"`
	MapKind     string `bun:"map_kind"`
}

// StorageAreas returns the active surface areas of the yard and shed maps of the marina, ordered by code; with
// id set, only that area whatever its kind and map (so callers can tell a wrong target from a missing one).
func StorageAreas(ctx context.Context, db bun.IDB, s MarinaScope, id string) ([]StorageArea, error) {
	out := []StorageArea{}
	q := db.NewSelect().Model(&out).ColumnExpr("dynamic_area.*").ColumnExpr("m.kind AS map_kind").
		Join("JOIN maps AS m ON m.id = dynamic_area.map_id AND m.marina_id = dynamic_area.marina_id").
		Where("dynamic_area.marina_id = ?", s.ID())
	if id != "" {
		if !ValidID(id) {
			return out, nil
		}
		q = q.Where("dynamic_area.id = ?", id)
	} else {
		q = q.Where("dynamic_area.kind = ?", "surface").Where("m.kind IN ('yard','shed')").Where("dynamic_area.active = ?", true)
	}
	err := q.OrderExpr("dynamic_area.code, dynamic_area.id").Scan(ctx)
	return out, err
}

// AreaCellsOf returns the distinct cells each of the given allocations holds on a surface area (one query).
func AreaCellsOf(ctx context.Context, db bun.IDB, s MarinaScope, areaID string, allocIDs []string) (map[string][]int, error) {
	out := map[string][]int{}
	ids := validIDs(allocIDs)
	if len(ids) == 0 || !ValidID(areaID) {
		return out, nil
	}
	var rows []struct {
		AllocationID string `bun:"allocation_id"`
		Cell         int    `bun:"cell"`
	}
	err := db.NewSelect().Model((*CellOccupancy)(nil)).ColumnExpr("DISTINCT allocation_id, cell").Where("marina_id = ?", s.ID()).
		Where("area_id = ?", areaID).Where("allocation_id IN (?)", bun.List(ids)).OrderExpr("allocation_id, cell").Scan(ctx, &rows)
	for _, r := range rows {
		out[r.AllocationID] = append(out[r.AllocationID], r.Cell)
	}
	return out, err
}

// AreaRow is an occupied (cell, night) of a surface area with its allocation.
type AreaRow struct {
	Cell         int           `bun:"cell"`
	Night        calendar.Date `bun:"night"`
	AllocationID string        `bun:"allocation_id"`
}

// AreaRowsBetween returns the cell rows of a surface area for nights [from, to], except those of the excluded
// allocations (the scenario's own), ordered by cell and night.
func AreaRowsBetween(ctx context.Context, db bun.IDB, s MarinaScope, areaID string, from, to calendar.Date, exclude []string) ([]AreaRow, error) {
	out := []AreaRow{}
	q := db.NewSelect().Model((*CellOccupancy)(nil)).Column("cell", "night", "allocation_id").Where("marina_id = ?", s.ID()).
		Where("area_id = ?", areaID).Where("night >= ?", from).Where("night <= ?", to)
	if ex := validIDs(exclude); len(ex) > 0 {
		q = q.Where("allocation_id NOT IN (?)", bun.List(ex))
	}
	err := q.OrderExpr("cell, night").Scan(ctx, &out)
	return out, err
}

// CountStorageContracts counts the storage contracts of a marina.
func CountStorageContracts(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*Contract)(nil)).Where("marina_id = ?", s.ID()).Where("kind = ?", ContractKindStorage).Count(ctx))
}
