package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/vessel"
	"github.com/coste360/porto/internal/store"
)

// Demo renewal history (dashboard "Tasso di rinnovo", spec 21.2): the renewal rate counts the annual and seasonal
// contracts whose last night falls in the period and how many were renewed. The annual contracts of the demo end
// on 31 December, so a year-to-date period needs seasonal ones: a small fleet of seasonal customers (May–September)
// with their history. Their boats are registered and their contracts created in March Y−1 for the season Y−1; on
// 1 September Y−1 most are renewed for the season Y (proposal and successor signed on paper), on 1 September Y most
// of those are renewed again for Y+1 (a few proposals still open). Everything goes through the services, through an
// App whose clock walks through those days (like seed_history.go). A no-op when the marina already has a seasonal
// contract, has no contract or no previous-year history.

const (
	seedSeasonalFleet   = 12 // seasonal contracts of Y−1
	seedSeasonalRenewY  = 10 // renewed for Y on 1 September Y−1
	seedSeasonalRenewY1 = 8  // renewal proposals for Y+1 on 1 September Y
	seedSeasonalSignY1  = 6  // of which signed
)

// seedSeasonalRenewals seeds the seasonal renewal history (see above).
func (a *App) seedSeasonalRenewals(ctx context.Context, p *auth.Principal, res *SeedResult) (err error) {
	if a.Contracts == nil || a.Vessels == nil || a.Documents == nil {
		return nil
	}
	db := a.DB.DB
	scope, err := store.ResolveMarinaScope(ctx, db, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("contracts").Where("marina_id = ?", scope.ID()).
		Where("term = ?", contract.TermSeasonal).Count(ctx); err != nil || n > 0 {
		return err
	}
	loc := seedPlannerLocation(ctx, a, scope)
	now := a.Clock.Now()
	today := calendar.In(now, loc)
	y := today.Year()
	if n, err := store.CountContractsBefore(ctx, db, scope, calendar.New(y, time.January, 1)); err != nil || n == 0 {
		return err // no previous-year history (and its price list)
	}
	berths, _, _, err := store.SeedResources(ctx, db, scope)
	if err != nil {
		return err
	}
	customers, err := store.SeedCustomerIDs(ctx, db, scope, 140)
	if err != nil || len(customers) < 100+seedSeasonalFleet {
		return err
	}
	customers = customers[100:]
	at := func(d calendar.Date, hour int) time.Time { return d.Time(loc).Add(time.Duration(hour) * time.Hour) }
	fake := clock.NewFake(at(calendar.New(y-1, time.March, 2), 10))
	past, err := NewWithOptions(ctx, a.Config, a.Version, a.ext, a.Log, Options{
		DB: a.DB, Clock: fake, Argon2: a.opts.Argon2, Transport: a.opts.Transport,
	})
	if err != nil {
		return err
	}
	defer func() { _ = past.Close() }()
	m := res.MarinaID
	s := &seedOcc{a: past, ctx: ctx, p: p, m: m, res: res}
	from, to := calendar.New(y-1, time.May, 1), calendar.New(y-1, time.October, 1)

	// Season Y−1: one boat and one contract per customer, on the first free berth that fits.
	var prev []*contract.Detail
	ci := 0
	for i := range berths {
		b := &berths[i]
		if len(prev) == seedSeasonalFleet || ci == len(customers) {
			break
		}
		if b.MooringType == "buoy" || b.MaxLOACm < 1000 || slices.Contains(b.Attributes, "transit_only") {
			continue
		}
		if busy, err := a.seasonsBusy(ctx, scope, b.ID, y); err != nil || busy {
			if err != nil {
				return err
			}
			continue // taken in one of the three seasons (rights, history transits): the renewals would fail
		}
		v, err := s.seasonalBoat(customers[ci], b, len(prev), y)
		if err != nil {
			return err
		}
		c, err := past.Contracts.CreateContract(ctx, p, m, contract.MooringInput{
			Term: contract.TermSeasonal, CustomerID: customers[ci], VesselID: v, StartsOn: from, EndsOn: &to, Activate: true,
			Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: b.ID, First: from, Last: to.AddDays(-1)}},
		})
		if err := s.skip(err); err != nil {
			return fmt.Errorf("seasonal contract: %w", err)
		}
		if c == nil {
			continue // berth taken in a season: the boat stays in the registry, the next berth gets the next customer
		}
		ci++
		prev = append(prev, c)
		res.HistoryContracts++
	}

	// 1 September Y−1: renewals for the season Y, signed on paper.
	fake.Set(at(calendar.New(y-1, time.September, 1), 10))
	var cur []*store.Contract
	for i, c := range prev {
		if i >= seedSeasonalRenewY {
			break
		}
		succ, err := s.renewSeasonal(c.Row.ID, c.Row.Version, true)
		if err != nil {
			return err
		}
		if succ != nil {
			cur = append(cur, succ)
			res.Contracts++
		}
	}

	// 2 October Y−1: the expiry job ends the season (renewed when the successor is signed).
	if err := s.expireOn(fake, at(calendar.New(y-1, time.October, 2), 2)); err != nil {
		return err
	}

	// 1 September Y (when already past): renewals for Y+1, most signed, a few waiting for the customer; then the
	// expiry job of 2 October Y.
	sep1, oct2 := calendar.New(y, time.September, 1), calendar.New(y, time.October, 2)
	if !at(sep1, 10).Before(now) {
		return nil
	}
	defer func() {
		if at(oct2, 2).Before(now) && err == nil {
			err = s.expireOn(fake, at(oct2, 2))
		}
	}()
	fake.Set(at(sep1, 10))
	for i, c := range cur {
		if i >= seedSeasonalRenewY1 {
			break
		}
		succ, err := s.renewSeasonal(c.ID, c.Version, i < seedSeasonalSignY1)
		if err != nil {
			return err
		}
		if succ != nil {
			res.Contracts++
		}
	}
	return nil
}

