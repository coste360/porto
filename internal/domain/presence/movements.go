package presence

import (
	"context"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Movement is one line of the movements of the day.
type Movement struct {
	Vessel       store.Vessel
	Contract     *store.Contract
	ContractCode string
	Resource     *store.ResourceRef
	ExpectedTime string // HH:MM
	PresenceID   string
	AllocationID string
}

// Movements are the expected and recorded movements of a day (spec 9.5; getMovements).
type Movements struct {
	Date                  calendar.Date
	ArrivalsExpected      []Movement
	DeparturesExpected    []Movement
	VesselsOut            []Movement
	ArrivedWithoutBooking []Movement
	Arrived               []Movement
	Departed              []Movement
}

// expecting reports whether a contract still expects its vessel (arrivals) or is running (departures).
func expecting(c *store.Contract) bool {
	switch contract.Status(c.Status) {
	case contract.StatusHeld, contract.StatusConfirmed, contract.StatusArrived, contract.StatusActive, contract.StatusDraft:
		return true
	}
	return false
}

// Movements returns the movements of date (default today), optionally on one map (map.view).
func (s *Service) Movements(ctx context.Context, p *auth.Principal, marinaID string, date *calendar.Date, mapID string) (*Movements, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	loc := location(ctx, db, scope)
	d := occupancy.Today(s.d.Clock, loc)
	if date != nil {
		d = *date
	}
	st, err := store.ContractOccupancySettings(ctx, db, scope, s.now())
	if err != nil {
		return nil, err
	}
	starting, err := store.MovementAllocations(ctx, db, scope, d, true, mapID)
	if err != nil {
		return nil, err
	}
	ending, err := store.MovementAllocations(ctx, db, scope, d.AddDays(-1), false, mapID)
	if err != nil {
		return nil, err
	}
	presences, err := store.PresencesOn(ctx, db, scope, d)
	if err != nil {
		return nil, err
	}
	outs, err := store.VesselsInState(ctx, db, scope, StateOut)
	if err != nil {
		return nil, err
	}
	var outIDs []string
	for _, o := range outs {
		outIDs = append(outIDs, o.VesselID)
	}
	outAllocs, err := store.VesselAllocationsCovering(ctx, db, scope, outIDs, d)
	if err != nil {
		return nil, err
	}
	// Load what the lines refer to.
	var cids, vids, rids, aids []string
	for _, list := range [][]store.ContractAllocation{starting, ending, outAllocs} {
		for _, a := range list {
			cids, vids, rids = append(cids, a.ContractID), append(vids, a.VesselID), append(rids, a.ResourceID)
		}
	}
	for _, pr := range presences {
		cids, vids, aids = append(cids, pr.ContractID), append(vids, pr.VesselID), append(aids, pr.AllocationID)
	}
	presAllocs, err := store.AllocationsByIDs(ctx, db, scope, aids)
	if err != nil {
		return nil, err
	}
	allocByID := map[string]store.Allocation{}
	for _, a := range presAllocs {
		allocByID[a.ID] = a
		rids = append(rids, a.ResourceID)
	}
	contracts, err := store.ContractsByIDs(ctx, db, scope, cids)
	if err != nil {
		return nil, err
	}
	cm := map[string]*store.Contract{}
	for i := range contracts {
		cm[contracts[i].ID] = &contracts[i]
	}
	vs, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]store.Vessel{}
	for i := range vs {
		vm[vs[i].ID] = vs[i]
	}
	refs, err := store.ResourceRefs(ctx, db, scope, rids)
	if err != nil {
		return nil, err
	}
	line := func(vesselID, contractID string, al *store.Allocation) Movement {
		m := Movement{Vessel: vm[vesselID]}
		if c := cm[contractID]; c != nil {
			m.Contract, m.ContractCode = c, contract.Code(c)
		}
		if al != nil {
			m.AllocationID = al.ID
			if ref, ok := refs[al.ResourceID]; ok {
				r := ref
				m.Resource = &r
			} else {
				m.Resource = &store.ResourceRef{Type: al.ResourceType, ID: al.ResourceID, MapID: al.MapID}
			}
		}
		return m
	}
	out := &Movements{
		Date: d, ArrivalsExpected: []Movement{}, DeparturesExpected: []Movement{}, VesselsOut: []Movement{},
		ArrivedWithoutBooking: []Movement{}, Arrived: []Movement{}, Departed: []Movement{},
	}
	arrivedToday, departedToday := map[string]bool{}, map[string]bool{}
	for _, pr := range presences {
		al, hasAlloc := allocByID[pr.AllocationID]
		if mapID != "" && (!hasAlloc || al.MapID != mapID) {
			continue
		}
		var alp *store.Allocation
		if hasAlloc {
			alp = &al
		}
		m := line(pr.VesselID, pr.ContractID, alp)
		m.PresenceID = pr.ID
		switch pr.Event {
		case EventArrived:
			arrivedToday[pr.VesselID] = true
			out.Arrived = append(out.Arrived, m)
			c := cm[pr.ContractID]
			if c == nil || (c.Kind == contract.KindTransit && c.CustomerID == "" && c.Status == string(contract.StatusHeld)) {
				out.ArrivedWithoutBooking = append(out.ArrivedWithoutBooking, m)
			}
		case EventDeparted:
			departedToday[pr.VesselID] = true
			out.Departed = append(out.Departed, m)
		}
	}
	for i := range starting {
		a := starting[i]
		c := cm[a.ContractID]
		if c == nil || !expecting(c) || arrivedToday[a.VesselID] || c.Status == string(contract.StatusArrived) {
			continue
		}
		// A segment change of the same contract is not an arrival.
		if !a.FirstNight.Equal(c.StartsOn) {
			continue
		}
		m := line(a.VesselID, a.ContractID, &a.Allocation)
		m.ExpectedTime = c.ExpectedArrivalTime
		if m.ExpectedTime == "" {
			m.ExpectedTime = st.DefaultArrivalTime
		}
		out.ArrivalsExpected = append(out.ArrivalsExpected, m)
	}
	for i := range ending {
		a := ending[i]
		c := cm[a.ContractID]
		if c == nil || !expecting(c) || departedToday[a.VesselID] || !a.LastNight.AddDays(1).Equal(c.EndsOn) {
			continue
		}
		m := line(a.VesselID, a.ContractID, &a.Allocation)
		m.ExpectedTime = c.ExpectedDepartureTime
		if m.ExpectedTime == "" {
			m.ExpectedTime = st.DefaultDepartureTime
		}
		out.DeparturesExpected = append(out.DeparturesExpected, m)
	}
	for i := range outAllocs {
		a := outAllocs[i]
		if a.Kind != store.AllocKindContract || (mapID != "" && a.MapID != mapID) {
			continue
		}
		out.VesselsOut = append(out.VesselsOut, line(a.VesselID, a.ContractID, &a.Allocation))
	}
	return out, nil
}
