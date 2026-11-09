package app

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Demo previous-year history (spec 21.2 comparisons, m7-plan §4): the season Y−1 of the demo marina, so that the
// "Anno precedente" of the dashboard and the period comparison have something to compare with. It is written by a
// second App on the same database whose clock walks through Y−1 (clock.Fake), so every service sees the day it
// would have seen: the price list LST-(Y−1) is created in December Y−2, the annual contracts in the first weeks of
// January Y−1 with the payment clause of the contract of Y they replay, the seasonal ones in March Y−1,
// each guest transit a few days before its arrival with its arrival and departure presences at their time, and a
// pass on every issue day runs the instalment issue job and a weekly one records the payments of the charges
// falling due (92 % paid on their due date, the rest left as old receivables). Contracts replay most of the annual and seasonal
// contracts of Y one year earlier on the same berth with the same customer and vessel (a few berths changed hands,
// so Y has new customers). The engine refusals tolerated by the current-year seed skip an item, so the invariants
// hold by construction. The step is a no-op when the marina already has a contract starting before 1 January Y.

// historyTransits is the number of guest transits of the previous-year history: 100 in the summer and 140 around
// the same day one year ago (as many as the current-year seed creates around today, seedOcc.transits).
const historyTransits = 240

// seedHistory seeds the previous year (see above) and adds the counts to res.
func (a *App) seedHistory(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.Contracts == nil || a.Presence == nil || a.PriceLists == nil {
		return nil
	}
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
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
	now := a.Clock.Now()
	today := calendar.In(now, loc)
	y := today.Year()
	jan1, prevJan1 := calendar.New(y, time.January, 1), calendar.New(y-1, time.January, 1)
	if n, err := store.CountContractsBefore(ctx, a.DB.DB, scope, jan1); err != nil || n > 0 {
		return err
	}
	if n, err := store.CountContracts(ctx, a.DB.DB, scope); err != nil || n == 0 {
		return err // no current year to mirror
	}
	at := func(d calendar.Date, hour int) time.Time { return d.Time(loc).Add(time.Duration(hour) * time.Hour) }
	past, fake, err := a.seedClockApp(ctx, at(prevJan1.AddDays(-31), 10))
	if err != nil {
		return err
	}
	defer func() { _ = past.Close() }()
	if err := past.seedPreviousPriceList(ctx, p, res.MarinaID, y-1); err != nil {
		return fmt.Errorf("price list: %w", err)
	}
	h := &seedOcc{
		a: past, ctx: ctx, p: p, m: res.MarinaID, res: res, today: today,
		rng:    rand.New(rand.NewPCG(2025, 9)), //nolint:gosec // demo data: deterministic by design
		yearTo: calendar.New(y-1, time.December, 31),
	}
	used := map[string]bool{}
	// Annual contracts of Y−1 (88 % of the annual contracts of Y), then the seasonal ones (May–September).
	for _, term := range []struct {
		name             string
		from             calendar.Date // start of the contracts of Y to replay
		prevFrom, prevTo calendar.Date
		createdOn        calendar.Date
		keepPercent      int
	}{
		{contract.TermAnnual, jan1, prevJan1, jan1, prevJan1, 88},
		{contract.TermSeasonal, calendar.New(y, time.May, 1), calendar.New(y-1, time.May, 1), calendar.New(y-1, time.October, 1), calendar.New(y-1, time.March, 2), 75},
	} {
		fake.Set(at(term.createdOn, 10))
		placements, err := store.SeedMooringPlacements(ctx, a.DB.DB, scope, term.from)
		if err != nil {
			return err
		}
		for i, pl := range placements {
			if pl.Term != term.name || used[pl.ResourceID] || h.rng.IntN(100) >= term.keepPercent {
				continue
			}
			if term.name == contract.TermAnnual {
				fake.Set(at(seedAnnualSignedOn(prevJan1, i, len(placements)), 10))
			}
			to := term.prevTo
			c, err := past.Contracts.CreateContract(ctx, p, res.MarinaID, contract.MooringInput{
				Term: term.name, CustomerID: pl.CustomerID, VesselID: pl.VesselID, StartsOn: term.prevFrom, EndsOn: &to, Activate: true,
				Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: pl.ResourceID, First: term.prevFrom, Last: to.AddDays(-1)}},
				Policy:     seedPolicyOf(pl.Policy),
			})
			if err := h.skip(err); err != nil {
				return fmt.Errorf("%s contract: %w", term.name, err)
			}
			if c != nil {
				used[pl.ResourceID] = true
				res.HistoryContracts++
			}
		}
	}
	berths, _, _, err := store.SeedResources(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	var free []*store.Berth
	for i := range berths {
		if !used[berths[i].ID] && berths[i].MooringType != "buoy" {
			free = append(free, &berths[i])
		}
	}
	if err := h.historyTransits(fake, at, free, today.AddDays(-365), now); err != nil {
		return err
	}
	// Pass through Y−1: issue the instalments on their issue days (seedInstallmentDays) and, weekly, record the
	// payments due by then (on their due date).
	decided := map[string]bool{}
	pay := rand.New(rand.NewPCG(2025, 51)) //nolint:gosec // demo data: deterministic by design
	start := prevJan1.AddDays(-28)
	days, err := seedInstallmentDays(ctx, a, scope, jan1.AddDays(-1))
	if err != nil {
		return err
	}
	issueOn := map[calendar.Date]bool{}
	for _, d := range days {
		issueOn[d] = true
	}
	for d := start; d.Before(jan1) && at(d, 9).Before(now); d = d.AddDays(1) {
		weekly := start.DaysUntil(d)%7 == 0
		if !weekly && !issueOn[d] {
			continue
		}
		fake.Set(at(d, 9))
		if _, err := past.Contracts.IssueMarina(ctx, res.MarinaID); err != nil {
			return fmt.Errorf("issue: %w", err)
		}
		if !weekly {
			continue
		}
		if err := past.seedHistoryPayments(ctx, p, scope, d, jan1, decided, pay, res); err != nil {
			return err
		}
	}
	return nil
}

