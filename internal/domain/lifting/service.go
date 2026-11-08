// Package lifting is the booking of lifting equipment (travel lift, crane, forklift, trailer) in 15-minute slots
// with an enabled operator (spec 18.7, m10-plan §11). INV-17: the unique keys of equipment_slots and operator_slots
// are the only guarantee against double booking (ADR 0003); a unique violation rolls back and answers 409, never a
// pre-check.
package lifting

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/lifting/slots"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
}

// Service is the lifting service. It is also the in-process outbox sink of the move requests (ext.EventSink).
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

var _ ext.EventSink = (*Service)(nil)

var purposes = map[string]bool{
	store.LiftPurposeHaulOut: true, store.LiftPurposeLaunch: true, store.LiftPurposeMove: true, store.LiftPurposeOther: true,
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	loc := time.UTC
	if tz, err := store.MarinaTimezone(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return calendar.In(s.d.Clock.Now(), loc)
}

func (s *Service) write(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardManage, auth.PermStorageManage)
	return scope, err
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action string, b *store.EquipmentBooking) error {
	diff := map[string]any{"status": b.Status, "purpose": b.Purpose, "source": b.Source}
	if b.EquipmentID != "" {
		diff["equipment_id"] = b.EquipmentID
	}
	if !b.Date.IsZero() {
		diff["date"] = b.Date
	}
	if b.FromSlot != nil {
		diff["from_slot"], diff["to_slot"] = *b.FromSlot, *b.ToSlot
	}
	if b.OperatorEmployeeID != "" {
		diff["operator_employee_id"] = b.OperatorEmployeeID
	}
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: "equipment_booking", EntityID: b.ID,
		Diff: diff,
	})
}

// changed writes equipment.booking_changed: ids, date and status only (INV-70).
func (s *Service) changed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.EquipmentBooking) error {
	if s.d.Outbox == nil {
		return nil
	}
	d := ""
	if !b.Date.IsZero() {
		d = b.Date.String()
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.EquipmentBookingChanged, event.EquipmentBookingChangedData{
		BookingID: b.ID, EquipmentID: b.EquipmentID, Date: d, Status: b.Status,
	})
	return err
}

// slotConflict is returned from inside the transaction when a slot insert hit the primary key; the 409 details are
// read after the rollback.
type slotConflict struct {
	operator bool
	id       string
	date     calendar.Date
	slots    []int
}

func (c *slotConflict) Error() string { return "lifting: slot conflict" }

// explain turns a slotConflict into the 409 error with the bookings holding the slots (best effort after rollback).
func (s *Service) explain(ctx context.Context, scope store.MarinaScope, err error) error {
	c, ok := errors.AsType[*slotConflict](err)
	if !ok {
		return err
	}
	taken, ids, qerr := store.SlotHolders(ctx, s.d.DB.DB, scope, c.operator, c.id, c.date, c.slots)
	if qerr != nil || len(taken) == 0 {
		taken = c.slots
	}
	if c.operator {
		return &OperatorBusyError{EmployeeID: c.id, Date: c.date, Slots: taken, BookingIDs: ids}
	}
	return &SlotTakenError{EquipmentID: c.id, Date: c.date, Slots: taken, BookingIDs: ids}
}

// checkRefs validates the references of a new booking against the marina.
func checkRefs(ctx context.Context, db bun.IDB, scope store.MarinaScope, in *BookingInput) error {
	v := &auth.ValidationError{}
	for _, r := range []struct{ table, id, field string }{
		{"vessels", in.VesselID, "vessel_id"},
		{"work_orders", in.WorkOrderID, "work_order_id"},
		{"contracts", in.ContractID, "contract_id"},
		{"storage_movements", in.StorageMovementID, "storage_movement_id"},
	} {
		if r.id == "" {
			continue
		}
		ok, err := store.RefExists(ctx, db, scope, r.table, r.id)
		if err != nil {
			return err
		}
		if !ok {
			v.Add(r.field, "not_found")
		}
	}
	return v.Err()
}

