//go:build integration

package occupancy

import (
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

const attempts = 100

// race runs fn(i) for i in [0, n) in parallel, released together.
func race(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

type outcome struct {
	mu    sync.Mutex
	wins  []*store.Allocation
	fails []error
}

func (o *outcome) add(a *store.Allocation, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil {
		o.fails = append(o.fails, err)
		return
	}
	o.wins = append(o.wins, a)
}

func (o *outcome) allTaken(t testing.TB) {
	t.Helper()
	for _, err := range o.fails {
		if !isTaken(err) {
			t.Fatalf("unexpected failure: %v", err)
		}
	}
}

func TestConcurrency_BerthAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		n := h.today.AddDays(20)
		vessels := make([]string, attempts)
		for i := range vessels {
			vessels[i] = h.vessel(t, 1000, 350)
		}
		rng := rand.New(rand.NewPCG(1, 2))
		specs := make([]engine.AllocSpec, attempts)
		for i := range specs {
			first := n.AddDays(-rng.IntN(6))
			specs[i] = berthSpec(h.berths[1], first, n.AddDays(rng.IntN(6)))
		}
		var o outcome
		race(attempts, func(i int) {
			o.add(h.allocate(ctxT(), transit(vessels[i], specs[i])))
		})
		if len(o.wins) != 1 {
			t.Fatalf("%d winners on night %s", len(o.wins), n)
		}
		o.allTaken(t)
		w := o.wins[0]
		if rows := h.count(t, "berth_occupancies"); rows != engine.Nights(w.FirstNight, w.LastNight) {
			t.Fatalf("%d rows for the winner's %d nights", rows, engine.Nights(w.FirstNight, w.LastNight))
		}
		h.assertInvariants(t)
	})
}

func TestConcurrency_CellAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(5)
		rng := rand.New(rand.NewPCG(3, 4))
		type try struct {
			vessel string
			x      geo.Cm
		}
		tries := make([]try, attempts)
		for i := range tries {
			tries[i] = try{vessel: h.vessel(t, 1000, int64(200+rng.IntN(300))), x: geo.Cm(5+rng.IntN(500)) * 10}
		}
		var o outcome
		race(attempts, func(i int) {
			a, err := h.allocate(ctxT(), transit(tries[i].vessel, areaSpec(h.area, tries[i].x, d0, d0.AddDays(2))))
			var pe *PlacementError
			if errors.As(err, &pe) {
				return // outside the area
			}
			o.add(a, err)
		})
		if len(o.wins) == 0 {
			t.Fatal("no winner")
		}
		o.allTaken(t)
		assertDisjointCells(t, h, o.wins)
		h.assertInvariants(t)
	})
}

func assertDisjointCells(t *testing.T, h *harbour, wins []*store.Allocation) {
	t.Helper()
	owner := map[[2]int64]string{}
	for _, w := range wins {
		rows, err := store.RowsOfAllocation(ctxT(), h.DB.DB, h.scope, w)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatalf("winner %s has no rows", w.ID)
		}
		for _, r := range rows {
			k := [2]int64{int64(r.Cell), int64(r.Night.DaysUntil(h.today))}
			if o, ok := owner[k]; ok && o != w.ID {
				t.Fatalf("cell %d night %s shared by %s and %s", r.Cell, r.Night, o, w.ID)
			}
			owner[k] = w.ID
		}
	}
}

func TestConcurrency_CellAllocationSurface(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(5)
		rng := rand.New(rand.NewPCG(5, 6))
		type try struct {
			vessel string
			p      engine.Placement
		}
		tries := make([]try, attempts)
		for i := range tries {
			tries[i] = try{
				vessel: h.vessel(t, int64(500+rng.IntN(800)), int64(200+rng.IntN(250))),
				p:      engine.Placement{X: geo.Cm(rng.IntN(25)) * 100, Y: geo.Cm(rng.IntN(15)) * 100, Rotation: geo.Angle(900 * rng.IntN(4))},
			}
		}
		var o outcome
		race(attempts, func(i int) {
			sp := engine.AllocSpec{ResourceType: engine.ResourceArea, ResourceID: h.surface, Placement: &tries[i].p, First: d0, Last: d0.AddDays(1)}
			a, err := h.allocate(ctxT(), transit(tries[i].vessel, sp))
			var pe *PlacementError
			if errors.As(err, &pe) {
				return
			}
			o.add(a, err)
		})
		if len(o.wins) == 0 {
			t.Fatal("no winner")
		}
		o.allTaken(t)
		assertDisjointCells(t, h, o.wins)
		h.assertInvariants(t)
	})
}

