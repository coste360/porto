package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

// Demo occupancy (m3-plan §10): annual, seasonal and monthly contracts, transits around today with presences,
// releases (some resold), blocks, a closure, vessels out, quotes and inquiries, all through the domain services
// as the demo owner. Expected refusals (a taken berth, a vessel that does not fit, a stay without price) skip that
// item: the demo is built from what the engine accepts, so the invariants hold by construction.

// seedSkippable reports the refusals the seed tolerates.
func seedSkippable(err error) bool {
	return errors.Is(err, occupancy.ErrResourceTaken) || errors.Is(err, occupancy.ErrFitBlocked) ||
		errors.Is(err, occupancy.ErrPlacementInvalid) || errors.Is(err, occupancy.ErrClosed) ||
		errors.Is(err, pricing.ErrNoPrice) || errors.Is(err, pricelist.ErrNoActiveList) ||
		errors.Is(err, contract.ErrPriceListChanged) || errors.Is(err, contract.ErrInvalidTransition) ||
		errors.Is(err, presence.ErrInvalidTransition) || errors.Is(err, occupancy.ErrReleaseInvalid) ||
		errors.Is(err, auth.ErrInvalidInput)
}

var seedGuestNames = []string{
	"Aurora", "Nausicaa", "Calypso", "Tramontana", "Gabbiano", "Delfino", "Sirena", "Stella Maris", "Zefiro", "Ondina",
	"Orca", "Pellicano", "Ariel", "Blue Moon", "Lady M", "Fortuna", "Kalimera", "Mistral", "Poseidon", "Vento",
}

type seedOcc struct {
	a      *App
	ctx    context.Context
	p      *auth.Principal
	m      string
	rng    *rand.Rand
	today  calendar.Date
	yearTo calendar.Date // last night priced by the active list
	res    *SeedResult
	guest  string
}

func (s *seedOcc) skip(err error) error {
	if err == nil || seedSkippable(err) {
		return nil
	}
	return err
}

