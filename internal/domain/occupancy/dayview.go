package occupancy

import (
	"context"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Day statuses of an element (DayStatus of the contract).
const (
	DayFree     = "free"
	DayReleased = "released"
	DayClosed   = "closed"
)

// Deadline states of the day view.
const (
	DeadlineOK       = "ok"
	DeadlineExpiring = "expiring"
	DeadlineExpired  = "expired"
	DeadlineMissing  = "missing"
)

// deadlineHorizon is the "expiring" window of documents and insurance (m3-plan §8.3).
const deadlineHorizon = 30

// VesselBrief is the vessel embedded in occupancy views (no registration, no owner).
type VesselBrief struct {
	ID    string
	Name  string
	LOA   geo.Cm
	Beam  geo.Cm
	Hull  string
	Draft *geo.Cm
}

// CustomerBrief is the customer embedded in views; DisplayName only with customers.read.
type CustomerBrief struct {
	ID          string
	DisplayName *string
}

// DayPresence is the presence state of the vessel of an element.
type DayPresence struct {
	State            string
	Since            time.Time
	ExpectedReturnOn *calendar.Date
	AwayDays         int
}

// DayRelease is the release covering the date.
type DayRelease struct {
	ID       string
	From, To calendar.Date
	Resold   bool
}

// DayFit is the current fit of the vessel on its place.
type DayFit struct {
	FillBP        int64
	Override      bool
	BlockingCodes []string
}

// DayDeadlines are the deadlines of an element.
type DayDeadlines struct {
	ContractEndsOn *calendar.Date
	Insurance      string
	Documents      string
}

// DayUtilities are the static utilities of a berth.
type DayUtilities struct {
	PowerA     *int
	PowerPhase string
	Water      bool
}

// DayElement is one berth, area or land resource on the date.
type DayElement struct {
	ResourceType      engine.ResourceType
	ResourceID        string
	Code              string
	Status            string
	AllocationID      string
	AllocationVersion int
	Kind              string
	HoldExpiresAt     *time.Time
	Contract          *ContractBrief
	Vessel            *VesselBrief
	Customer          *CustomerBrief
	Presence          *DayPresence
	Release           *DayRelease
	ArrivalToday      bool
	DepartureToday    bool
	Fit               *DayFit
	Deadlines         *DayDeadlines
	Utilities         *DayUtilities
	Anomalies         []string
}

// AreaPlacement is a vessel placed in a dynamic area on the date.
type AreaPlacement struct {
	AreaID            string
	AllocationID      string
	AllocationVersion int
	Placement         engine.Placement
	Linear            bool
	Cells             []int
	Status            string
	Kind              string
	Contract          *ContractBrief
	Vessel            VesselBrief
	Customer          *CustomerBrief
	Presence          *DayPresence
	Fit               *DayFit
	Anomalies         []string
}

// Unplaced is a vessel in port without an allocation covering the date.
type Unplaced struct {
	Vessel     VesselBrief
	PresenceID string
	Reason     string // arrived_without_booking
}

// DayView is the operational view of a map on a night (m3-plan §8.3).
type DayView struct {
	MapID          string
	Date           calendar.Date
	MapVersion     int
	GeneratedAt    time.Time
	Elements       []DayElement
	AreaPlacements []AreaPlacement
	Unplaced       []Unplaced
	AnomalyCount   int
}

func vesselBrief(v *store.Vessel) VesselBrief {
	vi := vesselOf(v, nil)
	return VesselBrief{ID: v.ID, Name: v.Name, LOA: vi.Fit.LOA, Beam: vi.Fit.Beam, Hull: v.HullType, Draft: vi.Fit.Draft}
}

func deadlineState(until *calendar.Date, date calendar.Date, missing string) string {
	switch {
	case until == nil:
		return missing
	case until.Before(date):
		return DeadlineExpired
	case !until.After(date.AddDays(deadlineHorizon)):
		return DeadlineExpiring
	}
	return DeadlineOK
}

// minutesSince returns the minutes from local midnight of date to t (negative before, ≥ 1440 after the day).
func minutesSince(t time.Time, date calendar.Date, loc *time.Location) int {
	return int(t.Sub(date.Time(loc)) / time.Minute)
}

// dayData is everything a day view or a map occupancy read needs about allocations.
type dayData struct {
	vessels   map[string]*store.Vessel
	presence  map[string]store.VesselPresence
	insurance map[string]calendar.Date
	docs      map[string]calendar.Date
	customers map[string]*store.Customer
}

func loadDayData(ctx context.Context, tx bun.IDB, scope store.MarinaScope, vesselIDs, customerIDs []string, withCustomers bool) (*dayData, error) {
	d := &dayData{vessels: map[string]*store.Vessel{}, customers: map[string]*store.Customer{}}
	vs, err := store.VesselsByIDs(ctx, tx, scope, uniq(vesselIDs))
	if err != nil {
		return nil, err
	}
	for i := range vs {
		d.vessels[vs[i].ID] = &vs[i]
	}
	if d.presence, err = store.VesselPresences(ctx, tx, scope, uniq(vesselIDs)); err != nil {
		return nil, err
	}
	if d.insurance, err = store.InsuranceUntil(ctx, tx, scope, uniq(vesselIDs)); err != nil {
		return nil, err
	}
	if d.docs, err = store.EarliestDocumentExpiries(ctx, tx, scope, uniq(vesselIDs), uniq(customerIDs)); err != nil {
		return nil, err
	}
	if withCustomers {
		cs, err := store.CustomersByIDs(ctx, tx, scope, uniq(customerIDs))
		if err != nil {
			return nil, err
		}
		for i := range cs {
			d.customers[cs[i].ID] = &cs[i]
		}
	}
	return d, nil
}

func (d *dayData) customer(id string) *CustomerBrief {
	if id == "" {
		return nil
	}
	cb := &CustomerBrief{ID: id}
	if c, ok := d.customers[id]; ok {
		n := customer.DisplayName(c)
		cb.DisplayName = &n
	}
	return cb
}

func (d *dayData) presenceOf(vesselID string, date calendar.Date, loc *time.Location) *DayPresence {
	vp, ok := d.presence[vesselID]
	if !ok {
		return nil
	}
	p := &DayPresence{State: vp.State, Since: vp.Since}
	if !vp.ExpectedReturnOn.IsZero() {
		e := vp.ExpectedReturnOn
		p.ExpectedReturnOn = &e
	}
	if vp.State != "in_port" {
		p.AwayDays = max(0, calendar.In(vp.Since, loc).DaysUntil(date))
	}
	return p
}

// GetDay returns the day view of a map (map.view; customer names only with customers.read).
func (s *Service) GetDay(ctx context.Context, p *auth.Principal, marinaID, mapID string, date *calendar.Date) (*DayView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	withCustomers := p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead)
	var out *DayView
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		now := s.now()
		day := calendar.In(now, loc)
		if date != nil {
			day = *date
		}
		out, err = s.buildDay(ctx, tx, scope, m, day, now, loc, withCustomers)
		return err
	})
	return out, err
}