func TestConcurrency_LandAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(5)
		v := h.vessel(t, 1000, 350)
		var o outcome
		race(attempts, func(i int) {
			o.add(h.allocate(ctxT(), NewAllocation{
				VesselID: strp(v), Kind: KindContract,
				Spec: engine.AllocSpec{ResourceType: engine.ResourceLand, ResourceID: h.lands[0], First: d0.AddDays(-(i % 3)), Last: d0.AddDays(i % 4)},
			}))
		})
		if len(o.wins) != 1 {
			t.Fatalf("%d winners", len(o.wins))
		}
		o.allTaken(t)
		h.assertInvariants(t)
	})
}

// 50 desk transits and 50 api holds on the same berth and nights: one winner (INV-01 across channels).
func TestConcurrency_ChannelVsDesk(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(5)
		vessels := make([]string, attempts)
		for i := range vessels {
			vessels[i] = h.vessel(t, 1000, 350)
		}
		exp := h.Clock.Now().Add(10 * time.Minute)
		var o outcome
		race(attempts, func(i int) {
			na := transit(vessels[i], berthSpec(h.berths[8], d0, d0.AddDays(2)))
			if i%2 == 1 {
				na.Kind, na.ExpiresAt = KindHold, &exp
			}
			o.add(h.allocate(ctxT(), na))
		})
		if len(o.wins) != 1 {
			t.Fatalf("%d winners", len(o.wins))
		}
		o.allTaken(t)
		h.assertInvariants(t)
	})
}

func TestConcurrency_ExpiredHoldReclaim(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(5)
		exp := h.Clock.Now().Add(time.Minute)
		hold := h.mustAllocate(t, NewAllocation{VesselID: strp(h.vessel(t, 1000, 350)), Kind: KindHold, ExpiresAt: &exp, Spec: berthSpec(h.berths[2], d0, d0.AddDays(3))})
		h.Clock.Advance(5 * time.Minute)
		vessels := make([]string, attempts)
		for i := range vessels {
			vessels[i] = h.vessel(t, 1000, 350)
		}
		var o outcome
		race(attempts, func(i int) {
			o.add(h.allocate(ctxT(), transit(vessels[i], berthSpec(h.berths[2], d0.AddDays(i%3), d0.AddDays(3)))))
		})
		if len(o.wins) != 1 {
			t.Fatalf("%d winners", len(o.wins))
		}
		o.allTaken(t)
		if a := h.alloc(t, hold.ID); a.Status != store.AllocExpired {
			t.Fatalf("hold %s", a.Status)
		}
		h.assertInvariants(t)
	})
}

// INV-06: parallel swaps and moves of two allocations: each operation is applied entirely or not at all.
func TestConcurrency_SwapVessels(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0, last := h.today, h.today.AddDays(30)
		va, vb := h.vessel(t, 1000, 350), h.vessel(t, 1000, 350)
		h.mustAllocate(t, transit(va, berthSpec(h.berths[1], d0, last)))
		h.mustAllocate(t, transit(vb, berthSpec(h.berths[2], d0, last)))
		current := func(vessel string) *store.Allocation {
			as, err := store.ListAllocations(ctxT(), db.DB, h.scope, store.AllocationFilter{VesselID: vessel, Status: store.AllocActive, From: d0.AddDays(5), To: d0.AddDays(5)})
			if err != nil || len(as) == 0 {
				return nil
			}
			return &as[len(as)-1]
		}
		var mu sync.Mutex
		ok := 0
		race(attempts, func(i int) {
			a, b := current(va), current(vb)
			if a == nil || b == nil {
				return
			}
			from := d0.AddDays(5 + i%5)
			var err error
			switch i % 3 {
			case 0:
				_, err = h.s.Swap(ctxT(), h.Owner, h.MarinaID, SwapInput{A: BaseVersion{a.ID, a.Version}, B: BaseVersion{b.ID, b.Version}, FromNight: &from})
			case 1:
				_, err = h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{
					AllocationID: a.ID, Version: a.Version, FromNight: &from,
					Target: Target{ResourceType: engine.ResourceBerth, ResourceID: h.berths[3+i%3]},
				})
			default:
				_, err = h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{
					AllocationID: b.ID, Version: b.Version, FromNight: &from,
					Target: Target{ResourceType: engine.ResourceBerth, ResourceID: h.berths[3+i%3]},
				})
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrVersionMismatch), errors.Is(err, ErrAllocationNotActive), isTaken(err), isInvalid(err):
			default:
				t.Errorf("unexpected: %v", err)
			}
		})
		if ok == 0 {
			t.Fatal("no operation applied")
		}
		for d := d0; !d.After(last); d = d.AddDays(1) {
			a, b := onNight(t, h, va, d), onNight(t, h, vb, d)
			if a == "" || b == "" || a == b {
				t.Fatalf("night %s: vessels on %q and %q", d, a, b)
			}
		}
		h.assertInvariants(t)
	})
}