// seedClockApp returns a second App on the same database whose clock is a clock.Fake starting at start: the demo
// seed writes through the services on the day each record would have been written, so audit rows, outbox events,
// issue dates and receipt dates carry that day. The caller closes it.
func (a *App) seedClockApp(ctx context.Context, start time.Time) (*App, *clock.Fake, error) {
	fake := clock.NewFake(start)
	past, err := NewWithOptions(ctx, a.Config, a.Version, a.ext, a.Log, Options{
		DB: a.DB, Clock: fake, Argon2: a.opts.Argon2, Transport: a.opts.Transport,
	})
	if err != nil {
		return nil, nil, err
	}
	return past, fake, nil
}

// seedPreviousPriceList creates and activates LST-(year) from the demo list document (same rules as LST-Y).
func (a *App) seedPreviousPriceList(ctx context.Context, p *auth.Principal, m string, year int) error {
	page, err := a.Catalog.ListItems(ctx, p, m, catalog.ItemFilter{}, "", store.MaxLimit)
	if err != nil {
		return err
	}
	items := map[string]string{}
	for _, it := range page.Items {
		items[it.Code] = it.ID
	}
	doc := seedListDoc(year, items)
	en := "Price list " + strconv.Itoa(year)
	doc.Name = catalog.I18nText{It: "Listino " + strconv.Itoa(year), En: &en}
	l, err := a.PriceLists.Create(ctx, p, m, pricelist.CreateInput{
		Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: doc.ContractTypes,
	})
	if err != nil {
		return err
	}
	saved, err := a.PriceLists.Save(ctx, p, m, l.ID, l.Version, doc)
	if err != nil {
		return err
	}
	_, err = a.PriceLists.Activate(ctx, p, m, l.ID, saved.Document.Version, calendar.Date{})
	return err
}