func (a *App) seedOccupancy(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := store.CountContracts(ctx, a.DB.DB, scope); err != nil || n > 0 {
		return err
	}
	if a.Contracts == nil || a.Occupancy == nil || a.Presence == nil {
		return nil
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
	s := &seedOcc{
		a: a, ctx: ctx, p: p, m: res.MarinaID, res: res, today: today,
		rng:    rand.New(rand.NewPCG(31, 4)), //nolint:gosec // demo data: deterministic by design
		yearTo: calendar.New(today.Year(), time.December, 31),
	}
	berths, areas, lands, err := store.SeedResources(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	vessels, err := store.AllVessels(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	owners, err := s.owners(scope, vessels)
	if err != nil {
		return err
	}
	if len(berths) == 0 || len(vessels) == 0 {
		return nil
	}
	return s.run(scope, berths, areas, lands, vessels, owners)
}

// owners maps vessels to a customer (the owner party, else the first party).
func (s *seedOcc) owners(scope store.MarinaScope, vessels []store.Vessel) (map[string]string, error) {
	ids := make([]string, 0, len(vessels))
	for _, v := range vessels {
		ids = append(ids, v.ID)
	}
	parties, err := store.PartiesOfVessels(s.ctx, s.a.DB.DB, scope, ids)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, pt := range parties {
		if _, ok := out[pt.VesselID]; !ok || pt.Role == "owner" {
			out[pt.VesselID] = pt.CustomerID
		}
	}
	return out, nil
}

func fits(v *store.Vessel, b *store.Berth, slack int64) bool {
	return v.LOACm <= b.MaxLOACm+slack && v.BeamCm <= b.MaxBeamCm && (v.LOACm > b.MaxLOACm) == (slack > 0)
}

func (s *seedOcc) run(scope store.MarinaScope, berths []store.Berth, areas []store.DynamicArea, lands []store.LandResource,
	vessels []store.Vessel, owners map[string]string,
) error {
	y := s.today.Year()
	jan1, nextJan1 := calendar.New(y, time.January, 1), calendar.New(y+1, time.January, 1)
	order := s.rng.Perm(len(berths))
	usedVessel := map[string]bool{}
	usedBerth := map[string]bool{}
	var parkings []string
	for _, l := range lands {
		if l.Kind == "parking" {
			parkings = append(parkings, l.ID)
		}
	}
	var annual []*contract.Detail
	overrides := 0
	misfit := map[string]int{} // vessel id → fit refusals (multihulls on berths without the tag, depths)
	pick := func(b *store.Berth, slack int64, tried map[string]bool) *store.Vessel {
		for i := range vessels {
			v := &vessels[i]
			if !usedVessel[v.ID] && !tried[v.ID] && misfit[v.ID] < 3 && owners[v.ID] != "" && fits(v, b, slack) {
				return v
			}
		}
		return nil
	}
	mooring := func(b *store.Berth, v *store.Vessel, term string, from, to calendar.Date, extras []contract.ExtraResource, override bool) (*contract.Detail, error) {
		in := contract.MooringInput{
			Term: term, CustomerID: owners[v.ID], VesselID: v.ID, StartsOn: from, EndsOn: &to, Activate: true,
			Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: b.ID, First: from, Last: to.AddDays(-1)}},
			Extras:     extras,
		}
		if override {
			in.FitOverride = &contract.FitOverrideInput{Reason: "misure in deroga"}
		}
		c, err := s.a.Contracts.CreateContract(s.ctx, s.p, s.m, in)
		if err != nil {
			if errors.Is(err, occupancy.ErrFitBlocked) {
				misfit[v.ID]++
			}
			return nil, s.skip(err)
		}
		usedVessel[v.ID], usedBerth[b.ID] = true, true
		return c, nil
	}

	// ~200 annual contracts (3 with a recorded fit override), 30 with a parking.
	for _, i := range order {
		if len(annual) >= 200 {
			break
		}
		b := &berths[i]
		tried := map[string]bool{}
		var c *contract.Detail
		override := false
		for attempt := 0; attempt < 6 && c == nil; attempt++ {
			override = overrides < 3 && len(annual)%60 == 59 && attempt == 0
			var v *store.Vessel
			if override {
				v = pick(b, 50, tried)
			}
			if v == nil {
				override = false
				v = pick(b, 0, tried)
			}
			if v == nil {
				break
			}
			tried[v.ID] = true
			var extras []contract.ExtraResource
			if len(annual) < 30 && len(annual) < len(parkings) {
				extras = []contract.ExtraResource{{LandResourceID: parkings[len(annual)]}}
			}
			var err error
			if c, err = mooring(b, v, contract.TermAnnual, jan1, nextJan1, extras, override); err != nil {
				return fmt.Errorf("annual contract: %w", err)
			}
		}
		if c == nil {
			continue
		}
		if override {
			overrides++
		}
		annual = append(annual, c)
		s.res.Contracts++
	}
	// 12 seasonal and 6 monthly around today.
	free := func() []*store.Berth {
		var out []*store.Berth
		for _, i := range order {
			if !usedBerth[berths[i].ID] {
				out = append(out, &berths[i])
			}
		}
		return out
	}
	seasonal, monthly := 0, 0
	for _, b := range free() {
		if seasonal >= 12 && monthly >= 6 {
			break
		}
		v := pick(b, 0, nil)
		if v == nil {
			continue
		}
		var c *contract.Detail
		var err error
		if seasonal < 12 {
			c, err = mooring(b, v, contract.TermSeasonal, calendar.New(y, time.May, 1), calendar.New(y, time.October, 1), nil, false)
			if c != nil {
				seasonal++
			}
		} else {
			start := s.today.AddDays(-10)
			end, _ := contract.DefaultEndsOn(contract.TermMonthly, start)
			c, err = mooring(b, v, contract.TermMonthly, start, end, nil, false)
			if c != nil {
				monthly++
			}
		}
		if err != nil {
			return fmt.Errorf("seasonal/monthly contract: %w", err)
		}
		if c != nil {
			s.res.Contracts++
		}
	}
	// Two contracts "posto in area" on the dynamic pier.
	if len(areas) > 0 {
		ar := areas[0]
		from := int64(0)
		if ar.FromCm != nil {
			from = *ar.FromCm
		}
		for k, off := range []int64{100, 1500} {
			for i := range vessels {
				v := &vessels[i]
				if usedVessel[v.ID] || owners[v.ID] == "" || v.LOACm > 1400 {
					continue
				}
				x := (from + off + int64(k)*10) / 10 * 10
				c, err := s.a.Contracts.CreateContract(s.ctx, s.p, s.m, contract.MooringInput{
					Term: contract.TermAnnual, CustomerID: owners[v.ID], VesselID: v.ID, StartsOn: jan1, EndsOn: &nextJan1, Activate: true,
					Placements: []contract.Segment{{ResourceType: store.ResourceArea, ResourceID: ar.ID, X: &x, First: jan1, Last: nextJan1.AddDays(-1)}},
				})
				if err := s.skip(err); err != nil {
					return fmt.Errorf("area contract: %w", err)
				}
				if c != nil {
					usedVessel[v.ID] = true
					s.res.Contracts++
				}
				break
			}
		}
	}
	if err := s.transits(free()); err != nil {
		return err
	}
	if err := s.releases(annual); err != nil {
		return err
	}
	if err := s.extras(free(), annual, vessels, owners, usedVessel); err != nil {
		return err
	}
	return s.count(scope)
}

// transits creates ~140 transits within today−20..today+30 (clipped to the priced year) on free berths with
// declared guest vessels: departed with presences, arrived, confirmed upcoming, 6 held, 3 cancelled, 2 no-show.
func (s *seedOcc) transits(free []*store.Berth) error {
	if len(free) == 0 {
		return nil
	}
	first, last := s.today.AddDays(-20), s.today.AddDays(30)
	if last.After(s.yearTo) {
		last = s.yearTo
	}
	if first.Year() != s.today.Year() {
		first = calendar.New(s.today.Year(), time.January, 1)
	}
	span := first.DaysUntil(last)
	if span < 2 {
		return nil
	}
	held, cancelled, noShow := 0, 0, 0
	for i := 0; i < 400 && s.res.Transits < 140; i++ {
		nights := 1 + s.rng.IntN(5)
		arr := first.AddDays(s.rng.IntN(span - 1))
		dep := arr.AddDays(nights)
		if dep.After(last.AddDays(1)) {
			dep = last.AddDays(1)
		}
		if !dep.After(arr) {
			continue
		}
		b := free[s.rng.IntN(len(free))]
		loa := min(b.MaxLOACm, 600+int64(s.rng.IntN(1400)))
		beam := min(b.MaxBeamCm, max(200, loa/3))
		in := contract.TransitInput{
			NewVessel: &contract.NewVessel{
				Name: fmt.Sprintf("%s %d", seedGuestNames[i%len(seedGuestNames)], i+1), LOACm: loa, BeamCm: beam, HullType: "monohull_sail",
			},
			CustomerID: s.firstCustomer(), Arrival: arr, Departure: dep,
			Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: b.ID, First: arr, Last: dep.AddDays(-1)}},
		}
		future := arr.After(s.today)
		switch {
		case future && held < 6:
			held++
		default:
			in.Confirm = &contract.ManualConfirmation{Reference: "demo"}
		}
		c, err := s.a.Contracts.CreateTransit(s.ctx, s.p, s.m, in)
		if err := s.skip(err); err != nil {
			return fmt.Errorf("transit: %w", err)
		}
		if c == nil {
			continue
		}
		s.res.Transits++
		if in.Confirm == nil {
			continue
		}
		switch {
		case future && cancelled < 3:
			_, err = s.a.Contracts.CancelTransit(s.ctx, s.p, s.m, c.Row.ID, c.Row.Version, "")
			cancelled++
		case !future && noShow < 2:
			_, err = s.a.Contracts.MarkNoShow(s.ctx, s.p, s.m, c.Row.ID, c.Row.Version)
			noShow++
		case !future:
			err = s.arriveDepart(c, arr, dep)
		}
		if err := s.skip(err); err != nil {
			return fmt.Errorf("transit state: %w", err)
		}
	}
	return nil
}

