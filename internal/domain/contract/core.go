package contract

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Entity types of the audit log.
const (
	entityContract = "contract"
	entityTransit  = "transit"
	entityQuote    = "quote"
	entityInquiry  = "inquiry"
)

// extraLine is an additional land resource of a mooring contract (a parking, a box), allocated for the whole
// period.
type extraLine struct {
	LandResourceID string
	ItemID         string
}

// newContract is everything createTx needs to write a contract or a transit with its lines and allocations.
type newContract struct {
	kind, term        string
	status            Status
	customerID        string
	billingCustomerID string
	vesselID          string
	startsOn, endsOn  calendar.Date
	channel           string
	quoteID           string
	price             *priced
	policy            Policy
	notes             string
	expectedArrival   string
	segments          []Segment
	extras            []extraLine
	services          []ServiceRequest
	allocKind         occupancy.Kind
	expiresAt         *time.Time
	arrival           bool                   // closures apply to the first night
	fit               *occupancy.FitOverride // applies to every resource line
	movedFrom         []string               // per segment: the hold this allocation replaces ("" none)
	priceIsBinding    bool                   // the price comes from an accepted quote option: no list re-check
	plan              string                 // M4: standard (default) or early_payment
	manualTotal       *int64                 // M4 imports: the total without a price snapshot
	skipPlan          bool                   // M4 imports: the residual plan is written by the caller
}

// createTx inserts a contract or transit, its lines and its allocations (one per segment, one per extra land
// resource) in tx. Any allocation conflict fails the whole transaction (INV-01).
func (s *Service) createTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, n newContract) (*store.Contract, []store.Allocation, error) {
	if err := s.requireAllocator(); err != nil {
		return nil, nil, err
	}
	if !n.priceIsBinding {
		if err := checkListStillActive(ctx, tx, scope, n.price); err != nil {
			return nil, nil, err
		}
	}
	now := s.now()
	year := s.today(ctx, tx, scope).Year()
	series := seriesOf(n.kind) // mooring, transit or storage (M9)
	number, err := store.NextNumber(ctx, tx, scope, series, year)
	if err != nil {
		return nil, nil, err
	}
	c := &store.Contract{
		ID: store.NewID(), MarinaID: scope.ID(), Kind: n.kind, Term: n.term, Number: number, Year: year,
		Status: string(n.status), CustomerID: n.customerID, BillingCustomerID: n.billingCustomerID, VesselID: n.vesselID,
		StartsOn: n.startsOn, EndsOn: n.endsOn, Channel: n.channel, QuoteID: n.quoteID,
		PriceListID: n.price.listID(), EngineVersion: n.price.engineVersion(), TotalCents: n.price.total(),
		DepositCents: n.price.deposit(), Policy: store.MustJSON(n.policy), Notes: n.notes,
		ExpectedArrivalTime: n.expectedArrival, CreatedByUserID: a.userID(), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if n.price != nil {
		c.PriceSnapshot = n.price.raw
	}
	if n.manualTotal != nil {
		c.TotalCents = *n.manualTotal
	}
	if c.Channel == "" {
		c.Channel = ChannelDesk
	}
	if n.status == StatusActive {
		c.ActivatedAt, c.SignedOnPaper = now, true
	}
	if err := store.InsertContract(ctx, tx, c); err != nil {
		return nil, nil, fmt.Errorf("insert contract: %w", err)
	}
	var lines []store.ContractLine
	line := func(kind, itemID, rtype, rid string) store.ContractLine {
		l := store.ContractLine{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, Seq: len(lines) + 1, LineKind: kind,
			ItemID: itemID, ResourceType: rtype, ResourceID: rid, QtyMilli: 1000, CreatedAt: now, UpdatedAt: now,
		}
		lines = append(lines, l)
		return l
	}
	type pending struct {
		lineID string
		seg    Segment
		moved  string
	}
	var todo []pending
	for i, g := range n.segments {
		l := line("resource", "", g.ResourceType, g.ResourceID)
		moved := ""
		if i < len(n.movedFrom) {
			moved = n.movedFrom[i]
		}
		todo = append(todo, pending{lineID: l.ID, seg: g, moved: moved})
	}
	for _, x := range n.extras {
		l := line("resource", x.ItemID, store.ResourceLand, x.LandResourceID)
		todo = append(todo, pending{lineID: l.ID, seg: Segment{
			ResourceType: store.ResourceLand, ResourceID: x.LandResourceID, First: n.startsOn, Last: n.endsOn.AddDays(-1),
		}})
	}
	for _, sv := range n.services {
		line("service", sv.ItemID, "", "")
		lines[len(lines)-1].QtyMilli = sv.QtyMilli
	}
	if err := store.InsertContractLines(ctx, tx, lines); err != nil {
		return nil, nil, fmt.Errorf("insert contract lines: %w", err)
	}
	allocs := make([]store.Allocation, 0, len(todo))
	for i, t := range todo {
		al, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
			lineID: t.lineID, vesselID: n.vesselID, kind: n.allocKind, seg: t.seg, expiresAt: n.expiresAt,
			fit: n.fit, movedFrom: t.moved, arrival: n.arrival && i == 0,
		})
		if err != nil {
			return nil, nil, err
		}
		allocs = append(allocs, *al)
	}
	return c, allocs, nil
}

