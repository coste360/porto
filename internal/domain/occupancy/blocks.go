package occupancy

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// BlockInput is a manual block of a berth, of cells of an area or of a land resource (spec 10.5).
type BlockInput struct {
	Target   Target
	From, To calendar.Date // inclusive nights
	Reason   string        // mandatory, no personal data by policy
	LOA      geo.Cm        // areas: size of the blocked footprint
	Beam     geo.Cm
}

// CreateBlock blocks a resource for a period (occupancy.move): kind block, no vessel, rows as any allocation.
func (s *Service) CreateBlock(ctx context.Context, p *auth.Principal, marinaID string, in BlockInput) (*Allocation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return nil, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	v := &auth.ValidationError{}
	if in.Reason == "" || utf8.RuneCountInString(in.Reason) > 200 {
		v.Add("reason", "required")
	}
	if in.To.Before(in.From) {
		v.Add("to", "before_from")
	} else if engine.Nights(in.From, in.To) > 400 {
		v.Add("to", "too_long")
	}
	if in.Target.ResourceType == engine.ResourceArea && (in.LOA <= 0 || in.Beam <= 0) {
		v.Add("loa_cm", "required")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	var out Allocation
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		na := NewAllocation{
			Kind: KindBlock, BlockReason: in.Reason, UserID: p.UserID,
			Spec: engine.AllocSpec{ResourceType: in.Target.ResourceType, ResourceID: in.Target.ResourceID, Placement: in.Target.Placement, First: in.From, Last: in.To},
		}
		if in.Target.ResourceType == engine.ResourceArea {
			na.BlockSize = &layout.Vessel{LOA: in.LOA, Beam: in.Beam}
		}
		a, err := s.AllocateTx(ctx, tx, scope, now, na)
		if err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "block.create", EntityType: "allocation",
			EntityID: a.ID, Diff: map[string]any{"resource_id": a.ResourceID, "from": in.From, "to": in.To},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.OccupancyAllocated, event.OccupancyAllocatedData{
			AllocationIDs: []string{a.ID}, ResourceType: a.ResourceType, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID,
			Kind: a.Kind, From: a.FirstNight, To: a.LastNight,
		}); err != nil {
			return err
		}
		views, err := allocationsOf(ctx, tx, scope, []store.Allocation{*a})
		if err != nil {
			return err
		}
		out = views[0]
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteBlock removes a block from today on (occupancy.move, If-Match): a block that has not started is
// cancelled; one that started keeps its past nights.
func (s *Service) DeleteBlock(ctx context.Context, p *auth.Principal, marinaID, allocID string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		as, err := store.LockAllocations(ctx, tx, scope, []string{allocID})
		if err != nil {
			return err
		}
		a := &as[0]
		if a.Kind != store.AllocKindBlock || a.Status != store.AllocActive {
			return ErrAllocationNotActive
		}
		if a.Version != ifMatch {
			return ErrVersionMismatch
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		today := calendar.In(now, loc)
		from := today
		if a.LastNight.Before(today) {
			return ErrAllocationNotActive
		}
		if !today.After(a.FirstNight) {
			from = a.FirstNight
			err = s.cancelLocked(ctx, tx, scope, now, a, StatusCancelled)
		} else {
			err = truncateLocked(ctx, tx, scope, now, a, today)
		}
		if err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "block.delete", EntityType: "allocation",
			EntityID: a.ID, Diff: map[string]any{"status": a.Status, "last_night": a.LastNight},
		}); err != nil {
			return err
		}
		_, err = s.d.Outbox.Write(ctx, tx, &scope, event.OccupancyReleased, event.OccupancyReleasedData{
			AllocationIDs: []string{a.ID}, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID, From: from,
			To: maxDate(from, as[0].LastNight), Reason: "block_removed",
		})
		return err
	})
}

func maxDate(a, b calendar.Date) calendar.Date {
	if b.After(a) {
		return b
	}
	return a
}

// Release sources (releases.source).
var releaseSources = map[string]bool{"customer_phone": true, "customer_desk": true, "customer_link": true, "dock": true, "office": true, "system": true}

// ReleaseInput is a release of nights of a contract allocation (spec 10.6).
type ReleaseInput struct {
	AllocationID string
	From, To     calendar.Date
	Source       string
	Notes        string
}

