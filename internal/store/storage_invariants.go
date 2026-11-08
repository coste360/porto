package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// CheckStorageInvariants runs the M9 storage invariant query on a marina and returns every violation (none on a
// coherent database). Used by tests and the acceptance scenarios.
//
//	Q20 (INV-16) every storage contract has its storage_contract_details row, and every active storage contract
//	    has active allocations on a surface area or a trailer spot covering each night of its period.
func CheckStorageInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	var missing []string
	if err := db.NewSelect().TableExpr("contracts AS c").Column("c.id").Where("c.marina_id = ?", marinaID).
		Where("c.kind = ?", ContractKindStorage).
		Where("NOT EXISTS (SELECT 1 FROM storage_contract_details AS d WHERE d.contract_id = c.id AND d.marina_id = c.marina_id)").
		OrderExpr("c.id").Scan(ctx, &missing); err != nil {
		return nil, fmt.Errorf("Q20: %w", err)
	}
	for _, id := range missing {
		out = append(out, Violation{Query: "Q20", IDs: []string{id}, Detail: "storage contract without details"})
	}
	var rows []struct {
		ID       string        `bun:"id"`
		StartsOn calendar.Date `bun:"starts_on"`
		EndsOn   calendar.Date `bun:"ends_on"`
		Nights   int64         `bun:"nights"`
	}
	// Nights covered by active vessel allocations of the contract on a surface area or a trailer spot.
	if err := db.NewSelect().TableExpr("contracts AS c").ColumnExpr("c.id, c.starts_on, c.ends_on").
		ColumnExpr(`COALESCE((SELECT SUM(`+nightsExpr(db)+`) FROM allocations AS a
			JOIN contract_lines AS l ON l.id = a.contract_line_id AND l.marina_id = a.marina_id
			LEFT JOIN dynamic_areas AS da ON a.resource_type = 'dynamic_area' AND da.id = a.resource_id
			LEFT JOIN land_resources AS lr ON a.resource_type = 'land_resource' AND lr.id = a.resource_id
			WHERE l.contract_id = c.id AND a.status = 'active'
			AND (da.kind = 'surface' OR lr.kind = 'trailer_spot')), 0) AS nights`).
		Where("c.marina_id = ?", marinaID).Where("c.kind = ?", ContractKindStorage).Where("c.status = 'active'").
		OrderExpr("c.id").Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("Q20: %w", err)
	}
	for _, r := range rows {
		if want := int64(r.StartsOn.DaysUntil(r.EndsOn)); r.Nights != want {
			out = append(out, Violation{Query: "Q20", IDs: []string{r.ID}, Detail: fmt.Sprintf("surface nights %d ≠ %d", r.Nights, want)})
		}
	}
	return out, nil
}

// nightsExpr is the number of nights of an allocation row a (last_night − first_night + 1) in the dialect.
func nightsExpr(db bun.IDB) string {
	if IsPostgres(db) {
		return "(a.last_night - a.first_night + 1)"
	}
	return "(CAST(julianday(a.last_night) - julianday(a.first_night) AS INTEGER) + 1)"
}
