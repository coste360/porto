package store

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of maintenance plans, tasks and task files (M9, m9-plan §8). Owned by UTILITIES. Tasks from plans are
// unique per (plan, element, due date) through maintenance_tasks_plan_due_uidx; field reports per (marina,
// client_ref) through maintenance_tasks_client_ref_uidx (INV-83).

// TaskFinalStatuses are the statuses a task never leaves.
var TaskFinalStatuses = []string{TaskDone, TaskCancelled}

// TaskLiveStatuses are the statuses of a task still to do.
var TaskLiveStatuses = []string{TaskOpen, TaskScheduled, TaskInProgress}

func emptyJSONArray(j JSON) JSON {
	if len(j) == 0 {
		return JSON("[]")
	}
	return j
}

// ---- plans ----

// InsertMaintenancePlan inserts a plan of the marina.
func InsertMaintenancePlan(ctx context.Context, tx bun.IDB, s MarinaScope, p *MaintenancePlan) error {
	p.MarinaID = s.ID()
	if p.Version == 0 {
		p.Version = 1
	}
	p.Checklist = emptyJSONArray(p.Checklist)
	p.CreatedAt, p.UpdatedAt = UTC(p.CreatedAt), UTC(p.UpdatedAt)
	_, err := tx.NewInsert().Model(p).Exec(ctx)
	return err
}

