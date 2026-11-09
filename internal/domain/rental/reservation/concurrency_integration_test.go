//go:build integration

package reservation

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// unitRowCount counts the rows of a unit (bookings and blocks).
func (e *renv) unitRowCount(unitID string) int {
	e.t.Helper()
	n, err := e.DB.NewSelect().TableExpr("rental_slots").Where("unit_id = ?", unitID).Count(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return int(n)
}

func isConflict(err error) bool {
	_, ok := errors.AsType[*SlotConflict](err)
	return ok
}

// TestConcurrency_RentalUnitSlots: 100 goroutines book random overlapping ranges (hourly, half day, multi-day) on
// one unit, a third with one of two skippers, while 20 reschedules and cancels and 10 blocks run on the same
// window. The winners' rows are exactly their expansions, the losers get a conflict with nothing written and Q32
// holds (INV-18, INV-19).
func TestConcurrency_RentalUnitSlots(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 2)
		ctx := fixture.Ctx()
		unit := e.units[0]
		// 20 bookings on later days, rescheduled into the window or cancelled concurrently.
		var movers []*BookingView
		for i := range 20 {
			day := d("2027-06-20").AddDays(i)
			movers = append(movers, e.mustCreate(CreateInput{Range: slots.Range{
				Start: slots.Point{Date: day, Slot: 20}, End: slots.Point{Date: day, Slot: 24},
			}}))
		}
		rng := rand.New(rand.NewPCG(11, 12))
		type job struct {
			in     CreateInput
			move   *BookingView
			newR   slots.Range
			cancel bool
			block  *BlockInput
		}
		var jobs []job
		days := []string{"2027-06-12", "2027-06-13"}
		for i := range 100 {
			in := CreateInput{UnitID: unit, FormulaID: e.hourly}
			day := d(days[rng.IntN(2)])
			switch i % 3 {
			case 0:
				start := 16 + rng.IntN(18)
				in.Range = slots.Range{Start: slots.Point{Date: day, Slot: start}, End: slots.Point{Date: day, Slot: start + 2 + rng.IntN(5)}}
			case 1:
				in.FormulaID = e.morning
				in.Range = slots.Range{Start: slots.Point{Date: day, Slot: 16}, End: slots.Point{Date: day, Slot: 26}}
			default:
				in.FormulaID = e.multi
				in.Range = slots.Range{Start: slots.Point{Date: d("2027-06-12"), Slot: 16 + rng.IntN(20)}, End: slots.Point{Date: d("2027-06-13"), Slot: 18 + rng.IntN(20)}}
			}
			if i%3 == 0 && i%2 == 0 {
				in.SkipperID = e.skippers[rng.IntN(2)]
			}
			jobs = append(jobs, job{in: in})
		}
		for i, m := range movers {
			if i%2 == 0 {
				day := d(days[rng.IntN(2)])
				start := 16 + rng.IntN(30-16)
				jobs = append(jobs, job{move: m, newR: slots.Range{Start: slots.Point{Date: day, Slot: start}, End: slots.Point{Date: day, Slot: start + 4}}})
			} else {
				jobs = append(jobs, job{move: m, cancel: true})
			}
		}
		for range 10 {
			day := d(days[rng.IntN(2)])
			start := 16 + rng.IntN(20)
			jobs = append(jobs, job{block: &BlockInput{UnitID: unit, Reason: store.RentalBlockMaintenance, Range: slots.Range{
				Start: slots.Point{Date: day, Slot: start}, End: slots.Point{Date: day, Slot: start + 2},
			}}})
		}
		rng.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
		var (
			mu        sync.Mutex
			wins      []string
			conflicts int
			wg        sync.WaitGroup
		)
		start := make(chan struct{})
		for _, j := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var err error
				switch {
				case j.block != nil:
					_, err = e.s.CreateBlock(ctx, e.Owner, e.MarinaID, *j.block)
				case j.cancel:
					_, err = e.s.Transition(ctx, e.Owner, e.MarinaID, j.move.ID, j.move.Version, TransitionInput{Action: ActionCancel})
				case j.move != nil:
					nr := j.newR
					_, err = e.s.Update(ctx, e.Owner, e.MarinaID, j.move.ID, j.move.Version, Patch{Range: &nr})
				default:
					var b *BookingView
					b, err = e.s.Create(ctx, e.Owner, e.MarinaID, j.in)
					if err == nil {
						mu.Lock()
						wins = append(wins, b.ID)
						mu.Unlock()
					}
				}
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
				case isConflict(err):
					conflicts++
				default:
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(wins) == 0 || conflicts == 0 {
			t.Fatalf("wins %d conflicts %d: the test must contend", len(wins), conflicts)
		}
		// Σ expansions of the live bookings + block rows = rows of the unit.
		live, _, err := store.ListRentalBookings(context.Background(), db.DB, e.scope, store.RentalBookingFilter{
			Statuses: []string{store.RentalStatusConfirmed, store.RentalStatusHeld},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		for i := range live {
			want += len(slots.Expand(RangeOf(&live[i]), 1))
		}
		blocks, err := store.ListRentalBlocks(context.Background(), db.DB, e.scope, store.RentalBlockFilter{UnitID: unit})
		if err != nil {
			t.Fatal(err)
		}
		for i := range blocks {
			want += slots.Count(blockRange(&blocks[i]))
		}
		if got := e.unitRowCount(unit); got != want {
			t.Fatalf("unit rows %d, want %d", got, want)
		}
		e.q32()
	})
}

