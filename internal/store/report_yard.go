package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Report queries of the yard (R-yard, M10, m10-plan §12, INV-61): the report reads the yard tables only through
// these functions, in its read transaction. Owned by ORDERS.

// ReportYardEntryRow is an approved entry with employee, order, task qualification and work type.
type ReportYardEntryRow struct {
	ID              string        `bun:"id"`
	WorkDate        calendar.Date `bun:"work_date"`
	EmployeeID      string        `bun:"employee_id"`
	EmployeeCode    string        `bun:"employee_code"`
	FirstName       string        `bun:"first_name"`
	LastName        string        `bun:"last_name"`
	Department      string        `bun:"department"`
	WorkOrderID     string        `bun:"work_order_id"`
	OrderYear       int           `bun:"order_year"`
	OrderNumber     int64         `bun:"order_number"`
	WorkTypeID      string        `bun:"work_type_id"`
	WorkTypeCode    string        `bun:"work_type_code"`
	QualificationID string        `bun:"qualification_id"`
	HoursMilli      int64         `bun:"hours_milli"`
	CostCents       int64         `bun:"cost_cents"`
}

// ReportYardEntries returns the approved entries with work date in [from, to].
func ReportYardEntries(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportYardEntryRow, error) {
	out := []ReportYardEntryRow{}
	err := db.NewSelect().TableExpr("timesheets AS e").
		ColumnExpr("e.id, e.work_date, e.employee_id, em.code AS employee_code, em.first_name, em.last_name, em.department").
		ColumnExpr("e.work_order_id, o.year AS order_year, o.number AS order_number").
		ColumnExpr("COALESCE(CAST(COALESCE(t.work_type_id, o.work_type_id) AS TEXT), '') AS work_type_id, COALESCE(wt.code, '') AS work_type_code").
		ColumnExpr("COALESCE(CAST(t.qualification_id AS TEXT), '') AS qualification_id, e.hours_milli, COALESCE(e.cost_cents, 0) AS cost_cents").
		Join("JOIN employees AS em ON em.id = e.employee_id AND em.marina_id = e.marina_id").
		Join("JOIN work_orders AS o ON o.id = e.work_order_id AND o.marina_id = e.marina_id").
		Join("LEFT JOIN work_tasks AS t ON t.id = e.task_id AND t.marina_id = e.marina_id").
		Join("LEFT JOIN work_types AS wt ON wt.id = COALESCE(t.work_type_id, o.work_type_id) AND wt.marina_id = e.marina_id").
		Where("e.marina_id = ?", s.ID()).Where("e.status = ?", TimesheetApproved).
		Where("e.work_date >= ?", from).Where("e.work_date <= ?", to).OrderExpr("e.work_date, e.id").Scan(ctx, &out)
	return out, err
}

// ReportYardStaff is the employees with calendars, absences overlapping the period and qualifications.
type ReportYardStaff struct {
	Employees      []Employee
	Calendars      []EmployeeCalendar
	Absences       []EmployeeAbsence
	Qualifications []EmployeeQualification
}

