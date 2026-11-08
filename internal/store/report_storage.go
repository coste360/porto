package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read-only queries of the storage report (m9-plan §11, owner PLANNER, INV-61). The allocations, the cell rows and
// the areas come from the M7 queries (ReportAllocations, ReportCellNights, ReportResources).

// ReportStorageExitRow is the planned launch date of a storage contract (storage_contract_details.launch_on).
type ReportStorageExitRow struct {
	ContractID string        `bun:"contract_id"`
	HaulOutOn  calendar.Date `bun:"haul_out_on,nullzero"`
	LaunchOn   calendar.Date `bun:"launch_on,nullzero"`
}

// ReportStorageExits returns the planned haul-out and launch dates of the given storage contracts.
func ReportStorageExits(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string) ([]ReportStorageExitRow, error) {
	out := []ReportStorageExitRow{}
	if len(contractIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("storage_contract_details").
		ColumnExpr("contract_id, haul_out_on, launch_on").
		Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(contractIDs)).
		OrderExpr("contract_id").Scan(ctx, &out)
	return out, err
}

// ReportCellObstacleRow is a cell held by a block or maintenance allocation of an area over a night range.
type ReportCellObstacleRow struct {
	AreaID       string        `bun:"area_id"`
	AllocationID string        `bun:"allocation_id"`
	Cell         int           `bun:"cell"`
	First        calendar.Date `bun:"first_night"`
	Last         calendar.Date `bun:"last_night"`
}

// ReportCellObstacles returns the cells of block and maintenance rows of the areas in [from, to], one row per
// (allocation, cell) with its night range in the period.
func ReportCellObstacles(ctx context.Context, db bun.IDB, s MarinaScope, areaIDs []string, from, to calendar.Date) ([]ReportCellObstacleRow, error) {
	out := []ReportCellObstacleRow{}
	if len(areaIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("cell_occupancies").
		ColumnExpr("area_id, allocation_id, cell, MIN(night) AS first_night, MAX(night) AS last_night").
		Where("marina_id = ?", s.ID()).Where("area_id IN (?)", bun.List(areaIDs)).
		Where("kind IN (?)", bun.List([]string{"block", "maintenance"})).
		Where("night >= ?", from).Where("night <= ?", to).
		GroupExpr("area_id, allocation_id, cell").OrderExpr("area_id, allocation_id, cell").Scan(ctx, &out)
	return out, err
}
