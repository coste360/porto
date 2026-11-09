//go:build integration

package reservation

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestReservation_CreateHoldConfirm(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 2, 1)
		h := e.mustCreate(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 26), Hold: true, Renter: &Renter{Name: "Ada Rossi", Phone: "+39 333 1234567"}})
		if h.Status != store.RentalStatusHeld || h.Number != 1 || h.Year != 2027 || h.UnitCode != "GOM0" || h.FormulaCode != "ORARIA" {
			t.Fatalf("held %+v", h.RentalBooking)
		}
		if !h.HoldExpiresAt.Equal(t0.Add(60 * time.Minute)) {
			t.Fatalf("hold expiry %v", h.HoldExpiresAt)
		}
		if h.TotalCents != 6000 || h.Kind != store.RentalKindHourly || h.Renter == nil || h.Renter.Name != "Ada Rossi" {
			t.Fatalf("view %+v renter %+v", h.RentalBooking, h.Renter)
		}
		if h.StartsAt != time.Date(2027, 6, 12, 8, 0, 0, 0, time.UTC) || h.EndsAt != time.Date(2027, 6, 12, 11, 0, 0, 0, time.UTC) {
			t.Fatalf("instants %v %v", h.StartsAt, h.EndsAt)
		}
		if u, sk := e.rows(h.ID); u != 7 || sk != 0 {
			t.Fatalf("rows %d %d", u, sk)
		}
		if h.RenterEnc == "" || h.RenterEnc == "Ada Rossi" {
			t.Fatal("renter must be encrypted")
		}
		e.q32()
		c, err := e.transition(h.ID, TransitionInput{Action: ActionConfirm})
		if err != nil {
			t.Fatal(err)
		}
		if c.Status != store.RentalStatusConfirmed || !c.HoldExpiresAt.IsZero() || c.Version != h.Version+1 {
			t.Fatalf("confirmed %+v", c.RentalBooking)
		}
		e.q32()
		if e.hooks.confirmed != 1 || len(e.Events(t, event.RentalConfirmed)) != 1 {
			t.Fatal("confirm side effects")
		}
		if got := e.eventChanges(event.RentalChanged); !slices.Equal(got, []string{"created"}) {
			t.Fatalf("changes %v", got)
		}
		// A confirmed create is numbered next and confirms at once.
		c2 := e.mustCreate(CreateInput{UnitID: e.units[1], FormulaID: e.morning, Range: r("2027-06-12", 16, "2027-06-12", 26), SkipperID: e.skippers[0]})
		if c2.Status != store.RentalStatusConfirmed || c2.Number != 2 || c2.TotalCents != 10*1000+10*500 {
			t.Fatalf("confirmed create %+v", c2.RentalBooking)
		}
		if u, sk := e.rows(c2.ID); u != 11 || sk != 10 {
			t.Fatalf("half-day rows %d %d", u, sk)
		}
		e.q32()
		if e.AuditCount(t, "rental_booking.create") != 2 || e.AuditCount(t, "rental_booking.confirm") != 1 {
			t.Fatal("audit")
		}
		// Invalid transitions.
		if _, err := e.transition(c.ID, TransitionInput{Action: ActionConfirm}); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("confirm twice: %v", err)
		}
		if _, err := e.s.Transition(fixture.Ctx(), e.Owner, e.MarinaID, c.ID, c.Version+5, TransitionInput{Action: ActionCancel}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		// List and filters.
		list, _, err := e.s.List(fixture.Ctx(), e.Owner, e.MarinaID, ListFilter{Status: store.RentalStatusConfirmed})
		if err != nil || len(list) != 2 {
			t.Fatalf("list %d %v", len(list), err)
		}
		list, _, err = e.s.List(fixture.Ctx(), e.Owner, e.MarinaID, ListFilter{UnitID: e.units[1]})
		if err != nil || len(list) != 1 || list[0].ID != c2.ID {
			t.Fatalf("unit list %v", err)
		}
	})
}

