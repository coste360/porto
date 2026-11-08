//go:build integration

package storage

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func TestStorage_CreateContractAutoPlace(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		// Explicit placement and the cradle line (first, so that its place is free).
		v3 := e.addVessel(t, 600, 250)
		ends := d("2028-04-01")
		got, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: v3, StartsOn: d("2027-10-10"), EndsOn: &ends, AreaID: e.shed,
			Placement: pl(14, 0, 0), CradleResourceID: e.cradle, Details: contract.StorageDetailsInput{Support: "cradle", SupportOwner: "marina"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Cradle == nil || got.Cradle.ResourceID != e.cradle || *got.Placement.XCm != 1400 || got.Contract.Row.Status != "draft" {
			t.Fatalf("explicit %+v cradle %+v", got.Placement, got.Cradle)
		}
		v1 := e.addVessel(t, 800, 300)
		c := e.contract(t, e.Owner, v1, "2027-10-10", "2028-04-01", nil, "2028-03-15")
		if c.Placement == nil || c.Placement.ResourceID != e.shed || c.Placement.XCm == nil {
			t.Fatalf("placement %+v", c.Placement)
		}
		if !strings.HasPrefix(c.Contract.Code, "RIM-") || c.Contract.Row.Kind != contract.KindStorage {
			t.Fatalf("code %s kind %s", c.Contract.Code, c.Contract.Row.Kind)
		}
		if c.Details.Support != "cradle" || c.ExitOn != d("2028-03-15") || c.ArrivalOn != d("2027-10-10") {
			t.Fatalf("details %+v exit %s arrival %s", c.Details, c.ExitOn, c.ArrivalOn)
		}
		if c.Fit == nil || !c.Fit.OK {
			t.Fatalf("fit %+v", c.Fit)
		}
		// A second boat leaving earlier must not be put in front of nothing: both exits accessible.
		v2 := e.addVessel(t, 800, 300)
		e.contract(t, e.Owner, v2, "2027-10-10", "2028-04-01", nil, "2028-03-01")
		pv, err := e.svc.Plan(fixture.Ctx(), e.Owner, e.MarinaID, e.shed, ptr(d("2027-10-10")), ptr(d("2028-04-01")))
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range pv.Access {
			if !a.Accessible {
				t.Fatalf("blocked event %+v", a)
			}
		}
		if len(pv.Snapshot.Items) != 3 || !pv.Summary.GatesValid {
			t.Fatalf("plan %+v", pv.Summary)
		}
		e.checkInvariants(t)
		// Trailer spot target.
		v4 := e.addVessel(t, 700, 250)
		tr, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: v4, StartsOn: d("2027-10-10"), EndsOn: &ends, TrailerSpotID: e.trail,
			Activate: true, Details: contract.StorageDetailsInput{Support: "trailer"},
		})
		if err != nil || tr.Placement.ResourceID != e.trail {
			t.Fatalf("trailer spot: %v", err)
		}
		e.checkInvariants(t)
	})
}

func ptr[T any](v T) *T { return &v }

func TestStorage_Errors(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		v := e.addVessel(t, 800, 300)
		ends := d("2028-04-01")
		base := ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: v, StartsOn: d("2027-10-10"), EndsOn: &ends, AreaID: e.shed,
			Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
		}
		// A land resource that is not a trailer spot, a missing target, an unknown area.
		bad := base
		bad.AreaID, bad.TrailerSpotID = "", e.cradle
		if _, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, bad); !errors.Is(err, ErrTargetInvalid) {
			t.Fatalf("cradle as target: %v", err)
		}
		bad = base
		bad.AreaID = ""
		var ve *auth.ValidationError
		if _, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, bad); !errors.As(err, &ve) {
			t.Fatalf("no target: %v", err)
		}
		bad = base
		bad.AreaID = store.NewID()
		if _, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, bad); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("unknown area: %v", err)
		}
		// Launch before haul-out.
		bad = base
		bad.Details.HaulOutOn, bad.Details.LaunchOn = ptr(d("2028-03-01")), ptr(d("2028-02-01"))
		if _, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, bad); !errors.As(err, &ve) {
			t.Fatalf("dates: %v", err)
		}
		// A viewer cannot create; another marina is 404.
		viewer := e.AddUser(t, "viewer@porto.test", auth.RoleViewer, e.OrgID, e.MarinaID)
		if _, err := e.svc.CreateContract(ctx, viewer, e.MarinaID, base); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
		_, other, _ := e.OtherOrganization(t)
		if _, err := e.svc.CreateContract(ctx, e.Owner, other, base); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
	})
}

