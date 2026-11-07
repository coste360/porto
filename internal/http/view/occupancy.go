package view

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
)

// Views of the occupancy engine (m3-plan §7.3, §8.3).

func datePtr(d *calendar.Date) *api.IsoDate {
	if d == nil || d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func cmPtrOf(c *geo.Cm) *api.Cm {
	if c == nil {
		return nil
	}
	v := int64(*c)
	return &v
}

func timePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func placementFields(p *engine.Placement) (x, y, rot *int64) {
	if p == nil {
		return nil, nil, nil
	}
	xv, yv, rv := int64(p.X), int64(p.Y), int64(p.Rotation)
	return &xv, &yv, &rv
}

// NightRanges converts night ranges.
func NightRanges(rs []engine.NightRange) []api.NightRange {
	out := make([]api.NightRange, 0, len(rs))
	for _, r := range rs {
		out = append(out, api.NightRange{From: r.From.String(), To: r.To.String()})
	}
	return out
}

// FitIssues converts fit issues.
func FitIssues(list []fit.Issue) []api.FitIssue {
	out := make([]api.FitIssue, 0, len(list))
	for _, is := range list {
		fi := api.FitIssue{Code: api.FitCode(is.Code)}
		if is.Limit != 0 || is.Actual != 0 {
			l, a := int64(is.Limit), int64(is.Actual)
			fi.LimitCm, fi.ActualCm = &l, &a
		}
		out = append(out, fi)
	}
	return out
}

// FitResult converts a fit result.
func FitResult(r fit.Result) api.FitResult {
	return api.FitResult{Ok: r.OK, Overridable: r.Overridable, FillBp: r.FillBP, Blocking: FitIssues(r.Blocking), Warnings: FitIssues(r.Warnings)}
}

// Allocation converts an allocation.
func Allocation(a *occupancy.Allocation) api.Allocation {
	x, y, rot := placementFields(a.Placement)
	out := api.Allocation{
		Id: a.ID, ContractId: String(a.ContractID), ContractLineId: String(a.ContractLineID), VesselId: String(a.VesselID),
		ResourceType: api.ResourceType(a.ResourceType), ResourceId: a.ResourceID, ResourceCode: String(a.ResourceCode), MapId: a.MapID,
		XCm: x, YCm: y, RotationDd: rot, FirstNight: a.First.String(), LastNight: a.Last.String(), Kind: api.AllocationKind(a.Kind),
		Status: api.AllocationStatus(a.Status), ExpiresAt: timePtr(a.ExpiresAt), BlockReason: String(a.BlockReason),
		MovedFromId: String(a.MovedFromID), Released: NightRanges(a.Released), Version: a.Version,
	}
	if a.FitOverride != nil {
		codes := a.FitOverride.Codes
		if codes == nil {
			codes = []string{}
		}
		out.FitOverride = &api.FitOverride{UserId: a.FitOverride.UserID, Reason: a.FitOverride.Reason, Codes: codes}
	}
	return out
}

// Allocations converts allocations.
func Allocations(as []occupancy.Allocation) []api.Allocation {
	out := make([]api.Allocation, len(as))
	for i := range as {
		out[i] = Allocation(&as[i])
	}
	return out
}

// MoveResult converts the result of a move, swap, layout save.
func MoveResult(m occupancy.MoveResult) api.MoveResult {
	return api.MoveResult{Allocations: Allocations(m.Allocations), UndoId: m.UndoID, UndoUntil: m.UndoUntil.UTC(), PriceEffect: priceEffect(m.PriceEffect)}
}

func priceEffect(e *occupancy.PriceEffect) *api.PriceEffect {
	if e == nil {
		return nil
	}
	applied := e.Applied
	return &api.PriceEffect{OldTotalCents: e.OldTotalCents, NewTotalCents: e.NewTotalCents, DeltaCents: e.DeltaCents, Applied: &applied}
}

func contractBrief(c *occupancy.ContractBrief) *api.ContractBrief {
	if c == nil {
		return nil
	}
	out := &api.ContractBrief{Id: c.ID, Kind: api.ContractKind(c.Kind), Status: api.ContractStatus(c.Status), Code: c.Code}
	if c.Term != "" {
		t := api.ContractTerm(c.Term)
		out.Term = &t
	}
	return out
}

func vesselBrief(v *occupancy.VesselBrief) api.VesselBrief {
	return api.VesselBrief{Id: v.ID, Name: v.Name, LoaCm: int64(v.LOA), BeamCm: int64(v.Beam), HullType: api.HullType(v.Hull), DraftCm: cmPtrOf(v.Draft)}
}

func customerBrief(c *occupancy.CustomerBrief) *api.CustomerBrief {
	if c == nil {
		return nil
	}
	return &api.CustomerBrief{Id: c.ID, DisplayName: c.DisplayName}
}

func dayPresence(p *occupancy.DayPresence) *api.DayPresence {
	if p == nil {
		return nil
	}
	return &api.DayPresence{State: api.PresenceState(p.State), Since: p.Since.UTC(), ExpectedReturnOn: datePtr(p.ExpectedReturnOn), AwayDays: p.AwayDays}
}

func dayFit(f *occupancy.DayFit) *api.DayFit {
	if f == nil {
		return nil
	}
	codes := make([]api.FitCode, len(f.BlockingCodes))
	for i, c := range f.BlockingCodes {
		codes[i] = api.FitCode(c)
	}
	return &api.DayFit{FillBp: f.FillBP, Override: f.Override, BlockingCodes: codes}
}

func anomalyCodes(list []string) []api.AnomalyCode {
	out := make([]api.AnomalyCode, len(list))
	for i, c := range list {
		out[i] = api.AnomalyCode(c)
	}
	return out
}

func cellsRange(linear bool, cells []int) api.CellsRange {
	if len(cells) == 0 {
		return api.CellsRange{}
	}
	if linear {
		f, l := cells[0], cells[len(cells)-1]
		return api.CellsRange{First: &f, Last: &l}
	}
	idx := append([]int(nil), cells...)
	return api.CellsRange{Indexes: &idx}
}

// DayView converts a day view.
func DayView(d *occupancy.DayView) api.DayView {
	out := api.DayView{
		MapId: d.MapID, Date: d.Date.String(), MapVersion: d.MapVersion, GeneratedAt: d.GeneratedAt.UTC(),
		Elements: make([]api.DayElement, 0, len(d.Elements)), AreaPlacements: make([]api.AreaPlacement, 0, len(d.AreaPlacements)),
		Unplaced: make([]api.UnplacedVessel, 0, len(d.Unplaced)),
	}
	out.Counts.Anomalies = d.AnomalyCount
	for i := range d.Elements {
		e := &d.Elements[i]
		el := api.DayElement{
			ResourceType: api.ResourceType(e.ResourceType), ResourceId: e.ResourceID, Code: e.Code, Status: api.DayStatus(e.Status),
			AllocationId: String(e.AllocationID), HoldExpiresAt: timePtr(e.HoldExpiresAt), Contract: contractBrief(e.Contract),
			Customer: customerBrief(e.Customer), Presence: dayPresence(e.Presence), ArrivalToday: e.ArrivalToday,
			DepartureToday: e.DepartureToday, Fit: dayFit(e.Fit), Anomalies: anomalyCodes(e.Anomalies),
		}
		if e.AllocationID != "" {
			v := e.AllocationVersion
			el.AllocationVersion = &v
			k := api.AllocationKind(e.Kind)
			el.Kind = &k
		}
		if e.Vessel != nil {
			vb := vesselBrief(e.Vessel)
			el.Vessel = &vb
		}
		if e.Release != nil {
			el.Release = &api.DayRelease{Id: e.Release.ID, From: e.Release.From.String(), To: e.Release.To.String(), Resold: e.Release.Resold}
		}
		if e.Deadlines != nil {
			el.Deadlines = &api.DayDeadlines{
				ContractEndsOn: datePtr(e.Deadlines.ContractEndsOn), Insurance: api.DeadlineState(e.Deadlines.Insurance),
				Documents: api.DeadlineState(e.Deadlines.Documents),
			}
		}
		if e.Utilities != nil {
			u := &api.DayUtilities{PowerA: e.Utilities.PowerA, Water: e.Utilities.Water}
			if e.Utilities.PowerPhase != "" {
				ph := api.PowerPhase(e.Utilities.PowerPhase)
				u.PowerPhase = &ph
			}
			el.Utilities = u
		}
		out.Elements = append(out.Elements, el)
	}
	for i := range d.AreaPlacements {
		a := &d.AreaPlacements[i]
		x, y, rot := placementFields(&a.Placement)
		ap := api.AreaPlacement{
			AreaId: a.AreaID, AllocationId: a.AllocationID, AllocationVersion: a.AllocationVersion, XCm: *x, RotationDd: rot,
			Cells: cellsRange(a.Linear, a.Cells), Status: api.DayStatus(a.Status), Kind: api.AllocationKind(a.Kind),
			Contract: contractBrief(a.Contract), Vessel: vesselBrief(&a.Vessel), Customer: customerBrief(a.Customer),
			Presence: dayPresence(a.Presence), Fit: dayFit(a.Fit), Anomalies: anomalyCodes(a.Anomalies),
		}
		if !a.Linear {
			ap.YCm = y
		}
		out.AreaPlacements = append(out.AreaPlacements, ap)
	}
	for i := range d.Unplaced {
		u := &d.Unplaced[i]
		out.Unplaced = append(out.Unplaced, api.UnplacedVessel{Vessel: vesselBrief(&u.Vessel), PresenceId: u.PresenceID, Reason: api.UnplacedVesselReason(u.Reason)})
	}
	return out
}

// MapOccupancy converts a map occupancy range.
func MapOccupancy(m *occupancy.MapOccupancy) api.MapOccupancy {
	out := api.MapOccupancy{From: m.From.String(), To: m.To.String(), Allocations: make([]api.MapAllocation, 0, len(m.Allocations))}
	for i := range m.Allocations {
		a := &m.Allocations[i]
		x, y, rot := placementFields(a.Placement)
		ma := api.MapAllocation{
			Id: a.ID, ResourceType: api.ResourceType(a.ResourceType), ResourceId: a.ResourceID, XCm: x, YCm: y, RotationDd: rot,
			FirstNight: a.First.String(), LastNight: a.Last.String(), Kind: api.AllocationKind(a.Kind),
			Status: api.AllocationStatus(a.Status), Released: NightRanges(a.Released), Version: a.Version,
			ExpiresAt: timePtr(a.ExpiresAt), Contract: contractBrief(a.Contract),
		}
		if a.Vessel != nil {
			vb := vesselBrief(a.Vessel)
			ma.Vessel = &vb
		}
		if a.ResourceType == engine.ResourceArea {
			c := cellsRange(a.Linear, a.Cells)
			ma.Cells = &c
		}
		out.Allocations = append(out.Allocations, ma)
	}
	return out
}

// Release converts a release.
func Release(r *occupancy.Release) api.Release {
	return api.Release{
		Id: r.ID, AllocationId: r.AllocationID, ContractId: r.ContractID, ResourceType: api.ResourceType(r.ResourceType),
		ResourceId: r.ResourceID, ResourceCode: String(r.ResourceCode), FromNight: r.From.String(), ToNight: r.To.String(),
		Status: api.ReleaseStatus(r.Status), Source: api.ReleaseSource(r.Source), ResoldAllocationId: String(r.ResoldAllocationID),
		ResoldNights: r.ResoldNights, CreditCents: r.CreditCents, Notes: r.Notes, RevokedAt: timePtr(r.RevokedAt),
		CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
}

// Releases converts releases.
func Releases(rs []occupancy.Release) []api.Release {
	out := make([]api.Release, len(rs))
	for i := range rs {
		out[i] = Release(&rs[i])
	}
	return out
}

// Closure converts a closure.
func Closure(c *occupancy.Closure) api.Closure {
	out := api.Closure{
		Id: c.ID, ScopeType: api.ClosureScope(c.ScopeType), ScopeId: String(c.ScopeID), FromDate: c.From.String(),
		ToDate: c.To.String(), Reason: c.Reason, Version: c.Version,
	}
	if c.ResourceType != "" {
		rt := api.ResourceType(c.ResourceType)
		out.ResourceType = &rt
	}
	return out
}

// Closures converts closures.
func Closures(cs []occupancy.Closure) []api.Closure {
	out := make([]api.Closure, len(cs))
	for i := range cs {
		out[i] = Closure(&cs[i])
	}
	return out
}

// OccupancySettings converts the occupancy settings.
func OccupancySettings(s *occupancy.Settings) api.OccupancySettings {
	return api.OccupancySettings{
		SafetyClearanceCm: s.SafetyClearanceCm, WasteThresholdBp: s.WasteThresholdBP, DraftHoldDays: s.DraftHoldDays,
		DeskHoldHours: s.DeskHoldHours, QuoteValidityDays: s.QuoteValidityDays, MaxSegments: s.MaxSegments,
		AbsentDaysThreshold: s.AbsentDaysThreshold, ArrivalGraceMinutes: s.ArrivalGraceMinutes,
		DefaultArrivalTime: s.DefaultArrivalTime, DefaultDepartureTime: s.DefaultDepartureTime, RepriceOnMove: s.RepriceOnMove,
		ReleasePolicy: api.ReleasePolicy(s.ReleasePolicy), Version: s.Version,
	}
}

// Solution converts a search solution (prices are set by the handler).
func Solution(s *occupancy.Solution) api.OccupancySolution {
	out := api.OccupancySolution{Key: s.Key, Score: s.Score, Fit: FitResult(s.Fit), Segments: make([]api.SolutionSegment, 0, len(s.Segments))}
	for _, g := range s.Segments {
		x, y, rot := placementFields(g.Placement)
		out.Segments = append(out.Segments, api.SolutionSegment{
			ResourceType: api.ResourceType(g.ResourceType), ResourceId: g.ResourceID, Code: g.Code, MapId: g.MapID,
			XCm: x, YCm: y, RotationDd: rot, FirstNight: g.First.String(), LastNight: g.Last.String(),
		})
	}
	if s.RequiresRelease != nil {
		out.RequiresRelease = &struct {
			AllocationId string `json:"allocation_id"` //nolint:revive // generated anonymous struct field
			ContractId   string `json:"contract_id"`   //nolint:revive // generated anonymous struct field
		}{AllocationId: s.RequiresRelease.AllocationID, ContractId: s.RequiresRelease.ContractID}
	}
	return out
}

// PlacementCheck converts a placement check.
func PlacementCheck(c *occupancy.CheckResult) api.PlacementCheck {
	out := api.PlacementCheck{Ok: c.OK, Fit: FitResult(c.Fit), PlacementError: String(c.PlacementError), ClosureId: String(c.ClosureID), Conflicts: make([]api.ConflictDetail, 0, len(c.Conflicts)), PriceEffect: priceEffect(c.PriceEffect)}
	for _, cf := range c.Conflicts {
		cd := api.ConflictDetail{ResourceType: api.ResourceType(cf.ResourceType), ResourceId: cf.ResourceID, AllocationIds: []string{cf.AllocationID}, From: cf.From.String(), To: cf.To.String(), VesselId: String(cf.VesselID)}
		if len(cf.Cells) > 0 {
			cells := append([]int(nil), cf.Cells...)
			cd.Cells = &cells
		}
		out.Conflicts = append(out.Conflicts, cd)
	}
	return out
}