func TestReservation_ValidationAndRules(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 2, 1)
		var re *slots.RangeError
		if _, err := e.create(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 21)}); !errors.As(err, &re) || re.Code != slots.CodeFormulaRangeMismatch {
			t.Fatalf("too short: %v", err)
		}
		if _, err := e.create(CreateInput{Range: r("2027-06-12", 10, "2027-06-12", 14)}); !errors.As(err, &re) || re.Code != slots.CodeOutsideRentalHours {
			t.Fatalf("before opening: %v", err)
		}
		if _, err := e.create(CreateInput{FormulaID: e.morning, Range: r("2027-06-12", 17, "2027-06-12", 26)}); !errors.As(err, &re) {
			t.Fatalf("half day off band: %v", err)
		}
		if _, err := e.create(CreateInput{FormulaID: e.withSk, Range: r("2027-06-12", 20, "2027-06-12", 24)}); !errors.Is(err, ErrSkipperRequired) {
			t.Fatalf("skipper required: %v", err)
		}
		if _, err := e.create(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24), InvoiceRequested: true}); !errors.Is(err, ErrCustomerRequired) {
			t.Fatalf("invoice without customer: %v", err)
		}
		six := 7
		var ve *auth.ValidationError
		if _, err := e.create(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24), PartySize: &six}); !errors.As(err, &ve) {
			t.Fatalf("party above seats: %v", err)
		}
		if _, err := e.create(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24), Extras: []fleet.ExtraQty{{ExtraID: e.extra, Qty: 9000}}}); !errors.As(err, &ve) {
			t.Fatalf("extra above max: %v", err)
		}
		e.setUnitStatus(e.units[1], store.RentalUnitMaintenance)
		if _, err := e.create(CreateInput{UnitID: e.units[1], Range: r("2027-06-12", 20, "2027-06-12", 24)}); !errors.Is(err, ErrUnitNotBookable) {
			t.Fatalf("maintenance unit: %v", err)
		}
		// Skipper licence and weekly hours.
		if _, err := db.NewUpdate().TableExpr("rental_skippers").Set("licence_expires_on = ?", d("2027-06-11")).Where("id = ?", e.skippers[0]).
			Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.create(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 20, "2027-06-12", 24)}); !errors.Is(err, ErrSkipperLicenceExpired) {
			t.Fatalf("skipper licence: %v", err)
		}
		if _, err := db.NewUpdate().TableExpr("rental_skippers").Set("licence_expires_on = NULL").Where("id = ?", e.skippers[0]).
			Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		// 2027-06-12 is a Saturday (6): hours 09:00-13:00 only.
		e.insert(&store.RentalSkipperHours{SkipperID: e.skippers[0], Weekday: 6, FromSlot: 18, ToSlot: 26, MarinaID: e.MarinaID})
		if _, err := e.create(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 24, "2027-06-12", 28)}); !errors.Is(err, ErrSkipperUnavailable) {
			t.Fatalf("outside hours: %v", err)
		}
		b := e.mustCreate(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 24, "2027-06-12", 28), AllowOutsideHours: true})
		if !b.AllowOutsideHours {
			t.Fatal("override not stored")
		}
		e.mustCreate(CreateInput{UnitID: e.units[0], FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 18, "2027-06-12", 22)})
		e.q32()
		// A rental user may book; a viewer may not.
		rent := e.rentalUser()
		if _, err := e.s.Create(fixture.Ctx(), rent, e.MarinaID, CreateInput{UnitID: e.units[0], FormulaID: e.hourly, Range: r("2027-06-14", 20, "2027-06-14", 24)}); err != nil {
			t.Fatalf("rental role: %v", err)
		}
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, err := e.s.Create(fixture.Ctx(), viewer, e.MarinaID, CreateInput{UnitID: e.units[0], FormulaID: e.hourly, Range: r("2027-06-15", 20, "2027-06-15", 24)}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
		_, other, otherOwner := e.OtherOrganization(t)
		if _, err := e.s.Get(fixture.Ctx(), otherOwner, other, b.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina id: %v", err)
		}
		if _, err := e.s.Get(fixture.Ctx(), e.Owner, other, b.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
	})
}