func (s *Service) buildDay(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m *store.Map, date calendar.Date, now time.Time, loc *time.Location, withCustomers bool) (*DayView, error) {
	cfg, rules, err := settings(ctx, tx, scope, now)
	if err != nil {
		return nil, err
	}
	res, err := store.LoadMapResources(ctx, tx, scope, m.ID, true)
	if err != nil {
		return nil, err
	}
	allocs, err := store.DayAllocations(ctx, tx, scope, m.ID, date.AddDays(-1), date, nil)
	if err != nil {
		return nil, err
	}
	rels, err := store.ReleasesCovering(ctx, tx, scope, date)
	if err != nil {
		return nil, err
	}
	closures, err := store.ListClosures(ctx, tx, scope, date, date)
	if err != nil {
		return nil, err
	}
	inPort, err := store.InPortVessels(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	var vesselIDs, customerIDs, allocIDs []string
	for _, a := range allocs {
		vesselIDs, customerIDs, allocIDs = append(vesselIDs, a.VesselID), append(customerIDs, a.CustomerID), append(allocIDs, a.ID)
	}
	var inPortIDs []string
	for _, vp := range inPort {
		inPortIDs = append(inPortIDs, vp.VesselID)
	}
	covering, err := store.VesselAllocationsCovering(ctx, tx, scope, inPortIDs, date)
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for _, c := range covering {
		covered[c.VesselID] = true
	}
	// in-port vessels without an allocation on the date: their last allocation, when it is on this map, carries
	// the anomaly (departure overdue or presence without allocation)
	var strayAllocIDs []string
	for _, vp := range inPort {
		if !covered[vp.VesselID] {
			vesselIDs = append(vesselIDs, vp.VesselID)
			if vp.AllocationID != "" {
				strayAllocIDs = append(strayAllocIDs, vp.AllocationID)
			}
		}
	}
	strays, err := store.DayAllocationsByIDs(ctx, tx, scope, strayAllocIDs)
	if err != nil {
		return nil, err
	}
	strayByID := map[string]*store.DayAllocation{}
	for i := range strays {
		if strays[i].MapID == m.ID {
			strayByID[strays[i].ID] = &strays[i]
		}
	}
	arrived, err := store.ArrivedPresenceAllocations(ctx, tx, scope, append(allocIDs, strayAllocIDs...))
	if err != nil {
		return nil, err
	}
	dd, err := loadDayData(ctx, tx, scope, vesselIDs, customerIDs, withCustomers)
	if err != nil {
		return nil, err
	}
	view := &DayView{MapID: m.ID, Date: date, MapVersion: m.Version, GeneratedAt: now, Elements: []DayElement{}, AreaPlacements: []AreaPlacement{}, Unplaced: []Unplaced{}}
	resources := map[string]*resource{}
	var order []string
	for i := range res.Berths {
		r := berthResource(&res.Berths[i])
		resources[r.ID], order = r, append(order, r.ID)
	}
	for i := range res.Areas {
		r, err := areaResource(&res.Areas[i])
		if err != nil {
			continue
		}
		resources[r.ID], order = r, append(order, r.ID)
	}
	for i := range res.Lands {
		r := landResource(&res.Lands[i])
		resources[r.ID], order = r, append(order, r.ID)
	}
	groups, err := store.GroupsOfResources(ctx, tx, scope, m.ID)
	if err != nil {
		return nil, err
	}
	for id, g := range groups {
		if r := resources[id]; r != nil {
			r.GroupIDs = g
		}
	}
	var parents map[string]string
	closed := map[string]bool{}
	for _, c := range closures {
		if c.ScopeType == store.ClosureStructure && parents == nil {
			if parents, err = store.StructureParents(ctx, tx, scope); err != nil {
				return nil, err
			}
		}
		for id, r := range resources {
			hit := false
			switch c.ScopeType {
			case store.ClosureMarina:
				hit = true
			case store.ClosureMap:
				hit = c.ScopeID == m.ID
			case store.ClosureStructure:
				hit = r.StructureID != "" && (r.StructureID == c.ScopeID || parents[r.StructureID] == c.ScopeID)
			case store.ClosureGroup:
				hit = slices.Contains(r.GroupIDs, c.ScopeID)
			case store.ClosureResource:
				hit = c.ScopeID == id
			}
			if hit {
				closed[id] = true
			}
		}
	}
	relByAlloc := map[string]*store.Release{}
	for i := range rels {
		relByAlloc[rels[i].AllocationID] = &rels[i]
	}
	defaultArrival := ParseMinutes(cfg.DefaultArrivalTime)
	nowMin := minutesSince(now, date, loc)
	var anomalyIn []engine.AnomalyElement
	brief := func(a *store.DayAllocation) *ContractBrief {
		return briefOf(a.ContractID, a.ContractKind, a.ContractTerm, a.ContractStatus, a.ContractYear, a.ContractNumber)
	}
	anomalyOf := func(a *store.DayAllocation, resID string, covers bool, fitBlocking bool, insuranceOK bool) engine.AnomalyElement {
		exp := defaultArrival
		if t := ParseMinutes(a.ArrivalTime); t >= 0 {
			exp = t
		}
		inPortNow := false
		if vp, ok := dd.presence[a.VesselID]; ok && vp.State == "in_port" {
			inPortNow = true
		}
		return engine.AnomalyElement{
			ResourceID: resID, AllocationID: a.ID, HasAllocation: covers, PresenceInPort: inPortNow, ContractKind: a.ContractKind,
			ContractStatus: a.ContractStatus, Arrival: a.StartsOn, Departure: a.EndsOn, ExpectedArrivalMins: exp,
			ArrivedPresence: arrived[a.ID], FitBlocking: fitBlocking, InsuranceOK: insuranceOK, HoldLost: a.HoldLost,
		}
	}
	// per-resource allocation covering the date, and transits ending the night before (departure today)
	byRes := map[string][]*store.DayAllocation{}
	movedOn := map[string]bool{} // allocations continued by a move: their last night is a berth change, not a departure
	for i := range allocs {
		byRes[allocs[i].ResourceID] = append(byRes[allocs[i].ResourceID], &allocs[i])
		if allocs[i].MovedFromID != "" {
			movedOn[allocs[i].MovedFromID] = true
		}
	}
	fitOf := func(a *store.DayAllocation, r *resource) (*DayFit, bool, bool) {
		v := dd.vessels[a.VesselID]
		if v == nil {
			return nil, false, true
		}
		vi := vesselOf(v, dd.insurance)
		res := fit.Check(vi.Fit, r.target(placementOf(&a.Allocation), vi.Layout), a.LastNight, rules)
		df := &DayFit{FillBP: res.FillBP, Override: len(a.FitOverride) > 0 && string(a.FitOverride) != "null", BlockingCodes: res.BlockingCodeList()}
		ins, ok := dd.insurance[a.VesselID]
		return df, !res.OK, ok && !ins.Before(date)
	}
	deadlinesOf := func(a *store.DayAllocation) *DayDeadlines {
		dl := &DayDeadlines{}
		if a.ContractKind == store.ContractKindMooring && !a.EndsOn.IsZero() {
			e := a.EndsOn
			dl.ContractEndsOn = &e
		}
		var ins *calendar.Date
		if d, ok := dd.insurance[a.VesselID]; ok {
			ins = &d
		}
		dl.Insurance = deadlineState(ins, date, DeadlineMissing)
		var earliest *calendar.Date
		for _, owner := range []string{a.VesselID, a.CustomerID} {
			if d, ok := dd.docs[owner]; ok && (earliest == nil || d.Before(*earliest)) {
				dc := d
				earliest = &dc
			}
		}
		dl.Documents = deadlineState(earliest, date, DeadlineOK)
		return dl
	}
	for _, id := range order {
		r := resources[id]
		el := DayElement{ResourceType: r.Type, ResourceID: r.ID, Code: r.Code, Status: DayFree, Anomalies: []string{}}
		if r.Type == engine.ResourceBerth {
			el.Utilities = &DayUtilities{PowerPhase: r.PowerPhase, Water: r.Water}
			if r.PowerA > 0 {
				pa := int(r.PowerA)
				el.Utilities.PowerA = &pa
			}
		}
		for _, a := range byRes[id] {
			covers := !a.FirstNight.After(date) && !a.LastNight.Before(date)
			if !covers {
				if a.LastNight.Equal(date.AddDays(-1)) && a.VesselID != "" && !movedOn[a.ID] {
					el.DepartureToday = true
				}
				continue
			}
			if r.Type == engine.ResourceArea {
				if a.VesselID == "" {
					continue // blocks in areas are drawn from the occupancy range
				}
				df, blocking, insOK := fitOf(a, r)
				ap := AreaPlacement{
					AreaID: id, AllocationID: a.ID, AllocationVersion: a.Version, Placement: *placementOf(&a.Allocation),
					Linear: r.Area.Kind == "linear", Status: a.Kind, Kind: a.Kind, Contract: brief(a), Customer: dd.customer(a.CustomerID),
					Presence: dd.presenceOf(a.VesselID, date, loc), Fit: df, Anomalies: []string{},
				}
				if rel := relByAlloc[a.ID]; rel != nil {
					ap.Status = DayReleased
				}
				if v := dd.vessels[a.VesselID]; v != nil {
					ap.Vessel = vesselBrief(v)
					vi := vesselOf(v, nil)
					cells, err := engine.Cells(engine.AllocSpec{ResourceType: engine.ResourceArea, Placement: placementOf(&a.Allocation)}, r.Area, vi.Layout)
					if err == nil {
						ap.Cells = cells
					}
				}
				view.AreaPlacements = append(view.AreaPlacements, ap)
				anomalyIn = append(anomalyIn, anomalyOf(a, a.ID, true, blocking, insOK))
				continue
			}
			el.AllocationID, el.AllocationVersion, el.Kind, el.Status = a.ID, a.Version, a.Kind, a.Kind
			if a.Kind == store.AllocKindHold && !a.ExpiresAt.IsZero() {
				t := a.ExpiresAt
				el.HoldExpiresAt = &t
			}
			el.Contract = brief(a)
			el.Customer = dd.customer(a.CustomerID)
			// The first night of a moved-in allocation is a change of place, not an arrival.
			el.ArrivalToday = a.FirstNight.Equal(date) && a.VesselID != "" && a.MovedFromID == ""
			if v := dd.vessels[a.VesselID]; v != nil {
				vb := vesselBrief(v)
				el.Vessel = &vb
				el.Presence = dd.presenceOf(a.VesselID, date, loc)
				df, blocking, insOK := fitOf(a, r)
				el.Fit = df
				el.Deadlines = deadlinesOf(a)
				anomalyIn = append(anomalyIn, anomalyOf(a, id, true, blocking, insOK))
			}
			if rel := relByAlloc[a.ID]; rel != nil {
				el.Status = DayReleased
				el.Release = &DayRelease{ID: rel.ID, From: rel.FromNight, To: rel.ToNight, Resold: rel.ResoldAllocationID != ""}
			}
		}
		if el.Status == DayFree {
			// a released night: the release of another allocation of this resource covering the date
			for i := range rels {
				if rels[i].ResourceID == id {
					el.Status = DayReleased
					el.Release = &DayRelease{ID: rels[i].ID, From: rels[i].FromNight, To: rels[i].ToNight, Resold: rels[i].ResoldAllocationID != ""}
					break
				}
			}
		}
		if el.Status == DayFree && closed[id] {
			el.Status = DayClosed
		}
		view.Elements = append(view.Elements, el)
	}
	for _, vp := range inPort {
		if covered[vp.VesselID] {
			continue
		}
		v := dd.vessels[vp.VesselID]
		if v == nil {
			continue
		}
		if a := strayByID[vp.AllocationID]; a != nil && resources[a.ResourceID] != nil {
			key := a.ResourceID
			if resources[a.ResourceID].Type == engine.ResourceArea {
				key = a.ID
			}
			anomalyIn = append(anomalyIn, anomalyOf(a, key, false, false, true))
			continue
		}
		view.Unplaced = append(view.Unplaced, Unplaced{Vessel: vesselBrief(v), PresenceID: vp.LastPresenceID, Reason: "arrived_without_booking"})
	}
	anomalies := engine.Anomalies(engine.AnomalyInput{Date: date, NowMinutes: nowMin, GraceMinutes: cfg.ArrivalGraceMinutes, Elements: anomalyIn})
	elIdx := map[string]int{}
	for i, e := range view.Elements {
		elIdx[e.ResourceID] = i
	}
	apIdx := map[string]int{}
	for i, ap := range view.AreaPlacements {
		apIdx[ap.AllocationID] = i
	}
	for _, an := range anomalies {
		if i, ok := apIdx[an.ResourceID]; ok {
			view.AreaPlacements[i].Anomalies = appendUnique(view.AreaPlacements[i].Anomalies, an.Code)
		} else if i, ok := elIdx[an.ResourceID]; ok {
			view.Elements[i].Anomalies = appendUnique(view.Elements[i].Anomalies, an.Code)
		} else {
			continue
		}
		view.AnomalyCount++
	}
	return view, nil
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// MapAllocation is an allocation of the map occupancy range (planner, drag preview).
type MapAllocation struct {
	Allocation
	Linear   bool
	Cells    []int
	Vessel   *VesselBrief
	Contract *ContractBrief
}

// MapOccupancy is the allocations of a map intersecting a range of nights.
type MapOccupancy struct {
	From, To    calendar.Date
	Allocations []MapAllocation
}

// MaxRangeNights bounds the map occupancy range.
const MaxRangeNights = 400

// GetMapOccupancy returns the active allocations of a map intersecting [from, to] (map.view; defaults
// today..today+30, at most 400 nights), optionally restricted to some resources.
func (s *Service) GetMapOccupancy(ctx context.Context, p *auth.Principal, marinaID, mapID string, from, to *calendar.Date, resourceIDs []string) (*MapOccupancy, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	var out *MapOccupancy
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetMap(ctx, tx, scope, mapID); err != nil {
			return err
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		today := calendar.In(s.now(), loc)
		f, t := today, today.AddDays(30)
		if from != nil {
			f = *from
			if to == nil {
				t = f.AddDays(30)
			}
		}
		if to != nil {
			t = *to
		}
		if t.Before(f) {
			return invalid("to", "before_from")
		}
		if engine.Nights(f, t) > MaxRangeNights {
			return invalid("to", "too_long")
		}
		allocs, err := store.DayAllocations(ctx, tx, scope, mapID, f, t, resourceIDs)
		if err != nil {
			return err
		}
		res, err := store.LoadMapResources(ctx, tx, scope, mapID, false)
		if err != nil {
			return err
		}
		areas := map[string]*resource{}
		for i := range res.Areas {
			if r, err := areaResource(&res.Areas[i]); err == nil {
				areas[r.ID] = r
			}
		}
		codes := map[string]string{}
		for _, b := range res.Berths {
			codes[b.ID] = b.Code
		}
		for _, a := range res.Areas {
			codes[a.ID] = a.Code
		}
		for _, l := range res.Lands {
			codes[l.ID] = l.Code
		}
		var ids, vids []string
		for _, a := range allocs {
			ids, vids = append(ids, a.ID), append(vids, a.VesselID)
		}
		rels, err := store.ActiveReleasesOf(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		vs, err := store.VesselsByIDs(ctx, tx, scope, uniq(vids))
		if err != nil {
			return err
		}
		vessels := map[string]*store.Vessel{}
		for i := range vs {
			vessels[vs[i].ID] = &vs[i]
		}
		out = &MapOccupancy{From: f, To: t, Allocations: []MapAllocation{}}
		for i := range allocs {
			a := &allocs[i]
			ma := MapAllocation{
				Allocation: allocationOf(&a.Allocation, codes[a.ResourceID], a.ContractID, rels),
				Contract:   briefOf(a.ContractID, a.ContractKind, a.ContractTerm, a.ContractStatus, a.ContractYear, a.ContractNumber),
			}
			if v := vessels[a.VesselID]; v != nil {
				vb := vesselBrief(v)
				ma.Vessel = &vb
			}
			if r := areas[a.ResourceID]; r != nil {
				ma.Linear = r.Area.Kind == "linear"
				if v := vessels[a.VesselID]; v != nil {
					if cells, err := engine.Cells(engine.AllocSpec{ResourceType: engine.ResourceArea, Placement: placementOf(&a.Allocation)}, r.Area, vesselOf(v, nil).Layout); err == nil {
						ma.Cells = cells
					}
				} else if rows, err := store.RowsOfAllocation(ctx, tx, scope, &a.Allocation); err == nil {
					for _, rw := range rows {
						ma.Cells = append(ma.Cells, rw.Cell)
					}
					slices.Sort(ma.Cells)
					ma.Cells = slices.Compact(ma.Cells)
				}
			}
			out.Allocations = append(out.Allocations, ma)
		}
		return nil
	})
	return out, err
}

// GetAllocation returns an allocation with its contract, releases (any status) and moved-from chain (map.view).
func (s *Service) GetAllocation(ctx context.Context, p *auth.Principal, marinaID, id string) (*AllocationDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	var out *AllocationDetail
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		a, err := store.GetAllocation(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		views, err := allocationsOf(ctx, tx, scope, []store.Allocation{*a})
		if err != nil {
			return err
		}
		d := &AllocationDetail{Allocation: views[0], Releases: []Release{}, MovedFromIDs: []string{}}
		if a.ContractLineID != "" {
			cs, err := store.ContractsOfLines(ctx, tx, scope, []string{a.ContractLineID})
			if err != nil {
				return err
			}
			if c, ok := cs[a.ContractLineID]; ok {
				d.Contract = briefOf(c.ContractID, c.Kind, c.Term, c.Status, c.Year, c.Number)
			}
		}
		rels, err := store.ReleasesOfAllocations(ctx, tx, scope, []string{a.ID})
		if err != nil {
			return err
		}
		withNotes := p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead)
		for i := range rels {
			d.Releases = append(d.Releases, releaseOf(&rels[i], views[0].ResourceCode, withNotes))
		}
		prev := a.MovedFromID
		for k := 0; prev != "" && k < 50; k++ {
			pa, err := store.GetAllocation(ctx, tx, scope, prev)
			if err != nil {
				break
			}
			d.MovedFromIDs = append(d.MovedFromIDs, pa.ID)
			prev = pa.MovedFromID
		}
		out = d
		return nil
	})
	return out, err
}

