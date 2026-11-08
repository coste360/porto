package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read-only query of the maintenance report (m9-plan §11, owner PLANNER, INV-61). Free text (title, description,
// notes) is never read.

// ReportMaintenanceTaskRow is a maintenance task with the display code of its element.
type ReportMaintenanceTaskRow struct {
	ID                string        `bun:"id"`
	ElementType       string        `bun:"element_type"`
	ElementID         string        `bun:"element_id"`
	ElementCode       string        `bun:"element_code"`
	Intervention      string        `bun:"intervention"`
	Source            string        `bun:"source"`
	Priority          string        `bun:"priority"`
	Status            string        `bun:"status"`
	DueOn             calendar.Date `bun:"due_on,nullzero"`
	DoneAt            time.Time     `bun:"done_at,nullzero"`
	CreatedAt         time.Time     `bun:"created_at"`
	UpdatedAt         time.Time     `bun:"updated_at"`
	InternalMinutes   *int64        `bun:"internal_minutes"`
	ExternalCostCents *int64        `bun:"external_cost_cents"`
}

// ReportMaintenanceTasks returns the tasks that may belong to [from, to]: due in the period, or created before
// toEnd and (pending, or closed at or after fromStart). The report applies the exact rule on local dates.
func ReportMaintenanceTasks(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date, fromStart, toEnd time.Time) ([]ReportMaintenanceTaskRow, error) {
	out := []ReportMaintenanceTaskRow{}
	err := db.NewSelect().TableExpr("maintenance_tasks AS t").
		ColumnExpr("t.id, COALESCE(t.element_type, '') AS element_type, t.element_id AS element_id").
		ColumnExpr(`COALESCE(CASE t.element_type
			WHEN 'berth' THEN (SELECT b.code FROM berths AS b WHERE b.id = t.element_id AND b.marina_id = t.marina_id)
			WHEN 'structure' THEN (SELECT h.code FROM harbor_structures AS h WHERE h.id = t.element_id AND h.marina_id = t.marina_id)
			WHEN 'dynamic_area' THEN (SELECT d.code FROM dynamic_areas AS d WHERE d.id = t.element_id AND d.marina_id = t.marina_id)
			WHEN 'land_resource' THEN (SELECT l.code FROM land_resources AS l WHERE l.id = t.element_id AND l.marina_id = t.marina_id)
			WHEN 'pedestal' THEN (SELECT p.code FROM pedestals AS p WHERE p.id = t.element_id AND p.marina_id = t.marina_id)
			WHEN 'equipment' THEN (SELECT e.code FROM equipment AS e WHERE e.id = t.element_id AND e.marina_id = t.marina_id)
			END, '') AS element_code`).
		ColumnExpr("COALESCE(t.intervention, '') AS intervention, t.source, t.priority, t.status, t.due_on").
		ColumnExpr("t.done_at, t.created_at, t.updated_at, t.internal_minutes, t.external_cost_cents").
		Where("t.marina_id = ?", s.ID()).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("(t.due_on >= ? AND t.due_on <= ?)", from, to).
				WhereOr("(t.created_at < ? AND (t.status IN (?) OR t.updated_at >= ? OR t.done_at >= ?))",
					toEnd, bun.List([]string{"open", "scheduled", "in_progress"}), fromStart, fromStart)
		}).
		OrderExpr("t.id").Scan(ctx, &out)
	return out, err
}
