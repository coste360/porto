package app

import (
	"context"
	"fmt"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// seedRentalBookings adds the M11 demo of BOOKING (m11-plan §16.2): bookings over two weeks around today (held,
// confirmed, cancelled, no-show, one expired hold whose job has not run yet) and blocks of a unit and a skipper.
// Checked-out, returned and closed rentals come from the handover demo, which checks out some of today's
// confirmed bookings. Everything goes through the reservation service as the demo owner; a no-op when the marina
// already has rental bookings or no fleet.
func (a *App) seedRentalBookings(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.RentalBookings == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if rows, _, err := store.ListRentalBookings(ctx, a.DB.DB, scope, store.RentalBookingFilter{Limit: 1}); err != nil || len(rows) > 0 {
		return err
	}
	units, err := store.AllRentalUnits(ctx, a.DB.DB, scope)
	if err != nil || len(units) == 0 {
		return err
	}
	unit := map[string]string{}
	for _, u := range units {
		unit[u.Code] = u.ID
	}
	formulas, err := store.ListRentalFormulasForBooking(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	formula := map[string]string{}
	for _, f := range formulas {
		formula[f.Code] = f.ID
	}
	skippers, err := store.ListRentalSkippersForPlanner(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	skipper := func(i int) string {
		if i < len(skippers) {
			return skippers[i].ID
		}
		return ""
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), loc)
	m := res.MarinaID
	span := func(day, from, toDay, to int) slots.Range {
		return slots.Range{Start: slots.Point{Date: today.AddDays(day), Slot: from}, End: slots.Point{Date: today.AddDays(toDay), Slot: to}}
	}
	type demo struct {
		unit, formula string
		rng           slots.Range
		skipper       int // -1 none
		hold          bool
		renter        string
		extras        bool
		then          string // cancel, no_show
	}
	demos := []demo{
		{SeedRentalUnitGOM58, SeedRentalHourly, span(0, 20, 0, 24), -1, false, "Cliente Demo Uno", true, ""},
		{SeedRentalUnitOPEN, SeedRentalMorning, span(0, 16, 0, 26), 0, false, "Cliente Demo Due", false, ""},
		{SeedRentalUnitSUP, SeedRentalHourly, span(0, 30, 0, 34), -1, false, "Cliente Demo Tre", false, ""},
		{SeedRentalUnitGOM65, SeedRentalDay, span(1, 16, 1, 38), -1, false, "Cliente Demo Quattro", false, ""},
		{SeedRentalUnitGOM58, SeedRentalAfternoon, span(1, 28, 1, 38), -1, true, "Cliente Demo Cinque", false, ""},
		{SeedRentalUnitSAIL, SeedRentalMultiDay, span(2, 18, 4, 36), 1, false, "Cliente Demo Sei", true, ""},
		{SeedRentalUnitGOM58, SeedRentalHourly, span(5, 22, 5, 28), -1, true, "Cliente Demo Sette", false, ""},
		{SeedRentalUnitGOM58, SeedRentalWeek, span(7, 16, 13, 38), -1, false, "Cliente Demo Otto", false, ""},
		{SeedRentalUnitOPEN, SeedRentalHourly, span(3, 20, 3, 24), -1, false, "Cliente Demo Nove", false, "cancel"},
		{SeedRentalUnitGOM65, SeedRentalHourly, span(-1, 20, -1, 24), -1, false, "Cliente Demo Dieci", false, "no_show"},
	}
	extras, err := a.RentalFleet.ListExtras(ctx, p, m, nil)
	if err != nil {
		return err
	}
	for i, d := range demos {
		in := reservation.CreateInput{
			UnitID: unit[d.unit], FormulaID: formula[d.formula], Range: d.rng, Hold: d.hold, AllowOutsideHours: true,
			Renter: &reservation.Renter{Name: d.renter, Phone: fmt.Sprintf("+39 000 00000%02d", i)},
		}
		if in.UnitID == "" || in.FormulaID == "" {
			continue
		}
		if d.skipper >= 0 {
			in.SkipperID = skipper(d.skipper)
		}
		if d.extras && len(extras) > 0 {
			in.Extras = []fleet.ExtraQty{{ExtraID: extras[0].ID, Qty: qty.One}}
		}
		b, err := a.RentalBookings.Create(ctx, p, m, in)
		if err != nil {
			return fmt.Errorf("rental booking %d: %w", i, err)
		}
		switch d.then {
		case "cancel":
			_, err = a.RentalBookings.Transition(ctx, p, m, b.ID, b.Version, reservation.TransitionInput{Action: reservation.ActionCancel, Reason: "Meteo avverso"})
		case "no_show":
			_, err = a.RentalBookings.Transition(ctx, p, m, b.ID, b.Version, reservation.TransitionInput{Action: reservation.ActionNoShow})
		}
		if err != nil {
			return fmt.Errorf("rental booking %d %s: %w", i, d.then, err)
		}
	}
	// An expired desk option: created two hours ago with the default 60-minute hold; the holds job cancels it.
	past := a.RentalBookings.WithClock(clock.NewFake(a.Clock.Now().Add(-2 * time.Hour)))
	if _, err := past.Create(ctx, p, m, reservation.CreateInput{
		UnitID: unit[SeedRentalUnitGOM65], FormulaID: formula[SeedRentalHourly], Range: span(6, 20, 6, 24), Hold: true,
		Renter: &reservation.Renter{Name: "Cliente Demo Opzione"},
	}); err != nil {
		return fmt.Errorf("rental expired hold: %w", err)
	}
	// Blocks: the SUP in maintenance for two days, the first skipper's day off.
	if _, err := a.RentalBookings.CreateBlock(ctx, p, m, reservation.BlockInput{
		UnitID: unit[SeedRentalUnitSUP], Range: span(8, 0, 9, 48), Reason: store.RentalBlockMaintenance, Note: "Revisione",
	}); err != nil {
		return fmt.Errorf("rental unit block: %w", err)
	}
	if sk := skipper(0); sk != "" {
		if _, err := a.RentalBookings.CreateBlock(ctx, p, m, reservation.BlockInput{
			SkipperID: sk, Range: span(10, 0, 10, 48), Reason: store.RentalBlockDayOff,
		}); err != nil {
			return fmt.Errorf("rental skipper block: %w", err)
		}
	}
	return nil
}