func TestReservation_Conflicts(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 2, 1)
		a := e.mustCreate(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24)})
		// 12:00-13:00 hits the buffer row 24 of the first booking.
		_, err := e.create(CreateInput{Range: r("2027-06-12", 24, "2027-06-12", 26)})
		var ce *SlotConflict
		if !errors.As(err, &ce) || !errors.Is(err, ErrSlotTaken) || ce.Target != TargetUnit {
			t.Fatalf("buffer collision: %v", err)
		}
		if ce.Date.String() != "2027-06-12" || !slices.Equal(ce.Slots, []int{24}) || !slices.Equal(ce.BookingIDs, []string{a.ID}) {
			t.Fatalf("conflict %+v", ce)
		}
		// 12:30 is the earliest start.
		e.mustCreate(CreateInput{Range: r("2027-06-12", 25, "2027-06-12", 27)})
		// The other unit is free; the skipper is the same guarantee.
		s1 := e.mustCreate(CreateInput{UnitID: e.units[1], FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-13", 20, "2027-06-13", 24)})
		_, err = e.create(CreateInput{UnitID: e.units[0], FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-13", 22, "2027-06-13", 26)})
		if !errors.As(err, &ce) || !errors.Is(err, ErrSkipperBusy) || ce.Target != TargetSkipper || !slices.Equal(ce.BookingIDs, []string{s1.ID}) {
			t.Fatalf("skipper busy: %v", err)
		}
		// The loser wrote nothing.
		var n int64
		n, err = db.NewSelect().TableExpr("rental_slots").Where("unit_id = ?", e.units[0]).Where("date = ?", d("2027-06-13")).Count(context.Background())
		if err != nil || n != 0 {
			t.Fatalf("loser rows %d %v", n, err)
		}
		// A skipper has no buffer: back to back is fine.
		e.mustCreate(CreateInput{UnitID: e.units[0], FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-13", 24, "2027-06-13", 26)})
		e.q32()
	})
}

func TestReservation_ExpiredHoldIsReclaimed(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 1)
		h := e.mustCreate(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 20, "2027-06-12", 26), Hold: true})
		// Before expiry the window is taken.
		if _, err := e.create(CreateInput{Range: r("2027-06-12", 22, "2027-06-12", 24)}); !errors.Is(err, ErrSlotTaken) {
			t.Fatalf("live hold: %v", err)
		}
		e.Clock.Advance(61 * time.Minute)
		// After expiry a sale wins at conflict time, before the job ran.
		w := e.mustCreate(CreateInput{Range: r("2027-06-12", 22, "2027-06-12", 24)})
		got := e.get(h.ID)
		if got.Status != store.RentalStatusHeld || got.SlotsLostAt.IsZero() {
			t.Fatalf("reclaimed hold %+v", got.RentalBooking)
		}
		if u, sk := e.rows(h.ID); u != 0 || sk != 0 {
			t.Fatalf("reclaimed rows %d %d", u, sk)
		}
		e.q32()
		// Confirming the reclaimed hold re-allocates or fails.
		if _, err := e.transition(h.ID, TransitionInput{Action: ActionConfirm}); !errors.Is(err, ErrHoldExpired) {
			t.Fatalf("confirm lost hold: %v", err)
		}
		// The job cancels it once.
		if err := e.s.RunHoldsExpire(context.Background()); err != nil {
			t.Fatal(err)
		}
		got = e.get(h.ID)
		if got.Status != store.RentalStatusCancelled || got.CancelReason != CancelReasonHoldExpired || !got.HoldExpiresAt.IsZero() {
			t.Fatalf("expired %+v", got.RentalBooking)
		}
		if err := e.s.RunHoldsExpire(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := e.AuditCount(t, "rental_booking.hold_expire"); n != 1 {
			t.Fatalf("expire audits %d", n)
		}
		if got := e.eventChanges(event.RentalCancelled); !slices.Equal(got, []string{CancelReasonHoldExpired}) {
			t.Fatalf("cancel events %v", got)
		}
		if e.get(w.ID).Status != store.RentalStatusConfirmed {
			t.Fatal("winner")
		}
		e.q32()
		// An expired hold whose rows are intact confirms (lazy semantics).
		h2 := e.mustCreate(CreateInput{Range: r("2027-06-14", 20, "2027-06-14", 24), Hold: true})
		e.Clock.Advance(2 * time.Hour)
		c, err := e.transition(h2.ID, TransitionInput{Action: ActionConfirm})
		if err != nil || c.Status != store.RentalStatusConfirmed {
			t.Fatalf("confirm intact expired hold: %v", err)
		}
		e.q32()
	})
}