// onNight returns the resource of the vessel's row on night d ("" when none).
func onNight(t *testing.T, h *harbour, vessel string, d calendar.Date) string {
	t.Helper()
	as, err := store.ListAllocations(ctxT(), h.DB.DB, h.scope, store.AllocationFilter{VesselID: vessel, Status: store.AllocActive, From: d, To: d})
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 1 {
		t.Fatalf("vessel has %d allocations on %s", len(as), d)
	}
	return as[0].ResourceID
}

func TestConcurrency_MoveVsAllocate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today.AddDays(3)
		target := h.berths[9]
		var movers []*store.Allocation
		for i := range attempts / 2 {
			v := h.vessel(t, 1000, 350)
			// every mover sits on a berth or in the area for the same nights
			sp := areaSpec(h.area, geo.Cm(30+(i-7)*130), d0, d0.AddDays(4))
			if i < 7 {
				sp = berthSpec(h.berths[1+i], d0, d0.AddDays(4))
			}
			a, err := h.allocate(ctxT(), transit(v, sp))
			if err != nil {
				continue
			}
			movers = append(movers, a)
		}
		transits := make([]string, attempts/2)
		for i := range transits {
			transits[i] = h.vessel(t, 1000, 350)
		}
		var mu sync.Mutex
		wins := 0
		race(attempts, func(i int) {
			var err error
			if i%2 == 0 && i/2 < len(movers) {
				m := movers[i/2]
				from := d0
				_, err = h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{
					AllocationID: m.ID, Version: m.Version, FromNight: &from,
					Target: Target{ResourceType: engine.ResourceBerth, ResourceID: target},
				})
			} else {
				_, err = h.allocate(ctxT(), transit(transits[i/2], berthSpec(target, d0, d0.AddDays(4))))
			}
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if !isTaken(err) {
				t.Errorf("unexpected: %v", err)
			}
		})
		if wins != 1 {
			t.Fatalf("%d winners on the target berth", wins)
		}
		h.assertInvariants(t)
	})
}

func TestConcurrency_LayoutSaveSingleWinner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v1, v2 := h.vessel(t, 1000, 350), h.vessel(t, 1000, 400)
		a1 := h.mustAllocate(t, transit(v1, areaSpec(h.area, 100, d0, d0.AddDays(5))))
		a2 := h.mustAllocate(t, transit(v2, areaSpec(h.area, 1000, d0, d0.AddDays(5))))
		base := []BaseVersion{{a1.ID, a1.Version}, {a2.ID, a2.Version}}
		var mu sync.Mutex
		ok := 0
		race(attempts, func(i int) {
			_, err := h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{
				AreaID: h.area, Date: d0, Base: base,
				Placements: []LayoutOp{{AllocationID: a1.ID, Placement: engine.Placement{X: geo.Cm(2000 + 10*(i%40))}}, {AllocationID: a2.ID, Placement: engine.Placement{X: 100}}},
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrVersionMismatch), isTaken(err):
			default:
				t.Errorf("unexpected: %v", err)
			}
		})
		if ok != 1 {
			t.Fatalf("%d saves won", ok)
		}
		h.assertInvariants(t)
	})
}

func TestConcurrency_RevokeVsResale(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		d0 := h.today
		v := h.vessel(t, 1000, 350)
		_, line := h.contractLine(t, store.ContractKindMooring, "active", v, store.ResourceBerth, h.berths[1], d0, d0.AddDays(60))
		a := h.mustAllocate(t, NewAllocation{ContractLineID: &line, VesselID: strp(v), Kind: KindContract, Spec: berthSpec(h.berths[1], d0, d0.AddDays(60))})
		rel, err := h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: a.ID, From: d0.AddDays(10), To: d0.AddDays(15), Source: "office"})
		if err != nil {
			t.Fatal(err)
		}
		buyers := make([]string, attempts)
		for i := range buyers {
			buyers[i] = h.vessel(t, 1000, 350)
		}
		var mu sync.Mutex
		revoked, sold := 0, 0
		race(attempts, func(i int) {
			var err error
			if i%2 == 0 {
				_, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, rel.ID, rel.Version, nil, nil)
			} else {
				_, err = h.allocate(ctxT(), transit(buyers[i], berthSpec(h.berths[1], d0.AddDays(10+i%3), d0.AddDays(12+i%3))))
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && i%2 == 0:
				revoked++
			case err == nil:
				sold++
			case isTaken(err), errors.Is(err, ErrVersionMismatch), errors.Is(err, ErrReleaseInvalid):
			default:
				t.Errorf("unexpected: %v", err)
			}
		})
		if revoked > 1 || (revoked == 1 && sold > 0) || revoked+sold == 0 {
			t.Fatalf("revoked %d, sold %d", revoked, sold)
		}
		h.assertInvariants(t)
	})
}

func isInvalid(err error) bool {
	var pe *PlacementError
	return errors.As(err, &pe) || errors.Is(err, auth.ErrInvalidInput)
}
