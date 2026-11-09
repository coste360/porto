package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/rental/handover"
	"github.com/coste360/porto/internal/domain/rental/reservation"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// seedRentalHandovers adds the M11 demo of HANDOVER (m11-plan §16.2): the closed rentals of the last three days
// (seedPastRentals), then today's first four confirmed rentals are checked out (identity and licence typed with
// document photos, contract signed on screen, cash deposit, checklist, boat photos); the first two are out at sea,
// one comes back with a damage and a partially retained deposit, one comes back clean with its deposit released. Everything goes through the handover service as the demo owner; a no-op when the
// marina already has handovers or no confirmed rental today.
func (a *App) seedRentalHandovers(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.RentalHandover == nil || a.RentalBookings == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("rental_handovers").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
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
	rows, _, err := store.ListRentalBookings(ctx, a.DB.DB, scope, store.RentalBookingFilter{
		StartsOn: today, Status: store.RentalStatusConfirmed, Oldest: true,
	})
	if err != nil || len(rows) == 0 {
		return err
	}
	set, err := store.LoadRentalSettings(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	if a.Cash != nil {
		if _, err := a.Cash.OpenSession(ctx, p, res.MarinaID, "", 0); err != nil && !errors.Is(err, cash.ErrSessionOpen) {
			return fmt.Errorf("rental handover cash session: %w", err)
		}
	}
	m := res.MarinaID
	at := func(d calendar.Date, slot int) time.Time {
		return d.Time(loc).Add(time.Duration(slot) * 30 * time.Minute)
	}
	past, fake, err := a.seedClockApp(ctx, at(today.AddDays(-6), 18))
	if err != nil {
		return err
	}
	defer func() { _ = past.Close() }()
	if err := past.seedPastRentals(ctx, p, scope, today, loc, set, fake); err != nil {
		return err
	}
	// Today's handovers happen at the booked times (never after now), not at the moment the seed runs.
	now := a.Clock.Now()
	for i := range rows {
		if i > 3 {
			break
		}
		b := &rows[i]
		out, back := seedHandoverTimes(at(b.StartsOn, b.StartSlot), at(b.EndsOn, b.EndSlot), now)
		fake.Set(out)
		if err := past.seedCheckOut(ctx, p, m, b, set, i, store.RentalDepositCash); err != nil {
			var ve *auth.ValidationError
			if errors.As(err, &ve) {
				return fmt.Errorf("rental check-out %d: %w %v", i, err, ve.Fields)
			}
			return fmt.Errorf("rental check-out %d: %w", i, err)
		}
		if i < 2 {
			continue // out at sea: the motorboat late on purpose (due back at 09:30), the sailboat due back at 19:00
		}
		fake.Set(back)
		if err := past.seedCheckIn(ctx, p, m, b.ID, set, i == 2); err != nil {
			return fmt.Errorf("rental check-in %d: %w", i, err)
		}
	}
	return nil
}

// seedHandoverTimes are the check-out (ten minutes before the start) and return (five minutes before the end)
// moments of a demo rental, never after now and the return never before the check-out.
func seedHandoverTimes(start, end, now time.Time) (out, back time.Time) {
	out = start.Add(-10 * time.Minute)
	if out.After(now) {
		out = now
	}
	back = end.Add(-5 * time.Minute)
	if back.After(now) {
		back = now
	}
	if back.Before(out) {
		back = out
	}
	return out, back
}

// seedPastRentals replays the rentals of the last three days on a, an App whose clock (fake) walks through them: each
// is booked two days before, checked out at its start (card deposit on the POS) and back at its end, one with a
// damage; the deposits are released or retained, so the rentals end closed.
func (a *App) seedPastRentals(ctx context.Context, p *auth.Principal, scope store.MarinaScope, today calendar.Date,
	loc *time.Location, set *store.RentalSettings, fake *clock.Fake,
) error {
	units, err := store.AllRentalUnits(ctx, a.DB.DB, scope)
	if err != nil {
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
	at := func(d calendar.Date, slot int) time.Time {
		return d.Time(loc).Add(time.Duration(slot) * 30 * time.Minute)
	}
	m := scope.ID()
	for i, r := range []struct {
		day           int
		unit, formula string
		from, to      int
		damaged       bool
	}{
		{-3, SeedRentalUnitGOM58, SeedRentalDay, 16, 38, false},
		{-3, SeedRentalUnitSUP, SeedRentalHourly, 20, 24, false},
		{-3, SeedRentalUnitOPEN60, SeedRentalMorning, 16, 26, false},
		{-2, SeedRentalUnitGOM65, SeedRentalMorning, 16, 26, true},
		{-2, SeedRentalUnitOPEN, SeedRentalAfternoon, 28, 38, false},
		{-2, SeedRentalUnitSAIL32, SeedRentalDay, 16, 38, false},
		{-1, SeedRentalUnitSAIL, SeedRentalDay, 16, 38, false},
		{-1, SeedRentalUnitGOM50, SeedRentalHourly, 22, 26, false},
		{-1, SeedRentalUnitSUP2, SeedRentalHourly, 28, 32, false},
	} {
		if unit[r.unit] == "" || formula[r.formula] == "" {
			continue
		}
		day := today.AddDays(r.day)
		fake.Set(at(day.AddDays(-2), 36))
		b, err := a.RentalBookings.Create(ctx, p, m, reservation.CreateInput{
			UnitID: unit[r.unit], FormulaID: formula[r.formula], AllowOutsideHours: true,
			Range:  slots.Range{Start: slots.Point{Date: day, Slot: r.from}, End: slots.Point{Date: day, Slot: r.to}},
			Renter: &reservation.Renter{Name: seedRenters[(23+i)%len(seedRenters)], Phone: fmt.Sprintf("+39 000 00001%02d", i)},
		})
		if err != nil {
			return fmt.Errorf("past rental %d: %w", i, err)
		}
		row, err := store.GetRentalBooking(ctx, a.DB.DB, scope, b.ID)
		if err != nil {
			return err
		}
		fake.Set(at(day, r.from).Add(-10 * time.Minute))
		if err := a.seedCheckOut(ctx, p, m, row, set, 10+i, store.RentalDepositCardPOS); err != nil {
			return fmt.Errorf("past rental check-out %d: %w", i, err)
		}
		fake.Set(at(day, r.to).Add(-5 * time.Minute))
		if err := a.seedCheckIn(ctx, p, m, b.ID, set, r.damaged); err != nil {
			return fmt.Errorf("past rental check-in %d: %w", i, err)
		}
		if cur, err := store.GetRentalBooking(ctx, a.DB.DB, scope, b.ID); err == nil && cur.Status == store.RentalStatusReturned {
			if _, err := a.RentalHandover.Close(ctx, p, m, b.ID, cur.Version); err != nil {
				return fmt.Errorf("past rental close %d: %w", i, err)
			}
		}
	}
	return nil
}

// seedPhoto is a small generated PNG (distinct bytes per shade).
func seedPhoto(shade uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for x := range 24 {
		for y := range 16 {
			img.Set(x, y, color.RGBA{R: shade, G: 120, B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func seedChecklist(set *store.RentalSettings, category string, okAll bool) []handover.ChecklistEntry {
	var list []store.RentalChecklistItem
	_ = json.Unmarshal(set.Checklist, &list)
	out := []handover.ChecklistEntry{}
	for _, it := range list {
		if len(it.Categories) > 0 && !slices.Contains(it.Categories, category) {
			continue
		}
		e := handover.ChecklistEntry{Key: it.Key, OK: true}
		if !okAll && it.Key == "hull" {
			e.OK, e.Note = false, "Graffio sul tubolare di dritta"
		}
		out = append(out, e)
	}
	return out
}

func (a *App) seedCheckOut(ctx context.Context, p *auth.Principal, m string, b *store.RentalBooking, set *store.RentalSettings, i int, deposit string) error {
	h := a.RentalHandover
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, m)
	if err != nil {
		return err
	}
	unit, err := store.GetRentalUnit(ctx, a.DB.DB, scope, b.UnitID)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), time.UTC)
	if _, err := h.PutIdentity(ctx, p, m, b.ID, handover.IdentityInput{
		DocumentType: "identity_card", DocumentNumber: fmt.Sprintf("CA%05dDM", 10000+i), DocumentExpiresOn: today.AddDays(1500),
		LicenceNumber: fmt.Sprintf("LN-DEMO-%03d", i), LicenceCategory: "Entro 12 miglia", LicenceExpiresOn: today.AddDays(1500),
	}); err != nil {
		return err
	}
	kinds := []string{store.RentalFileIDDocument, store.RentalFileLicence}
	for range max(set.CheckoutMinPhotos, 1) {
		kinds = append(kinds, store.RentalFileBoat)
	}
	for k, kind := range kinds {
		if _, err := h.UploadPhoto(ctx, p, m, b.ID, store.RentalPhaseCheckout, handover.PhotoInput{
			Kind: kind, ContentType: "image/png", Body: seedPhoto(uint8(20*i + k)),
		}); err != nil {
			return err
		}
	}
	if _, err := h.SignContract(ctx, p, m, b.ID, handover.SignInput{
		Driver: handover.SignScreen, PNG: seedPhoto(uint8(200 + i)), DeviceLabel: "Telefono pontile",
	}); err != nil {
		return err
	}
	if unit.DepositCents > 0 {
		if _, err := h.StartDeposit(ctx, p, m, b.ID, handover.DepositInput{Mode: deposit}); err != nil {
			return err
		}
	}
	fuel := 8
	hours := qty.Milli(unit.EngineHoursMilli + 500)
	if _, err := h.SaveChecklist(ctx, p, m, b.ID, store.RentalPhaseCheckout, handover.ChecklistInput{
		ClientRef: "seed-co-" + b.ID[len(b.ID)-12:], Items: seedChecklist(set, unit.Category, true), FuelEighths: &fuel, EngineHours: &hours,
	}); err != nil {
		return err
	}
	cur, err := store.GetRentalBooking(ctx, a.DB.DB, scope, b.ID)
	if err != nil {
		return err
	}
	_, err = h.CheckOut(ctx, p, m, b.ID, cur.Version, handover.CheckOutInput{})
	return err
}

func (a *App) seedCheckIn(ctx context.Context, p *auth.Principal, m, bookingID string, set *store.RentalSettings, damaged bool) error {
	h := a.RentalHandover
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, m)
	if err != nil {
		return err
	}
	b, err := store.GetRentalBooking(ctx, a.DB.DB, scope, bookingID)
	if err != nil {
		return err
	}
	unit, err := store.GetRentalUnit(ctx, a.DB.DB, scope, b.UnitID)
	if err != nil {
		return err
	}
	fuel := 6
	hours := qty.Milli(unit.EngineHoursMilli + 3500)
	if _, err := h.SaveChecklist(ctx, p, m, b.ID, store.RentalPhaseCheckin, handover.ChecklistInput{
		ClientRef: "seed-ci-" + b.ID[len(b.ID)-12:], Items: seedChecklist(set, unit.Category, !damaged), FuelEighths: &fuel, EngineHours: &hours,
	}); err != nil {
		return err
	}
	for k := range max(set.CheckinMinPhotos, 1) {
		if _, err := h.UploadPhoto(ctx, p, m, b.ID, store.RentalPhaseCheckin, handover.PhotoInput{
			Kind: store.RentalFileBoat, ContentType: "image/png", Body: seedPhoto(uint8(150 + k)),
		}); err != nil {
			return err
		}
	}
	in := handover.CheckInInput{ReturnedAt: a.Clock.Now(), DepositDecision: handover.DecisionRelease}
	if damaged && b.DepositCents > 0 {
		estimate := min(money.Cents(8000), money.Cents(b.DepositCents))
		d, err := h.CreateDamage(ctx, p, m, b.ID, handover.DamageInput{Description: "Graffio sul tubolare di dritta", Estimate: estimate})
		if err != nil {
			return err
		}
		if _, err := h.UploadPhoto(ctx, p, m, b.ID, store.RentalPhaseCheckin, handover.PhotoInput{
			Kind: store.RentalFileDamage, DamageID: d.ID, ContentType: "image/png", Body: seedPhoto(177),
		}); err != nil {
			return err
		}
		in.DepositDecision, in.Retain = handover.DecisionRetain, estimate
	}
	_, err = h.CheckIn(ctx, p, m, b.ID, b.Version, in)
	return err
}