func TestReservation_ExtendHoldCancelNoShow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 0)
		h := e.mustCreate(CreateInput{Range: r("2027-06-10", 20, "2027-06-10", 24), Hold: true})
		until := t0.Add(3 * time.Hour)
		x, err := e.transition(h.ID, TransitionInput{Action: ActionExtendHold, Until: until})
		if err != nil || !x.HoldExpiresAt.Equal(until) {
			t.Fatalf("extend hold: %v", err)
		}
		var exp []time.Time
		if err := db.NewSelect().TableExpr("rental_slots").ColumnExpr("DISTINCT expires_at").Where("booking_id = ?", h.ID).
			Scan(context.Background(), &exp); err != nil || len(exp) != 1 || !exp[0].Equal(until) {
			t.Fatalf("row expiry %v %v", exp, err)
		}
		e.q32()
		if _, err := e.transition(h.ID, TransitionInput{Action: ActionExtendHold, Until: t0.Add(-time.Minute)}); err == nil {
			t.Fatal("past until accepted")
		}
		c, err := e.transition(h.ID, TransitionInput{Action: ActionCancel, Reason: "cliente rinuncia"})
		if err != nil || c.Status != store.RentalStatusCancelled || c.CancelReason != "cliente rinuncia" {
			t.Fatalf("cancel: %v", err)
		}
		if u, _ := e.rows(h.ID); u != 0 {
			t.Fatal("cancel rows")
		}
		if e.hooks.ended != 1 {
			t.Fatal("ended hook")
		}
		// No-show only after the start.
		b := e.mustCreate(CreateInput{Range: r("2027-06-10", 22, "2027-06-10", 26)})
		if _, err := e.transition(b.ID, TransitionInput{Action: ActionNoShow}); err == nil {
			t.Fatal("no-show before start")
		}
		e.Clock.Advance(90 * time.Minute) // 11:30 Rome
		ns, err := e.transition(b.ID, TransitionInput{Action: ActionNoShow})
		if err != nil || ns.Status != store.RentalStatusNoShow || ns.NoShowAt.IsZero() {
			t.Fatalf("no-show: %v", err)
		}
		if e.hooks.ended != 2 {
			t.Fatal("no-show hook")
		}
		if _, err := e.transition(b.ID, TransitionInput{Action: ActionCancel}); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("terminal: %v", err)
		}
		e.q32()
	})
}