// allocation is one AllocateTx call of a contract line.
type allocation struct {
	lineID    string
	vesselID  string
	kind      occupancy.Kind
	seg       Segment
	expiresAt *time.Time
	fit       *occupancy.FitOverride
	movedFrom string
	arrival   bool
}

func (s *Service) allocateLine(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, now time.Time, in allocation) (*store.Allocation, error) {
	na := occupancy.NewAllocation{
		Kind: in.kind, Spec: in.seg.spec(in.seg.First, in.seg.Last), ExpiresAt: in.expiresAt, FitOverride: in.fit,
		UserID: a.userID(), Arrival: in.arrival,
	}
	if in.lineID != "" {
		id := in.lineID
		na.ContractLineID = &id
	}
	if in.vesselID != "" {
		v := in.vesselID
		na.VesselID = &v
	}
	if in.movedFrom != "" {
		m := in.movedFrom
		na.MovedFromID = &m
	}
	return s.alloc.AllocateTx(ctx, tx, scope, now, na)
}

// contractAllocs returns the allocations of a contract (any status), ordered by first night.
func contractAllocs(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) ([]store.Allocation, error) {
	rows, err := store.AllocationsOfContracts(ctx, tx, scope, []string{contractID})
	if err != nil {
		return nil, err
	}
	out := make([]store.Allocation, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Allocation)
	}
	return out, nil
}

func activeOnly(as []store.Allocation) []store.Allocation {
	out := make([]store.Allocation, 0, len(as))
	for _, a := range as {
		if a.Status == store.AllocActive {
			out = append(out, a)
		}
	}
	return out
}

func idsOf(as []store.Allocation) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}

// cancelAllocs ends every active allocation of as with status and emits occupancy.released (reason cancel).
func (s *Service) cancelAllocs(ctx context.Context, tx bun.IDB, scope store.MarinaScope, as []store.Allocation, status occupancy.AllocStatus, reason string) error {
	for _, al := range activeOnly(as) {
		if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, status); err != nil {
			return err
		}
		if reason == "" {
			continue
		}
		if err := s.emit(ctx, tx, scope, event.OccupancyReleased, event.OccupancyReleasedData{
			AllocationIDs: []string{al.ID}, ResourceIDs: []string{al.ResourceID}, MapID: al.MapID,
			From: al.FirstNight, To: al.LastNight, Reason: reason,
		}); err != nil {
			return err
		}
	}
	return nil
}