// historyTransits creates the guest transits of the previous year: 100 in the summer (June–September), 140 from
// 20 days before to 30 days after the same day one year ago (the window of the current-year transits). Each is
// created and confirmed 3 to 30 days before its arrival (seedTransitBookedOn), arrives at 15:00 and leaves at 10:00 of its departure day, the clock set to each moment; a few no-shows.
func (s *seedOcc) historyTransits(fake *clock.Fake, at func(calendar.Date, int) time.Time, free []*store.Berth, aroundDay calendar.Date, now time.Time) error {
	if len(free) == 0 {
		return nil
	}
	y := aroundDay.Year()
	summer := [2]calendar.Date{calendar.New(y, time.June, 1), calendar.New(y, time.September, 30)}
	around := [2]calendar.Date{aroundDay.AddDays(-20), aroundDay.AddDays(30)}
	if around[1].After(s.yearTo) {
		around[1] = s.yearTo
	}
	done, noShow := 0, 0
	for i := 0; i < 3*historyTransits && done < historyTransits; i++ {
		win := summer
		if i%12 < 7 {
			win = around
		}
		span := win[0].DaysUntil(win[1])
		if span < 2 {
			continue
		}
		nights := 1 + s.rng.IntN(6)
		arr := win[0].AddDays(s.rng.IntN(span - 1))
		dep := arr.AddDays(nights)
		if dep.After(s.yearTo.AddDays(1)) {
			dep = s.yearTo.AddDays(1)
		}
		if !dep.After(arr) || !at(dep, 10).Before(now) {
			continue
		}
		b := free[s.rng.IntN(len(free))]
		loa := min(b.MaxLOACm, 600+int64(s.rng.IntN(1400)))
		beam := min(b.MaxBeamCm, max(200, loa/3))
		hull := "monohull_sail"
		if s.rng.IntN(3) == 0 {
			hull = "monohull_motor"
		}
		fake.Set(at(seedTransitBookedOn(arr, s.today, s.rng), 11))
		c, err := s.a.Contracts.CreateTransit(s.ctx, s.p, s.m, contract.TransitInput{
			NewVessel: &contract.NewVessel{
				Name: fmt.Sprintf("%s %d", seedGuestNames[(i+7)%len(seedGuestNames)], 200+i), LOACm: loa, BeamCm: beam, HullType: hull,
			},
			CustomerID: s.firstCustomer(), Arrival: arr, Departure: dep,
			Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: b.ID, First: arr, Last: dep.AddDays(-1)}},
			Confirm:    &contract.ManualConfirmation{Reference: "demo"},
		})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("history transit: %w", err)
		}
		if c == nil {
			continue
		}
		done++
		s.res.HistoryTransits++
		if noShow < 3 && i%41 == 40 {
			fake.Set(at(arr.AddDays(1), 9))
			_, err = s.a.Contracts.MarkNoShow(s.ctx, s.p, s.m, c.Row.ID, c.Row.Version)
			noShow++
			if err := s.skip(err); err != nil {
				return fmt.Errorf("history no-show: %w", err)
			}
			continue
		}
		for _, ev := range []struct {
			event string
			when  time.Time
		}{{presence.EventArrived, at(arr, 15)}, {presence.EventDeparted, at(dep, 10)}} {
			fake.Set(ev.when)
			if _, err := s.a.Presence.Record(s.ctx, s.p, s.m, presence.Input{VesselID: c.Row.VesselID, Event: ev.event}); err != nil {
				if err := s.skip(err); err != nil {
					return fmt.Errorf("history presence: %w", err)
				}
				break
			}
			s.res.Presences++
		}
	}
	return nil
}

// seedHistoryPayments records, on day, the payments of the open charges of Y−1 (issued before jan1) due by day:
// 92 % paid on their due date, the others decided once and left open as old receivables.
func (a *App) seedHistoryPayments(ctx context.Context, p *auth.Principal, scope store.MarinaScope, day, jan1 calendar.Date,
	decided map[string]bool, rng *rand.Rand, res *SeedResult,
) error {
	open, err := store.OpenPositiveCharges(ctx, a.DB.DB, scope, store.OpenChargesFilter{})
	if err != nil {
		return err
	}
	methods := []string{store.PaymentMethodBankTransfer, store.PaymentMethodBankTransfer, store.PaymentMethodCardPOS, store.PaymentMethodCheque}
	for _, ch := range open {
		if decided[ch.ID] || ch.CustomerID == "" || !ch.IssuedOn.Before(jan1) || ch.DueOn.After(day) {
			continue
		}
		decided[ch.ID] = true
		if rng.IntN(100) >= 92 {
			continue
		}
		amount := money.Cents(ch.ResidualCents())
		if amount <= 0 {
			continue
		}
		on := ch.DueOn
		if on.Before(ch.IssuedOn) {
			on = ch.IssuedOn
		}
		if _, err := a.Payments.Record(ctx, p, scope.ID(), payment.RecordInput{
			CustomerID: ch.CustomerID, Kind: store.PaymentKindPayment, Method: methods[rng.IntN(len(methods))], Amount: amount,
			ReceivedOn: on, Matches: []payment.MatchInput{{ChargeID: ch.ID, Amount: amount}},
		}); err != nil {
			return fmt.Errorf("history payment: %w", err)
		}
		res.HistoryPayments++
	}
	return nil
}
