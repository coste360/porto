package occupancy

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// SolutionSegment is one segment of a solution with the code and map of its resource.
type SolutionSegment struct {
	ResourceType engine.ResourceType
	ResourceID   string
	Code         string
	MapID        string
	Placement    *engine.Placement
	First, Last  calendar.Date
}

// Solution is a search solution as the API shows it (prices are added by the caller).
type Solution struct {
	Key             string
	Segments        []SolutionSegment
	Score           int64
	Fit             fit.Result
	RequiresRelease *engine.OutCand
}

type fitIssueJSON struct {
	Code     string `json:"code"`
	LimitCm  *int64 `json:"limit_cm,omitempty"`
	ActualCm *int64 `json:"actual_cm,omitempty"`
}

func issuesJSON(list []fit.Issue) []fitIssueJSON {
	out := make([]fitIssueJSON, 0, len(list))
	for _, is := range list {
		f := fitIssueJSON{Code: is.Code}
		if is.Limit != 0 || is.Actual != 0 {
			l, a := int64(is.Limit), int64(is.Actual)
			f.LimitCm, f.ActualCm = &l, &a
		}
		out = append(out, f)
	}
	return out
}

// MarshalJSON renders the solution as the OccupancySolution schema (used in problem details).
func (s Solution) MarshalJSON() ([]byte, error) {
	type seg struct {
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		Code         string `json:"code"`
		MapID        string `json:"map_id"`
		XCm          *int64 `json:"x_cm,omitempty"`
		YCm          *int64 `json:"y_cm,omitempty"`
		RotationDd   *int64 `json:"rotation_dd,omitempty"`
		FirstNight   string `json:"first_night"`
		LastNight    string `json:"last_night"`
	}
	type rr struct {
		AllocationID string `json:"allocation_id"`
		ContractID   string `json:"contract_id"`
	}
	out := struct {
		Key             string `json:"key"`
		Segments        []seg  `json:"segments"`
		Score           int64  `json:"score"`
		Fit             any    `json:"fit"`
		RequiresRelease *rr    `json:"requires_release,omitempty"`
	}{Key: s.Key, Score: s.Score, Segments: []seg{}}
	for _, g := range s.Segments {
		sg := seg{ResourceType: string(g.ResourceType), ResourceID: g.ResourceID, Code: g.Code, MapID: g.MapID, FirstNight: g.First.String(), LastNight: g.Last.String()}
		if g.Placement != nil {
			x, y, r := int64(g.Placement.X), int64(g.Placement.Y), int64(g.Placement.Rotation)
			sg.XCm, sg.YCm, sg.RotationDd = &x, &y, &r
		}
		out.Segments = append(out.Segments, sg)
	}
	out.Fit = map[string]any{
		"ok": s.Fit.OK, "overridable": s.Fit.Overridable, "fill_bp": s.Fit.FillBP,
		"blocking": issuesJSON(s.Fit.Blocking), "warnings": issuesJSON(s.Fit.Warnings),
	}
	if s.RequiresRelease != nil {
		out.RequiresRelease = &rr{AllocationID: s.RequiresRelease.AllocationID, ContractID: s.RequiresRelease.ContractID}
	}
	return json.Marshal(out)
}

// SearchInput is a search request (spec 10.7; searchOccupancy).
type SearchInput struct {
	Purpose            string // transit, contract
	VesselID           string
	Vessel             *fit.Vessel // dimensions when there is no vessel yet
	Arrival, Departure calendar.Date
	Term               string
	ResourceTypes      []engine.ResourceType
	Prefs              engine.Prefs
	ConsiderOut        bool
	MaxSegments        *int
	Limit              int
	Channel            string // desk (default), api
}

// SearchResult is the outcome of a search.
type SearchResult struct {
	Solutions  []Solution
	Considered int
	Vessel     fit.Vessel // the effective vessel searched
}