// Compaction is a compaction proposal of a linear area.
type Compaction struct {
	Placements []engine.Proposal
	FreedCm    geo.Cm
	Base       []BaseVersion
}

// ProposeCompaction proposes sliding the vessels of a linear area toward the axis start for [from, to]
// (occupancy.move): vessels covering the whole range move, the others are obstacles. The client applies it
// with a layout save carrying Base.
func (s *Service) ProposeCompaction(ctx context.Context, p *auth.Principal, marinaID, areaID string, from, to calendar.Date) (*Compaction, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return nil, err
	}
	if to.Before(from) {
		return nil, invalid("to", "before_from")
	}
	var out *Compaction
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		area, err := store.DynamicAreaByID(ctx, tx, scope, areaID)
		if err != nil {
			return err
		}
		r, err := areaResource(area)
		if err != nil || r.Area.Kind != "linear" {
			return &PlacementError{Reason: PlacementNotAllowed}
		}
		allocs, err := store.ListAllocations(ctx, tx, scope, store.AllocationFilter{
			ResourceType: store.ResourceArea, ResourceIDs: []string{areaID}, Status: store.AllocActive, From: from, To: to,
		})
		if err != nil {
			return err
		}
		var vids []string
		for _, a := range allocs {
			vids = append(vids, a.VesselID)
		}
		vs, err := store.VesselsByIDs(ctx, tx, scope, uniq(vids))
		if err != nil {
			return err
		}
		vessels := map[string]*store.Vessel{}
		for i := range vs {
			vessels[vs[i].ID] = &vs[i]
		}
		var placed []engine.Placed
		out = &Compaction{Placements: []engine.Proposal{}, Base: []BaseVersion{}}
		for i := range allocs {
			a := &allocs[i]
			pl := placementOf(a)
			if pl == nil {
				continue
			}
			pd := engine.Placed{AllocationID: a.ID, X: pl.X, First: a.FirstNight, Last: a.LastNight}
			if v := vessels[a.VesselID]; v != nil {
				pd.Width = engine.FootprintWidth(*r.Area, vesselOf(v, nil).Layout)
				pd.Fixed = a.FirstNight.After(from) || a.LastNight.Before(to)
				out.Base = append(out.Base, BaseVersion{AllocationID: a.ID, Version: a.Version})
			} else {
				cells, err := s.cellsOf(ctx, tx, scope, a)
				if err != nil || len(cells) == 0 {
					continue
				}
				pd.X, pd.Width, pd.Fixed = geo.Cm(cells[0])*r.Area.Cell, geo.Cm(cells[len(cells)-1]-cells[0]+1)*r.Area.Cell, true
			}
			placed = append(placed, pd)
		}
		out.Placements = engine.Compact(*r.Area, placed, 0)
		if out.Placements == nil {
			out.Placements = []engine.Proposal{}
		}
		before := engine.FreeTail(*r.Area, placed)
		after := slices.Clone(placed)
		for i := range after {
			for _, pr := range out.Placements {
				if pr.AllocationID == after[i].AllocationID {
					after[i].X = pr.X
				}
			}
		}
		out.FreedCm = max(0, engine.FreeTail(*r.Area, after)-before)
		return nil
	})
	return out, err
}