// firstCustomer is the demo customer guest transits are billed to ("" when the registry is empty).
func (s *seedOcc) firstCustomer() string {
	if s.guest != "" {
		return s.guest
	}
	scope, err := store.ResolveMarinaScope(s.ctx, s.a.DB.DB, s.m)
	if err != nil {
		return ""
	}
	page, err := store.ListCustomers(s.ctx, s.a.DB.DB, scope, store.CustomerFilter{}, "", 1)
	if err != nil || len(page.Items) == 0 {
		return ""
	}
	s.guest = page.Items[0].ID
	return s.guest
}

// arriveDepart records the arrival (at 15:00 of the arrival day) and, when the departure is past, the departure
// (at 10:00) of a confirmed transit.
func (s *seedOcc) arriveDepart(c *contract.Detail, arr, dep calendar.Date) error {
	loc := time.UTC
	if l, err := calendar.LoadLocation("Europe/Rome"); err == nil {
		loc = l
	}
	at := arr.Time(loc).Add(15 * time.Hour)
	if at.After(s.a.Clock.Now()) {
		at = s.a.Clock.Now()
	}
	if _, err := s.a.Presence.Record(s.ctx, s.p, s.m, presence.Input{VesselID: c.Row.VesselID, Event: presence.EventArrived, At: &at}); err != nil {
		return err
	}
	s.res.Presences++
	if dep.After(s.today) {
		return nil
	}
	out := dep.Time(loc).Add(10 * time.Hour)
	if out.After(s.a.Clock.Now()) {
		out = s.a.Clock.Now()
	}
	if _, err := s.a.Presence.Record(s.ctx, s.p, s.m, presence.Input{VesselID: c.Row.VesselID, Event: presence.EventDeparted, At: &out}); err != nil {
		return err
	}
	s.res.Presences++
	return nil
}

