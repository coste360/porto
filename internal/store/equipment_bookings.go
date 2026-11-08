package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the lifting equipment bookings (M10, m10-plan §11). Owned by PLANNER. INV-17: the primary keys of
// equipment_slots (equipment_id, date, slot) and operator_slots (employee_id, date, slot) are the only guarantee
// against double booking (ADR 0003). InsertEquipmentSlots / InsertOperatorSlots write one row per slot in one
// statement: a unique violation aborts the caller's transaction, which rolls back and answers 409. Never check
// for free slots before inserting as a guarantee.

// BookingView is a booking with the vessel name.
type BookingView struct {
	EquipmentBooking `bun:",extend"`
	VesselName       string `bun:"vessel_name"`
}

// InsertEquipmentBooking inserts a booking.
func InsertEquipmentBooking(ctx context.Context, tx bun.IDB, s MarinaScope, b *EquipmentBooking) error {
	b.MarinaID = s.ID()
	if b.Version == 0 {
		b.Version = 1
	}
	b.CreatedAt, b.UpdatedAt = UTC(b.CreatedAt), UTC(b.UpdatedAt)
	_, err := tx.NewInsert().Model(b).Exec(ctx)
	return err
}

// GetEquipmentBooking returns a booking of the marina.
func GetEquipmentBooking(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*EquipmentBooking, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	b := new(EquipmentBooking)
	if err := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return b, nil
}

// LockEquipmentBooking locks a booking row (PostgreSQL FOR UPDATE; SQLite has one writer) and returns it.
func LockEquipmentBooking(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*EquipmentBooking, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	b := new(EquipmentBooking)
	q := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return b, nil
}

