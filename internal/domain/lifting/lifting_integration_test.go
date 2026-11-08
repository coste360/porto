//go:build integration

package lifting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/yardplan"
	"github.com/coste360/porto/internal/domain/yardplan/yardtest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// The clock is 2027-06-02 10:00 in Rome; the default lift window is slots 28..76 (07:00-19:00).

type lenv struct {
	*yardtest.Env
	s         *Service
	plan      *yardplan.Service
	scope     store.MarinaScope
	lift      string // travel lift, 50 t
	crane     string
	operators []string
}

func ip(v int) *int { return &v }

func d(s string) calendar.Date { return calendar.MustParse(s) }

func newLenv(t testing.TB, db *store.DB, nOperators int) *lenv {
	t.Helper()
	e := yardtest.New(t, db)
	l := &lenv{
		Env: e, scope: e.Scope(t),
		s:    NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log}),
		plan: yardplan.NewService(yardplan.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log}),
	}
	now := store.UTC(e.Clock.Now())
	capKg := int64(50000)
	for i, kind := range []string{"travel_lift", "crane"} {
		eq := &store.Equipment{
			ID: store.NewID(), Code: fmt.Sprintf("EQ%d", i), Name: kind, Kind: kind, CapacityKg: &capKg, Active: true,
			Bookable: true, DefaultMinutes: 30, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertEquipment(fixture.Ctx(), db.DB, l.scope, eq); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			l.lift = eq.ID
		} else {
			l.crane = eq.ID
		}
	}
	op := e.Qualification(t, "GRUISTA", true)
	plain := e.Qualification(t, "MECC", false)
	for i := range nOperators {
		emp, err := l.plan.CreateEmployee(fixture.Ctx(), e.Owner, e.MarinaID, yardplan.EmployeeInput{
			Code: fmt.Sprintf("OP%d", i), FirstName: "Op", LastName: "Erator", QualificationIDs: []string{op},
		})
		if err != nil {
			t.Fatal(err)
		}
		l.operators = append(l.operators, emp.Row.ID)
	}
	if _, err := l.plan.CreateEmployee(fixture.Ctx(), e.Owner, e.MarinaID, yardplan.EmployeeInput{
		Code: "MEC", FirstName: "Mec", LastName: "Canico", QualificationIDs: []string{plain},
	}); err != nil {
		t.Fatal(err)
	}
	if nOperators > 0 {
		if _, err := l.s.PutOperators(fixture.Ctx(), e.Owner, e.MarinaID, l.lift, l.operators); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func (l *lenv) invariants(t testing.TB) {
	t.Helper()
	vs, err := store.CheckLiftingInvariants(fixture.Ctx(), l.DB.DB, l.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func (l *lenv) book(date string, from, to int, op string) (*store.BookingView, error) {
	return l.s.CreateBooking(fixture.Ctx(), l.Owner, l.MarinaID, BookingInput{
		EquipmentID: l.lift, Date: d(date), FromSlot: ip(from), ToSlot: ip(to), Purpose: store.LiftPurposeHaulOut, VesselID: l.Vessel,
		OperatorEmployeeID: op,
	})
}

func TestLifting_BookingRules(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		l := newLenv(t, db, 2)
		ctx := fixture.Ctx()
		b, err := l.book("2027-06-03", 36, 40, l.operators[0])
		if err != nil {
			t.Fatal(err)
		}
		if b.Status != store.LiftBookingBooked || b.Source != store.LiftSourceOffice || b.VesselName != "Libeccio" || b.Version != 2 {
			t.Fatalf("booking %+v", b.EquipmentBooking)
		}
		// same machine, overlapping slots: 409 with the holder
		_, err = l.book("2027-06-03", 38, 42, "")
		var st *SlotTakenError
		if !errors.As(err, &st) || st.EquipmentID != l.lift || len(st.Slots) != 2 || st.Slots[0] != 38 || len(st.BookingIDs) != 1 || st.BookingIDs[0] != b.ID {
			t.Fatalf("slot taken: %v %+v", err, st)
		}
		// same operator on the crane at the same time: 409 operator_busy
		if _, err := l.s.PutOperators(ctx, l.Owner, l.MarinaID, l.crane, l.operators); err != nil {
			t.Fatal(err)
		}
		_, err = l.s.CreateBooking(ctx, l.Owner, l.MarinaID, BookingInput{
			EquipmentID: l.crane, Date: d("2027-06-03"), FromSlot: ip(39),
			ToSlot: ip(41), Purpose: store.LiftPurposeOther, OperatorEmployeeID: l.operators[0],
		})
		var ob *OperatorBusyError
		if !errors.As(err, &ob) || ob.EmployeeID != l.operators[0] || len(ob.Slots) != 1 {
			t.Fatalf("operator busy: %v", err)
		}
		// adjacent slots are free; window, operator, capacity, out of service
		if _, err := l.book("2027-06-03", 40, 42, l.operators[1]); err != nil {
			t.Fatalf("adjacent: %v", err)
		}
		if _, err := l.book("2027-06-03", 26, 30, ""); !errors.Is(err, ErrOutsideWindow) {
			t.Fatalf("window: %v", err)
		}
		if _, err := l.book("2027-06-01", 30, 32, ""); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("past: %v", err)
		}
		if _, err := l.book("2027-06-04", 30, 32, store.NewID()); !errors.Is(err, ErrOperatorNotEnabled) {
			t.Fatalf("operator: %v", err)
		}
		l.SetDisplacement(t, 60000)
		if _, err := l.book("2027-06-04", 30, 32, ""); !errors.Is(err, ErrCapacityExceeded) {
			t.Fatalf("capacity: %v", err)
		}
		l.SetDisplacement(t, 9000)
		now := store.UTC(l.Clock.Now())
		l.Insert(t, &store.MaintenanceTask{
			ID: store.NewID(), MarinaID: l.MarinaID, ElementType: store.ElementEquipment, ElementID: l.lift, Source: store.TaskSourceOffice,
			Title: "Revisione", Priority: "normal", Status: store.TaskScheduled, ScheduledFrom: d("2027-06-05"), ScheduledTo: d("2027-06-06"),
			OutOfService: true, Checklist: store.JSON(`[]`), ChecklistResults: store.JSON(`[]`), AllocationIDs: store.JSON(`[]`),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		})
		if _, err := l.book("2027-06-05", 30, 32, ""); !errors.Is(err, ErrOutOfService) {
			t.Fatalf("out of service: %v", err)
		}
		day, err := l.s.ListDay(ctx, l.Owner, l.MarinaID, DayFilter{Date: d("2027-06-05")})
		if err != nil || len(day.Equipment) != 2 || !day.Equipment[0].OutOfService || day.Window.From != 28 || day.Window.To != 76 {
			t.Fatalf("day %+v %v", day, err)
		}
		// reschedule moves the slots in one transaction; onto a taken slot it is 409 and nothing changes
		b2, err := l.s.UpdateBooking(ctx, l.Owner, l.MarinaID, b.ID, int64(b.Version), BookingPatch{FromSlot: ip(44), ToSlot: ip(48)})
		if err != nil || *b2.FromSlot != 44 || b2.Version != 3 {
			t.Fatalf("reschedule %+v %v", b2, err)
		}
		if _, err := l.s.UpdateBooking(ctx, l.Owner, l.MarinaID, b.ID, int64(b2.Version), BookingPatch{FromSlot: ip(40), ToSlot: ip(44)}); !errors.Is(err, ErrSlotTaken) {
			t.Fatalf("reschedule onto taken: %v", err)
		}
		if _, err := l.s.UpdateBooking(ctx, l.Owner, l.MarinaID, b.ID, 1, BookingPatch{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if _, err := l.book("2027-06-03", 36, 40, ""); err != nil {
			t.Fatalf("old slots released: %v", err)
		}
		// request -> book; complete; cancel
		req, err := l.s.CreateBooking(ctx, l.Owner, l.MarinaID, BookingInput{Purpose: store.LiftPurposeLaunch, VesselID: l.Vessel, RequestOnly: true})
		if err != nil || req.Status != store.LiftBookingRequested {
			t.Fatalf("request %+v %v", req, err)
		}
		day, _ = l.s.ListDay(ctx, l.Owner, l.MarinaID, DayFilter{Date: d("2027-06-03")})
		if len(day.Requests) != 1 || len(day.Bookings) != 3 {
			t.Fatalf("day requests %d bookings %d", len(day.Requests), len(day.Bookings))
		}
		eq := l.lift
		booked, err := l.s.UpdateBooking(ctx, l.Owner, l.MarinaID, req.ID, int64(req.Version), BookingPatch{EquipmentID: &eq, Date: ptrDate(d("2027-06-04")), FromSlot: ip(30)})
		if err != nil || booked.Status != store.LiftBookingBooked || *booked.ToSlot != 32 {
			t.Fatalf("book request %+v %v", booked, err)
		}
		if _, err := l.s.CompleteBooking(ctx, l.Owner, l.MarinaID, req.ID, int64(booked.Version)); err != nil {
			t.Fatal(err)
		}
		if _, err := l.s.CancelBooking(ctx, l.Owner, l.MarinaID, req.ID, int64(booked.Version)+1, ""); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("cancel done: %v", err)
		}
		c, err := l.s.CancelBooking(ctx, l.Owner, l.MarinaID, b.ID, int64(b2.Version), "meteo")
		if err != nil || c.Status != store.LiftBookingCancelled {
			t.Fatalf("cancel %+v %v", c, err)
		}
		if _, err := l.book("2027-06-03", 44, 48, ""); err != nil {
			t.Fatalf("cancelled slots released: %v", err)
		}
		// permissions and isolation
		worker := l.AddUser(t, "yard@porto.test", auth.RoleYard, l.OrgID, l.MarinaID)
		if _, err := l.s.ListDay(ctx, worker, l.MarinaID, DayFilter{}); err != nil {
			t.Fatalf("worker day: %v", err)
		}
		if _, err := l.s.CreateBooking(ctx, worker, l.MarinaID, BookingInput{Purpose: "other", RequestOnly: true}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("worker books: %v", err)
		}
		_, om, other := l.OtherOrganization(t)
		if _, err := l.s.CancelBooking(ctx, other, om, booked.ID, 1, ""); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		if _, err := l.s.PutOperators(ctx, l.Owner, l.MarinaID, l.lift, []string{l.operators[0], mustEmployee(t, l, "MEC")}); !errors.Is(err, ErrOperatorNotEnabled) {
			t.Fatalf("non operator: %v", err)
		}
		for _, ev := range l.Events(t, event.EquipmentBookingChanged) {
			var data map[string]any
			if err := json.Unmarshal(ev.Payload, &data); err != nil {
				t.Fatal(err)
			}
			for k := range data {
				if k != "booking_id" && k != "equipment_id" && k != "date" && k != "status" {
					t.Fatalf("unexpected key %s in %s", k, ev.Payload)
				}
			}
		}
		l.invariants(t)
	})
}

func ptrDate(v calendar.Date) *calendar.Date { return &v }

func mustEmployee(t *testing.T, l *lenv, code string) string {
	t.Helper()
	list, _, err := l.plan.ListEmployees(fixture.Ctx(), l.Owner, l.MarinaID, store.EmployeeFilter{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Row.Code == code {
			return e.Row.ID
		}
	}
	t.Fatalf("no employee %s", code)
	return ""
}

func TestLifting_WorkOrderCancelAndMoveRequests(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		l := newLenv(t, db, 1)
		ctx := fixture.Ctx()
		o := l.WorkOrder(t, store.WorkOrderPlanned, d("2027-06-20"))
		b, err := l.s.CreateBooking(ctx, l.Owner, l.MarinaID, BookingInput{
			EquipmentID: l.lift, Date: d("2027-06-07"), FromSlot: ip(30),
			ToSlot: ip(34), Purpose: store.LiftPurposeHaulOut, WorkOrderID: o.ID,
		})
		if err != nil || b.Source != store.LiftSourceWorkOrder || b.VesselID != l.Vessel {
			t.Fatalf("order booking %+v %v", b, err)
		}
		r, err := l.s.CreateBooking(ctx, l.Owner, l.MarinaID, BookingInput{Purpose: store.LiftPurposeLaunch, WorkOrderID: o.ID, RequestOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		day, err := l.s.ListDay(ctx, l.Owner, l.MarinaID, DayFilter{WorkOrderID: o.ID})
		if err != nil || len(day.Bookings) != 1 || len(day.Requests) != 1 {
			t.Fatalf("order bookings %+v %v", day, err)
		}
		err = db.RunInTx(ctx, func(ctx2 context.Context, tx bun.Tx) error {
			return l.s.CancelForWorkOrderTx(ctx2, tx, l.scope, audit.System, o.ID, d("2027-06-02"))
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{b.ID, r.ID} {
			got, _ := l.s.GetBooking(ctx, l.scope, id)
			if got.Status != store.LiftBookingCancelled {
				t.Fatalf("not cancelled %+v", got.EquipmentBooking)
			}
		}
		l.invariants(t)

		// move-request sink: off by default, then on (idempotent per allocation)
		h := contractHarbourMove(t, l)
		ev := ext.Event{Type: event.OccupancyMoved, MarinaID: l.MarinaID, Data: h}
		if err := l.s.Handle(ctx, ev); err != nil {
			t.Fatal(err)
		}
		if n := countMove(t, l); n != 0 {
			t.Fatalf("sink off: %d", n)
		}
		now := store.UTC(l.Clock.Now())
		l.Insert(t, &store.YardSettings{
			MarinaID: l.MarinaID, TestVessel: store.JSON(`{}`), QuoteValidityDays: 30, LiftFromSlot: 28,
			LiftToSlot: 76, LiftRequestOnMove: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		})
		for range 2 {
			if err := l.s.Handle(ctx, ev); err != nil {
				t.Fatal(err)
			}
		}
		if n := countMove(t, l); n != 1 {
			t.Fatalf("move requests %d", n)
		}
	})
}

// TestLifting_ClosedWorkOrderRefusesBookings: a cancelled or invoiced order takes no booking and no request
// (409 work_order_closed); the order is locked and re-checked inside the booking transaction, so nothing is written.
func TestLifting_ClosedWorkOrderRefusesBookings(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		l := newLenv(t, db, 1)
		ctx := fixture.Ctx()
		for _, status := range []string{store.WorkOrderCancelled, store.WorkOrderInvoiced} {
			o := l.WorkOrder(t, status, d("2027-06-20"))
			for _, in := range []BookingInput{
				{EquipmentID: l.lift, Date: d("2027-06-07"), FromSlot: ip(30), ToSlot: ip(34), Purpose: store.LiftPurposeHaulOut, WorkOrderID: o.ID},
				{Purpose: store.LiftPurposeLaunch, WorkOrderID: o.ID, RequestOnly: true},
			} {
				if _, err := l.s.CreateBooking(ctx, l.Owner, l.MarinaID, in); !errors.Is(err, ErrWorkOrderClosed) {
					t.Fatalf("%s order, request=%v: %v, want ErrWorkOrderClosed", status, in.RequestOnly, err)
				}
			}
			n, err := l.DB.NewSelect().TableExpr("equipment_bookings").Where("work_order_id = ?", o.ID).Count(ctx)
			if err != nil || n != 0 {
				t.Fatalf("%s order: %d bookings written %v", status, n, err)
			}
		}
		l.invariants(t)
	})
}

func countMove(t *testing.T, l *lenv) int {
	t.Helper()
	n, err := l.DB.NewSelect().TableExpr("equipment_bookings").Where("source = ?", store.LiftSourceMove).
		Where("purpose = ?", store.LiftPurposeHaulOut).Count(fixture.Ctx())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// contractHarbourMove books a transit on berth A1, inserts a yard map and an allocation on it moved from the
// transit's (as the occupancy service would on a water -> yard move) and returns the occupancy.moved payload.
func contractHarbourMove(t *testing.T, l *lenv) json.RawMessage {
	t.Helper()
	ctx := fixture.Ctx()
	l.Transit(t, l.Berths[0], "2027-06-02", "2027-06-10", true)
	var src store.Allocation
	if err := l.DB.NewSelect().Model(&src).Where("marina_id = ?", l.MarinaID).Where("resource_type = 'berth'").Limit(1).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	var water store.Map
	if err := l.DB.NewSelect().Model(&water).Where("id = ?", src.MapID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	yard := water
	yard.ID, yard.Kind = store.NewID(), "yard"
	l.Insert(t, &yard)
	dst := src
	dst.ID, dst.MapID, dst.MovedFromID = store.NewID(), yard.ID, src.ID
	l.Insert(t, &dst)
	data, err := json.Marshal(event.OccupancyMovedData{AllocationIDs: []string{dst.ID, src.ID}, Operation: "move"})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestConcurrency_EquipmentSlots (INV-17): 100 goroutines book random overlapping windows on one travel lift with
// 3 operators, then 20 reschedule or cancel winners. The winners' slot sets are disjoint per equipment and per
// operator, the slot rows are exactly the winners' ranges, the losers got 409 with nothing written, Q31 is empty.
func TestConcurrency_EquipmentSlots(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		l := newLenv(t, db, 3)
		ctx := fixture.Ctx()
		rng := rand.New(rand.NewPCG(10, 17))
		type try struct {
			from, to int
			op       string
		}
		tries := make([]try, 100)
		for i := range tries {
			from := 28 + rng.IntN(40)
			tries[i] = try{from: from, to: from + 1 + rng.IntN(8)}
			if k := rng.IntN(4); k < 3 {
				tries[i].op = l.operators[k]
			}
		}
		var mu sync.Mutex
		var winners []*store.BookingView
		var wg sync.WaitGroup
		for _, tr := range tries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b, err := l.book("2027-06-03", tr.from, tr.to, tr.op)
				switch {
				case err == nil:
					mu.Lock()
					winners = append(winners, b)
					mu.Unlock()
				case errors.Is(err, ErrSlotTaken), errors.Is(err, ErrOperatorBusy):
				default:
					t.Errorf("book: %v", err)
				}
			}()
		}
		wg.Wait()
		if len(winners) == 0 {
			t.Fatal("no winner")
		}
		// 20 concurrent reschedules and cancels of winners.
		for i := range 20 {
			b := winners[i%len(winners)]
			from := 28 + rng.IntN(40)
			to := from + 1 + rng.IntN(6)
			cancel := i%2 == 1
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if cancel {
					_, err = l.s.CancelBooking(ctx, l.Owner, l.MarinaID, b.ID, int64(b.Version), "")
				} else {
					_, err = l.s.UpdateBooking(ctx, l.Owner, l.MarinaID, b.ID, int64(b.Version), BookingPatch{FromSlot: ip(from), ToSlot: ip(to)})
				}
				if err != nil && !errors.Is(err, ErrSlotTaken) && !errors.Is(err, ErrOperatorBusy) && !errors.Is(err, ErrVersionMismatch) &&
					!errors.Is(err, ErrInvalidTransition) {
					t.Errorf("change: %v", err)
				}
			}()
		}
		wg.Wait()
		var live []store.EquipmentBooking
		if err := db.NewSelect().Model(&live).Where("marina_id = ?", l.MarinaID).Where("status = ?", store.LiftBookingBooked).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		eqSlots, opSlots := map[int]string{}, map[string]string{}
		want := 0
		for _, b := range live {
			for s := *b.FromSlot; s < *b.ToSlot; s++ {
				if other, ok := eqSlots[s]; ok {
					t.Fatalf("slot %d of the lift booked by %s and %s", s, other, b.ID)
				}
				eqSlots[s] = b.ID
				want++
				if b.OperatorEmployeeID != "" {
					k := fmt.Sprintf("%s/%d", b.OperatorEmployeeID, s)
					if other, ok := opSlots[k]; ok {
						t.Fatalf("operator slot %s booked by %s and %s", k, other, b.ID)
					}
					opSlots[k] = b.ID
				}
			}
		}
		n, err := db.NewSelect().TableExpr("equipment_slots").Where("marina_id = ?", l.MarinaID).Count(ctx)
		if err != nil || int(n) != want {
			t.Fatalf("slot rows %d, want %d (%v)", n, want, err)
		}
		total, err := db.NewSelect().TableExpr("equipment_bookings").Where("marina_id = ?", l.MarinaID).Count(ctx)
		if err != nil || int(total) != len(winners) {
			t.Fatalf("bookings %d, winners %d: a loser wrote a row", total, len(winners))
		}
		l.invariants(t)
	})
}
