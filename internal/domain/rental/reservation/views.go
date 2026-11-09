package reservation

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Fiscal modes of the rental charges.
const (
	FiscalReceipt = "receipt"
	FiscalInvoice = "invoice"
)

// views builds the views of booking rows (unit and formula codes, instants, renter, snapshot, extras, charges).
func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.RentalBooking) ([]BookingView, error) {
	out := make([]BookingView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	loc := location(ctx, db, scope)
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	extras, err := store.ListRentalBookingExtras(ctx, db, scope, ids...)
	if err != nil {
		return nil, err
	}
	charges, err := store.ListRentalChargeRows(ctx, db, scope, ids...)
	if err != nil {
		return nil, err
	}
	units := map[string]*store.RentalUnit{}
	formulas := map[string]*store.RentalFormula{}
	extraItems := map[string]string{}
	for i := range rows {
		b := rows[i]
		v := BookingView{RentalBooking: b, FiscalMode: FiscalReceipt}
		if b.InvoiceRequested {
			v.FiscalMode = FiscalInvoice
		}
		if _, ok := units[b.UnitID]; !ok {
			if u, err := store.GetRentalUnit(ctx, db, scope, b.UnitID); err == nil {
				units[b.UnitID] = u
			} else {
				units[b.UnitID] = nil
			}
		}
		if u := units[b.UnitID]; u != nil {
			v.UnitCode = u.Code
		}
		if _, ok := formulas[b.FormulaID]; !ok {
			if f, err := store.GetRentalFormula(ctx, db, scope, b.FormulaID); err == nil {
				formulas[b.FormulaID] = f
			} else {
				formulas[b.FormulaID] = nil
			}
		}
		if f := formulas[b.FormulaID]; f != nil {
			v.FormulaCode, v.Kind = f.Code, f.Kind
		}
		v.StartsAt = slots.Instant(b.StartsOn, b.StartSlot, loc).UTC()
		v.EndsAt = slots.Instant(b.EndsOn, b.EndSlot, loc).UTC()
		v.Renter = s.decryptRenter(b.ID, b.RenterEnc)
		_ = decodeJSON(b.PriceSnapshot, &v.Snapshot)
		if v.Kind == "" {
			v.Kind = string(v.Snapshot.Kind)
		}
		v.LicenceRequired = v.Snapshot.LicenceRequired
		if b.DepositIntentID != "" && b.DepositStatus == store.RentalDepositStatusPending {
			v.CheckoutURL, v.DepositExpiresAt, _ = store.RentalIntentCheckout(ctx, db, scope, b.DepositIntentID)
		}
		v.Extras, v.ExtraItems = []store.RentalBookingExtra{}, map[string]string{}
		for _, x := range extras {
			if x.BookingID == b.ID {
				v.Extras = append(v.Extras, x)
				if _, ok := extraItems[x.ExtraID]; !ok {
					if row, err := store.GetRentalExtra(ctx, db, scope, x.ExtraID); err == nil {
						extraItems[x.ExtraID] = row.ItemID
					}
				}
				v.ExtraItems[x.ExtraID] = extraItems[x.ExtraID]
			}
		}
		v.Charges = []ChargeRef{}
		for _, c := range charges {
			if c.BookingID == b.ID {
				v.Charges = append(v.Charges, ChargeRef{
					ChargeID: c.ChargeID, Kind: c.Kind, Status: c.PaymentStatus, Total: money.Cents(c.TotalCents),
					Paid: money.Cents(c.PaidAmountCents), FiscalMode: c.FiscalMode,
				})
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// unpaid returns the residual of the live charges of a booking (main, return, adjustments), or the snapshot total
// while no main charge exists (confirmed and later).
func unpaid(v *BookingView) money.Cents {
	var total money.Cents
	hasMain := false
	for _, c := range v.Charges {
		if c.Status == store.ChargeCancelled || c.Kind == store.RentalChargeReplaced {
			continue
		}
		if c.Kind == store.RentalChargeMain {
			hasMain = true
		}
		if r := c.Total - c.Paid; r > 0 {
			total += r
		}
	}
	if !hasMain && v.Status != store.RentalStatusHeld {
		total += money.Cents(v.TotalCents)
	}
	return total
}

// Planner returns the planner rows and segments of a window.
func (s *Service) Planner(ctx context.Context, p *auth.Principal, marinaID string, q PlannerQuery) (*Planner, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if q.From.IsZero() {
		v.Add("from", "required")
	}
	if q.To.IsZero() {
		v.Add("to", "required")
	}
	if !q.From.IsZero() && !q.To.IsZero() && (q.To.Before(q.From) || q.From.DaysUntil(q.To) > 62) {
		v.Add("to", "out_of_range")
	}
	switch q.View {
	case "":
		q.View = ViewDays
	case ViewHours, ViewHalfDays, ViewDays:
	default:
		v.Add("view", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	settings, err := settingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	units, err := store.ListRentalUnitsForPlanner(ctx, db, scope, q.Category, false)
	if err != nil {
		return nil, err
	}
	skippers, err := store.ListRentalSkippersForPlanner(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	unitRows, err := store.ListRentalWindowRows(ctx, db, scope, false, q.From, q.To)
	if err != nil {
		return nil, err
	}
	skRows, err := store.ListRentalWindowRows(ctx, db, scope, true, q.From, q.To)
	if err != nil {
		return nil, err
	}
	out := &Planner{From: q.From, To: q.To, View: q.View, Settings: settings, Rows: []PlannerRow{}}
	byUnit, bySkipper := groupRows(unitRows), groupRows(skRows)
	for _, u := range units {
		out.Rows = append(out.Rows, PlannerRow{
			Kind: "unit", ID: u.ID, Code: u.Code, Category: u.Category, Status: u.Status, Seats: u.Seats,
			Segments: Segments(byUnit[u.ID]),
		})
	}
	for _, sk := range skippers {
		out.Rows = append(out.Rows, PlannerRow{Kind: "skipper", ID: sk.ID, Code: sk.Name, Segments: Segments(bySkipper[sk.ID])})
	}
	return out, nil
}

func groupRows(rows []store.RentalWindowRow) map[string][]store.RentalWindowRow {
	out := map[string][]store.RentalWindowRow{}
	for _, r := range rows {
		out[r.ResourceID] = append(out[r.ResourceID], r)
	}
	return out
}

// Segments turns the ordered rows of one resource into runs of contiguous slots of the same owner and kind.
func Segments(rows []store.RentalWindowRow) []Segment {
	out := []Segment{}
	for i := 0; i < len(rows); {
		r := rows[i]
		j := i + 1
		for j < len(rows) {
			p, c := rows[j-1], rows[j]
			contiguous := (p.Date.Equal(c.Date) && p.Slot+1 == c.Slot) ||
				(p.Slot == slots.PerDay-1 && c.Slot == 0 && p.Date.AddDays(1).Equal(c.Date))
			if !contiguous || c.BookingID != r.BookingID || c.BlockID != r.BlockID || c.Kind != r.Kind {
				break
			}
			j++
		}
		last := rows[j-1]
		seg := Segment{
			BookingID: r.BookingID, BlockID: r.BlockID, Kind: r.Kind, Status: r.Status, Number: Number(r.Year, r.Number),
			Reason: r.Reason, From: slots.Point{Date: r.Date, Slot: r.Slot}, To: slots.Point{Date: last.Date, Slot: last.Slot + 1},
		}
		out = append(out, seg)
		i = j
	}
	return out
}

// freeFor reports whether keys are free on a resource, ignoring expired holds (a pre-check only: the slot keys
// decide at booking time).
func freeFor(owners []store.RentalSlotOwner, keys []slots.Key, now time.Time) bool {
	want := make(map[slots.Key]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	for _, o := range owners {
		if !want[slots.Key{Date: o.Date, Slot: o.Slot}] {
			continue
		}
		if !o.ExpiresAt.IsZero() && o.ExpiresAt.Before(now) {
			continue
		}
		return false
	}
	return true
}

// engineKWMilli sums the engine power of a vessel (kW in the registry, milli here).
func engineKWMilli(ctx context.Context, db bun.IDB, scope store.MarinaScope, vesselID string) int64 {
	raw, err := store.RentalVesselEnginesJSON(ctx, db, scope, vesselID)
	if err != nil {
		return 0
	}
	var engines []struct {
		PowerKW *int64 `json:"power_kw"`
	}
	_ = decodeJSON(raw, &engines)
	var total int64
	for _, e := range engines {
		if e.PowerKW != nil {
			total += *e.PowerKW * 1000
		}
	}
	return total
}

// Availability searches free units (and skippers) for a range.
func (s *Service) Availability(ctx context.Context, p *auth.Principal, marinaID string, q AvailabilityQuery) (*Availability, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if !slots.WellFormed(q.Range) {
		return nil, invalid("ends_on", "invalid")
	}
	db := s.d.DB.DB
	now := s.now()
	settings, err := settingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	formulas, err := store.ListRentalFormulasForBooking(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	if q.FormulaID != "" {
		var keep []store.RentalFormula
		for _, f := range formulas {
			if f.ID == q.FormulaID {
				keep = append(keep, f)
			}
		}
		if keep == nil {
			return nil, invalid("formula_id", "not_found")
		}
		formulas = keep
	}
	fitting := []store.RentalFormula{}
	for _, f := range formulas {
		if _, err := slots.ValidateRange(FormulaSlots(&f), settings.Slots(), q.Range); err == nil {
			fitting = append(fitting, f)
		}
	}
	units, err := store.ListRentalUnitsForPlanner(ctx, db, scope, q.Category, false)
	if err != nil {
		return nil, err
	}
	keys := slots.Expand(q.Range, settings.Row.BufferSlots)
	rangeKeys := keys[:slots.Count(q.Range)]
	d0, d1 := keys[0].Date, keys[len(keys)-1].Date
	out := &Availability{Units: []AvailableUnit{}, Skippers: []AvailableSkipper{}}
	for _, u := range units {
		if u.Status != store.RentalUnitAvailable {
			continue
		}
		au := AvailableUnit{UnitID: u.ID, Code: u.Code, Category: u.Category, Seats: u.Seats, FormulaIDs: []string{}}
		for i := range fitting {
			if formulaFits(&fitting[i], u.Category) {
				au.FormulaIDs = append(au.FormulaIDs, fitting[i].ID)
			}
		}
		if len(au.FormulaIDs) == 0 {
			continue
		}
		owners, err := store.RentalSlotOwners(ctx, db, scope, false, u.ID, d0, d1)
		if err != nil {
			return nil, err
		}
		if !freeFor(owners, keys, now) {
			continue
		}
		au.LicenceRequired = price.LicenceRequired(u.Licence, engineKWMilli(ctx, db, scope, u.VesselID),
			settings.Row.LicenceThresholdKWMilli, false)
		out.Units = append(out.Units, au)
	}
	if !q.Skippers {
		return out, nil
	}
	skippers, err := store.ListRentalSkippersForPlanner(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	for _, sk := range skippers {
		if !sk.LicenceExpiresOn.IsZero() && sk.LicenceExpiresOn.Before(q.Range.End.Date) {
			continue
		}
		owners, err := store.RentalSlotOwners(ctx, db, scope, true, sk.ID, d0, d1)
		if err != nil {
			return nil, err
		}
		if !freeFor(owners, rangeKeys, now) {
			continue
		}
		hours, err := store.ListRentalSkipperHours(ctx, db, scope, sk.ID)
		if err != nil {
			return nil, err
		}
		out.Skippers = append(out.Skippers, AvailableSkipper{SkipperID: sk.ID, Name: sk.Name, WithinHours: WithinHours(hours, q.Range)})
	}
	return out, nil
}

// Day returns the day view (zero date = today in the marina time zone): pickups of the date (held and confirmed,
// late after no_show_after_minutes), boats out, returns due on the date and late returns.
func (s *Service) Day(ctx context.Context, p *auth.Principal, marinaID string, q DayQuery) (*Day, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	now := s.now()
	loc := location(ctx, db, scope)
	date := q.Date
	if date.IsZero() {
		date = calendar.In(now, loc)
	}
	settings, err := settingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	pick, _, err := store.ListRentalBookings(ctx, db, scope, store.RentalBookingFilter{
		StartsOn: date, Statuses: []string{store.RentalStatusHeld, store.RentalStatusConfirmed}, Oldest: true,
	})
	if err != nil {
		return nil, err
	}
	out, _, err := store.ListRentalBookings(ctx, db, scope, store.RentalBookingFilter{
		Statuses: []string{store.RentalStatusCheckedOut}, Oldest: true,
	})
	if err != nil {
		return nil, err
	}
	all, err := s.views(ctx, db, scope, append(pick, out...))
	if err != nil {
		return nil, err
	}
	customers := map[string]string{}
	day := &Day{Date: date, Pickups: []DayEntry{}, Out: []DayEntry{}, ReturnsDue: []DayEntry{}, Late: []DayEntry{}}
	noShowAfter := time.Duration(settings.Row.NoShowAfterMinutes) * time.Minute
	for i := range all {
		v := &all[i]
		e := DayEntry{
			BookingID: v.ID, Number: Number(v.Year, v.Number), Status: v.Status, UnitID: v.UnitID, UnitCode: v.UnitCode,
			SkipperID: v.SkipperID, Range: v.Range(), StartsAt: v.StartsAt, EndsAt: v.EndsAt, DepositStatus: v.DepositStatus,
			Unpaid: unpaid(v), RenterName: s.renterName(ctx, db, scope, v, customers),
		}
		switch v.Status {
		case store.RentalStatusHeld, store.RentalStatusConfirmed:
			if now.After(v.StartsAt.Add(noShowAfter)) {
				e.Late, e.LateMinutes = true, int(now.Sub(v.StartsAt)/time.Minute)
			}
			day.Pickups = append(day.Pickups, e)
		case store.RentalStatusCheckedOut:
			if now.After(v.EndsAt) {
				e.Late, e.LateMinutes = true, int(now.Sub(v.EndsAt)/time.Minute)
			}
			day.Out = append(day.Out, e)
			if v.EndsOn.Equal(date) {
				day.ReturnsDue = append(day.ReturnsDue, e)
			}
			if e.Late {
				day.Late = append(day.Late, e)
			}
		}
	}
	return day, nil
}

// renterName returns the walk-in renter name or the customer display name (rental.manage only; never logged).
func (s *Service) renterName(ctx context.Context, db bun.IDB, scope store.MarinaScope, v *BookingView, cache map[string]string) string {
	if v.Renter != nil && v.Renter.Name != "" {
		return v.Renter.Name
	}
	if v.CustomerID == "" {
		return ""
	}
	if n, ok := cache[v.CustomerID]; ok {
		return n
	}
	name := ""
	if c, err := store.GetCustomer(ctx, db, scope, v.CustomerID); err == nil {
		name = strings.TrimSpace(c.CompanyName)
		if name == "" {
			name = strings.TrimSpace(c.FirstName + " " + c.LastName)
		}
	}
	cache[v.CustomerID] = name
	return name
}
