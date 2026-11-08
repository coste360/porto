package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read-only queries of the utilities report (m9-plan §11, owner PLANNER, INV-61).

// ReportConsumptionRow is a utility consumption with its meter and contract.
type ReportConsumptionRow struct {
	ID             string        `bun:"id"`
	MeterID        string        `bun:"meter_id"`
	MeterCode      string        `bun:"meter_code"`
	Kind           string        `bun:"kind"`
	MeterItemID    string        `bun:"meter_item_id"`
	ResourceType   string        `bun:"resource_type"`
	ResourceID     string        `bun:"resource_id"`
	ContractID     string        `bun:"contract_id"`
	ContractKind   string        `bun:"contract_kind"`
	ContractYear   int           `bun:"contract_year"`
	ContractNumber int64         `bun:"contract_number"`
	FirstNight     calendar.Date `bun:"first_night"`
	LastNight      calendar.Date `bun:"last_night"`
	QtyMilli       int64         `bun:"qty_milli"`
	Status         string        `bun:"status"`
	ChargeID       string        `bun:"charge_id"`
}

// ReportConsumptions returns the consumptions overlapping [from, to] with meter code, kind and tariff item and the
// contract number.
func ReportConsumptions(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportConsumptionRow, error) {
	out := []ReportConsumptionRow{}
	err := db.NewSelect().TableExpr("utility_consumptions AS u").
		ColumnExpr("u.id, u.meter_id, m.code AS meter_code, m.kind, m.item_id AS meter_item_id").
		ColumnExpr("COALESCE(u.resource_type, m.resource_type, '') AS resource_type").
		ColumnExpr("COALESCE(u.resource_id, m.resource_id) AS resource_id").
		ColumnExpr("u.contract_id AS contract_id, COALESCE(c.kind, '') AS contract_kind").
		ColumnExpr("COALESCE(c.year, 0) AS contract_year, COALESCE(c.number, 0) AS contract_number").
		ColumnExpr("u.first_night, u.last_night, u.qty_milli, u.status, u.charge_id AS charge_id").
		Join("JOIN utility_meters AS m ON m.id = u.meter_id AND m.marina_id = u.marina_id").
		Join("LEFT JOIN contracts AS c ON c.id = u.contract_id AND c.marina_id = u.marina_id").
		Where("u.marina_id = ?", s.ID()).Where("u.first_night <= ?", to).Where("u.last_night >= ?", from).
		OrderExpr("m.code, u.first_night, u.id").Scan(ctx, &out)
	return out, err
}

// ReportUtilityLineRow is a line of a utility charge (the report matches it to its consumption by charge, nights
// and quantity).
type ReportUtilityLineRow struct {
	ChargeID    string        `bun:"charge_id"`
	Seq         int64         `bun:"seq"`
	ServiceFrom calendar.Date `bun:"service_from,nullzero"`
	ServiceTo   calendar.Date `bun:"service_to,nullzero"`
	QtyMilli    int64         `bun:"qty_milli"`
	GrossCents  int64         `bun:"gross_cents"`
}

// ReportUtilityLines returns the lines of the given charges, by charge and seq.
func ReportUtilityLines(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) ([]ReportUtilityLineRow, error) {
	out := []ReportUtilityLineRow{}
	if len(chargeIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("charge_lines").
		ColumnExpr("charge_id, seq, service_from, service_to, qty_milli, gross_cents").
		Where("marina_id = ?", s.ID()).Where("charge_id IN (?)", bun.List(chargeIDs)).
		OrderExpr("charge_id, seq").Scan(ctx, &out)
	return out, err
}

// ReportUtilityItems returns the default tariff items of the marina (utility_settings power and water items;
// empty without a settings row).
func ReportUtilityItems(ctx context.Context, db bun.IDB, s MarinaScope) (power, water string, err error) {
	var rows []UtilitySettings
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", nil
	}
	return rows[0].PowerItemID, rows[0].WaterItemID, nil
}