// releases gives back 10 stretches of annual berths in today..today+30; 4 are resold to transits.
func (s *seedOcc) releases(annual []*contract.Detail) error {
	resold := 0
	for i := 0; i < len(annual) && s.res.Releases < 10; i += 7 {
		c := annual[i]
		var al *store.Allocation
		for j := range c.Allocations {
			if c.Allocations[j].ResourceType == store.ResourceBerth {
				al = &c.Allocations[j]
			}
		}
		if al == nil {
			continue
		}
		from := s.today.AddDays(1 + s.rng.IntN(10))
		to := from.AddDays(2 + s.rng.IntN(6))
		if to.After(al.LastNight) || to.After(s.yearTo) {
			continue
		}
		_, err := s.a.Occupancy.CreateRelease(s.ctx, s.p, s.m, occupancy.ReleaseInput{AllocationID: al.ID, From: from, To: to, Source: "customer_phone"})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("release: %w", err)
		}
		if err != nil {
			continue
		}
		s.res.Releases++
		if resold >= 4 {
			continue
		}
		t, err := s.a.Contracts.CreateTransit(s.ctx, s.p, s.m, contract.TransitInput{
			NewVessel:  &contract.NewVessel{Name: fmt.Sprintf("Ospite %d", resold+1), LOACm: 900, BeamCm: 300, HullType: "monohull_motor"},
			CustomerID: s.firstCustomer(), Arrival: from, Departure: to.AddDays(1), Confirm: &contract.ManualConfirmation{},
			Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: al.ResourceID, First: from, Last: to}},
		})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("resale: %w", err)
		}
		if t != nil {
			resold++
			s.res.Transits++
		}
	}
	return nil
}

