//go:build integration

package acceptance

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// quotaGroup puts every berth of the world in a group with an online count quota.
func (w *m8World) quotaGroup(t *testing.T, value int64, berths []string) string {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(w.clock.Now())
	g := &store.BerthGroup{
		ID: store.NewID(), MarinaID: w.marina, Code: "Q" + strconv.Itoa(int(value)), Name: store.JSON(`{"it":"Quota"}`), Kind: "custom",
		OnlineQuotaKind: "count", OnlineQuotaValue: value, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := w.h.app.DB.NewInsert().Model(g).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range berths {
		var mapID string
		if err := w.h.app.DB.NewSelect().TableExpr("berths").Column("map_id").Where("id = ?", id).Scan(ctx, &mapID); err != nil {
			t.Fatal(err)
		}
		if _, err := w.h.app.DB.NewInsert().Model(&store.BerthGroupMember{
			ID: store.NewID(), MarinaID: w.marina, GroupID: g.ID, MapID: mapID, ResourceType: "berth", ResourceID: id, CreatedAt: now,
		}).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return g.ID
}

func (w *m8World) onlineCreate(ctx context.Context, key string, arrival, departure calendar.Date) (*booking.Created, error) {
	return w.h.app.Booking.Create(ctx, "online", booking.CreateInput{
		Arrival: arrival, Departure: departure, Category: "L12", Locale: "it", IdempotencyKey: key, BodySHA256: "body-" + key,
		Vessel: booking.Vessel{LOACm: 1000, BeamCm: 350},
	})
}

func (w *m8World) claimsPerNight(t *testing.T, groupID string) map[string]int {
	t.Helper()
	type row struct {
		Night calendar.Date `bun:"night"`
		N     int           `bun:"n"`
	}
	var rows []row
	if err := w.h.app.DB.NewRaw("SELECT night, count(*) AS n FROM online_quota_claims WHERE group_id = ? GROUP BY night", groupID).
		Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.Night.String()] = r.N
	}
	return out
}

// TestConcurrency_OnlineQuotaNeverExceeded (INV-12): 100 online bookings in parallel on 10 berths with a quota of 3:
// exactly 3 win, used = claims ≤ cap on every night (Q19).
func TestConcurrency_OnlineQuotaNeverExceeded(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		extra := contracttest.InsertHarbour(t, db, w.marina, store.UTC(w.clock.Now()), 7)
		if _, err := db.NewUpdate().Table("berths").Set("online_sellable = ?", true).Where("marina_id = ?", w.marina).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		g := w.quotaGroup(t, 3, append(append([]string{}, w.harbour.Berths...), extra.Berths...))
		arr, dep := calendar.MustParse(m8Arrival), calendar.MustParse(m8Departure)
		var wins atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make(chan error, 100)
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := w.onlineCreate(context.Background(), "k-q-"+strconv.Itoa(1000+i), arr, dep)
				switch {
				case err == nil:
					wins.Add(1)
				case errors.Is(err, booking.ErrUnavailable):
				default:
					errs <- err
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("unexpected error: %v", err)
		}
		if wins.Load() != 3 {
			t.Fatalf("winners %d, want 3", wins.Load())
		}
		for night, n := range w.claimsPerNight(t, g) {
			if n > 3 {
				t.Fatalf("night %s: %d claims over the quota", night, n)
			}
		}
		w.invariants(t)
	})
}