// book validates the window of b under the equipment lock and writes its slot rows (old rows deleted first).
// Order: equipment lock, window, bookable, out of service, capacity, operator, then the slot inserts: a taken slot
// is a primary key violation (INV-17), never a pre-check.
func (s *Service) book(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.EquipmentBooking) error {
	v := &auth.ValidationError{}
	if b.EquipmentID == "" {
		v.Add("equipment_id", "required")
	}
	if b.Date.IsZero() {
		v.Add("date", "required")
	}
	if b.FromSlot == nil || b.ToSlot == nil {
		v.Add("from_slot", "required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if b.Date.Before(s.today(ctx, tx, scope)) {
		return invalid("date", "in_the_past")
	}
	eq, err := store.LockEquipment(ctx, tx, scope, b.EquipmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return invalid("equipment_id", "not_found")
		}
		return err
	}
	if !eq.Active || !eq.Bookable {
		return invalid("equipment_id", "not_bookable")
	}
	set, err := store.GetLiftSettings(ctx, tx, scope)
	if err != nil {
		return err
	}
	lift := slots.Window{From: set.FromSlot, To: set.ToSlot}
	if !slots.SafeWindow(lift) {
		lift = slots.Window{From: slots.DefaultLiftFrom, To: slots.DefaultLiftTo}
	}
	w := slots.Window{From: *b.FromSlot, To: *b.ToSlot}
	if !w.Valid() {
		return invalid("to_slot", "invalid")
	}
	if !slots.Within(w, lift) {
		return ErrOutsideWindow
	}
	oos, err := store.EquipmentOutOfService(ctx, tx, scope, b.Date)
	if err != nil {
		return err
	}
	if oos[eq.ID] {
		return ErrOutOfService
	}
	if b.VesselID != "" && eq.CapacityKg != nil {
		disp, err := store.VesselDisplacement(ctx, tx, scope, b.VesselID)
		if err != nil {
			return notFound(err)
		}
		if disp > *eq.CapacityKg {
			return ErrCapacityExceeded
		}
	}
	if b.OperatorEmployeeID != "" {
		ok, err := store.IsEquipmentOperator(ctx, tx, scope, eq.ID, b.OperatorEmployeeID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrOperatorNotEnabled
		}
	}
	if err := store.DeleteBookingSlots(ctx, tx, scope, b.ID); err != nil {
		return err
	}
	rng := slots.Range(w)
	if err := store.InsertEquipmentSlots(ctx, tx, scope, b.ID, eq.ID, b.Date, rng); err != nil {
		if store.IsUniqueViolation(err) {
			return &slotConflict{id: eq.ID, date: b.Date, slots: rng}
		}
		return err
	}
	if err := store.InsertOperatorSlots(ctx, tx, scope, b.ID, b.OperatorEmployeeID, b.Date, rng); err != nil {
		if store.IsUniqueViolation(err) {
			return &slotConflict{operator: true, id: b.OperatorEmployeeID, date: b.Date, slots: rng}
		}
		return err
	}
	return nil
}

func cleanNote(n string) (string, error) {
	n = strings.TrimSpace(n)
	if utf8.RuneCountInString(n) > 500 {
		return "", invalid("note", "too_long")
	}
	return n, nil
}

// ListDay returns the day calendar: window, bookable equipment with out-of-service, bookings of the day and the open
// requests; with a work order, every booking and request of that order (yard.read, yard.manage, storage.read,
// storage.manage or yard.timesheets).
func (s *Service) ListDay(ctx context.Context, p *auth.Principal, marinaID string, f DayFilter) (*DayView, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardRead, auth.PermYardManage, auth.PermStorageRead,
		auth.PermStorageManage, auth.PermYardTimesheets)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if f.Status != "" {
		switch f.Status {
		case store.LiftBookingRequested, store.LiftBookingBooked, store.LiftBookingDone, store.LiftBookingCancelled:
		default:
			return nil, invalid("status", "invalid")
		}
	}
	date := f.Date
	if date.IsZero() {
		date = s.today(ctx, db, scope)
	}
	set, err := store.GetLiftSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	view := &DayView{Date: date, Window: slots.Window{From: set.FromSlot, To: set.ToSlot}}
	eqs, err := store.BookableEquipment(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	oos, err := store.EquipmentOutOfService(ctx, db, scope, date)
	if err != nil {
		return nil, err
	}
	for _, e := range eqs {
		if f.EquipmentID != "" && e.ID != f.EquipmentID {
			continue
		}
		view.Equipment = append(view.Equipment, DayEquipment{Row: e, OutOfService: oos[e.ID]})
	}
	bf := store.BookingFilter{EquipmentID: f.EquipmentID, Status: f.Status, WorkOrderID: f.WorkOrderID}
	if f.WorkOrderID == "" {
		bf.Date = date
		if f.Status == "" {
			bf.Statuses = []string{store.LiftBookingBooked, store.LiftBookingDone}
		}
	} else if f.Status == "" {
		bf.Statuses = []string{store.LiftBookingBooked, store.LiftBookingDone, store.LiftBookingCancelled}
	}
	if view.Bookings, err = store.ListEquipmentBookings(ctx, db, scope, bf); err != nil {
		return nil, err
	}
	if view.Requests, err = store.ListEquipmentBookings(ctx, db, scope, store.BookingFilter{Requests: true, WorkOrderID: f.WorkOrderID}); err != nil {
		return nil, err
	}
	return view, nil
}