// CreateRelease releases nights of a contract allocation (occupancy.move): from today on, inside the allocation,
// not overlapping another active release; the rows of those nights are deleted (m3-plan §3.4, §13 D15).
func (s *Service) CreateRelease(ctx context.Context, p *auth.Principal, marinaID string, in ReleaseInput) (*Release, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return nil, err
	}
	if !releaseSources[in.Source] || in.Source == "system" || in.Source == "customer_link" {
		return nil, invalid("source", "invalid")
	}
	if in.To.Before(in.From) {
		return nil, invalid("to_night", "before_from")
	}
	if utf8.RuneCountInString(in.Notes) > 2000 {
		return nil, invalid("notes", "too_long")
	}
	var out Release
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		as, err := store.LockAllocations(ctx, tx, scope, []string{in.AllocationID})
		if err != nil {
			return err
		}
		a := &as[0]
		if a.Status != store.AllocActive {
			return &ReleaseError{Reason: "not_active"}
		}
		if a.Kind != store.AllocKindContract {
			return &ReleaseError{Reason: "not_contract"}
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		if in.From.Before(calendar.In(now, loc)) {
			return &ReleaseError{Reason: "past_night"}
		}
		if in.From.Before(a.FirstNight) || in.To.After(a.LastNight) {
			return &ReleaseError{Reason: "outside_allocation"}
		}
		rels, err := store.ActiveReleasesOf(ctx, tx, scope, []string{a.ID})
		if err != nil {
			return err
		}
		for _, r := range rels {
			if !r.ToNight.Before(in.From) && !r.FromNight.After(in.To) {
				return &ReleaseError{Reason: "overlapping"}
			}
		}
		contracts, err := store.ContractsOfAllocations(ctx, tx, scope, []string{a.ID})
		if err != nil {
			return err
		}
		if contracts[a.ID] == "" {
			return &ReleaseError{Reason: "not_contract"}
		}
		if _, err := store.DeleteAllocationRows(ctx, tx, scope, a, in.From, in.To); err != nil {
			return err
		}
		v := a.Version
		if err := store.UpdateAllocation(ctx, tx, scope, a, v, now); err != nil {
			return err
		}
		r := &store.Release{
			ID: store.NewID(), AllocationID: a.ID, ContractID: contracts[a.ID], ResourceType: a.ResourceType, ResourceID: a.ResourceID,
			FromNight: in.From, ToNight: in.To, Status: store.ReleaseActive, Source: in.Source, Notes: in.Notes,
			CreatedByUserID: p.UserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertRelease(ctx, tx, scope, r); err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "release.create", EntityType: "release",
			EntityID: r.ID, Diff: map[string]any{"allocation_id": a.ID, "from": in.From, "to": in.To, "source": in.Source},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ReleaseCreated, event.ReleaseData{
			ReleaseID: r.ID, AllocationID: a.ID, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID, From: in.From, To: in.To,
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.OccupancyReleased, event.OccupancyReleasedData{
			AllocationIDs: []string{a.ID}, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID, From: in.From, To: in.To, Reason: "release",
		}); err != nil {
			return err
		}
		refs, err := store.ResourceRefs(ctx, tx, scope, []string{a.ResourceID})
		if err != nil {
			return err
		}
		out = releaseOf(r, refs[a.ResourceID].Code, true)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeResult is the outcome of a revoke.
type RevokeResult struct {
	Release        Release
	RestoredNights int
	Remaining      *Release
}

// RevokeRelease gives back released nights to their allocation (occupancy.move, If-Match): the range defaults to
// the whole release from today on and must be inside it, never before today. Every night must still be free: a resold night is a *ConflictError
// whose Alternatives are a search for the vessel on the taken nights (nothing changes). A partial revoke
// shortens the release, splitting it when the range is in the middle.
func (s *Service) RevokeRelease(ctx context.Context, p *auth.Principal, marinaID, releaseID string, ifMatch int, from, to *calendar.Date) (*RevokeResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermOccupancyMove)
	if err != nil {
		return nil, err
	}
	var out RevokeResult
	var vesselID string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		r, err := store.GetRelease(ctx, tx, scope, releaseID)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		if r.Status != store.ReleaseActive {
			return &ReleaseError{Reason: "not_active"}
		}
		loc, err := marinaLocation(ctx, tx, scope)
		if err != nil {
			return err
		}
		today := calendar.In(now, loc)
		// Past nights are history (as for releases and moves): the default range starts today at the earliest,
		// an explicit range must not start before today.
		f, l := r.FromNight, r.ToNight
		if f.Before(today) {
			f = today
		}
		if from != nil {
			if from.Before(today) {
				return &ReleaseError{Reason: "past_nights"}
			}
			f = *from
		}
		if to != nil {
			l = *to
		}
		if l.Before(today) {
			return &ReleaseError{Reason: "past_nights"}
		}
		if l.Before(f) || f.Before(r.FromNight) || l.After(r.ToNight) {
			return &ReleaseError{Reason: "outside_release"}
		}
		as, err := store.LockAllocations(ctx, tx, scope, []string{r.AllocationID})
		if err != nil {
			return err
		}
		a := &as[0]
		vesselID = a.VesselID
		if a.Status != store.AllocActive {
			return &ReleaseError{Reason: "not_active"}
		}
		res, v, err := s.specInputs(ctx, tx, scope, a)
		if err != nil {
			return err
		}
		rows, err := engine.Plan(engine.AllocSpec{
			ResourceType: engine.ResourceType(a.ResourceType), ResourceID: a.ResourceID, Placement: placementOf(a), First: f, Last: l,
		}, res.Area, v)
		if err != nil {
			return err
		}
		if err := insertPlan(ctx, tx, scope, now, a, rows); err != nil {
			return err
		}
		rv := r.Version
		var remaining *store.Release
		switch {
		case f.Equal(r.FromNight) && l.Equal(r.ToNight):
			r.Status, r.RevokedAt = store.ReleaseRevoked, now
		case f.Equal(r.FromNight):
			r.FromNight = l.AddDays(1)
		case l.Equal(r.ToNight):
			r.ToNight = f.AddDays(-1)
		default:
			tail := *r
			tail.ID, tail.FromNight, tail.Version, tail.CreatedAt, tail.UpdatedAt = store.NewID(), l.AddDays(1), 1, now, now
			tail.ResoldAllocationID, tail.ResoldNights = "", 0
			remaining = &tail
			r.ToNight = f.AddDays(-1)
		}
		if err := store.UpdateRelease(ctx, tx, scope, r, rv, now); err != nil {
			return err
		}
		if remaining != nil {
			if err := store.InsertRelease(ctx, tx, scope, remaining); err != nil {
				return err
			}
		}
		av := a.Version
		if err := store.UpdateAllocation(ctx, tx, scope, a, av, now); err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "release.revoke", EntityType: "release",
			EntityID: r.ID, Diff: map[string]any{"from": f, "to": l, "status": r.Status},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.OccupancyAllocated, event.OccupancyAllocatedData{
			AllocationIDs: []string{a.ID}, ResourceType: a.ResourceType, ResourceIDs: []string{a.ResourceID}, MapID: a.MapID,
			Kind: a.Kind, From: f, To: l,
		}); err != nil {
			return err
		}
		refs, err := store.ResourceRefs(ctx, tx, scope, []string{a.ResourceID})
		if err != nil {
			return err
		}
		code := refs[a.ResourceID].Code
		out = RevokeResult{Release: releaseOf(r, code, true), RestoredNights: engine.Nights(f, l)}
		if remaining != nil {
			rem := releaseOf(remaining, code, true)
			out.Remaining = &rem
		}
		return nil
	})
	if ce, ok := errors.AsType[*ConflictError](err); ok && vesselID != "" {
		ce.Alternatives = s.alternatives(ctx, p, marinaID, vesselID, ce.From, ce.To)
		return nil, ce
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseFilter selects releases (listReleases).
type ReleaseFilter struct {
	From, To   calendar.Date
	ResourceID string
	Status     string
}

// ListReleases returns a page of releases (contracts.read); notes only with customers.read.
func (s *Service) ListReleases(ctx context.Context, p *auth.Principal, marinaID string, f ReleaseFilter, cursor string, limit int) ([]Release, string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListReleases(ctx, s.d.DB.DB, scope, store.ReleaseFilter(f), cursor, limit)
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, len(page.Items))
	for i, r := range page.Items {
		ids[i] = r.ResourceID
	}
	refs, err := store.ResourceRefs(ctx, s.d.DB.DB, scope, ids)
	if err != nil {
		return nil, "", err
	}
	withNotes := p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead)
	out := make([]Release, len(page.Items))
	for i := range page.Items {
		out[i] = releaseOf(&page.Items[i], refs[page.Items[i].ResourceID].Code, withNotes)
	}
	return out, page.NextCursor, nil
}