func TestReservation_Reschedule(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 2, 1)
		a := e.mustCreate(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24)})
		other := e.mustCreate(CreateInput{UnitID: e.units[1], Range: r("2027-06-12", 20, "2027-06-12", 24)})
		ctx := fixture.Ctx()
		nr := r("2027-06-12", 26, "2027-06-12", 32)
		u, err := e.s.Update(ctx, e.Owner, e.MarinaID, a.ID, a.Version, Patch{Range: &nr, Extras: []fleet.ExtraQty{{ExtraID: e.extra}}, ExtrasSet: true})
		if err != nil {
			t.Fatal(err)
		}
		if u.StartSlot != 26 || u.TotalCents != 6*1000+300 || len(u.Extras) != 1 || u.Extras[0].QtyMilli != 1000 {
			t.Fatalf("rescheduled %+v extras %+v", u.RentalBooking, u.Extras)
		}
		if e.hooks.repriced != 1 {
			t.Fatal("reprice hook")
		}
		e.q32()
		// Moving onto the other booking's unit and time is a conflict and changes nothing.
		ou := e.units[1]
		back := r("2027-06-12", 20, "2027-06-12", 24)
		if _, err := e.s.Update(ctx, e.Owner, e.MarinaID, a.ID, u.Version, Patch{UnitID: &ou, Range: &back}); !errors.Is(err, ErrSlotTaken) {
			t.Fatalf("move onto other: %v", err)
		}
		if got := e.get(a.ID); got.UnitID != e.units[0] || got.StartSlot != 26 || got.Version != u.Version {
			t.Fatal("failed reschedule changed the booking")
		}
		// Stale version.
		if _, err := e.s.Update(ctx, e.Owner, e.MarinaID, a.ID, a.Version, Patch{Note: ptr("x")}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		// Note only: no reprice, no reallocation.
		n, err := e.s.Update(ctx, e.Owner, e.MarinaID, a.ID, u.Version, Patch{Note: ptr("chiavi in reception")})
		if err != nil || n.Note != "chiavi in reception" || e.hooks.repriced != 1 {
			t.Fatalf("note: %v", err)
		}
		// keep_price needs marina.configure.
		rent := e.rentalUser()
		later := r("2027-06-12", 30, "2027-06-12", 34)
		if _, err := e.s.Update(ctx, rent, e.MarinaID, a.ID, n.Version, Patch{Range: &later, KeepPrice: true}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("keep price by rental: %v", err)
		}
		k, err := e.s.Update(ctx, e.Owner, e.MarinaID, a.ID, n.Version, Patch{Range: &later, KeepPrice: true})
		if err != nil || k.TotalCents != u.TotalCents {
			t.Fatalf("keep price: %v", err)
		}
		// A reschedule onto a unit in maintenance is refused.
		e.setUnitStatus(e.units[1], store.RentalUnitMaintenance)
		if _, err := e.s.Update(ctx, e.Owner, e.MarinaID, a.ID, k.Version, Patch{UnitID: &ou}); !errors.Is(err, ErrUnitNotBookable) {
			t.Fatalf("maintenance: %v", err)
		}
		_ = other
		e.q32()
	})
}

func ptr[T any](v T) *T { return &v }

