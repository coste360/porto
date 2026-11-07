//go:build integration

package contract

import (
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestTransit_Lifecycle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		c := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-13", false)
		if c.Row.Status != string(StatusHeld) || c.Code != "TRN-2027-0001" || c.Row.TotalCents != 3*3420 {
			t.Fatalf("held transit: status %s code %s total %d", c.Row.Status, c.Code, c.Row.TotalCents)
		}
		if len(c.Allocations) != 1 || c.Allocations[0].Kind != store.AllocKindHold || e.rows(t, c.Allocations[0].ID) != 3 {
			t.Fatalf("allocations %+v", c.Allocations)
		}
		if c.HoldExpiresAt.IsZero() || !c.HoldExpiresAt.Equal(store.UTC(t0.Add(24*time.Hour))) {
			t.Fatalf("hold expiry %v", c.HoldExpiresAt)
		}
		// Overlapping transit on the same berth: 409, nothing written.
		_, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, TransitInput{
			VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-06-12"), Departure: d("2027-06-14"),
			Placements: []Segment{berthSeg(e.berths[0], "2027-06-12", "2027-06-13")},
		})
		if !errors.Is(err, occupancy.ErrResourceTaken) {
			t.Fatalf("overlap: %v", err)
		}
		c, err = e.svc.ConfirmTransit(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, ManualConfirmation{Reference: "ricevuta 7", PayAtDeparture: true})
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.Status != string(StatusConfirmed) || c.Row.ConfirmationSource != SourceManual || !c.Row.PayAtDeparture ||
			c.Allocations[0].Kind != store.AllocKindTransit || !c.HoldExpiresAt.IsZero() {
			t.Fatalf("confirmed: %+v allocs %+v", c.Row, c.Allocations)
		}
		// Confirm twice: invalid transition.
		if _, err := e.svc.ConfirmTransit(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, ManualConfirmation{}); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("second confirm: %v", err)
		}
		// Stale version.
		if _, err := e.svc.CancelTransit(ctx, e.Owner, e.MarinaID, c.Row.ID, 1, ""); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale cancel: %v", err)
		}
		// Stay change: one more night.
		dep := d("2027-06-14")
		c, eff, err := e.svc.ChangeStay(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, StayChange{Departure: &dep})
		if err != nil {
			t.Fatal(err)
		}
		if !c.Row.EndsOn.Equal(dep) || e.rows(t, c.Allocations[0].ID) != 4 || eff.DeltaCents != 3420 {
			t.Fatalf("stay: ends %s rows %d effect %+v", c.Row.EndsOn, e.rows(t, c.Allocations[0].ID), eff)
		}
		if n := e.count(t, "outbox_events", "type", event.TransitConfirmed); n != 1 {
			t.Fatalf("transit.confirmed events %d", n)
		}
		// Cancel frees every row.
		c, err = e.svc.CancelTransit(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, "cambio programma")
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.Status != string(StatusCancelled) || c.Allocations[0].Status != store.AllocCancelled || e.rows(t, c.Allocations[0].ID) != 0 {
			t.Fatalf("cancelled: %+v", c.Allocations)
		}
		// The berth is free again.
		e.transit(t, e.Owner, e.berths[0], "2027-06-12", "2027-06-14", true)
		e.invariants(t)
	})
}

func TestTransit_PermissionsAndIsolation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		yard := e.AddUser(t, "yard@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		c := e.transit(t, dock, e.berths[1], "2027-06-10", "2027-06-12", true)
		if _, err := e.svc.GetContract(ctx, yard, e.MarinaID, c.Row.ID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard read: %v", err)
		}
		in := TransitInput{
			VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-06-20"), Departure: d("2027-06-21"),
			Placements: []Segment{berthSeg(e.berths[1], "2027-06-20", "2027-06-20")},
		}
		if _, err := e.svc.CreateTransit(ctx, yard, e.MarinaID, in); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard create: %v", err)
		}
		in.FitOverride = &FitOverrideInput{Reason: "deroga"}
		if _, err := e.svc.CreateTransit(ctx, dock, e.MarinaID, in); !errors.Is(err, occupancy.ErrOverrideForbidden) {
			t.Fatalf("dock override: %v", err)
		}
		_, other, otherOwner := e.OtherOrganization(t)
		if _, err := e.svc.GetContract(ctx, otherOwner, other, c.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		if _, err := e.svc.GetContract(ctx, otherOwner, e.MarinaID, c.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign principal: %v", err)
		}
		// A customer of another marina is 404.
		in.FitOverride = nil
		in.CustomerID = store.NewID()
		if _, err := e.svc.CreateTransit(ctx, dock, e.MarinaID, in); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign customer: %v", err)
		}
		// Confirmation without a customer.
		in.CustomerID, in.Confirm = "", &ManualConfirmation{}
		if _, err := e.svc.CreateTransit(ctx, dock, e.MarinaID, in); !errors.Is(err, ErrCustomerRequired) {
			t.Fatalf("no customer: %v", err)
		}
	})
}

// TestTransit_ConfirmAfterHoldExpiry: a hold past its expiry (not yet reclaimed by the job) is lost; the
// confirmation allocates the nights again, or fails with resource_taken when someone else took them.
func TestTransit_ConfirmAfterHoldExpiry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		a := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-12", false)
		b := e.transit(t, e.Owner, e.berths[1], "2027-06-10", "2027-06-12", false)
		e.Clock.Advance(25 * time.Hour)
		c, err := e.svc.ConfirmTransit(ctx, e.Owner, e.MarinaID, a.Row.ID, a.Row.Version, ManualConfirmation{})
		if err != nil {
			t.Fatal(err)
		}
		act := activeOnly(c.Allocations)
		if c.Row.Status != string(StatusConfirmed) || len(act) != 1 || act[0].Kind != store.AllocKindTransit || act[0].MovedFromID != a.Allocations[0].ID ||
			e.rows(t, act[0].ID) != 2 {
			t.Fatalf("confirmed after expiry %+v", c.Allocations)
		}
		// Someone takes b's berth (its expired hold is reclaimed at conflict time, INV-05).
		e.transit(t, e.Owner, e.berths[1], "2027-06-11", "2027-06-12", true)
		if _, err := e.svc.ConfirmTransit(ctx, e.Owner, e.MarinaID, b.Row.ID, b.Row.Version, ManualConfirmation{}); !errors.Is(err, occupancy.ErrResourceTaken) {
			t.Fatalf("confirm of a lost hold on a taken berth: %v", err)
		}
		e.invariants(t)
	})
}