// GetBooking returns a booking (any read permission of ListDay).
func (s *Service) GetBooking(ctx context.Context, scope store.MarinaScope, id string) (*store.BookingView, error) {
	b, err := store.GetBookingView(ctx, s.d.DB.DB, scope, id)
	return b, notFound(err)
}

// CreateBooking creates a request (no slots) or a booking with its slots in one transaction (yard.manage or
// storage.manage). A taken slot is 409 equipment_slot_taken (or operator_busy) with nothing written.
func (s *Service) CreateBooking(ctx context.Context, p *auth.Principal, marinaID string, in BookingInput) (*store.BookingView, error) {
	scope, err := s.write(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !purposes[in.Purpose] {
		v.Add("purpose", "invalid")
	}
	if in.Purpose != store.LiftPurposeOther && in.VesselID == "" && in.WorkOrderID == "" {
		v.Add("vessel_id", "required")
	}
	note, err := cleanNote(in.Note)
	if err != nil {
		return nil, err
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := checkRefs(ctx, s.d.DB.DB, scope, &in); err != nil {
		return nil, err
	}
	if in.WorkOrderID != "" {
		vessel, _, err := store.WorkOrderVessel(ctx, s.d.DB.DB, scope, in.WorkOrderID)
		if err != nil {
			return nil, notFound(err)
		}
		if in.VesselID == "" {
			in.VesselID = vessel
		} else if in.VesselID != vessel {
			return nil, invalid("vessel_id", "not_of_work_order")
		}
	}
	source := store.LiftSourceOffice
	switch {
	case in.WorkOrderID != "":
		source = store.LiftSourceWorkOrder
	case in.ContractID != "" || in.StorageMovementID != "":
		source = store.LiftSourceStorage
	}
	now := s.now()
	b := &store.EquipmentBooking{
		ID: store.NewID(), EquipmentID: in.EquipmentID, Date: in.Date, Purpose: in.Purpose, VesselID: in.VesselID,
		WorkOrderID: in.WorkOrderID, ContractID: in.ContractID, StorageMovementID: in.StorageMovementID,
		OperatorEmployeeID: in.OperatorEmployeeID, Status: store.LiftBookingRequested, Source: source, Note: note,
		CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
	}
	request := in.RequestOnly || (in.EquipmentID == "" && in.FromSlot == nil && in.ToSlot == nil)
	if !request {
		b.FromSlot, b.ToSlot, b.Status = in.FromSlot, in.ToSlot, store.LiftBookingBooked
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		// The order is locked before the equipment (the lock order of the order cancel, which cancels its future
		// bookings in the same transaction), then its status re-checked: a cancelled or invoiced order takes no
		// booking or request (READ COMMITTED: lock then re-check).
		if b.WorkOrderID != "" {
			o, err := store.LockWorkOrder(ctx, tx, scope, b.WorkOrderID)
			if err != nil {
				return notFound(err)
			}
			if o.Status == store.WorkOrderCancelled || o.Status == store.WorkOrderInvoiced {
				return ErrWorkOrderClosed
			}
		}
		if request {
			if b.EquipmentID != "" {
				if _, err := store.GetEquipment(ctx, tx, scope, b.EquipmentID); err != nil {
					return invalid("equipment_id", "not_found")
				}
			}
			b.OperatorEmployeeID = ""
			if err := store.InsertEquipmentBooking(ctx, tx, scope, b); err != nil {
				return err
			}
		} else {
			// The booking row goes first as a bare request (the slot rows reference it), then the window is validated
			// and the slots written, then the row becomes booked; the whole transaction rolls back on a conflict.
			bare := *b
			bare.EquipmentID, bare.FromSlot, bare.ToSlot, bare.OperatorEmployeeID = "", nil, nil, ""
			bare.Status = store.LiftBookingRequested
			if err := store.InsertEquipmentBooking(ctx, tx, scope, &bare); err != nil {
				return err
			}
			if err := s.book(ctx, tx, scope, b); err != nil {
				return err
			}
			if err := store.UpdateEquipmentBooking(ctx, tx, scope, b, bare.Version, now); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "equipment_booking.create", b); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b)
	})
	if err != nil {
		return nil, s.explain(ctx, scope, err)
	}
	return s.GetBooking(ctx, scope, b.ID)
}