// Search searches places for a vessel (transits.desk; purpose contract also contracts.write). The snapshot is read
// in one read transaction with a fixed number of queries and solved in memory (m3-plan §3.6).
func (s *Service) Search(ctx context.Context, p *auth.Principal, marinaID string, in SearchInput) (*SearchResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	if in.Purpose == engine.PurposeContract && !p.Can(scope.OrgID(), scope.ID(), auth.PermContractsWrite) {
		return nil, auth.ErrForbidden
	}
	v := &auth.ValidationError{}
	if in.Purpose != engine.PurposeTransit && in.Purpose != engine.PurposeContract {
		v.Add("purpose", "invalid")
	}
	if !in.Departure.After(in.Arrival) {
		v.Add("departure", "before_arrival")
	} else if engine.Nights(in.Arrival, in.Departure.AddDays(-1)) > 400 {
		v.Add("departure", "too_long")
	}
	if in.VesselID == "" && in.Vessel == nil {
		v.Add("vessel_id", "required")
	}
	if in.Channel == "" {
		in.Channel = "desk"
	}
	if in.Channel != "desk" && in.Channel != "api" {
		v.Add("channel", "invalid")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	return s.search(ctx, scope, in)
}

// search runs a search in scope without authorisation (the callers authorised).
func (s *Service) search(ctx context.Context, scope store.MarinaScope, in SearchInput) (*SearchResult, error) {
	var out *SearchResult
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		cfg, rules, err := settings(ctx, tx, scope, now)
		if err != nil {
			return err
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		var vessel fit.Vessel
		if in.VesselID != "" {
			vi, err := loadVessel(ctx, tx, scope, in.VesselID)
			if err != nil {
				return err
			}
			vessel = vi.Fit
		} else {
			vessel = *in.Vessel
		}
		snap, considered, err := loadSnapshot(ctx, tx, scope, in, now, loc, cfg)
		if err != nil {
			return err
		}
		maxSeg := cfg.MaxSegments
		if in.MaxSegments != nil {
			maxSeg = min(max(*in.MaxSegments, 0), 2)
		}
		if in.Channel != "desk" {
			maxSeg = 0
		}
		req := engine.Request{
			Vessel: vessel, VesselID: in.VesselID, Arrival: in.Arrival, Departure: in.Departure, Purpose: in.Purpose,
			ResourceTypes: in.ResourceTypes, Prefs: in.Prefs, Channel: in.Channel, MaxSegments: maxSeg, ConsiderOut: in.ConsiderOut,
			Limit: in.Limit,
		}
		sols := engine.Solve(*snap, req, rules)
		out = &SearchResult{Solutions: solutionsOf(snap, sols), Considered: considered, Vessel: vessel}
		return nil
	})
	return out, err
}

func solutionsOf(snap *engine.Snapshot, sols []engine.Solution) []Solution {
	codes := map[string][2]string{}
	for _, b := range snap.Berths {
		codes[b.ID] = [2]string{b.Code, b.MapID}
	}
	for _, a := range snap.Areas {
		codes[a.ID] = [2]string{a.Code, a.MapID}
	}
	for _, l := range snap.Lands {
		codes[l.ID] = [2]string{l.Code, l.MapID}
	}
	out := make([]Solution, 0, len(sols))
	for _, s := range sols {
		sol := Solution{Key: s.Key, Score: s.Score, Fit: s.Fit, RequiresRelease: s.RequiresRelease}
		for _, g := range s.Segments {
			c := codes[g.ResourceID]
			sol.Segments = append(sol.Segments, SolutionSegment{
				ResourceType: g.ResourceType, ResourceID: g.ResourceID, Code: c[0], MapID: c[1], Placement: g.Placement, First: g.First, Last: g.Last,
			})
		}
		out = append(out, sol)
	}
	return out
}

type contractPolicy struct {
	ReleasePolicy string `json:"release_policy"`
}