// extras seeds blocks, a closure, vessels out, quotes and inquiries.
func (s *seedOcc) extras(free []*store.Berth, annual []*contract.Detail, vessels []store.Vessel, owners map[string]string, used map[string]bool) error {
	// 5 blocks for works.
	blocks := 0
	for i := 0; i < len(free) && blocks < 5; i++ {
		b := free[len(free)-1-i]
		from := s.today.AddDays(2 + i)
		_, err := s.a.Occupancy.CreateBlock(s.ctx, s.p, s.m, occupancy.BlockInput{
			Target: occupancy.Target{ResourceType: "berth", ResourceID: b.ID}, From: from, To: from.AddDays(3), Reason: "Lavori alla colonnina",
		})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("block: %w", err)
		}
		if err == nil {
			blocks++
		}
	}
	// A regatta weekend closure on the head of pier E (resource scope on its first head berth).
	for _, b := range free {
		if len(b.Code) > 2 && b.Code[:2] == "E-" {
			sat := s.today.AddDays((6 - s.today.ISOWeekday() + 7) % 7)
			_, err := s.a.Occupancy.CreateClosure(s.ctx, s.p, s.m, occupancy.Closure{
				ScopeType: "resource", ScopeID: b.ID, ResourceType: "berth", From: sat, To: sat.AddDays(1), Reason: "Regata sociale",
			})
			if err := s.skip(err); err != nil {
				return fmt.Errorf("closure: %w", err)
			}
			break
		}
	}
	// 25 annual vessels out with an expected return.
	out := 0
	for _, c := range annual {
		if out >= 25 {
			break
		}
		at := s.a.Clock.Now().Add(-time.Duration(1+s.rng.IntN(48)) * time.Hour)
		ret := s.today.AddDays(3 + s.rng.IntN(20))
		_, err := s.a.Presence.Record(s.ctx, s.p, s.m, presence.Input{VesselID: c.Row.VesselID, Event: presence.EventDeparted, At: &at, ExpectedReturnOn: &ret})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("vessel out: %w", err)
		}
		if err == nil {
			out++
			s.res.Presences++
		}
	}
	// 8 quotes: 2 sent with holds expiring in 3 days, 1 accepted (draft contract), 1 declined, 4 drafts.
	// Vessels without a contract first, then the others (a moored vessel may ask for a stay elsewhere).
	var qv []*store.Vessel
	for _, wantUsed := range []bool{false, true} {
		for i := range vessels {
			if used[vessels[i].ID] == wantUsed && owners[vessels[i].ID] != "" {
				qv = append(qv, &vessels[i])
			}
		}
	}
	start := s.today.AddDays(5)
	if start.After(s.yearTo.AddDays(-3)) {
		start = s.today
	}
	chosen := map[string]bool{}
	for i, k := 0, 0; s.res.Quotes < 8 && k < len(qv); k++ {
		v := qv[k]
		valid := s.today.AddDays(3)
		ends := start.AddDays(2)
		var opts []contract.OptionInput
		for _, b := range free {
			if len(opts) == 2 {
				break
			}
			if chosen[b.ID] || v.LOACm > b.MaxLOACm || v.BeamCm > b.MaxBeamCm || v.HullType == "catamaran" || v.HullType == "trimaran" {
				continue
			}
			chosen[b.ID] = true
			opts = append(opts, contract.OptionInput{Hold: i < 3, Placements: []contract.Segment{
				{ResourceType: store.ResourceBerth, ResourceID: b.ID, First: start, Last: ends.AddDays(-1)},
			}})
		}
		if len(opts) == 0 {
			continue
		}
		q, err := s.a.Contracts.CreateQuote(s.ctx, s.p, s.m, contract.QuoteInput{
			Kind: contract.KindTransit, CustomerID: owners[v.ID], VesselID: v.ID, StartsOn: start, EndsOn: ends, ValidUntil: &valid,
			Options: opts,
		})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("quote: %w", err)
		}
		if q == nil {
			continue
		}
		s.res.Quotes++
		i++
		switch i - 1 {
		case 0, 1:
			_, err = s.a.Contracts.MarkQuoteSent(s.ctx, s.p, s.m, q.Row.ID, q.Row.Version)
		case 2:
			_, _, err = s.a.Contracts.AcceptQuote(s.ctx, s.p, s.m, q.Row.ID, q.Row.Version, contract.AcceptInput{OptionID: q.Options[0].Row.ID})
		case 3:
			_, err = s.a.Contracts.DeclineQuote(s.ctx, s.p, s.m, q.Row.ID, q.Row.Version, "")
		}
		if err := s.skip(err); err != nil {
			return fmt.Errorf("quote state: %w", err)
		}
	}
	// 12 inquiries, 6 refused for lack of space.
	outcomes := []string{"declined_no_space", "open", "declined_size", "lost"}
	for i := range 12 {
		outcome := outcomes[i%len(outcomes)]
		if i < 6 {
			outcome = "declined_no_space"
		}
		arr := s.today.AddDays(s.rng.IntN(20))
		_, err := s.a.Contracts.CreateInquiry(s.ctx, s.p, s.m, contract.InquiryInput{
			Purpose: "transit", Source: []string{"phone", "email", "desk"}[i%3], Arrival: arr, Departure: arr.AddDays(1 + s.rng.IntN(4)),
			LOACm: 1000 + int64(s.rng.IntN(1500)), BeamCm: 350 + int64(s.rng.IntN(200)), Outcome: outcome,
		})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("inquiry: %w", err)
		}
		if err == nil {
			s.res.Inquiries++
		}
	}
	return nil
}

// count fills the allocation and row counts of the result.
func (s *seedOcc) count(scope store.MarinaScope) error {
	db := s.a.DB.DB
	for _, c := range []struct {
		table string
		dst   *int
	}{
		{"allocations", &s.res.Allocations},
		{"berth_occupancies", &s.res.BerthRows},
		{"cell_occupancies", &s.res.CellRows},
		{"land_occupancies", &s.res.LandRows},
	} {
		n, err := db.NewSelect().TableExpr(c.table).Where("marina_id = ?", scope.ID()).Count(s.ctx)
		if err != nil {
			return err
		}
		*c.dst = int(n)
	}
	return nil
}
