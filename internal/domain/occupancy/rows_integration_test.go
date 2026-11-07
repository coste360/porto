//go:build integration

package occupancy

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"
	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// INV-03 and INV-10: random sequences of allocate, move, swap, layout, release, revoke, truncate, resize, cancel
// and undo on a small harbour; after every step the invariant queries (Q1–Q16, including rows = plan) are empty.
func TestAllocation_RowsMatchFootprint(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newHarbour(t, db)
		vessels := []string{h.vessel(t, 1000, 350), h.vessel(t, 900, 300), h.vessel(t, 1150, 390), h.vessel(t, 700, 250)}
		var contracts []string // contract allocations ids
		var undos []string
		expected := func(err error) bool {
			var pe *PlacementError
			var fe *FitError
			return err == nil || isTaken(err) || errors.As(err, &pe) || errors.As(err, &fe) || errors.Is(err, ErrVersionMismatch) ||
				errors.Is(err, ErrAllocationNotActive) || errors.Is(err, ErrReleaseInvalid) || errors.Is(err, ErrUndoUnavailable) ||
				errors.Is(err, auth.ErrInvalidInput) || errors.Is(err, ErrClosed)
		}
		rapid.Check(t, func(rt *rapid.T) {
			for range rapid.IntRange(1, 6).Draw(rt, "steps") {
				d := h.today.AddDays(rapid.IntRange(0, 20).Draw(rt, "day"))
				n := rapid.IntRange(0, 8).Draw(rt, "nights")
				v := rapid.SampledFrom(vessels).Draw(rt, "vessel")
				var spec engine.AllocSpec
				if rapid.Bool().Draw(rt, "area") {
					spec = areaSpec(h.area, geo.Cm(rapid.IntRange(3, 550).Draw(rt, "x"))*10, d, d.AddDays(n))
				} else {
					spec = berthSpec(rapid.SampledFrom(h.berths).Draw(rt, "berth"), d, d.AddDays(n))
				}
				active, err := store.ListAllocations(ctxT(), db.DB, h.scope, store.AllocationFilter{Status: store.AllocActive})
				if err != nil {
					rt.Fatal(err)
				}
				var vesselAllocs []store.Allocation
				for _, a := range active {
					if a.VesselID != "" {
						vesselAllocs = append(vesselAllocs, a)
					}
				}
				pick := func(label string) *store.Allocation {
					if len(vesselAllocs) == 0 {
						return nil
					}
					a := vesselAllocs[rapid.IntRange(0, len(vesselAllocs)-1).Draw(rt, label)]
					return &a
				}
				op := rapid.SampledFrom([]string{"transit", "contract", "move", "swap", "layout", "release", "revoke", "truncate", "resize", "cancel", "undo"}).Draw(rt, "op")
				switch op {
				case "transit":
					_, err = h.allocate(ctxT(), transit(v, spec))
				case "contract":
					// an expired mooring contract: its rows are history, no coverage rule applies to random edits
					_, line := h.contractLine(rt, store.ContractKindMooring, "expired", v, string(spec.ResourceType), spec.ResourceID, spec.First, spec.Last)
					var a *store.Allocation
					a, err = h.allocate(ctxT(), NewAllocation{ContractLineID: &line, VesselID: &v, Kind: KindContract, Spec: spec})
					if err == nil {
						contracts = append(contracts, a.ID)
					}
				case "move":
					if a := pick("src"); a != nil {
						var res MoveResult
						tg := berthTarget(rapid.SampledFrom(h.berths).Draw(rt, "to"))
						if rapid.Bool().Draw(rt, "to_area") {
							tg = areaTarget(h.area, geo.Cm(rapid.IntRange(3, 550).Draw(rt, "tx"))*10)
						}
						var to *calendar.Date
						if rapid.Bool().Draw(rt, "interval") {
							x := a.FirstNight.AddDays(rapid.IntRange(0, 5).Draw(rt, "to_night"))
							to = &x
						}
						res, err = h.s.Move(ctxT(), h.Owner, h.MarinaID, MoveInput{AllocationID: a.ID, Version: a.Version, Target: tg, ToNight: to})
						if err == nil {
							undos = append(undos, res.UndoID)
						}
					}
				case "swap":
					a, b := pick("a"), pick("b")
					if a != nil && b != nil && a.ID != b.ID {
						var res MoveResult
						res, err = h.s.Swap(ctxT(), h.Owner, h.MarinaID, SwapInput{A: BaseVersion{a.ID, a.Version}, B: BaseVersion{b.ID, b.Version}})
						if err == nil {
							undos = append(undos, res.UndoID)
						}
					}
				case "layout":
					cur, _ := store.ListAllocations(ctxT(), db.DB, h.scope, store.AllocationFilter{ResourceIDs: []string{h.area}, Status: store.AllocActive, From: h.today, To: h.today})
					var base []BaseVersion
					var ops []LayoutOp
					for _, a := range cur {
						if a.VesselID == "" {
							continue
						}
						base = append(base, BaseVersion{a.ID, a.Version})
						ops = append(ops, LayoutOp{AllocationID: a.ID, Placement: engine.Placement{X: geo.Cm(rapid.IntRange(3, 550).Draw(rt, "lx")) * 10}})
					}
					if len(ops) > 0 {
						_, err = h.s.SaveLayout(ctxT(), h.Owner, h.MarinaID, LayoutInput{AreaID: h.area, Date: h.today, Base: base, Placements: ops})
					}
				case "release":
					if len(contracts) > 0 {
						a, gerr := store.GetAllocation(ctxT(), db.DB, h.scope, rapid.SampledFrom(contracts).Draw(rt, "c"))
						if gerr == nil && a.Status == store.AllocActive {
							from := a.FirstNight.AddDays(rapid.IntRange(0, 3).Draw(rt, "rf"))
							_, err = h.s.CreateRelease(ctxT(), h.Owner, h.MarinaID, ReleaseInput{AllocationID: a.ID, From: from, To: from.AddDays(rapid.IntRange(0, 3).Draw(rt, "rl")), Source: "office"})
						}
					}
				case "revoke":
					rels, _ := store.ListReleases(ctxT(), db.DB, h.scope, store.ReleaseFilter{Status: store.ReleaseActive}, "", 50)
					if len(rels.Items) > 0 {
						r := rels.Items[rapid.IntRange(0, len(rels.Items)-1).Draw(rt, "rel")]
						_, err = h.s.RevokeRelease(ctxT(), h.Owner, h.MarinaID, r.ID, r.Version, nil, nil)
					}
				case "truncate", "resize", "cancel":
					if a := pick("t"); a != nil {
						err = db.RunInTx(ctxT(), func(ctx context.Context, tx bun.Tx) error {
							switch op {
							case "truncate":
								return h.s.TruncateTx(ctx, tx, h.scope, a.ID, a.FirstNight.AddDays(rapid.IntRange(0, 6).Draw(rt, "tn")))
							case "resize":
								first := a.FirstNight.AddDays(rapid.IntRange(-2, 2).Draw(rt, "rs"))
								return h.s.ResizeTx(ctx, tx, h.scope, h.s.now(), a.ID, first, first.AddDays(rapid.IntRange(0, 8).Draw(rt, "rn")))
							}
							return h.s.CancelTx(ctx, tx, h.scope, a.ID, StatusCancelled)
						})
					}
				case "undo":
					if len(undos) > 0 {
						_, err = h.s.Undo(ctxT(), h.Owner, h.MarinaID, rapid.SampledFrom(undos).Draw(rt, "u"))
					}
				}
				if !expected(err) {
					rt.Fatalf("%s: unexpected error %v", op, err)
				}
				vs, err := store.CheckOccupancyInvariants(ctxT(), db.DB, h.MarinaID, h.today)
				if err != nil {
					rt.Fatal(err)
				}
				for _, x := range vs {
					if len(x.IDs) > 0 {
						if a, err := store.GetAllocation(ctxT(), db.DB, h.scope, x.IDs[0]); err == nil {
							rows, _ := store.RowsOfAllocation(ctxT(), db.DB, h.scope, a)
							rt.Logf("allocation %+v rows %d first %+v", *a, len(rows), rows[:min(3, len(rows))])
						}
					}
					rt.Fatalf("after %s: %s %v %s", op, x.Query, x.IDs, x.Detail)
				}
			}
		})
	})
}