func TestReservation_Blocks(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 1)
		ctx := fixture.Ctx()
		b := e.mustCreate(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24)})
		_, err := e.s.CreateBlock(ctx, e.Owner, e.MarinaID, BlockInput{UnitID: e.units[0], Range: r("2027-06-12", 16, "2027-06-13", 38), Reason: store.RentalBlockMaintenance})
		var ce *SlotConflict
		if !errors.As(err, &ce) || !slices.Equal(ce.BookingIDs, []string{b.ID}) {
			t.Fatalf("block over booking: %v", err)
		}
		blk, err := e.s.CreateBlock(ctx, e.Owner, e.MarinaID, BlockInput{UnitID: e.units[0], Range: r("2027-06-13", 0, "2027-06-14", 48), Reason: store.RentalBlockMaintenance, Note: "tagliando"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.create(CreateInput{Range: r("2027-06-13", 20, "2027-06-13", 24)})
		if !errors.As(err, &ce) || !slices.Equal(ce.BlockIDs, []string{blk.ID}) {
			t.Fatalf("booking over block: %v", err)
		}
		// Skipper day off.
		off, err := e.s.CreateBlock(ctx, e.Owner, e.MarinaID, BlockInput{SkipperID: e.skippers[0], Range: r("2027-06-15", 0, "2027-06-15", 48), Reason: store.RentalBlockDayOff})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.create(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-15", 20, "2027-06-15", 24)}); !errors.Is(err, ErrSkipperBusy) {
			t.Fatalf("skipper day off: %v", err)
		}
		if _, err := e.s.CreateBlock(ctx, e.Owner, e.MarinaID, BlockInput{UnitID: e.units[0], SkipperID: e.skippers[0], Range: r("2027-06-16", 0, "2027-06-16", 48), Reason: store.RentalBlockOther}); err == nil {
			t.Fatal("two targets accepted")
		}
		// Move and delete.
		nr := r("2027-06-20", 0, "2027-06-20", 48)
		m, err := e.s.UpdateBlock(ctx, e.Owner, e.MarinaID, blk.ID, blk.Version, BlockPatch{Range: &nr})
		if err != nil || m.FromOn.String() != "2027-06-20" {
			t.Fatalf("move block: %v", err)
		}
		e.mustCreate(CreateInput{Range: r("2027-06-13", 20, "2027-06-13", 24)})
		if err := e.s.DeleteBlock(ctx, e.Owner, e.MarinaID, off.ID, off.Version); err != nil {
			t.Fatal(err)
		}
		e.mustCreate(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-15", 20, "2027-06-15", 24)})
		list, err := e.s.ListBlocks(ctx, e.Owner, e.MarinaID, BlockFilter{From: d("2027-06-01"), To: d("2027-06-30")})
		if err != nil || len(list) != 1 {
			t.Fatalf("blocks %d %v", len(list), err)
		}
		e.q32()
	})
}