// ReportYardStaffOf returns the active employees of the marina with their calendars, qualifications and the
// absences overlapping [from, to].
func ReportYardStaffOf(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) (*ReportYardStaff, error) {
	out := &ReportYardStaff{}
	if err := db.NewSelect().Model(&out.Employees).Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("code").Scan(ctx); err != nil {
		return nil, err
	}
	if err := db.NewSelect().Model(&out.Calendars).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, err
	}
	if err := db.NewSelect().Model(&out.Absences).Where("marina_id = ?", s.ID()).Where("from_on <= ?", to).Where("to_on >= ?", from).Scan(ctx); err != nil {
		return nil, err
	}
	if err := db.NewSelect().Model(&out.Qualifications).Where("marina_id = ?", s.ID()).OrderExpr("employee_id, qualification_id").Scan(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// ReportYardDayHours are hours of an employee (or a bay) on a day.
type ReportYardDayHours struct {
	Key        string        `bun:"key"`
	WorkDate   calendar.Date `bun:"work_date"`
	HoursMilli int64         `bun:"hours_milli"`
}

// ReportYardAssigned returns Σ assigned hours per employee and day in [from, to].
func ReportYardAssigned(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportYardDayHours, error) {
	out := []ReportYardDayHours{}
	err := db.NewSelect().TableExpr("task_assignments").ColumnExpr("employee_id AS key, work_date, SUM(hours_milli) AS hours_milli").
		Where("marina_id = ?", s.ID()).Where("work_date >= ?", from).Where("work_date <= ?", to).
		GroupExpr("employee_id, work_date").Scan(ctx, &out)
	return out, err
}

// ReportYardActual returns Σ approved hours per employee and day in [from, to].
func ReportYardActual(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportYardDayHours, error) {
	out := []ReportYardDayHours{}
	err := db.NewSelect().TableExpr("timesheets").ColumnExpr("employee_id AS key, work_date, SUM(hours_milli) AS hours_milli").
		Where("marina_id = ?", s.ID()).Where("status = ?", TimesheetApproved).Where("work_date >= ?", from).Where("work_date <= ?", to).
		GroupExpr("employee_id, work_date").Scan(ctx, &out)
	return out, err
}

// ReportYardBayAssigned returns Σ assigned hours per day of the tasks whose order sits at a resource (key = the
// resource code, "" without a position).
func ReportYardBayAssigned(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportYardDayHours, error) {
	out := []ReportYardDayHours{}
	err := db.NewSelect().TableExpr("task_assignments AS a").
		ColumnExpr("COALESCE(b.code, da.code, lr.code, '') AS key, a.work_date, SUM(a.hours_milli) AS hours_milli").
		Join("JOIN work_tasks AS t ON t.id = a.task_id AND t.marina_id = a.marina_id").
		Join("JOIN work_orders AS o ON o.id = t.work_order_id AND o.marina_id = t.marina_id").
		Join("LEFT JOIN allocations AS al ON al.id = o.allocation_id AND al.marina_id = o.marina_id").
		Join("LEFT JOIN berths AS b ON al.resource_type = 'berth' AND b.id = al.resource_id").
		Join("LEFT JOIN dynamic_areas AS da ON al.resource_type = 'dynamic_area' AND da.id = al.resource_id").
		Join("LEFT JOIN land_resources AS lr ON al.resource_type = 'land_resource' AND lr.id = al.resource_id").
		Where("a.marina_id = ?", s.ID()).Where("a.work_date >= ?", from).Where("a.work_date <= ?", to).
		GroupExpr("COALESCE(b.code, da.code, lr.code, ''), a.work_date").Scan(ctx, &out)
	return out, err
}

// ReportYardOrderData is everything the margins views need of a set of orders (the account inputs).
type ReportYardOrderData struct {
	Orders        []WorkOrder
	Lines         []WorkOrderLine
	Tasks         []WorkTask
	Entries       []ApprovedEntryRow
	Movements     []StockMovement
	External      []WorkOrderExternalCost
	Items         []Item
	ItemCosts     map[string]int64
	EmployeeQuals []EmployeeQualification
	WorkTypes     []WorkType
	Settings      YardSettings
	HasSettings   bool
}

// ReportYardOrders returns the non-cancelled orders that are open, invoiced in [from, to], or have an entry, a
// movement or an external cost in [from, to], with their account inputs. Active marks the orders with activity.
func ReportYardOrders(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date, fromInstant, toInstant time.Time) (*ReportYardOrderData, map[string]bool, error) {
	out := &ReportYardOrderData{ItemCosts: map[string]int64{}}
	active := map[string]bool{}
	var act []string
	if err := db.NewRaw(`SELECT work_order_id FROM timesheets WHERE marina_id = ? AND status = ? AND work_date >= ? AND work_date <= ?
		UNION SELECT work_order_id FROM stock_movements WHERE marina_id = ? AND moved_on >= ? AND moved_on <= ?
		UNION SELECT work_order_id FROM work_order_external_costs WHERE marina_id = ? AND incurred_on >= ? AND incurred_on <= ?`,
		s.ID(), TimesheetApproved, from, to, s.ID(), from, to, s.ID(), from, to).Scan(ctx, &act); err != nil {
		return nil, nil, err
	}
	for _, id := range act {
		active[id] = true
	}
	q := db.NewSelect().Model(&out.Orders).Where("marina_id = ?", s.ID()).Where("status <> ?", WorkOrderCancelled).
		WhereGroup(" AND ", func(g *bun.SelectQuery) *bun.SelectQuery {
			g = g.Where("status IN (?)", bun.List(WorkOrderOpenStatuses)).
				WhereOr("(invoiced_at >= ? AND invoiced_at < ?)", UTC(fromInstant), UTC(toInstant))
			if len(act) > 0 {
				g = g.WhereOr("id IN (?)", bun.List(act))
			}
			return g
		})
	if err := q.OrderExpr("year, number").Scan(ctx); err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(out.Orders))
	for _, o := range out.Orders {
		ids = append(ids, o.ID)
	}
	var err error
	if out.Lines, err = WorkOrderLines(ctx, db, s, ids); err != nil {
		return nil, nil, err
	}
	if out.Tasks, err = WorkTasksOfOrders(ctx, db, s, ids); err != nil {
		return nil, nil, err
	}
	if out.Entries, err = ApprovedEntriesOfOrders(ctx, db, s, ids); err != nil {
		return nil, nil, err
	}
	if out.Movements, err = StockMovementsOfOrders(ctx, db, s, ids); err != nil {
		return nil, nil, err
	}
	if out.External, err = ExternalCostsOfOrders(ctx, db, s, ids); err != nil {
		return nil, nil, err
	}
	if err := db.NewSelect().Model(&out.Items).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, nil, err
	}
	var costs []YardItemSettings
	if err := db.NewSelect().Model(&costs).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, nil, err
	}
	for _, c := range costs {
		out.ItemCosts[c.ItemID] = c.CostCents
	}
	if err := db.NewSelect().Model(&out.EmployeeQuals).Where("marina_id = ?", s.ID()).OrderExpr("employee_id, qualification_id").Scan(ctx); err != nil {
		return nil, nil, err
	}
	if err := db.NewSelect().Model(&out.WorkTypes).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, nil, err
	}
	if out.Settings, out.HasSettings, err = WorkOrderYardSettings(ctx, db, s); err != nil {
		return nil, nil, err
	}
	return out, active, nil
}