// loadSnapshot reads the snapshot of a search (store.LoadSearchSnapshot) and shapes it for the solver.
func loadSnapshot(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in SearchInput, now time.Time, loc *time.Location, cfg *store.OccupancySettings) (*engine.Snapshot, int, error) {
	first, last := in.Arrival, in.Departure.AddDays(-1)
	raw, err := store.LoadSearchSnapshot(ctx, tx, scope, first, last, now, in.ConsiderOut, in.VesselID)
	if err != nil {
		return nil, 0, err
	}
	snap := &engine.Snapshot{
		Today: calendar.In(now, loc), Busy: map[string][]engine.NightRange{}, AreaBusy: map[string]map[calendar.Date][]engine.Interval{},
		OutOK: map[string]engine.OutCand{}, Previous: map[string]string{},
	}
	structOf := map[string]string{}
	mapOf := map[string]string{}
	for _, r := range raw.Resources {
		mapOf[r.ID], structOf[r.ID] = r.MapID, r.StructureID
		if r.Type == store.ResourceLand {
			snap.Lands = append(snap.Lands, engine.LandCand{
				ID: r.ID, Code: r.Code, MapID: r.MapID, GroupIDs: raw.Groups[r.ID], OnlineSellable: r.OnlineSellable,
				Target: fit.Target{Kind: fit.TargetLand, LandKind: r.LandKind, MaxLOA: geo.Cm(r.MaxLOACm), MaxBeam: geo.Cm(r.MaxBeamCm), Length: geo.Cm(r.LengthCm), Width: geo.Cm(r.WidthCm)},
			})
			continue
		}
		snap.Berths = append(snap.Berths, engine.BerthCand{
			ID: r.ID, Code: r.Code, MapID: r.MapID, StructureID: r.StructureID, GroupIDs: raw.Groups[r.ID], Water: r.Water,
			TransitDedicated: slices.Contains(r.Attributes, TagTransitOnly), OnlineSellable: r.OnlineSellable,
			Target: fit.Target{
				Kind: fit.TargetBerth, MaxLOA: geo.Cm(r.MaxLOACm), MaxBeam: geo.Cm(r.MaxBeamCm), Length: geo.Cm(r.LengthCm),
				Width: geo.Cm(r.WidthCm), Depth: cmPtr(r.DepthCm), MaxAirDraft: cmPtr(r.MaxAirDraftCm),
				MultihullOK: slices.Contains(r.Attributes, TagMultihullOK), OverhangAllowed: r.OverhangAllowed, PowerA: i64Ptr(r.PowerA),
			},
		})
	}
	for i := range raw.Areas {
		a := &raw.Areas[i]
		mapOf[a.ID], structOf[a.ID] = a.MapID, a.StructureID
		res, err := areaResource(a)
		if err != nil || res.Area.Kind != layout.AreaLinear {
			continue
		}
		snap.Areas = append(snap.Areas, engine.AreaCand{
			ID: a.ID, Code: a.Code, MapID: a.MapID, StructureID: a.StructureID, GroupIDs: raw.Groups[a.ID], Target: res.Target,
			Mode: res.Area.Mode, CellCm: res.Area.Cell, MarginCm: res.Area.Margin, LengthCm: res.Area.Length,
			CellCount: layout.CellCount(*res.Area), DepthProfile: res.Depth,
		})
	}
	nights := map[string][]calendar.Date{}
	owners := map[string]map[string]bool{}
	cells := map[string]map[calendar.Date][]int{}
	for _, r := range raw.Rows {
		if r.Type == store.ResourceArea {
			if cells[r.Resource] == nil {
				cells[r.Resource] = map[calendar.Date][]int{}
			}
			cells[r.Resource][r.Night] = append(cells[r.Resource][r.Night], r.Cell)
			continue
		}
		nights[r.Resource] = append(nights[r.Resource], r.Night)
		if owners[r.Resource] == nil {
			owners[r.Resource] = map[string]bool{}
		}
		owners[r.Resource][r.AllocationID] = true
	}
	for id, ns := range nights {
		snap.Busy[id] = engine.GroupNights(ns)
	}
	for id, byNight := range cells {
		snap.AreaBusy[id] = map[calendar.Date][]engine.Interval{}
		for d, cs := range byNight {
			slices.Sort(cs)
			snap.AreaBusy[id][d] = engine.GroupCells(cs)
		}
	}
	for _, o := range raw.Out {
		policy := unmarshalJSON[contractPolicy](o.Policy).ReleasePolicy
		if policy == "" {
			policy = cfg.ReleasePolicy
		}
		ow := owners[o.ResourceID]
		if policy == "never" || o.FirstNight.After(first) || o.LastNight.Before(last) || len(ow) != 1 || !ow[o.AllocationID] {
			continue
		}
		if !calendar.In(o.Since, loc).Before(in.Arrival) || (!o.ExpectedReturnOn.IsZero() && o.ExpectedReturnOn.Before(in.Departure)) {
			continue
		}
		snap.OutOK[o.ResourceID] = engine.OutCand{AllocationID: o.AllocationID, ContractID: o.ContractID}
	}
	var parents map[string]string
	for _, c := range raw.Closures {
		cl := engine.Closure{ID: c.ID, From: c.FromDate, To: c.ToDate}
		if c.ScopeType != store.ClosureMarina {
			if c.ScopeType == store.ClosureStructure && parents == nil {
				if parents, err = store.StructureParents(ctx, tx, scope); err != nil {
					return nil, 0, err
				}
			}
			for id := range mapOf {
				hit := false
				switch c.ScopeType {
				case store.ClosureMap:
					hit = mapOf[id] == c.ScopeID
				case store.ClosureStructure:
					hit = structOf[id] != "" && (structOf[id] == c.ScopeID || parents[structOf[id]] == c.ScopeID)
				case store.ClosureGroup:
					hit = slices.Contains(raw.Groups[id], c.ScopeID)
				case store.ClosureResource:
					hit = id == c.ScopeID
				}
				if hit {
					cl.ResourceIDs = append(cl.ResourceIDs, id)
				}
			}
			if len(cl.ResourceIDs) == 0 {
				continue
			}
		}
		snap.Closures = append(snap.Closures, cl)
	}
	if raw.Previous != "" && in.VesselID != "" {
		snap.Previous[in.VesselID] = raw.Previous
	}
	considered := len(snap.Berths) + len(snap.Areas) + len(snap.Lands)
	return snap, considered, nil
}