func TestReservation_HandoverSeams(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 1)
		scope := e.scope
		actor := audit.Actor{Type: audit.ActorUser, ID: e.OwnerID}
		// 2027-06-10 10:00-13:00 Rome (slots 20-26) with a skipper.
		b := e.mustCreate(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-10", 20, "2027-06-10", 26)})
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			_, err := e.s.ReturnTx(ctx, tx, scope, actor, b.ID, 0, t0)
			return err
		}); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("return before check-out: %v", err)
		}
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			_, err := e.s.CheckOutTx(ctx, tx, scope, actor, b.ID, b.Version+1, t0)
			return err
		}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale check-out: %v", err)
		}
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			got, err := e.s.CheckOutTx(ctx, tx, scope, actor, b.ID, b.Version, t0)
			if err == nil && (got.Status != store.RentalStatusCheckedOut || !got.CheckedOutAt.Equal(t0)) {
				t.Errorf("checked out %+v", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(e.Events(t, event.RentalCheckedOut)) != 1 {
			t.Fatal("checked_out event")
		}
		// Extension to 14:00 reallocates and reprices.
		x, err := e.transition(b.ID, TransitionInput{Action: ActionExtend, End: &slots.Point{Date: d("2027-06-10"), Slot: 28}})
		if err != nil || x.EndSlot != 28 || x.TotalCents != 8*1500 {
			t.Fatalf("extend: %v", err)
		}
		if u, sk := e.rows(b.ID); u != 9 || sk != 8 {
			t.Fatalf("extended rows %d %d", u, sk)
		}
		e.q32()
		// Early return at 11:10 Rome: slot_up = 23 (11:30); unit keeps 20-22 booked + 23 buffer, skipper keeps 20-22.
		ret := time.Date(2027, 6, 10, 9, 10, 0, 0, time.UTC)
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			_, err := e.s.ReturnTx(ctx, tx, scope, actor, b.ID, x.Version, ret)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if u, sk := e.rows(b.ID); u != 4 || sk != 3 {
			t.Fatalf("early return rows %d %d", u, sk)
		}
		e.q32()
		// The released slots are bookable again (11:30 is the buffer, 12:00 is free).
		e.mustCreate(CreateInput{Range: r("2027-06-10", 24, "2027-06-10", 28)})
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			got, err := e.s.CloseTx(ctx, tx, scope, actor, b.ID)
			if err == nil && got.Status != store.RentalStatusClosed {
				t.Errorf("closed %+v", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		e.q32()
		// Late return keeps the rows.
		late := e.mustCreate(CreateInput{Range: r("2027-06-11", 20, "2027-06-11", 24)})
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			if _, err := e.s.CheckOutTx(ctx, tx, scope, actor, late.ID, 0, t0); err != nil {
				return err
			}
			_, err := e.s.ReturnTx(ctx, tx, scope, actor, late.ID, 0, time.Date(2027, 6, 11, 11, 0, 0, 0, time.UTC))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if u, _ := e.rows(late.ID); u != 5 {
			t.Fatalf("late return rows %d", u)
		}
		// Reprice and cancel seams.
		c := e.mustCreate(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24)})
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			res, err := e.s.RepriceTx(ctx, tx, scope, actor, c.ID)
			if err == nil && res.Delta != 0 {
				t.Errorf("delta %d", res.Delta)
			}
			if err != nil {
				return err
			}
			got, err := e.s.CancelTx(ctx, tx, scope, actor, c.ID, "meteo")
			if err == nil && got.Status != store.RentalStatusCancelled {
				t.Errorf("cancelled %+v", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(e.Events(t, event.RentalReturned)) != 2 || len(e.Events(t, event.RentalCancelled)) != 1 {
			t.Fatal("events")
		}
		e.q32()
	})
}

func TestReservation_PlannerDayAvailability(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 3, 1)
		ctx := fixture.Ctx()
		pick := e.mustCreate(CreateInput{Range: r("2027-06-10", 20, "2027-06-10", 24), Renter: &Renter{Name: "Bruno Verdi"}})
		e.mustCreate(CreateInput{UnitID: e.units[1], FormulaID: e.multi, SkipperID: e.skippers[0], Range: r("2027-06-10", 18, "2027-06-12", 38)})
		hold := e.mustCreate(CreateInput{UnitID: e.units[2], Range: r("2027-06-11", 20, "2027-06-11", 24), Hold: true})
		e.setUnitStatus(e.units[2], store.RentalUnitRetired)
		p, err := e.s.Planner(ctx, e.Owner, e.MarinaID, PlannerQuery{From: d("2027-06-10"), To: d("2027-06-12"), View: ViewHours})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Rows) != 3 || p.Rows[0].Code != "GOM0" || p.Rows[2].Kind != "skipper" {
			t.Fatalf("rows %+v", p.Rows)
		}
		segs := p.Rows[0].Segments
		if len(segs) != 2 || segs[0].Kind != store.RentalSlotBooked || segs[0].From.Slot != 20 || segs[0].To.Slot != 24 ||
			segs[1].Kind != store.RentalSlotBuffer || segs[0].Number != "NL2027-000001" || segs[0].Status != store.RentalStatusConfirmed {
			t.Fatalf("unit 0 segments %+v", segs)
		}
		// The multi-day booking is one run across midnight, then its buffer.
		m := p.Rows[1].Segments
		if len(m) != 2 || m[0].From.Date.String() != "2027-06-10" || m[0].To.Date.String() != "2027-06-12" || m[0].To.Slot != 38 {
			t.Fatalf("multi-day segments %+v", m)
		}
		if sk := p.Rows[2].Segments; len(sk) != 1 || sk[0].To.Slot != 38 {
			t.Fatalf("skipper segments %+v", sk)
		}
		if _, err := e.s.Planner(ctx, e.Owner, e.MarinaID, PlannerQuery{}); err == nil {
			t.Fatal("missing window accepted")
		}
		// Availability: unit 0 busy 10:00-12:00, unit 1 busy, unit 2 retired.
		av, err := e.s.Availability(ctx, e.Owner, e.MarinaID, AvailabilityQuery{Range: r("2027-06-10", 20, "2027-06-10", 24), Skippers: true})
		if err != nil || len(av.Units) != 0 || len(av.Skippers) != 0 {
			t.Fatalf("availability busy %+v %v", av, err)
		}
		av, err = e.s.Availability(ctx, e.Owner, e.MarinaID, AvailabilityQuery{Range: r("2027-06-10", 26, "2027-06-10", 30)})
		if err != nil || len(av.Units) != 1 || av.Units[0].Code != "GOM0" || !slices.Contains(av.Units[0].FormulaIDs, e.hourly) || slices.Contains(av.Units[0].FormulaIDs, e.day) {
			t.Fatalf("availability free %+v %v", av, err)
		}
		// Day view at 11:30 Rome: the 10:00 pickup is late (no_show_after 60 min).
		e.Clock.Set(time.Date(2027, 6, 10, 9, 30, 0, 0, time.UTC))
		day, err := e.s.Day(ctx, e.Owner, e.MarinaID, DayQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if day.Date.String() != "2027-06-10" || len(day.Pickups) != 2 || len(day.Out) != 0 {
			t.Fatalf("day %+v", day)
		}
		var first DayEntry
		for _, p := range day.Pickups {
			if p.BookingID == pick.ID {
				first = p
			}
		}
		if !first.Late || first.LateMinutes != 90 || first.RenterName != "Bruno Verdi" || first.Unpaid != 4000 {
			t.Fatalf("pickup %+v", first)
		}
		_ = hold
	})
}