// ReportYardBookingRow is a lifting booking with its equipment code.
type ReportYardBookingRow struct {
	ID            string        `bun:"id"`
	EquipmentID   string        `bun:"equipment_id"`
	EquipmentCode string        `bun:"equipment_code"`
	Date          calendar.Date `bun:"date"`
	FromSlot      int           `bun:"from_slot"`
	ToSlot        int           `bun:"to_slot"`
	Purpose       string        `bun:"purpose"`
	Status        string        `bun:"status"`
	WorkOrderID   string        `bun:"work_order_id"`
}

// ReportYardBookings returns the booked and done lifting bookings with date in [from, to] and the bookable
// equipment ids.
func ReportYardBookings(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]ReportYardBookingRow, []string, error) {
	out := []ReportYardBookingRow{}
	err := db.NewSelect().TableExpr("equipment_bookings AS b").
		ColumnExpr("b.id, b.equipment_id, eq.code AS equipment_code, b.date, b.from_slot, b.to_slot, b.purpose, b.status").
		ColumnExpr("COALESCE(CAST(b.work_order_id AS TEXT), '') AS work_order_id").
		Join("JOIN equipment AS eq ON eq.id = b.equipment_id AND eq.marina_id = b.marina_id").
		Where("b.marina_id = ?", s.ID()).Where("b.status IN (?)", bun.List([]string{"booked", "done"})).
		Where("b.date >= ?", from).Where("b.date <= ?", to).OrderExpr("b.date, eq.code, b.from_slot").Scan(ctx, &out)
	if err != nil {
		return nil, nil, err
	}
	var eq []string
	if err := db.NewSelect().TableExpr("equipment").Column("id").Where("marina_id = ?", s.ID()).Where("bookable = ?", true).
		Where("active = ?", true).OrderExpr("id").Scan(ctx, &eq); err != nil {
		return nil, nil, err
	}
	return out, eq, nil
}