// emitAllocated writes occupancy.allocated for each allocation.
func (s *Service) emitAllocated(ctx context.Context, tx bun.IDB, scope store.MarinaScope, as []store.Allocation, contractID string) error {
	for _, al := range as {
		if err := s.emit(ctx, tx, scope, event.OccupancyAllocated, event.OccupancyAllocatedData{
			AllocationIDs: []string{al.ID}, ResourceType: al.ResourceType, ResourceIDs: []string{al.ResourceID},
			MapID: al.MapID, Kind: al.Kind, From: al.FirstNight, To: al.LastNight, ContractID: contractID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// subject is what the events of a transition describe.
type subject struct {
	contract *store.Contract
	quote    *store.Quote
	change   string             // internal *.changed events
	expired  []store.Allocation // occupancy.hold_expired
	// M4 (m4-plan §7.5): contract.signed and contract.renewed.
	driver    string // paper (default), otp, provider
	successor string // contract.renewed: the successor contract
	tacit     bool
}

// emitTransition writes the events of a transition row, in order (m3-plan §4, §7.5).
func (s *Service) emitTransition(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t Transition, sub subject) error {
	for _, typ := range t.Events {
		var data any
		c, q := sub.contract, sub.quote
		switch typ {
		case event.ContractCreated, event.ContractActivated, event.ContractTerminated:
			data = event.ContractData{ContractID: c.ID, Kind: c.Kind, Term: c.Term, Status: c.Status, From: c.StartsOn, To: c.EndsOn.AddDays(-1)}
		case event.TransitHeld, event.TransitConfirmed, event.TransitCancelled, event.TransitArrived, event.TransitDeparted:
			d := event.TransitData{ContractID: c.ID, Status: string(t.To), From: c.StartsOn, To: c.EndsOn.AddDays(-1)}
			if typ == event.TransitConfirmed {
				d.ConfirmationSource = c.ConfirmationSource
			}
			data = d
		case event.ContractSigned:
			driver := sub.driver
			if driver == "" {
				driver = "paper"
			}
			data = event.ContractSignedData{
				ContractID: c.ID, Kind: c.Kind, Term: c.Term, Status: string(t.To), SignatureRequestID: c.SignatureRequestID, Driver: driver,
			}
		case event.ContractRenewed:
			data = event.ContractRenewedData{
				ContractID: c.ID, SuccessorContractID: sub.successor, Kind: c.Kind, Term: c.Term, From: c.StartsOn,
				To: c.EndsOn.AddDays(-1), Tacit: sub.tacit,
			}
		case event.ContractChanged:
			data = event.ContractChangedData{ContractID: c.ID, Change: sub.change}
		case event.TransitChanged:
			data = event.TransitChangedData{ContractID: c.ID, Change: sub.change}
		case event.QuoteSent, event.QuoteAccepted, event.QuoteExpired:
			data = event.QuoteData{QuoteID: q.ID, Status: string(t.To), ContractID: q.ContractID}
		case event.QuoteChanged:
			data = event.QuoteChangedData{QuoteID: q.ID, Change: sub.change}
		case event.OccupancyHoldExpired:
			if len(sub.expired) == 0 {
				continue
			}
			d := event.OccupancyHoldExpiredData{From: sub.expired[0].FirstNight, To: sub.expired[0].LastNight, MapID: sub.expired[0].MapID}
			for _, al := range sub.expired {
				d.AllocationIDs = append(d.AllocationIDs, al.ID)
				d.ResourceIDs = append(d.ResourceIDs, al.ResourceID)
				if al.FirstNight.Before(d.From) {
					d.From = al.FirstNight
				}
				if al.LastNight.After(d.To) {
					d.To = al.LastNight
				}
			}
			if c != nil {
				d.ContractID = c.ID
			}
			if q != nil {
				d.QuoteID = q.ID
			}
			data = d
		default:
			return fmt.Errorf("contract: no payload for event %s", typ)
		}
		if err := s.emit(ctx, tx, scope, typ, data); err != nil {
			return err
		}
	}
	return nil
}

// updateContract writes c (version-checked against expected).
func updateContract(ctx context.Context, tx bun.IDB, scope store.MarinaScope, expected int, c *store.Contract, now time.Time) error {
	c.UpdatedAt = now
	if err := store.UpdateContract(ctx, tx, scope, expected, c); err != nil {
		return versionErr(err)
	}
	return store.SetContractM4(ctx, tx, scope, c)
}

// lockContract loads a contract of the given kind ("" any) for a state change, checking ifMatch (0 = skip).
func lockContract(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id, kind string, ifMatch int) (*store.Contract, error) {
	c, err := store.LockContract(ctx, tx, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if kind != "" && (c.Kind == KindTransit) != (kind == KindTransit) {
		return nil, notFound(store.ErrNotFound)
	}
	if ifMatch != 0 && c.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	return c, nil
}

// convertOrReallocate turns the live holds of a contract into allocations of kind (confirmTx, ActivateContract):
// holds still valid are converted in place, holds expired but not reclaimed yet are expired first, and every
// lapsed allocation that ends a live chain (lapsedChainEnds) is allocated again on its own resource and nights,
// with moved_from_id = the lapsed one. A taken night fails with *occupancy.ConflictError. It returns the
// re-allocated allocations and the converted hold ids.
func (s *Service) convertOrReallocate(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, now time.Time,
	c *store.Contract, allocs []store.Allocation, kind occupancy.Kind,
) ([]store.Allocation, []string, error) {
	var holds []string
	for i := range allocs {
		al := &allocs[i]
		if al.Status != store.AllocActive {
			continue
		}
		if al.Kind == store.AllocKindHold && !al.ExpiresAt.After(now) {
			// Expired but not reclaimed yet: it is lost, it is allocated again below.
			if err := s.alloc.CancelTx(ctx, tx, scope, al.ID, occupancy.StatusExpired); err != nil {
				return nil, nil, err
			}
			al.Status = store.AllocExpired
			continue
		}
		if al.Kind == store.AllocKindHold {
			holds = append(holds, al.ID)
		}
	}
	if len(holds) > 0 {
		if err := s.alloc.ConvertTx(ctx, tx, scope, now, holds, kind, nil); err != nil {
			return nil, nil, err
		}
	}
	var reallocated []store.Allocation
	for _, al := range lapsedChainEnds(allocs) {
		var fit *occupancy.FitOverride
		if len(al.FitOverride) > 0 {
			fit = &occupancy.FitOverride{}
			if jsonUnmarshal(al.FitOverride, fit) != nil {
				fit = nil
			}
		}
		n, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
			lineID: al.ContractLineID, vesselID: c.VesselID, kind: kind, seg: segmentOf(&al), fit: fit, movedFrom: al.ID,
		})
		if err != nil {
			return nil, nil, err
		}
		reallocated = append(reallocated, *n)
	}
	return reallocated, holds, nil
}

// lapsedChainEnds returns the lapsed (expired) allocations of a contract that still own their nights: an expired
// allocation is replaced only by an allocation that moved_from it and starts on or before its last night (a
// re-allocation of the same nights). A successor that starts after it (the next leg of an interval move, an
// extension) does not replace it, so after an interval move or an extension every lapsed leg is returned, each
// once. Superseded and cancelled allocations never are (their nights moved on or are gone).
func lapsedChainEnds(allocs []store.Allocation) []store.Allocation {
	var out []store.Allocation
	for _, al := range allocs {
		if al.Status != store.AllocExpired || al.ContractLineID == "" {
			continue
		}
		replaced := false
		for _, b := range allocs {
			if b.ID != al.ID && b.MovedFromID == al.ID && !b.FirstNight.After(al.LastNight) {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, al)
		}
	}
	return out
}