// LockEquipment locks an equipment row and returns it (the out-of-service read happens under this lock).
func LockEquipment(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Equipment, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(Equipment)
	q := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// UpdateEquipmentBooking writes every mutable column when the version equals expected and bumps it.
func UpdateEquipmentBooking(ctx context.Context, tx bun.IDB, s MarinaScope, b *EquipmentBooking, expected int, now time.Time) error {
	b.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(b).
		Column("equipment_id", "date", "from_slot", "to_slot", "operator_employee_id", "status", "note", "done_at", "cancelled_at",
			"updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", b.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetEquipmentBooking(ctx, tx, s, b.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	b.Version = expected + 1
	return nil
}

// DeleteBookingSlots deletes the equipment and operator slot rows of a booking.
func DeleteBookingSlots(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID string) error {
	if _, err := tx.NewDelete().TableExpr("equipment_slots").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).
		Exec(ctx); err != nil {
		return err
	}
	_, err := tx.NewDelete().TableExpr("operator_slots").Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).Exec(ctx)
	return err
}

// InsertEquipmentSlots writes one equipment_slots row per slot (one statement). A taken slot is a unique
// violation (INV-17).
func InsertEquipmentSlots(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID, equipmentID string, date calendar.Date, slots []int) error {
	if len(slots) == 0 {
		return nil
	}
	rows := make([]EquipmentSlot, 0, len(slots))
	for _, sl := range slots {
		rows = append(rows, EquipmentSlot{EquipmentID: equipmentID, Date: date, Slot: sl, MarinaID: s.ID(), BookingID: bookingID})
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// InsertOperatorSlots writes one operator_slots row per slot (one statement). A busy operator is a unique violation.
func InsertOperatorSlots(ctx context.Context, tx bun.IDB, s MarinaScope, bookingID, employeeID string, date calendar.Date, slots []int) error {
	if len(slots) == 0 || employeeID == "" {
		return nil
	}
	rows := make([]OperatorSlot, 0, len(slots))
	for _, sl := range slots {
		rows = append(rows, OperatorSlot{EmployeeID: employeeID, Date: date, Slot: sl, MarinaID: s.ID(), BookingID: bookingID})
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// SlotHolders returns, for the given slots of an equipment (or an operator when operator is true) on a date, the
// slots that are taken and the bookings holding them (used after a rolled-back insert to explain the 409).
func SlotHolders(ctx context.Context, db bun.IDB, s MarinaScope, operator bool, id string, date calendar.Date, slots []int) ([]int, []string, error) {
	if len(slots) == 0 || !ValidID(id) {
		return nil, nil, nil
	}
	table, col := "equipment_slots", "equipment_id"
	if operator {
		table, col = "operator_slots", "employee_id"
	}
	var rows []struct {
		Slot      int    `bun:"slot"`
		BookingID string `bun:"booking_id"`
	}
	if err := db.NewSelect().TableExpr(table).Column("slot", "booking_id").Where("marina_id = ?", s.ID()).
		Where("? = ?", bun.Ident(col), id).Where("date = ?", date).Where("slot IN (?)", bun.List(slots)).
		OrderExpr("slot").Scan(ctx, &rows); err != nil {
		return nil, nil, err
	}
	var taken []int
	var ids []string
	seen := map[string]bool{}
	for _, r := range rows {
		taken = append(taken, r.Slot)
		if !seen[r.BookingID] {
			seen[r.BookingID] = true
			ids = append(ids, r.BookingID)
		}
	}
	return taken, ids, nil
}

// BookingFilter selects bookings.
type BookingFilter struct {
	Date        calendar.Date // zero = any
	EquipmentID string
	Status      string
	Statuses    []string
	WorkOrderID string
	Requests    bool // only requested (any date)
	FromDate    calendar.Date
	ToDate      calendar.Date
}

// ListEquipmentBookings returns the bookings matching f with the vessel name, ordered by date, equipment, slot.
func ListEquipmentBookings(ctx context.Context, db bun.IDB, s MarinaScope, f BookingFilter) ([]BookingView, error) {
	var rows []BookingView
	q := db.NewSelect().Model(&rows).ModelTableExpr("equipment_bookings AS b").ColumnExpr("b.*").
		ColumnExpr("COALESCE(v.name, '') AS vessel_name").
		Join("LEFT JOIN vessels AS v ON v.id = b.vessel_id AND v.marina_id = b.marina_id").
		Where("b.marina_id = ?", s.ID())
	if !f.Date.IsZero() {
		q = q.Where("b.date = ?", f.Date)
	}
	if !f.FromDate.IsZero() {
		q = q.Where("b.date >= ?", f.FromDate)
	}
	if !f.ToDate.IsZero() {
		q = q.Where("b.date <= ?", f.ToDate)
	}
	if f.EquipmentID != "" {
		if !ValidID(f.EquipmentID) {
			return nil, nil
		}
		q = q.Where("b.equipment_id = ?", f.EquipmentID)
	}
	switch {
	case f.Requests:
		q = q.Where("b.status = ?", LiftBookingRequested)
	case f.Status != "":
		q = q.Where("b.status = ?", f.Status)
	case len(f.Statuses) > 0:
		q = q.Where("b.status IN (?)", bun.List(f.Statuses))
	}
	if f.WorkOrderID != "" {
		if !ValidID(f.WorkOrderID) {
			return nil, nil
		}
		q = q.Where("b.work_order_id = ?", f.WorkOrderID)
	}
	if err := q.OrderExpr("b.date, b.equipment_id, b.from_slot, b.id").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetBookingView returns one booking with the vessel name.
func GetBookingView(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*BookingView, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	var rows []BookingView
	if err := db.NewSelect().Model(&rows).ModelTableExpr("equipment_bookings AS b").ColumnExpr("b.*").
		ColumnExpr("COALESCE(v.name, '') AS vessel_name").
		Join("LEFT JOIN vessels AS v ON v.id = b.vessel_id AND v.marina_id = b.marina_id").
		Where("b.marina_id = ?", s.ID()).Where("b.id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// OpenBookingsOfWorkOrder returns the requested bookings and the booked ones on or after from of a work order.
func OpenBookingsOfWorkOrder(ctx context.Context, db bun.IDB, s MarinaScope, workOrderID string, from calendar.Date) ([]EquipmentBooking, error) {
	var rows []EquipmentBooking
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("work_order_id = ?", workOrderID).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("status = ?", LiftBookingRequested).
				WhereOr("status = ? AND date >= ?", LiftBookingBooked, from)
		}).OrderExpr("id").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// BookableEquipment returns the active bookable equipment of the marina ordered by code.
func BookableEquipment(ctx context.Context, db bun.IDB, s MarinaScope) ([]Equipment, error) {
	var rows []Equipment
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("active = ?", true).
		Where("bookable = ?", true).OrderExpr("code, id").Scan(ctx); err != nil {
		return nil, err
	}
	return rows, nil
}

// EquipmentOutOfService reports, per equipment id, whether a scheduled or in-progress maintenance task flagged out of
// service covers date (no nights = from the start of the work until it is done; m9-plan §8, read only).
func EquipmentOutOfService(ctx context.Context, db bun.IDB, s MarinaScope, date calendar.Date) (map[string]bool, error) {
	var rows []MaintenanceTask
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("element_type = ?", ElementEquipment).
		Where("out_of_service = ?", true).Where("status IN (?)", bun.List([]string{TaskScheduled, TaskInProgress})).
		Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, t := range rows {
		if !t.ScheduledFrom.IsZero() && date.Before(t.ScheduledFrom) {
			continue
		}
		if !t.ScheduledTo.IsZero() && date.After(t.ScheduledTo) {
			continue
		}
		if t.ScheduledFrom.IsZero() && t.Status == TaskScheduled {
			continue
		}
		out[t.ElementID] = true
	}
	return out, nil
}

// EquipmentOperatorIDs returns the enabled operators of the equipment (all equipment when ids is nil).
func EquipmentOperatorIDs(ctx context.Context, db bun.IDB, s MarinaScope, equipmentIDs []string) (map[string][]string, error) {
	var rows []EquipmentOperator
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if equipmentIDs != nil {
		if len(equipmentIDs) == 0 {
			return map[string][]string{}, nil
		}
		q = q.Where("equipment_id IN (?)", bun.List(equipmentIDs))
	}
	if err := q.OrderExpr("equipment_id, employee_id").Scan(ctx); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		out[r.EquipmentID] = append(out[r.EquipmentID], r.EmployeeID)
	}
	return out, nil
}

// ReplaceEquipmentOperators replaces the operators of an equipment.
func ReplaceEquipmentOperators(ctx context.Context, tx bun.IDB, s MarinaScope, equipmentID string, employeeIDs []string) error {
	if _, err := tx.NewDelete().TableExpr("equipment_operators").Where("marina_id = ?", s.ID()).
		Where("equipment_id = ?", equipmentID).Exec(ctx); err != nil {
		return err
	}
	ids := planUniqSorted(employeeIDs)
	if len(ids) == 0 {
		return nil
	}
	rows := make([]EquipmentOperator, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, EquipmentOperator{EquipmentID: equipmentID, EmployeeID: id, MarinaID: s.ID()})
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// IsEquipmentOperator reports whether an employee is an enabled operator of an equipment.
func IsEquipmentOperator(ctx context.Context, db bun.IDB, s MarinaScope, equipmentID, employeeID string) (bool, error) {
	if !ValidID(equipmentID) || !ValidID(employeeID) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr("equipment_operators").Where("marina_id = ?", s.ID()).
		Where("equipment_id = ?", equipmentID).Where("employee_id = ?", employeeID).Count(ctx)
	return n > 0, err
}

// LiftSettings is what lifting reads of yard_settings (defaults when the marina never saved them).
type LiftSettings struct {
	FromSlot      int
	ToSlot        int
	RequestOnMove bool
}

// GetLiftSettings reads the lift window and the move-request flag of the marina (07:00-19:00 and off by default).
func GetLiftSettings(ctx context.Context, db bun.IDB, s MarinaScope) (LiftSettings, error) {
	out := LiftSettings{FromSlot: 28, ToSlot: 76}
	var rows []YardSettings
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return out, err
	}
	if len(rows) == 1 {
		out = LiftSettings{FromSlot: rows[0].LiftFromSlot, ToSlot: rows[0].LiftToSlot, RequestOnMove: rows[0].LiftRequestOnMove}
	}
	return out, nil
}

// RefExists reports whether a row of an operational table referenced by a booking exists in the marina
// (vessels, work_orders, contracts, storage_movements, allocations).
func RefExists(ctx context.Context, db bun.IDB, s MarinaScope, table, id string) (bool, error) {
	switch table {
	case "vessels", "work_orders", "contracts", "storage_movements", "allocations":
	default:
		return false, ErrNotFound
	}
	if !ValidID(id) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", s.ID()).Where("id = ?", id).Count(ctx)
	return n > 0, err
}

// VesselDisplacement returns the displacement of a vessel in kg (0 when unknown).
func VesselDisplacement(ctx context.Context, db bun.IDB, s MarinaScope, vesselID string) (int64, error) {
	var v Vessel
	if err := db.NewSelect().Model(&v).Column("id", "displacement_kg").Where("marina_id = ?", s.ID()).Where("id = ?", vesselID).
		Scan(ctx); err != nil {
		return 0, notFound(err)
	}
	return v.DisplacementKg, nil
}

// WorkOrderVessel returns the vessel and status of a work order.
func WorkOrderVessel(ctx context.Context, db bun.IDB, s MarinaScope, id string) (vesselID, status string, err error) {
	if !ValidID(id) {
		return "", "", ErrNotFound
	}
	var o WorkOrder
	if err := db.NewSelect().Model(&o).Column("id", "vessel_id", "status").Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Scan(ctx); err != nil {
		return "", "", notFound(err)
	}
	return o.VesselID, o.Status, nil
}

// MapKindsOfAllocations returns, for each allocation id, its map kind and the map kind of the allocation it was moved
// from (empty when none), with its vessel and first night (move requests, D30).
type MovedAllocation struct {
	ID          string        `bun:"id"`
	VesselID    string        `bun:"vessel_id"`
	FirstNight  calendar.Date `bun:"first_night"`
	MapKind     string        `bun:"map_kind"`
	FromMapKind string        `bun:"from_map_kind"`
}

// MovedAllocations returns the allocations with their map kind and the map kind of their source allocation.
func MovedAllocations(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]MovedAllocation, error) {
	want := planUniqSorted(ids)
	var valid []string
	for _, id := range want {
		if ValidID(id) {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return nil, nil
	}
	var rows []MovedAllocation
	if err := db.NewSelect().TableExpr("allocations AS a").
		ColumnExpr("a.id, COALESCE(CAST(a.vessel_id AS TEXT), '') AS vessel_id, a.first_night, m.kind AS map_kind").
		ColumnExpr("COALESCE(fm.kind, '') AS from_map_kind").
		Join("JOIN maps AS m ON m.id = a.map_id AND m.marina_id = a.marina_id").
		Join("LEFT JOIN allocations AS f ON f.id = a.moved_from_id AND f.marina_id = a.marina_id").
		Join("LEFT JOIN maps AS fm ON fm.id = f.map_id AND fm.marina_id = f.marina_id").
		Where("a.marina_id = ?", s.ID()).Where("a.id IN (?)", bun.List(valid)).OrderExpr("a.id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
