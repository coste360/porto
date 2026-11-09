//go:build integration

package presence_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestRecordSensorTx covers the automatic presence of the M12 reconciliation (m12-plan §8.2 R3/R3b, D12): source
// sensor, audit actor system, presence event, no contract hook; transits, other events and offline replays refused;
// a second departure is ErrInvalidTransition.
func TestRecordSensorTx(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		scope := e.Scope(t)
		vessel := e.AddVessel(t, "Maestrale", 900, 300, "monohull_motor")
		c, err := e.Contracts.CreateContract(ctx, e.Owner, e.MarinaID, contract.MooringInput{
			Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: vessel, StartsOn: contracttest.D("2027-01-01"),
			Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[2], "2027-01-01", "2027-12-31")}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		alloc := c.Allocations[0].ID
		tr := e.Transit(t, e.Berths[0], "2027-06-01", "2027-06-04", true)
		record := func(ctx context.Context, v, a, ev string, at time.Time) (*store.Presence, error) {
			var out *store.Presence
			err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				var err error
				out, err = e.Presence.RecordSensorTx(ctx, tx, scope, v, a, ev, at)
				return err
			})
			return out, err
		}
		left := e.Clock.Now().Add(-6 * time.Hour)
		// A vessel without a presence row departs (no contract hook, contract unchanged).
		pr, err := record(context.Background(), vessel, alloc, presence.EventDeparted, left)
		if err != nil {
			t.Fatalf("departure: %v", err)
		}
		if pr.Source != presence.SourceSensor || pr.UserID != "" || !pr.At.Equal(store.UTC(left)) || pr.ContractID != c.Row.ID {
			t.Fatalf("presence %+v", pr)
		}
		vp, _ := store.GetVesselPresence(context.Background(), db.DB, scope, vessel)
		if vp.State != presence.StateOut {
			t.Fatalf("state %+v", vp)
		}
		got, _ := store.GetContract(context.Background(), db.DB, scope, c.Row.ID)
		if got.Status != c.Row.Status || got.Version != c.Row.Version {
			t.Fatal("contract changed by a sensor presence")
		}
		var actors []string
		if err := db.NewSelect().Table("audit_log").Column("actor_type").Where("action = 'presence.record'").Where("entity_id = ?", pr.ID).Scan(context.Background(), &actors); err != nil || len(actors) != 1 || actors[0] != "system" {
			t.Fatalf("audit %v %v", actors, err)
		}
		if len(e.Events(t, event.PresenceDeparted)) != 1 {
			t.Fatal("presence event")
		}
		// Twice: invalid transition.
		if _, err := record(context.Background(), vessel, alloc, presence.EventDeparted, left); !errors.Is(err, presence.ErrInvalidTransition) {
			t.Fatalf("second departure: %v", err)
		}
		// Return.
		if _, err := record(context.Background(), vessel, alloc, presence.EventArrived, e.Clock.Now().Add(-time.Hour)); err != nil {
			t.Fatalf("return: %v", err)
		}
		// Transits, haul-outs, a missing allocation and offline replays are refused.
		if _, err := record(context.Background(), e.Vessel, tr.Allocations[0].ID, presence.EventArrived, e.Clock.Now()); !errors.Is(err, presence.ErrSensorNotAllowed) {
			t.Fatalf("transit: %v", err)
		}
		if _, err := record(context.Background(), vessel, alloc, presence.EventHauledOut, e.Clock.Now()); !errors.Is(err, presence.ErrSensorNotAllowed) {
			t.Fatalf("haul-out: %v", err)
		}
		if _, err := record(context.Background(), vessel, "", presence.EventDeparted, e.Clock.Now()); !errors.Is(err, presence.ErrSensorNotAllowed) {
			t.Fatalf("no allocation: %v", err)
		}
		off := reqctx.With(context.Background(), &reqctx.Info{OfflineQueuedAt: e.Clock.Now()})
		if _, err := record(off, vessel, alloc, presence.EventDeparted, e.Clock.Now()); !errors.Is(err, presence.ErrSensorNotAllowed) {
			t.Fatalf("offline: %v", err)
		}
		var n int64
		n, _ = db.NewSelect().Table("presences").Where("source = 'sensor'").Where("contract_id = ?", tr.Row.ID).Count(context.Background())
		if n != 0 {
			t.Fatal("sensor presence on a transit (Q43)")
		}
		// Only annual moorings (INV-100): a seasonal mooring is refused.
		v2 := e.AddVessel(t, "Libeccio", 900, 300, "monohull_motor")
		sc, err := e.Contracts.CreateContract(ctx, e.Owner, e.MarinaID, contract.MooringInput{
			Term: contract.TermSeasonal, CustomerID: e.Customer, VesselID: v2, StartsOn: contracttest.D("2027-05-01"),
			Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[3], "2027-05-01", "2027-09-30")}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := record(context.Background(), v2, sc.Allocations[0].ID, presence.EventDeparted, left); !errors.Is(err, presence.ErrSensorNotAllowed) {
			t.Fatalf("seasonal: %v", err)
		}
		if vs, err := store.CheckSensorAlertInvariants(context.Background(), db.DB, e.MarinaID); err != nil || len(vs) != 0 {
			t.Fatalf("Q42/Q43 %v %+v", err, vs)
		}
		// Q43 flags a sensor presence on a mooring that is not annual (here the annual contract rewritten in place).
		if _, err := db.NewUpdate().Table("contracts").Set("term = 'seasonal'").Where("id = ?", c.Row.ID).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		vs, err := store.CheckSensorAlertInvariants(context.Background(), db.DB, e.MarinaID)
		if err != nil || len(vs) == 0 || vs[0].Query != "Q43" || vs[0].Detail != "not_annual_mooring" {
			t.Fatalf("Q43 on a seasonal mooring: %v %+v", err, vs)
		}
	})
}
