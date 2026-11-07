package occupancy

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

func init() { store.RegisterPlanCheck(CheckPlans) }

// CheckPlans is the part of store.CheckOccupancyInvariants that needs the pure engines (m3-plan §6):
// Q14 (INV-07) every active vessel allocation passes fit.Check or carries a fit_override with a reason; Q15
// (INV-03) the rows of every active allocation are exactly engine.Plan(spec with its active releases). Blocks
// have no vessel: their rows must be the same cells on every non-released night of their period.
func CheckPlans(ctx context.Context, db bun.IDB, marinaID string, today calendar.Date) ([]store.Violation, error) {
	scope, err := store.SystemMarinaScope(ctx, db, marinaID)
	if err != nil {
		return nil, err
	}
	allocs, err := store.ListAllocations(ctx, db, scope, store.AllocationFilter{Status: store.AllocActive})
	if err != nil {
		return nil, err
	}
	rows, err := store.OccupancyRowsOfMarina(ctx, db, marinaID)
	if err != nil {
		return nil, err
	}
	byAlloc := map[string][]engine.Row{}
	for _, r := range rows {
		byAlloc[r.AllocationID] = append(byAlloc[r.AllocationID], engine.Row{Resource: r.Resource, Cell: r.Cell, Night: r.Night})
	}
	res, err := store.LoadMapResources(ctx, db, scope, "", false)
	if err != nil {
		return nil, err
	}
	resources := map[string]*resource{}
	for i := range res.Berths {
		resources[res.Berths[i].ID] = berthResource(&res.Berths[i])
	}
	for i := range res.Areas {
		if r, err := areaResource(&res.Areas[i]); err == nil {
			resources[r.ID] = r
		}
	}
	for i := range res.Lands {
		resources[res.Lands[i].ID] = landResource(&res.Lands[i])
	}
	var ids, vids []string
	for _, a := range allocs {
		ids, vids = append(ids, a.ID), append(vids, a.VesselID)
	}
	rels, err := store.ActiveReleasesOf(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, uniq(vids))
	if err != nil {
		return nil, err
	}
	ins, err := store.InsuranceUntil(ctx, db, scope, uniq(vids))
	if err != nil {
		return nil, err
	}
	vessels := map[string]*vesselInfo{}
	for i := range vs {
		vessels[vs[i].ID] = vesselOf(&vs[i], ins)
	}
	_, rules, err := settings(ctx, db, scope, today.Time(time.UTC))
	if err != nil {
		return nil, err
	}
	var out []store.Violation
	for i := range allocs {
		a := &allocs[i]
		r := resources[a.ResourceID]
		if r == nil {
			out = append(out, store.Violation{Query: "Q15", IDs: []string{a.ID}, Detail: "resource missing"})
			continue
		}
		got := byAlloc[a.ID]
		slices.SortFunc(got, engine.CompareRows)
		spec := specOf(a, rels)
		if a.VesselID == "" {
			if err := checkBlockRows(spec, got); err != "" {
				out = append(out, store.Violation{Query: "Q15", IDs: []string{a.ID}, Detail: err})
			}
			continue
		}
		v := vessels[a.VesselID]
		if v == nil {
			out = append(out, store.Violation{Query: "Q15", IDs: []string{a.ID}, Detail: "vessel missing"})
			continue
		}
		want, err := engine.Plan(spec, r.Area, v.Layout)
		if err != nil {
			out = append(out, store.Violation{Query: "Q15", IDs: []string{a.ID}, Detail: "plan: " + err.Error()})
			continue
		}
		slices.SortFunc(want, engine.CompareRows)
		if !slices.Equal(got, want) {
			out = append(out, store.Violation{Query: "Q15", IDs: []string{a.ID}, Detail: fmt.Sprintf("%d rows, plan %d", len(got), len(want))})
		}
		res := fit.Check(v.Fit, r.target(spec.Placement, v.Layout), a.LastNight, rules)
		if !res.OK {
			o := unmarshalJSON[FitOverride](a.FitOverride)
			if len(a.FitOverride) == 0 || o.Reason == "" || (o.UserID == "" && o.APIKeyID == "") {
				out = append(out, store.Violation{Query: "Q14", IDs: []string{a.ID}, Detail: fmt.Sprint(res.BlockingCodeList())})
			}
		}
	}
	return out, nil
}

// checkBlockRows checks a block's rows: one cell set repeated on every planned night ("" when fine).
func checkBlockRows(spec engine.AllocSpec, got []engine.Row) string {
	nights := engine.PlanNights(spec)
	if len(got) == 0 {
		if len(nights) == 0 {
			return ""
		}
		return "block without rows"
	}
	var cells []int
	for _, r := range got {
		if r.Night.Equal(got[0].Night) {
			cells = append(cells, r.Cell)
		}
	}
	var want []engine.Row
	for _, n := range nights {
		for _, c := range cells {
			want = append(want, engine.Row{Resource: spec.ResourceID, Cell: c, Night: n})
		}
	}
	slices.SortFunc(want, engine.CompareRows)
	if !slices.Equal(got, want) {
		return fmt.Sprintf("block rows %d, plan %d", len(got), len(want))
	}
	return ""
}