// TestConcurrency_RentalSkipperSlots: 100 goroutines book the same window on 100 units with the same skipper.
// Exactly one wins; the other 99 get skipper_busy and write no unit rows.
func TestConcurrency_RentalSkipperSlots(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 100, 1)
		ctx := fixture.Ctx()
		var (
			mu         sync.Mutex
			wins, busy int
			wg         sync.WaitGroup
		)
		start := make(chan struct{})
		for _, u := range e.units {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := e.s.Create(ctx, e.Owner, e.MarinaID, CreateInput{
					UnitID: u, FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 20, "2027-06-12", 26),
				})
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					wins++
				case errors.Is(err, ErrSkipperBusy):
					busy++
				default:
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 || busy != 99 {
			t.Fatalf("wins %d busy %d", wins, busy)
		}
		n, err := db.NewSelect().TableExpr("rental_slots").Where("marina_id = ?", e.MarinaID).Count(context.Background())
		if err != nil || n != 7 {
			t.Fatalf("unit rows %d %v", n, err)
		}
		e.q32()
	})
}

// TestConcurrency_RentalHoldReclaim: an expired hold sits on the window; 100 goroutines create confirmed bookings
// on it. One wins, the expired booking's rows are gone, and the job then cancels it once (INV-05 extended).
func TestConcurrency_RentalHoldReclaim(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newRenv(t, db, 1, 1)
		ctx := fixture.Ctx()
		h := e.mustCreate(CreateInput{FormulaID: e.withSk, SkipperID: e.skippers[0], Range: r("2027-06-12", 20, "2027-06-12", 26), Hold: true})
		e.Clock.Advance(2 * time.Hour)
		var (
			mu              sync.Mutex
			wins, conflicts int
			wg              sync.WaitGroup
		)
		start := make(chan struct{})
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				in := CreateInput{UnitID: e.units[0], FormulaID: e.hourly, Range: r("2027-06-12", 20+i%3, "2027-06-12", 24+i%3)}
				_, err := e.s.Create(ctx, e.Owner, e.MarinaID, in)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					wins++
				case isConflict(err):
					conflicts++
				default:
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 || conflicts != 99 {
			t.Fatalf("wins %d conflicts %d", wins, conflicts)
		}
		if u, sk := e.rows(h.ID); u != 0 || sk != 0 {
			t.Fatalf("expired hold rows %d %d", u, sk)
		}
		if err := e.s.RunHoldsExpire(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := e.s.RunHoldsExpire(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := e.get(h.ID); got.Status != store.RentalStatusCancelled || e.AuditCount(t, "rental_booking.hold_expire") != 1 {
			t.Fatalf("expired hold %+v", got.RentalBooking)
		}
		e.q32()
	})
}