// GetMaintenancePlan returns a plan of the marina.
func GetMaintenancePlan(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*MaintenancePlan, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	p := new(MaintenancePlan)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// UpdateMaintenancePlan writes the mutable columns when the version equals expected and bumps it.
func UpdateMaintenancePlan(ctx context.Context, tx bun.IDB, s MarinaScope, p *MaintenancePlan, expected int, now time.Time) error {
	p.UpdatedAt = UTC(now)
	p.Checklist = emptyJSONArray(p.Checklist)
	res, err := tx.NewUpdate().Model(p).
		Column("name", "intervention", "intervention_label", "period_kind", "period_every", "period_month_day",
			"period_meter_id", "period_starts_on", "lead_days", "assignee_user_id", "supplier_name", "checklist",
			"requires_free", "nights", "active", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", p.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetMaintenancePlan(ctx, tx, s, p.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	p.Version = expected + 1
	return nil
}

// MaintenancePlanFilter filters the plans.
type MaintenancePlanFilter struct {
	Active      *bool
	ElementType string // plans with at least one target of this type
}

// ListMaintenancePlans returns a page of plans ordered by id.
func ListMaintenancePlans(ctx context.Context, db bun.IDB, s MarinaScope, f MaintenancePlanFilter, cursor string, limit int) (Page[MaintenancePlan], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[MaintenancePlan]{}, err
	}
	limit = ClampLimit(limit)
	var rows []MaintenancePlan
	q := db.NewSelect().Model(&rows).Where("maintenance_plan.marina_id = ?", s.ID())
	if f.Active != nil {
		q = q.Where("maintenance_plan.active = ?", *f.Active)
	}
	if f.ElementType != "" {
		q = q.Where("EXISTS (SELECT 1 FROM maintenance_plan_targets AS t WHERE t.plan_id = maintenance_plan.id AND t.element_type = ?)", f.ElementType)
	}
	if after != "" {
		q = q.Where("maintenance_plan.id > ?", after)
	}
	if err := q.OrderExpr("maintenance_plan.id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[MaintenancePlan]{}, err
	}
	return pageOf(rows, limit, func(p MaintenancePlan) string { return p.ID }), nil
}

// ActiveMaintenancePlans returns the active plans of the marina.
func ActiveMaintenancePlans(ctx context.Context, db bun.IDB, s MarinaScope) ([]MaintenancePlan, error) {
	var out []MaintenancePlan
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("id").Scan(ctx)
	return out, err
}

// ReplaceMaintenancePlanTargets replaces the targets of a plan.
func ReplaceMaintenancePlanTargets(ctx context.Context, tx bun.IDB, s MarinaScope, planID string, targets []MaintenancePlanTarget) error {
	if _, err := tx.NewDelete().Model((*MaintenancePlanTarget)(nil)).Where("marina_id = ?", s.ID()).Where("plan_id = ?", planID).Exec(ctx); err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	for i := range targets {
		targets[i].PlanID, targets[i].MarinaID = planID, s.ID()
	}
	_, err := tx.NewInsert().Model(&targets).Exec(ctx)
	return err
}

// MaintenancePlanTargets returns the targets of the given plans, ordered by plan, type and element.
func MaintenancePlanTargets(ctx context.Context, db bun.IDB, s MarinaScope, planIDs []string) ([]MaintenancePlanTarget, error) {
	out := []MaintenancePlanTarget{}
	if len(planIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("plan_id IN (?)", bun.List(planIDs)).
		OrderExpr("plan_id, element_type, element_id").Scan(ctx)
	return out, err
}

// ---- tasks ----

func normaliseTask(t *MaintenanceTask) {
	t.Checklist = emptyJSONArray(t.Checklist)
	t.ChecklistResults = emptyJSONArray(t.ChecklistResults)
	t.AllocationIDs = emptyJSONArray(t.AllocationIDs)
	t.DoneAt = UTC(t.DoneAt)
	t.CreatedAt, t.UpdatedAt = UTC(t.CreatedAt), UTC(t.UpdatedAt)
}

// InsertMaintenanceTask inserts a task of the marina.
func InsertMaintenanceTask(ctx context.Context, tx bun.IDB, s MarinaScope, t *MaintenanceTask) error {
	t.MarinaID = s.ID()
	if t.Version == 0 {
		t.Version = 1
	}
	normaliseTask(t)
	_, err := tx.NewInsert().Model(t).Exec(ctx)
	return err
}

// InsertMaintenanceTaskIfAbsent inserts a task unless a unique key holds it already (the plan task of the same
// due date, or the report of the same client_ref): it reports whether the row was inserted.
func InsertMaintenanceTaskIfAbsent(ctx context.Context, tx bun.IDB, s MarinaScope, t *MaintenanceTask) (bool, error) {
	t.MarinaID = s.ID()
	if t.Version == 0 {
		t.Version = 1
	}
	normaliseTask(t)
	res, err := tx.NewInsert().Model(t).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// GetMaintenanceTask returns a task of the marina.
func GetMaintenanceTask(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*MaintenanceTask, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(MaintenanceTask)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// LockMaintenanceTask reads a task FOR UPDATE on PostgreSQL (SQLite write transactions are serialised).
func LockMaintenanceTask(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*MaintenanceTask, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	t := new(MaintenanceTask)
	q := tx.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// MaintenanceTaskByClientRef returns the task of a field report by its client_ref.
func MaintenanceTaskByClientRef(ctx context.Context, db bun.IDB, s MarinaScope, clientRef string) (*MaintenanceTask, error) {
	t := new(MaintenanceTask)
	if err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("client_ref = ?", clientRef).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// UpdateMaintenanceTask writes the mutable columns when the version equals expected and bumps it.
func UpdateMaintenanceTask(ctx context.Context, tx bun.IDB, s MarinaScope, t *MaintenanceTask, expected int, now time.Time) error {
	t.UpdatedAt = now
	normaliseTask(t)
	res, err := tx.NewUpdate().Model(t).
		Column("title", "description", "priority", "status", "due_on", "scheduled_from", "scheduled_to", "out_of_service",
			"checklist_results", "outcome", "outcome_notes", "outcome_client_ref", "internal_minutes", "external_cost_cents",
			"supplier_ref", "assignee_user_id", "allocation_ids", "done_at", "done_by_user_id", "cancel_reason", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", t.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetMaintenanceTask(ctx, tx, s, t.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	t.Version = expected + 1
	return nil
}

// MaintenanceTaskFilter filters the task list.
type MaintenanceTaskFilter struct {
	Statuses       []string
	Priority       string
	ElementType    string
	ElementID      string
	PlanID         string
	AssigneeUserID string
	// OverdueBefore lists only live tasks due before this date (overdue on that day).
	OverdueBefore calendar.Date
}

const taskCursorPrefix = "t1:"

// farDue sorts tasks without a due date last.
const farDue = "9999-12-31"

func encodeTaskCursor(t *MaintenanceTask) string {
	due := farDue
	if !t.DueOn.IsZero() {
		due = t.DueOn.String()
	}
	return base64.RawURLEncoding.EncodeToString([]byte(taskCursorPrefix + due + "|" + t.ID))
}

func decodeTaskCursor(c string) (string, string, error) {
	if c == "" {
		return "", "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", ErrInvalidCursor
	}
	rest, ok := strings.CutPrefix(string(b), taskCursorPrefix)
	if !ok {
		return "", "", ErrInvalidCursor
	}
	due, id, ok := strings.Cut(rest, "|")
	if !ok || !ValidID(id) {
		return "", "", ErrInvalidCursor
	}
	if _, err := calendar.Parse(due); err != nil {
		return "", "", ErrInvalidCursor
	}
	return due, id, nil
}

// ListMaintenanceTasks returns a page of tasks ordered by due date (earliest, hence overdue, first; no date last)
// then id.
func ListMaintenanceTasks(ctx context.Context, db bun.IDB, s MarinaScope, f MaintenanceTaskFilter, cursor string, limit int) (Page[MaintenanceTask], error) {
	afterDue, afterID, err := decodeTaskCursor(cursor)
	if err != nil {
		return Page[MaintenanceTask]{}, err
	}
	limit = ClampLimit(limit)
	var rows []MaintenanceTask
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if len(f.Statuses) > 0 {
		q = q.Where("status IN (?)", bun.List(f.Statuses))
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", f.Priority)
	}
	if f.ElementType != "" {
		q = q.Where("element_type = ?", f.ElementType)
	}
	if f.ElementID != "" {
		if !ValidID(f.ElementID) {
			return Page[MaintenanceTask]{Items: []MaintenanceTask{}}, nil
		}
		q = q.Where("element_id = ?", f.ElementID)
	}
	if f.PlanID != "" {
		if !ValidID(f.PlanID) {
			return Page[MaintenanceTask]{Items: []MaintenanceTask{}}, nil
		}
		q = q.Where("plan_id = ?", f.PlanID)
	}
	if f.AssigneeUserID != "" {
		if !ValidID(f.AssigneeUserID) {
			return Page[MaintenanceTask]{Items: []MaintenanceTask{}}, nil
		}
		q = q.Where("assignee_user_id = ?", f.AssigneeUserID)
	}
	if !f.OverdueBefore.IsZero() {
		q = q.Where("status IN (?)", bun.List(TaskLiveStatuses)).Where("due_on IS NOT NULL").Where("due_on < ?", f.OverdueBefore)
	}
	key := "COALESCE(due_on, '" + farDue + "')"
	if afterID != "" {
		q = q.Where("("+key+" > ? OR ("+key+" = ? AND id > ?))", afterDue, afterDue, afterID)
	}
	if err := q.OrderExpr(key + ", id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[MaintenanceTask]{}, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		return Page[MaintenanceTask]{Items: rows, NextCursor: encodeTaskCursor(&rows[len(rows)-1])}, nil
	}
	if rows == nil {
		rows = []MaintenanceTask{}
	}
	return Page[MaintenanceTask]{Items: rows}, nil
}

// LiveTaskOfTarget reports whether a task of the plan on the element is still to do.
func LiveTaskOfTarget(ctx context.Context, db bun.IDB, s MarinaScope, planID, elementType, elementID string) (bool, error) {
	n, err := db.NewSelect().Model((*MaintenanceTask)(nil)).Where("marina_id = ?", s.ID()).Where("plan_id = ?", planID).
		Where("element_type = ?", elementType).Where("element_id = ?", elementID).
		Where("status IN (?)", bun.List(TaskLiveStatuses)).Count(ctx)
	return n > 0, err
}

// LastDoneTaskOfTarget returns the latest done task of the plan on the element (ErrNotFound when none).
func LastDoneTaskOfTarget(ctx context.Context, db bun.IDB, s MarinaScope, planID, elementType, elementID string) (*MaintenanceTask, error) {
	t := new(MaintenanceTask)
	err := db.NewSelect().Model(t).Where("marina_id = ?", s.ID()).Where("plan_id = ?", planID).
		Where("element_type = ?", elementType).Where("element_id = ?", elementID).Where("status = ?", TaskDone).
		OrderExpr("done_at DESC, id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return t, nil
}

// MaintenanceWorklist returns the live tasks assigned to userID or due on or before dueBy, ordered by due date.
func MaintenanceWorklist(ctx context.Context, db bun.IDB, s MarinaScope, userID string, dueBy calendar.Date) ([]MaintenanceTask, error) {
	out := []MaintenanceTask{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status IN (?)", bun.List(TaskLiveStatuses))
	if ValidID(userID) {
		q = q.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("assignee_user_id = ?", userID).WhereOr("(due_on IS NOT NULL AND due_on <= ?)", dueBy)
		})
	} else {
		q = q.Where("due_on IS NOT NULL").Where("due_on <= ?", dueBy)
	}
	err := q.OrderExpr("COALESCE(due_on, '" + farDue + "'), id").Limit(int64(MaxLimit)).Scan(ctx)
	return out, err
}

// LiveTasksWithElement returns the live tasks of the marina that target an element.
func LiveTasksWithElement(ctx context.Context, db bun.IDB, s MarinaScope) ([]MaintenanceTask, error) {
	out := []MaintenanceTask{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status IN (?)", bun.List(TaskLiveStatuses)).
		Where("element_id IS NOT NULL").OrderExpr("id").Scan(ctx)
	return out, err
}

// ---- task files ----

// InsertMaintenanceTaskFile links a stored file to a task.
func InsertMaintenanceTaskFile(ctx context.Context, tx bun.IDB, s MarinaScope, f *MaintenanceTaskFile) error {
	f.MarinaID = s.ID()
	f.CreatedAt = UTC(f.CreatedAt)
	_, err := tx.NewInsert().Model(f).Exec(ctx)
	return err
}

// MaintenanceTaskFileWithFile is a task file with its stored file metadata.
type MaintenanceTaskFileWithFile struct {
	ID              string    `bun:"id"`
	MarinaID        string    `bun:"marina_id"`
	TaskID          string    `bun:"task_id"`
	FileID          string    `bun:"file_id"`
	Kind            string    `bun:"kind"`
	CreatedByUserID string    `bun:"created_by_user_id"`
	CreatedAt       time.Time `bun:"created_at"`
	ContentType     string    `bun:"content_type"`
	SHA256          string    `bun:"sha256"`
	StorageKey      string    `bun:"storage_key"`
}

// MaintenanceTaskFiles returns the files of the given tasks with their content type, oldest first.
func MaintenanceTaskFiles(ctx context.Context, db bun.IDB, s MarinaScope, taskIDs []string) ([]MaintenanceTaskFileWithFile, error) {
	out := []MaintenanceTaskFileWithFile{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("maintenance_task_files AS mtf").
		ColumnExpr("CAST(mtf.id AS TEXT) AS id, CAST(mtf.marina_id AS TEXT) AS marina_id, CAST(mtf.task_id AS TEXT) AS task_id").
		ColumnExpr("CAST(mtf.file_id AS TEXT) AS file_id, mtf.kind, COALESCE(CAST(mtf.created_by_user_id AS TEXT), '') AS created_by_user_id, mtf.created_at").
		ColumnExpr("sf.content_type, sf.sha256, sf.storage_key").
		Join("JOIN stored_files AS sf ON sf.id = mtf.file_id AND sf.marina_id = mtf.marina_id").
		Where("mtf.marina_id = ?", s.ID()).Where("mtf.task_id IN (?)", bun.List(taskIDs)).
		OrderExpr("mtf.created_at, mtf.id").Scan(ctx, &out)
	return out, err
}

// ---- elements ----

// ElementInfo is the display code and map of a maintenance element.
type ElementInfo struct {
	Type, ID, Code, MapID string
	StructureID           string // berths and dynamic areas: their structure
}

var elementTables = map[string]string{
	ElementBerth: "berths", ElementStructure: "harbor_structures", ElementDynamicArea: "dynamic_areas",
	ElementLandResource: "land_resources", ElementPedestal: "pedestals", ElementEquipment: "equipment",
	ElementSensor: "sensor_devices",
}

// ElementInfos returns the elements of the marina among refs (type → ids), keyed by type + ":" + id. Unknown or
// foreign ids are absent.
func ElementInfos(ctx context.Context, db bun.IDB, s MarinaScope, refs map[string][]string) (map[string]ElementInfo, error) {
	out := map[string]ElementInfo{}
	for typ, ids := range refs {
		table := elementTables[typ]
		var valid []string
		for _, id := range ids {
			if ValidID(id) {
				valid = append(valid, id)
			}
		}
		if table == "" || len(valid) == 0 {
			continue
		}
		var rows []struct {
			ID          string `bun:"id"`
			Code        string `bun:"code"`
			MapID       string `bun:"map_id"`
			StructureID string `bun:"structure_id"`
		}
		q := db.NewSelect().TableExpr(table).ColumnExpr("CAST(id AS TEXT) AS id, code").
			ColumnExpr("COALESCE(CAST(map_id AS TEXT), '') AS map_id").
			Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(valid))
		if typ == ElementBerth || typ == ElementDynamicArea {
			q = q.ColumnExpr("COALESCE(CAST(structure_id AS TEXT), '') AS structure_id")
		} else {
			q = q.ColumnExpr("'' AS structure_id")
		}
		if err := q.Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[typ+":"+r.ID] = ElementInfo{Type: typ, ID: r.ID, Code: r.Code, MapID: r.MapID, StructureID: r.StructureID}
		}
	}
	return out, nil
}

// BerthsOfStructure returns the ids of the berths of a structure.
func BerthsOfStructure(ctx context.Context, db bun.IDB, s MarinaScope, structureID string) ([]string, error) {
	var out []string
	if !ValidID(structureID) {
		return out, nil
	}
	err := db.NewSelect().TableExpr("berths").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", s.ID()).
		Where("structure_id = ?", structureID).OrderExpr("id").Scan(ctx, &out)
	return out, err
}

// MeterValueAt returns the value of the latest non-void reading of a meter at or before at (ok false when none).
func MeterValueAt(ctx context.Context, db bun.IDB, s MarinaScope, meterID string, at time.Time) (int64, bool, error) {
	var rows []MeterReading
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("meter_id = ?", meterID).
		Where("status <> ?", ReadingVoid)
	if !at.IsZero() {
		q = q.Where("read_at <= ?", UTC(at))
	}
	if err := q.OrderExpr("read_at DESC").Limit(1).Scan(ctx); err != nil {
		return 0, false, err
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	return rows[0].ValueMilli, true, nil
}