// UpdateBooking books a request or reschedules a booking: the slot rows are rewritten in one transaction (yard.manage
// or storage.manage, If-Match).
func (s *Service) UpdateBooking(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch BookingPatch) (*store.BookingView, error) {
	scope, err := s.write(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockEquipmentBooking(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(b.Version) != version {
			return ErrVersionMismatch
		}
		if b.Status != store.LiftBookingRequested && b.Status != store.LiftBookingBooked {
			return ErrInvalidTransition
		}
		action := "equipment_booking.reschedule"
		if b.Status == store.LiftBookingRequested {
			action = "equipment_booking.book"
		}
		if patch.EquipmentID != nil {
			b.EquipmentID = *patch.EquipmentID
		}
		if patch.Date != nil {
			b.Date = *patch.Date
		}
		if patch.FromSlot != nil {
			b.FromSlot = patch.FromSlot
		}
		if patch.ToSlot != nil {
			b.ToSlot = patch.ToSlot
		}
		if patch.ClearOperator {
			b.OperatorEmployeeID = ""
		} else if patch.OperatorEmployeeID != nil {
			b.OperatorEmployeeID = *patch.OperatorEmployeeID
		}
		if patch.Note != nil {
			if b.Note, err = cleanNote(*patch.Note); err != nil {
				return err
			}
		}
		if b.FromSlot != nil && b.ToSlot == nil {
			eq, err := store.GetEquipment(ctx, tx, scope, b.EquipmentID)
			if err == nil {
				w := slots.Duration(*b.FromSlot, eq.DefaultMinutes)
				b.ToSlot = &w.To
			}
		}
		b.Status = store.LiftBookingBooked
		if err := s.book(ctx, tx, scope, b); err != nil {
			return err
		}
		if err := store.UpdateEquipmentBooking(ctx, tx, scope, b, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), action, b); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b)
	})
	if err != nil {
		return nil, s.explain(ctx, scope, err)
	}
	return s.GetBooking(ctx, scope, id)
}

// cancelTx cancels a locked booking: explicit delete of its slot rows (the cascade is not relied on) and status.
func (s *Service) cancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, b *store.EquipmentBooking, reason string) error {
	if b.Status != store.LiftBookingRequested && b.Status != store.LiftBookingBooked {
		return ErrInvalidTransition
	}
	if err := store.DeleteBookingSlots(ctx, tx, scope, b.ID); err != nil {
		return err
	}
	expected := b.Version
	b.Status, b.CancelledAt = store.LiftBookingCancelled, s.now()
	if reason != "" {
		b.Note = reason
	}
	if err := store.UpdateEquipmentBooking(ctx, tx, scope, b, expected, s.now()); err != nil {
		return versionErr(err)
	}
	if err := s.audit(ctx, tx, scope, actor, "equipment_booking.cancel", b); err != nil {
		return err
	}
	return s.changed(ctx, tx, scope, b)
}

// CancelBooking cancels a request or a booking and releases its slots (yard.manage or storage.manage, If-Match).
func (s *Service) CancelBooking(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, reason string) (*store.BookingView, error) {
	scope, err := s.write(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	reason, err = cleanNote(reason)
	if err != nil {
		return nil, invalid("reason", "too_long")
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockEquipmentBooking(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(b.Version) != version {
			return ErrVersionMismatch
		}
		return s.cancelTx(ctx, tx, scope, p.Actor(), b, reason)
	})
	if err != nil {
		return nil, err
	}
	return s.GetBooking(ctx, scope, id)
}

// CompleteBooking marks a booked booking done (yard.manage or storage.manage, If-Match); its slots stay as history.
func (s *Service) CompleteBooking(ctx context.Context, p *auth.Principal, marinaID, id string, version int64) (*store.BookingView, error) {
	scope, err := s.write(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockEquipmentBooking(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if int64(b.Version) != version {
			return ErrVersionMismatch
		}
		if b.Status != store.LiftBookingBooked {
			return ErrInvalidTransition
		}
		b.Status, b.DoneAt = store.LiftBookingDone, s.now()
		if err := store.UpdateEquipmentBooking(ctx, tx, scope, b, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "equipment_booking.complete", b); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b)
	})
	if err != nil {
		return nil, err
	}
	return s.GetBooking(ctx, scope, id)
}