// alternatives searches places for a vessel on the nights [from, to] (best effort: errors give none).
func (s *Service) alternatives(ctx context.Context, p *auth.Principal, marinaID, vesselID string, from, to calendar.Date) []Solution {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil || to.Before(from) {
		return nil
	}
	res, err := s.search(ctx, scope, SearchInput{
		Purpose: engine.PurposeContract, VesselID: vesselID, Arrival: from, Departure: to.AddDays(1), Limit: 5, Channel: "desk",
	})
	if err != nil {
		return nil
	}
	return res.Solutions
}

// Alternatives returns up to five places for a vessel on [from, to] for problem details (map.view). The contract
// service calls it after a resource_taken on stay changes and quote acceptance.
func (s *Service) Alternatives(ctx context.Context, p *auth.Principal, marinaID, vesselID string, from, to calendar.Date) []Solution {
	return s.alternatives(ctx, p, marinaID, vesselID, from, to)
}

// Conflict is a conflicting allocation found by Check.
type Conflict struct {
	ResourceType engine.ResourceType
	ResourceID   string
	AllocationID string
	VesselID     string
	From, To     calendar.Date
	Cells        []int
}

// CheckInput is a placement check (checkPlacement): an existing allocation (its vessel; its own rows are
// ignored) or a vessel, a target and nights.
type CheckInput struct {
	AllocationID string
	VesselID     string
	Target       Target
	From, To     calendar.Date
}

// CheckResult is the advisory outcome of Check (the write re-checks everything, INV-01).
type CheckResult struct {
	OK             bool
	Fit            fit.Result
	PlacementError string
	Conflicts      []Conflict
	ClosureID      string
	PriceEffect    *PriceEffect // moving allocation_id there: the price effect (reported only)
}