func TestRental_StatusWritesOnlyInReservationCallers(t *testing.T) {
	// Covered by the source scan in status_writes_test.go; this integration check makes sure the store writer
	// refuses a stale version.
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 0)
		b := e.mustCreate(CreateInput{Range: r("2027-06-12", 20, "2027-06-12", 24)})
		row := b.RentalBooking
		row.Status = store.RentalStatusClosed
		err := store.WriteRentalBookingState(context.Background(), db.DB, e.scope, &row, b.Version+1, t0)
		if !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale write: %v", err)
		}
	})
}

// TestReservation_ExtendHalfDay: a checked-out half-day booking cannot grow within its own formula (its range is
// the band), so the extension names a formula that fits the new range; the snapshot, rows and formula follow.
func TestReservation_ExtendHalfDay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 0)
		actor := audit.Actor{Type: audit.ActorUser, ID: e.OwnerID}
		b := e.mustCreate(CreateInput{FormulaID: e.morning, Range: r("2027-06-10", 16, "2027-06-10", 26)})
		if err := e.withTx(func(ctx context.Context, tx bun.Tx) error {
			_, err := e.s.CheckOutTx(ctx, tx, e.scope, actor, b.ID, 0, t0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		end := &slots.Point{Date: d("2027-06-10"), Slot: 28}
		var re *slots.RangeError
		if _, err := e.transition(b.ID, TransitionInput{Action: ActionExtend, End: end}); !errors.As(err, &re) {
			t.Fatalf("extend within half_day: %v", err)
		}
		x, err := e.transition(b.ID, TransitionInput{Action: ActionExtend, End: end, FormulaID: e.hourly})
		if err != nil {
			t.Fatalf("extend with hourly: %v", err)
		}
		if x.FormulaID != e.hourly || x.EndSlot != 28 || x.TotalCents != 12*1000 || x.Status != store.RentalStatusCheckedOut {
			t.Fatalf("extended %+v", x.RentalBooking)
		}
		if u, _ := e.rows(b.ID); u != 13 {
			t.Fatalf("rows %d, want 12 + buffer", u)
		}
		e.q32()
	})
}