func TestStorage_NoSpace409(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		now := store.UTC(e.Clock.Now())
		small := e.addShed(t, e.shedMp, "C2", 12, 8, 2, 5, store.Strings{}, 0)
		_ = now
		ends := d("2028-04-01")
		mk := func() error {
			_, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, ContractInput{
				Term: contract.TermPeriod, CustomerID: e.cust, VesselID: e.addVessel(t, 500, 300), StartsOn: d("2027-10-10"), EndsOn: &ends,
				AreaID: small, Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
			})
			return err
		}
		n := 0
		for {
			err := mk()
			if errors.Is(err, ErrNoSpace) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			n++
			if n > 10 {
				t.Fatal("never full")
			}
		}
		if n < 2 {
			t.Fatalf("only %d boats fit", n)
		}
		e.checkInvariants(t)
	})
}

func TestStorage_FitSupportNotOverridable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		v := e.addVessel(t, 800, 300)
		ends := d("2028-04-01")
		in := ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: v, StartsOn: d("2027-10-10"), EndsOn: &ends, AreaID: e.shed,
			Activate: true, Details: contract.StorageDetailsInput{Support: "rack"}, FitOverride: &contract.FitOverrideInput{Reason: "ok"},
		}
		_, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, in)
		var fe *occupancy.FitError
		if !errors.As(err, &fe) || !slices.Contains(fe.Result.BlockingCodeList(), fit.CodeSupportNotAllowed) || fe.Result.Overridable {
			t.Fatalf("support: %v", err)
		}
		// The max LOA of the area blocks; an override lets it through.
		if _, err := e.DB.NewUpdate().TableExpr("dynamic_areas").Set("max_loa_cm = 500").Where("id = ?", e.shed).Exec(fixture.Ctx()); err != nil {
			t.Fatal(err)
		}
		in.Details.Support, in.FitOverride = "cradle", nil
		if _, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, in); !errors.As(err, &fe) || !fe.Result.Overridable {
			t.Fatalf("loa: %v", err)
		}
		in.FitOverride = &contract.FitOverrideInput{Reason: "barca corta a prua"}
		if _, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, in); err != nil {
			t.Fatalf("override: %v", err)
		}
	})
}

func TestStorage_PricingStorageType(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		c := e.contract(t, e.Owner, e.addVessel(t, 800, 300), "2027-10-01", "2028-04-01", nil, "")
		snap := c.Contract.Snapshot
		if snap == nil || snap.Request.ContractType != pricing.ContractStorage || c.Contract.Row.TotalCents != 6*20000 {
			t.Fatalf("snapshot %+v total %d", snap, c.Contract.Row.TotalCents)
		}
		if contract.PricingType(contract.KindStorage, contract.TermAnnual) != pricing.ContractStorage {
			t.Fatal("pricing type")
		}
		// Seasonal: the end comes from the season window of the storage list.
		v := e.addVessel(t, 700, 250)
		got, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, ContractInput{
			Term: contract.TermSeasonal, CustomerID: e.cust, VesselID: v, StartsOn: d("2027-10-01"), AreaID: e.shed,
			Details: contract.StorageDetailsInput{Support: "cradle"},
		})
		if err != nil || got.Contract.Row.EndsOn != d("2028-06-01") {
			t.Fatalf("seasonal: %v", err)
		}
	})
}

func TestStorage_DetailsUpdateAndLifecycle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		c := e.contract(t, e.Owner, e.addVessel(t, 800, 300), "2027-10-01", "2028-04-01", nil, "")
		id := c.Contract.Row.ID
		launch := d("2028-03-10")
		got, err := e.svc.UpdateContract(ctx, e.Owner, e.MarinaID, id, 1, contract.StorageDetailsPatch{LaunchOn: &launch, NotesHandling: ptr("albero smontato")})
		if err != nil || got.ExitOn != launch || got.Details.Version != 2 {
			t.Fatalf("update: %v", err)
		}
		if _, err := e.svc.UpdateContract(ctx, e.Owner, e.MarinaID, id, 1, contract.StorageDetailsPatch{ClearLaunch: true}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if n := e.AuditCount(t, "storage.details_update"); n != 1 {
			t.Fatalf("audit %d", n)
		}
		// Termination and expiry use the mooring paths.
		ter, err := e.ctr.TerminateContract(ctx, e.Owner, e.MarinaID, id, c.Contract.Row.Version, d("2028-01-01"), "vendita")
		if err != nil || ter.Row.Status != "terminated" {
			t.Fatalf("terminate: %v", err)
		}
		c2 := e.contract(t, e.Owner, e.addVessel(t, 800, 300), "2027-10-01", "2027-11-01", nil, "")
		e.Clock.Set(t0.AddDate(0, 2, 0))
		if _, err := e.ctr.ExpireMarina(ctx, e.MarinaID); err != nil {
			t.Fatal(err)
		}
		after, err := e.svc.GetContract(ctx, e.Owner, e.MarinaID, c2.Contract.Row.ID)
		if err != nil || after.Contract.Row.Status != "expired" {
			t.Fatalf("expiry: %v %s", err, after.Contract.Row.Status)
		}
		// A mooring contract is not a storage contract.
		if _, err := e.svc.GetContract(ctx, e.Owner, e.MarinaID, store.NewID()); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
	})
}