// Check tells whether a vessel could be placed on a target for nights (map.view): fit, footprint and the rows
// held by other allocations (expired holds are free). Advisory only.
func (s *Service) Check(ctx context.Context, p *auth.Principal, marinaID string, in CheckInput) (*CheckResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	if in.To.Before(in.From) {
		return nil, invalid("to_night", "before_from")
	}
	if engine.Nights(in.From, in.To) > 400 {
		return nil, invalid("to_night", "too_long")
	}
	out := &CheckResult{Conflicts: []Conflict{}}
	var moving *store.Allocation
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		vesselID := in.VesselID
		var exclude []string
		if in.AllocationID != "" {
			a, err := store.GetAllocation(ctx, tx, scope, in.AllocationID)
			if err != nil {
				return err
			}
			if a.VesselID == "" {
				return &PlacementError{Reason: PlacementNotAllowed, AllocationID: a.ID}
			}
			vesselID, exclude, moving = a.VesselID, []string{a.ID}, a
		}
		if vesselID == "" {
			return invalid("vessel_id", "required")
		}
		v, err := loadVessel(ctx, tx, scope, vesselID)
		if err != nil {
			return err
		}
		r, err := loadResource(ctx, tx, scope, in.Target.ResourceType, in.Target.ResourceID)
		if err != nil {
			return err
		}
		_, rules, err := settings(ctx, tx, scope, now)
		if err != nil {
			return err
		}
		out.Fit = fit.Check(v.Fit, r.target(in.Target.Placement, v.Layout), in.To, rules)
		if in.AllocationID == "" {
			c, err := closureOn(ctx, tx, scope, r, in.From)
			if err != nil {
				return err
			}
			if c != nil {
				out.ClosureID = c.ID
			}
		}
		cells, err := engine.Cells(engine.AllocSpec{ResourceType: in.Target.ResourceType, ResourceID: r.ID, Placement: in.Target.Placement}, r.Area, v.Layout)
		if err != nil {
			pe := placementError(err)
			if pe == nil {
				return err
			}
			out.PlacementError = pe.Reason
			return nil
		}
		owners, err := store.OccupancyOwners(ctx, tx, scope, string(in.Target.ResourceType), r.ID, in.From, in.To, cells, exclude)
		if err != nil {
			return err
		}
		byAlloc := map[string]*Conflict{}
		var order []string
		for _, o := range owners {
			if o.Kind == store.AllocKindHold && !o.ExpiresAt.IsZero() && o.ExpiresAt.Before(now) {
				continue
			}
			c := byAlloc[o.AllocationID]
			if c == nil {
				c = &Conflict{ResourceType: r.Type, ResourceID: r.ID, AllocationID: o.AllocationID, From: o.Night, To: o.Night}
				byAlloc[o.AllocationID] = c
				order = append(order, o.AllocationID)
			}
			if o.Night.Before(c.From) {
				c.From = o.Night
			}
			if o.Night.After(c.To) {
				c.To = o.Night
			}
			if r.Type == engine.ResourceArea && !slices.Contains(c.Cells, o.Cell) {
				c.Cells = append(c.Cells, o.Cell)
			}
		}
		if len(order) > 0 {
			as, err := store.AllocationsByIDs(ctx, tx, scope, order)
			if err != nil {
				return err
			}
			for _, a := range as {
				byAlloc[a.ID].VesselID = a.VesselID
			}
		}
		for _, id := range order {
			slices.Sort(byAlloc[id].Cells)
			out.Conflicts = append(out.Conflicts, *byAlloc[id])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.OK = out.Fit.OK && out.PlacementError == "" && len(out.Conflicts) == 0 && out.ClosureID == ""
	if moving != nil {
		from := in.From
		if from.Before(moving.FirstNight) {
			from = moving.FirstNight
		}
		if leg, ok := legOf(moving, moving.Version, in.Target, from, &in.To); ok {
			// Advisory: a price that cannot be computed (no_price, no pricing.read) is simply not reported.
			if mp, err := s.priceLegs(ctx, p, marinaID, []MoveLeg{leg}, nil); err == nil {
				out.PriceEffect = mp.effect()
			}
		}
	}
	return out, nil
}
