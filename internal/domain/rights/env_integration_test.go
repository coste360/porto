//go:build integration

package rights

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// testEnv is the contract test harbour (berths A1..A4, a vessel, a customer, an active 2027 list) with the rights
// service on top (no docs or signature services: transfers complete on paper or through the subject callback).
type testEnv struct {
	*contracttest.Env
	Rights *Service
}

func newEnv(t *testing.T, db *store.DB) *testEnv {
	t.Helper()
	ce := contracttest.New(t, db)
	s := NewService(Deps{
		DB: db, Clock: ce.Clock, Audit: ce.Audit, Outbox: ce.Outbox, Keys: ce.Keys, Occupancy: ce.Occupancy, Prices: ce.Prices, Log: ce.Log,
	})
	return &testEnv{Env: ce, Rights: s}
}

var ctx = fixture.Ctx()

func d(s string) calendar.Date { return calendar.MustParse(s) }

// today is the marina date of contracttest.T0 (2027-06-02).
const today = "2027-06-02"

func (e *testEnv) right(t *testing.T, berth string, from, to string, holders []allot.Share, vessel string) *Right {
	t.Helper()
	in := RightInput{BerthID: berth, Kind: KindOwnershipShare, FromOn: d(from), ToOn: d(to), Holders: holders, Title: Title{Deed: "atto", Number: "1"}}
	if vessel != "" {
		in.Vessels = []VesselInput{{VesselID: vessel, Primary: true}}
	}
	r, err := e.Rights.CreateRight(ctx, e.Owner, e.MarinaID, in)
	if err != nil {
		t.Fatalf("create right: %v", err)
	}
	return r
}

func (e *testEnv) one(id string) []allot.Share { return []allot.Share{{CustomerID: id, Millis: 1000}} }

// ownerRows counts the berth_occupancies rows of kind owner on a berth.
func (e *testEnv) ownerRows(t *testing.T, berth string) int {
	t.Helper()
	n, err := e.DB.NewSelect().TableExpr("berth_occupancies").Where("berth_id = ?", berth).Where("kind = 'owner'").Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func (e *testEnv) invariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckOccupancyInvariants(context.Background(), e.DB.DB, e.MarinaID, d(today))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s %v %s", v.Query, v.IDs, v.Detail)
	}
	e.sharesInvariant(t)
}

// sharesInvariant runs C40 (INV-40: the holder shares of every active right sum to 1000) and C31h (INV-31: the
// live holder instalments of every fee line and common run sum to their line).
func (e *testEnv) sharesInvariant(t *testing.T) {
	t.Helper()
	vs, err := store.CheckContractInvariants(context.Background(), e.DB.DB, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.Query == "C40" || v.Query == "C31h" {
			t.Errorf("invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
	}
}

func (e *testEnv) scope(t *testing.T) store.MarinaScope { return e.Scope(t) }

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

func isErr(err, target error) bool { return errors.Is(err, target) }