// seasonalBoat registers the boat of a seasonal customer sized for berth b, with an insurance policy valid until
// the end of Y+2.
func (s *seedOcc) seasonalBoat(customerID string, b *store.Berth, i, y int) (string, error) {
	loa := min(b.MaxLOACm-40, 900+int64(i%5)*60)
	beam := min(b.MaxBeamCm-10, loa*10/33)
	draft := loa / 9
	if b.DepthCm > 0 {
		draft = min(draft, b.DepthCm-30)
	}
	name := fmt.Sprintf("%s %s", seedGuestNames[(i+3)%len(seedGuestNames)], romanSuffix(2+i/len(seedGuestNames)))
	hull := []string{"monohull_sail", "monohull_motor"}[i%2]
	v, err := s.a.Vessels.Create(s.ctx, s.p, s.m, vessel.Patch{
		Name: &name, HullType: &hull, LOACm: &loa, BeamCm: &beam, DraftCm: &draft, Flag: ptrOf("IT"),
		Registration: ptrOf(fmt.Sprintf("GA%05d", 7000+i)),
	}, []vessel.PartyInput{{CustomerID: customerID, Role: "owner", ShareMillis: ptrOf(int64(1000))}})
	if err != nil {
		return "", fmt.Errorf("seasonal boat: %w", err)
	}
	exp := calendar.New(y+2, time.December, 31)
	if _, err := s.a.Documents.Create(s.ctx, s.p, s.m, document.Input{
		OwnerType: document.OwnerVessel, VesselID: v.Row.ID, Type: "insurance_policy", Number: fmt.Sprintf("POL-S%05d", 100+i),
		Issuer: "Assicurazioni Demo", IssuedOn: exp.AddDays(-3 * 365), ExpiresOn: exp,
	}); err != nil {
		return "", fmt.Errorf("seasonal insurance: %w", err)
	}
	return v.Row.ID, nil
}

// renewSeasonal proposes the renewal of a contract for the next season and, when sign, activates its successor
// (paper signature). It returns the successor, nil when the engine refused the renewal.
func (s *seedOcc) renewSeasonal(id string, version int, sign bool) (*store.Contract, error) {
	pr, err := s.a.Contracts.RenewContract(s.ctx, s.p, s.m, id, version, contract.RenewInput{})
	if err := s.skip(err); err != nil {
		return nil, fmt.Errorf("seasonal renewal: %w", err)
	}
	if pr == nil || pr.Successor == nil {
		return nil, nil
	}
	if !sign {
		return pr.Successor, nil
	}
	d, err := s.a.Contracts.ActivateContract(s.ctx, s.p, s.m, pr.Successor.ID, pr.Successor.Version)
	if err := s.skip(err); err != nil {
		return nil, fmt.Errorf("seasonal renewal activate: %w", err)
	}
	if d == nil {
		return nil, nil
	}
	return &d.Row, nil
}

// seasonsBusy reports whether berth has an occupied night in the seasons (May–September) of y−1, y or y+1.
func (a *App) seasonsBusy(ctx context.Context, scope store.MarinaScope, berthID string, y int) (bool, error) {
	q := a.DB.NewSelect().TableExpr("berth_occupancies").Where("marina_id = ?", scope.ID()).Where("berth_id = ?", berthID).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			for yy := y - 1; yy <= y+1; yy++ {
				q = q.WhereOr("night BETWEEN ? AND ?", calendar.New(yy, time.May, 1), calendar.New(yy, time.September, 30))
			}
			return q
		})
	n, err := q.Count(ctx)
	return n > 0, err
}

// expireOn runs the contract expiry job of the marina at moment at.
func (s *seedOcc) expireOn(fake *clock.Fake, at time.Time) error {
	fake.Set(at)
	if _, err := s.a.Contracts.ExpireMarina(s.ctx, s.m); err != nil {
		return fmt.Errorf("seasonal expiry: %w", err)
	}
	return nil
}