// ListOperators returns the enabled operators of an equipment (yard.read, yard.manage, storage.read or storage.manage).
func (s *Service) ListOperators(ctx context.Context, p *auth.Principal, marinaID, equipmentID string) ([]string, error) {
	scope, _, err := p.AuthorizeAny(ctx, s.d.DB.DB, marinaID, auth.PermYardRead, auth.PermYardManage, auth.PermStorageRead,
		auth.PermStorageManage)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetEquipment(ctx, s.d.DB.DB, scope, equipmentID); err != nil {
		return nil, notFound(err)
	}
	ops, err := store.EquipmentOperatorIDs(ctx, s.d.DB.DB, scope, []string{equipmentID})
	if err != nil {
		return nil, err
	}
	if ops[equipmentID] == nil {
		return []string{}, nil
	}
	return ops[equipmentID], nil
}

// PutOperators replaces the enabled operators of an equipment; each must be an employee of the marina holding a
// lift-operator qualification (yard.manage).
func (s *Service) PutOperators(ctx context.Context, p *auth.Principal, marinaID, equipmentID string, employeeIDs []string) ([]string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermYardManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.LockEquipment(ctx, tx, scope, equipmentID); err != nil {
			return notFound(err)
		}
		for _, id := range employeeIDs {
			if _, err := store.GetEmployee(ctx, tx, scope, id); err != nil {
				return invalid("employee_ids", "not_found")
			}
		}
		ok, err := store.LiftOperatorQualified(ctx, tx, scope, employeeIDs)
		if err != nil {
			return err
		}
		for _, id := range employeeIDs {
			if !ok[id] {
				return ErrOperatorNotEnabled
			}
		}
		if err := store.ReplaceEquipmentOperators(ctx, tx, scope, equipmentID, employeeIDs); err != nil {
			return err
		}
		return s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "equipment.operators", EntityType: "equipment",
			EntityID: equipmentID, Diff: map[string]any{"employee_ids": employeeIDs},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.ListOperators(ctx, p, marinaID, equipmentID)
}

// CancelForWorkOrderTx implements workorder.BookingCanceller: the requested and booked bookings of the order on or
// after from are cancelled (slot rows deleted) in the caller's transaction.
func (s *Service) CancelForWorkOrderTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, workOrderID string, from calendar.Date) error {
	rows, err := store.OpenBookingsOfWorkOrder(ctx, tx, scope, workOrderID, from)
	if err != nil {
		return err
	}
	for _, r := range rows {
		b, err := store.LockEquipmentBooking(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		if err := s.cancelTx(ctx, tx, scope, actor, b, ""); err != nil {
			return err
		}
	}
	return nil
}

// Name implements ext.EventSink.
func (s *Service) Name() string { return SinkName }

func isLand(kind string) bool { return kind == "yard" || kind == "shed" }

// Handle implements ext.EventSink: occupancy.moved water <-> yard/shed creates a requested booking (purpose haul_out
// or launch, source move) when yard_settings.lift_request_on_move is on. Idempotent per allocation through the unique
// partial index equipment_bookings_move_uidx (a replay is a unique violation, ignored).
func (s *Service) Handle(ctx context.Context, ev ext.Event) error {
	if ev.Type != event.OccupancyMoved || ev.MarinaID == "" {
		return nil
	}
	var data event.OccupancyMovedData
	if err := json.Unmarshal(ev.Data, &data); err != nil || data.Undo || len(data.AllocationIDs) == 0 {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, s.d.DB.DB, ev.MarinaID)
	if err != nil {
		return nil
	}
	set, err := store.GetLiftSettings(ctx, s.d.DB.DB, scope)
	if err != nil || !set.RequestOnMove {
		return err
	}
	moved, err := store.MovedAllocations(ctx, s.d.DB.DB, scope, data.AllocationIDs)
	if err != nil {
		return err
	}
	for _, m := range moved {
		purpose := ""
		switch {
		case m.FromMapKind == "water" && isLand(m.MapKind):
			purpose = store.LiftPurposeHaulOut
		case isLand(m.FromMapKind) && m.MapKind == "water":
			purpose = store.LiftPurposeLaunch
		}
		if purpose == "" || m.VesselID == "" {
			continue
		}
		now := s.now()
		b := &store.EquipmentBooking{
			ID: store.NewID(), Date: m.FirstNight, Purpose: purpose, VesselID: m.VesselID, AllocationID: m.ID,
			Status: store.LiftBookingRequested, Source: store.LiftSourceMove, CreatedAt: now, UpdatedAt: now,
		}
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if err := store.InsertEquipmentBooking(ctx, tx, scope, b); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, scope, audit.System, "equipment_booking.request_on_move", b); err != nil {
				return err
			}
			return s.changed(ctx, tx, scope, b)
		})
		if err != nil && !store.IsUniqueViolation(err) {
			return err
		}
	}
	return nil
}