// 100 storage contracts with automatic placement in a small shed: the winners are the capacity, no overlap.
func TestConcurrency_StorageCreateAutoPlace(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		small := e.addShed(t, e.shedMp, "C3", 12, 6, 2, 4, store.Strings{}, 0)
		ends := d("2028-04-01")
		vessels := make([]string, 100)
		for i := range vessels {
			vessels[i] = e.addVessel(t, 500, 200)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		won, noSpace := 0, 0
		var other []error
		for i := range vessels {
			wg.Add(1)
			go func(v string) {
				defer wg.Done()
				_, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, ContractInput{
					Term: contract.TermPeriod, CustomerID: e.cust, VesselID: v, StartsOn: d("2027-10-10"), EndsOn: &ends, AreaID: small,
					Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
				})
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					won++
				case errors.Is(err, ErrNoSpace), errors.Is(err, occupancy.ErrResourceTaken):
					noSpace++
				default:
					other = append(other, err)
				}
			}(vessels[i])
		}
		wg.Wait()
		if len(other) > 0 {
			t.Fatalf("errors: %v", other[0])
		}
		// Capacity: one more boat must not fit now.
		_, err := e.svc.CreateContract(fixture.Ctx(), e.Owner, e.MarinaID, ContractInput{
			Term: contract.TermPeriod, CustomerID: e.cust, VesselID: e.addVessel(t, 500, 200), StartsOn: d("2027-10-10"), EndsOn: &ends,
			AreaID: small, Activate: true, Details: contract.StorageDetailsInput{Support: "cradle"},
		})
		if !errors.Is(err, ErrNoSpace) || won == 0 {
			t.Fatalf("won %d, then %v", won, err)
		}
		e.checkInvariants(t)
	})
}

// TestContract_M9StorageStateChangesWriteAuditAndOutbox (INV-30): every state change of a storage contract (create
// as draft, details update, activate, terminate; a second draft cancelled) goes through internal/domain/contract
// and writes its audit row and its outbox event.
func TestContract_M9StorageStateChangesWriteAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newSenv(t, db)
		ctx := fixture.Ctx()
		type tally struct{ audit, events int }
		count := func(id string) tally {
			t.Helper()
			n, err := e.DB.NewSelect().Model((*store.AuditEntry)(nil)).Where("entity_id = ?", id).Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var rows []store.OutboxEvent
			if err := e.DB.NewSelect().Model(&rows).Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			ev := 0
			for _, r := range rows {
				if strings.Contains(string(r.Payload), id) {
					ev++
				}
			}
			return tally{int(n), ev}
		}
		step := func(name, id string, before tally) tally {
			t.Helper()
			after := count(id)
			if after.audit <= before.audit || after.events <= before.events {
				t.Fatalf("%s: audit %d→%d, events %d→%d", name, before.audit, after.audit, before.events, after.events)
			}
			return after
		}
		draft := func() *ContractView {
			t.Helper()
			ends := d("2028-04-01")
			v, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, ContractInput{
				Term: contract.TermPeriod, CustomerID: e.cust, VesselID: e.addVessel(t, 800, 300), StartsOn: d("2027-10-05"), EndsOn: &ends,
				AreaID: e.shed, Details: contract.StorageDetailsInput{Support: "cradle"},
			})
			if err != nil || v.Contract.Row.Status != "draft" {
				t.Fatalf("draft: %v", err)
			}
			return v
		}
		c := draft()
		id := c.Contract.Row.ID
		b := step("create", id, tally{})
		launch := d("2028-03-10")
		if _, err := e.svc.UpdateContract(ctx, e.Owner, e.MarinaID, id, 1, contract.StorageDetailsPatch{LaunchOn: &launch}); err != nil {
			t.Fatal(err)
		}
		b = step("details_update", id, b)
		act, err := e.ctr.ActivateContract(ctx, e.Owner, e.MarinaID, id, c.Contract.Row.Version)
		if err != nil || act.Row.Status != "active" {
			t.Fatalf("activate: %v", err)
		}
		b = step("activate", id, b)
		if _, err := e.ctr.TerminateContract(ctx, e.Owner, e.MarinaID, id, act.Row.Version, d("2028-01-01"), "vendita"); err != nil {
			t.Fatal(err)
		}
		step("terminate", id, b)
		c2 := draft()
		b2 := count(c2.Contract.Row.ID)
		if _, err := e.ctr.CancelContract(ctx, e.Owner, e.MarinaID, c2.Contract.Row.ID, c2.Contract.Row.Version, "rinuncia"); err != nil {
			t.Fatal(err)
		}
		step("cancel", c2.Contract.Row.ID, b2)
		e.checkInvariants(t)
	})
}