// TestConcurrency_NoDoubleSaleDeskOnlineChannel (INV-13, m8-plan §14.3): 120 goroutines (40 desk, 40 online, 40
// channel) on 3 online berths with a quota of 2, same or shifted stays: at most one winner per resource-night,
// online plus channel claims within the quota, every loser refused with resource_taken, booking_unavailable or a
// channel rejection.
func TestConcurrency_NoDoubleSaleDeskOnlineChannel(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.connectChannel(t)
		g := w.quotaGroup(t, 2, w.harbour.Berths)
		p := w.deskPrincipal(t)
		stays := [][2]string{{"2027-06-10", "2027-06-12"}, {"2027-06-11", "2027-06-13"}, {"2027-06-09", "2027-06-11"}}
		var desk, online, chWins atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make(chan error, 120)
		for i := range 120 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				st := stays[i%3]
				arr, dep := calendar.MustParse(st[0]), calendar.MustParse(st[1])
				ctx := context.Background()
				switch i % 3 {
				case 0: // desk: explicit placement on an online berth (A3; A1 and A2 are raced by online and channel too)
					berth := w.harbour.Berths[[]int{2, 2, 0}[(i/3)%3]]
					_, err := w.h.app.Contracts.CreateTransit(ctx, p, w.marina, contract.TransitInput{
						NewVessel: &contract.NewVessel{Name: "Desk " + strconv.Itoa(i), LOACm: 1000, BeamCm: 350},
						Arrival:   arr, Departure: dep,
						Placements: []contract.Segment{contracttest.BerthSeg(berth, st[0], dep.AddDays(-1).String())},
					})
					switch {
					case err == nil:
						desk.Add(1)
					case errors.Is(err, occupancy.ErrResourceTaken):
					default:
						errs <- err
					}
				case 1:
					_, err := w.onlineCreate(ctx, "k-mix-"+strconv.Itoa(1000+i), arr, dep)
					switch {
					case err == nil:
						online.Add(1)
					case errors.Is(err, booking.ErrUnavailable):
					default:
						errs <- err
					}
				default:
					res := m8Reservation("MIX-"+strconv.Itoa(i), "mix-e-"+strconv.Itoa(i), ext.ChannelReservationNew, true)
					res.Arrival, res.Departure = st[0], st[1]
					if err := w.h.app.Channels.ApplyReservation(ctx, w.scope, "fake", res); err != nil {
						errs <- err
					}
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("unexpected error: %v", err)
		}
		booked, _ := db.NewSelect().TableExpr("channel_reservations").Where("marina_id = ? AND status = 'booked'", w.marina).Count(context.Background())
		rejected, _ := db.NewSelect().TableExpr("channel_reservations").Where("marina_id = ? AND status = 'rejected'", w.marina).Count(context.Background())
		chWins.Store(int32(booked))
		if booked+rejected != 40 {
			t.Fatalf("channel outcomes %d booked + %d rejected", booked, rejected)
		}
		total := desk.Load() + online.Load() + chWins.Load()
		if desk.Load() == 0 || online.Load()+chWins.Load() == 0 {
			t.Fatalf("every channel should win somewhere: desk %d online %d channel %d", desk.Load(), online.Load(), chWins.Load())
		}
		_ = total
		w.invariants(t) // Q1 (one row per resource-night), Q6, Q7, Q8, Q15, Q17, Q19
		for night, n := range w.claimsPerNight(t, g) {
			if n > 2 {
				t.Fatalf("night %s: %d online/channel claims over the quota of 2", night, n)
			}
		}
		// Every winner holds whole stays: the occupied berth-nights equal the nights of the live transits.
		var rows int
		if err := db.NewRaw("SELECT count(*) FROM berth_occupancies WHERE marina_id = ?", w.marina).Scan(context.Background(), &rows); err != nil {
			t.Fatal(err)
		}
		var allocs []store.Allocation
		if err := db.NewSelect().Model(&allocs).Where("marina_id = ? AND status = 'active'", w.marina).Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		nights := 0
		for _, a := range allocs {
			nights += a.FirstNight.DaysUntil(a.LastNight) + 1
		}
		if rows != nights || rows > 3*4 {
			t.Fatalf("occupied rows %d, allocated nights %d (3 berths × 4 nights at most)", rows, nights)
		}
		t.Logf("winners: desk %d, online %d, channel %d", desk.Load(), online.Load(), chWins.Load())
	})
}

// TestConcurrency_ChannelReservationOnce (INV-89): the same event applied 100 times in parallel books once.
func TestConcurrency_ChannelReservationOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.connectChannel(t)
		res := m8Reservation("ONCE-1", "once-e1", ext.ChannelReservationNew, true)
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make(chan error, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if err := w.h.app.Channels.ApplyReservation(context.Background(), w.scope, "fake", res); err != nil {
					errs <- err
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("apply: %v", err)
		}
		for table, want := range map[string]int64{"channel_reservations": 1, "channel_events": 1, "online_bookings": 1} {
			n, _ := db.NewSelect().TableExpr(table).Where("marina_id = ?", w.marina).Count(context.Background())
			if n != want {
				t.Fatalf("%s: %d rows", table, n)
			}
		}
		w.invariants(t)
	})
}

// TestConcurrency_CreateBookingSameKey: 100 parallel creations with one Idempotency-Key make one booking, all
// answered with the same booking and token.
func TestConcurrency_CreateBookingSameKey(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		arr, dep := calendar.MustParse(m8Arrival), calendar.MustParse(m8Departure)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var mu sync.Mutex
		ids := map[string]bool{}
		errs := make(chan error, 100)
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				c, err := w.onlineCreate(context.Background(), "k-same-key", arr, dep)
				if err != nil {
					errs <- err
					return
				}
				mu.Lock()
				ids[c.BookingID+"|"+c.BookingToken] = true
				mu.Unlock()
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("create: %v", err)
		}
		n, _ := db.NewSelect().TableExpr("online_bookings").Where("marina_id = ?", w.marina).Count(context.Background())
		if n != 1 || len(ids) != 1 {
			t.Fatalf("bookings %d, distinct answers %d", n, len(ids))
		}
		w.invariants(t)
	})
}
