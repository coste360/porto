package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/qty"
)

// Occupancy operations (m3-plan §7.3, first table): settings, day view, map occupancy, allocations, search,
// placement check, moves, swaps, layout saves, compaction, undo, blocks, releases and closures.

// invalid answers a validation error, unless the marina is not the caller's: then 404 first, so another
// marina's routes never reveal anything (cross-marina sweep, INV-73).
func (h *Handler) invalid(w http.ResponseWriter, r *http.Request, marinaID string, err error) {
	if _, merr := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); merr != nil {
		respond.Error(w, r, merr)
		return
	}
	respond.Error(w, r, err)
}

func reqDate(v *auth.ValidationError, field, s string) calendar.Date {
	d, err := calendar.Parse(s)
	if err != nil {
		v.Add(field, "invalid")
	}
	return d
}

func placementTarget(p api.PlacementInput) occupancy.Target {
	t := occupancy.Target{ResourceType: engine.ResourceType(p.ResourceType), ResourceID: p.ResourceId}
	if p.XCm != nil {
		pl := &engine.Placement{X: geo.Cm(*p.XCm)}
		if p.YCm != nil {
			pl.Y = geo.Cm(*p.YCm)
		}
		if p.RotationDd != nil {
			pl.Rotation = geo.Angle(*p.RotationDd)
		}
		t.Placement = pl
	}
	return t
}

func overrideReason(o *api.FitOverrideInput) *string {
	if o == nil {
		return nil
	}
	r := strings.TrimSpace(o.Reason)
	return &r
}

// GetOccupancySettings implements api.ServerInterface.
func (h *Handler) GetOccupancySettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Occupancy.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, view.OccupancySettings(s))
}

// UpdateOccupancySettings implements api.ServerInterface.
func (h *Handler) UpdateOccupancySettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateOccupancySettingsParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.OccupancySettingsUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.Occupancy.UpdateSettings(r.Context(), principal(r), marinaID, version, occupancy.Settings{
		SafetyClearanceCm: in.SafetyClearanceCm, WasteThresholdBP: in.WasteThresholdBp, DraftHoldDays: in.DraftHoldDays,
		DeskHoldHours: in.DeskHoldHours, QuoteValidityDays: in.QuoteValidityDays, MaxSegments: in.MaxSegments,
		AbsentDaysThreshold: in.AbsentDaysThreshold, ArrivalGraceMinutes: in.ArrivalGraceMinutes,
		DefaultArrivalTime: in.DefaultArrivalTime, DefaultDepartureTime: in.DefaultDepartureTime,
		RepriceOnMove: in.RepriceOnMove, ReleasePolicy: string(in.ReleasePolicy),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, view.OccupancySettings(s))
}

// GetMapDay implements api.ServerInterface.
func (h *Handler) GetMapDay(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.GetMapDayParams) {
	v := &auth.ValidationError{}
	date := parseDate(v, "date", params.Date)
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	if date != nil && date.IsZero() {
		date = nil
	}
	d, err := h.Occupancy.GetDay(r.Context(), principal(r), marinaID, mapID, date)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.DayView(d))
}

// GetMapOccupancy implements api.ServerInterface.
func (h *Handler) GetMapOccupancy(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, mapID api.MapId, params api.GetMapOccupancyParams) {
	v := &auth.ValidationError{}
	from, to := parseDate(v, "from", params.From), parseDate(v, "to", params.To)
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	var ids []string
	if params.ResourceIds != nil {
		ids = []string{}
		for _, id := range strings.Split(*params.ResourceIds, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}
	m, err := h.Occupancy.GetMapOccupancy(r.Context(), principal(r), marinaID, mapID, from, to, ids)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MapOccupancy(m))
}

// GetAllocation implements api.ServerInterface.
func (h *Handler) GetAllocation(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, allocationID api.AllocationId) {
	d, err := h.Occupancy.GetAllocation(r.Context(), principal(r), marinaID, allocationID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, d.Version)
	respond.JSON(w, http.StatusOK, view.Allocation(&d.Allocation))
}

// contractTypeOf maps a search purpose and term to the price list contract type (m3-plan §13 D10).
func contractTypeOf(purpose string, term *api.ContractTerm) pricing.ContractType {
	if purpose == engine.PurposeTransit || term == nil {
		return pricing.ContractTransit
	}
	switch *term {
	case "annual":
		return pricing.ContractAnnual
	case "seasonal":
		return pricing.ContractSeasonal
	}
	return pricing.ContractMonthly
}

// SearchOccupancy implements api.ServerInterface. Each solution is priced through the M2 engine on its first
// segment's resource; a pricing failure is reported on the solution, never as an error of the search.
func (h *Handler) SearchOccupancy(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.OccupancySearchRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	si := occupancy.SearchInput{
		Purpose: string(in.Purpose), VesselID: str(in.VesselId), Arrival: reqDate(v, "arrival", in.Arrival),
		Departure: reqDate(v, "departure", in.Departure), ConsiderOut: in.ConsiderOut != nil && *in.ConsiderOut,
		MaxSegments: in.MaxSegments, Limit: intOr(in.Limit, 0), Channel: str(enumPtr(in.Channel)),
	}
	if in.Term != nil {
		si.Term = string(*in.Term)
	}
	if in.Vessel != nil && si.VesselID == "" {
		fv := fit.Vessel{LOA: geo.Cm(in.Vessel.LoaCm), Beam: geo.Cm(in.Vessel.BeamCm), Hull: string(in.Vessel.HullType)}
		if in.Vessel.DraftCm != nil {
			d := geo.Cm(*in.Vessel.DraftCm)
			fv.Draft = &d
		}
		if in.Vessel.AirDraftCm != nil {
			d := geo.Cm(*in.Vessel.AirDraftCm)
			fv.AirDraft = &d
		}
		si.Vessel = &fv
	}
	if in.ResourceTypes != nil {
		for _, t := range *in.ResourceTypes {
			si.ResourceTypes = append(si.ResourceTypes, engine.ResourceType(t))
		}
	}
	if p := in.Prefs; p != nil {
		if p.GroupIds != nil {
			si.Prefs.GroupIDs = *p.GroupIds
		}
		if p.StructureIds != nil {
			si.Prefs.StructureIDs = *p.StructureIds
		}
		if p.PowerA != nil {
			pa := int64(*p.PowerA)
			si.Prefs.PowerA = &pa
		}
		si.Prefs.Water = p.Water != nil && *p.Water
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	res, err := h.Occupancy.Search(r.Context(), principal(r), marinaID, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	var services []pricing.ServiceRequest
	if in.Services != nil {
		for _, s := range *in.Services {
			q := qty.Milli(1000)
			if s.QtyMilli != nil {
				q = qty.Milli(*s.QtyMilli)
			}
			services = append(services, pricing.ServiceRequest{ItemID: s.ItemId, Qty: q})
		}
	}
	out := api.OccupancySearchResult{Considered: res.Considered, Solutions: make([]api.OccupancySolution, 0, len(res.Solutions))}
	for i := range res.Solutions {
		sol := view.Solution(&res.Solutions[i])
		h.priceSolution(r.Context(), r, marinaID, &sol, &res.Solutions[i], in, si, res.Vessel, services)
		out.Solutions = append(out.Solutions, sol)
	}
	respond.JSON(w, http.StatusOK, out)
}

func (h *Handler) priceSolution(ctx context.Context, r *http.Request, marinaID string, out *api.OccupancySolution, sol *occupancy.Solution,
	in api.OccupancySearchRequest, si occupancy.SearchInput, vessel fit.Vessel, services []pricing.ServiceRequest,
) {
	if h.PriceLists == nil || len(sol.Segments) == 0 {
		return
	}
	seg := sol.Segments[0]
	ch := pricing.ChannelDesk
	if si.Channel == "api" {
		ch = pricing.ChannelAPI
	}
	q := pricelist.QuoteRequest{
		ContractType: contractTypeOf(si.Purpose, in.Term), Arrival: si.Arrival, Departure: si.Departure,
		Resource: &pricelist.SimResource{Type: string(seg.ResourceType), ID: seg.ResourceID}, CustomerID: str(in.CustomerId),
		Channel: ch, Services: services,
	}
	if si.VesselID != "" {
		q.VesselID = si.VesselID
	} else {
		q.Vessel = &pricelist.SimVessel{LOA: vessel.LOA, Beam: vessel.Beam, HullType: vessel.Hull}
	}
	bd, resolved, err := h.PriceLists.Quote(ctx, principal(r), marinaID, q)
	if err != nil {
		slug := "no_price"
		var reason *string
		if t, _, ok := respond.MapError(err); ok {
			slug = string(t)
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		out.PricingError = &struct {
			Reason *string `json:"reason,omitempty"`
			Type   string  `json:"type"`
		}{Reason: reason, Type: slug}
		return
	}
	out.Pricing = &api.PriceSummary{PriceListId: resolved.PriceListID, TotalCents: int64(bd.Total), DepositCents: int64(bd.Deposit), EngineVersion: bd.EngineVersion}
}

// CheckPlacement implements api.ServerInterface.
func (h *Handler) CheckPlacement(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.PlacementCheckRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ci := occupancy.CheckInput{
		AllocationID: str(in.AllocationId), VesselID: str(in.VesselId), Target: placementTarget(in.Target),
		From: reqDate(v, "from_night", in.FromNight), To: reqDate(v, "to_night", in.ToNight),
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	res, err := h.Occupancy.Check(r.Context(), principal(r), marinaID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.PlacementCheck(res))
}

// MoveAllocation implements api.ServerInterface.
func (h *Handler) MoveAllocation(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.MoveAllocationParams) {
	var in api.MoveRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	mi := occupancy.MoveInput{
		AllocationID: in.AllocationId, Version: in.Version, Target: placementTarget(in.Target),
		FromNight: parseDate(v, "from_night", in.FromNight), ToNight: parseDate(v, "to_night", in.ToNight),
		FitOverride: overrideReason(in.FitOverride), Reprice: in.Reprice,
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	res, err := h.Occupancy.Move(r.Context(), principal(r), marinaID, mi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MoveResult(res))
}

// SwapAllocations implements api.ServerInterface.
func (h *Handler) SwapAllocations(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.SwapAllocationsParams) {
	var in api.SwapRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	si := occupancy.SwapInput{
		A:         occupancy.BaseVersion{AllocationID: in.A.AllocationId, Version: in.A.Version},
		B:         occupancy.BaseVersion{AllocationID: in.B.AllocationId, Version: in.B.Version},
		FromNight: parseDate(v, "from_night", in.FromNight), FitOverride: overrideReason(in.FitOverride),
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	res, err := h.Occupancy.Swap(r.Context(), principal(r), marinaID, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MoveResult(res))
}

// SaveAreaLayout implements api.ServerInterface.
func (h *Handler) SaveAreaLayout(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, areaID api.AreaId, _ api.SaveAreaLayoutParams) {
	var in api.AreaLayoutSave
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	li := occupancy.LayoutInput{AreaID: areaID, Date: reqDate(v, "date", in.Date), FromNight: parseDate(v, "from_night", in.FromNight), FitOverrides: map[string]string{}}
	for _, b := range in.Base {
		li.Base = append(li.Base, occupancy.BaseVersion{AllocationID: b.AllocationId, Version: b.Version})
	}
	for _, p := range in.Placements {
		pl := engine.Placement{X: geo.Cm(p.XCm)}
		if p.YCm != nil {
			pl.Y = geo.Cm(*p.YCm)
		}
		if p.RotationDd != nil {
			pl.Rotation = geo.Angle(*p.RotationDd)
		}
		li.Placements = append(li.Placements, occupancy.LayoutOp{AllocationID: p.AllocationId, Placement: pl})
	}
	if in.FitOverrides != nil {
		for _, o := range *in.FitOverrides {
			li.FitOverrides[o.AllocationId] = strings.TrimSpace(o.Reason)
		}
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	res, err := h.Occupancy.SaveLayout(r.Context(), principal(r), marinaID, li)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.MoveResult(res))
}

// ProposeCompaction implements api.ServerInterface.
func (h *Handler) ProposeCompaction(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, areaID api.AreaId) {
	var in api.CompactionRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	from, to := reqDate(v, "from", in.From), reqDate(v, "to", in.To)
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	c, err := h.Occupancy.ProposeCompaction(r.Context(), principal(r), marinaID, areaID, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.CompactionProposal{FreedCm: int64(c.FreedCm), Base: make([]api.VersionedAllocation, 0, len(c.Base))}
	out.Placements = make([]struct {
		AllocationId string `json:"allocation_id"` //nolint:revive // generated anonymous struct field
		XCm          api.Cm `json:"x_cm"`
	}, 0, len(c.Placements))
	for _, p := range c.Placements {
		out.Placements = append(out.Placements, struct {
			AllocationId string `json:"allocation_id"` //nolint:revive // generated anonymous struct field
			XCm          api.Cm `json:"x_cm"`
		}{AllocationId: p.AllocationID, XCm: int64(p.X)})
	}
	for _, b := range c.Base {
		out.Base = append(out.Base, api.VersionedAllocation{AllocationId: b.AllocationID, Version: b.Version})
	}
	respond.JSON(w, http.StatusOK, out)
}

// UndoMove implements api.ServerInterface.
func (h *Handler) UndoMove(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, undoID api.UndoId, _ api.UndoMoveParams) {
	as, err := h.Occupancy.Undo(r.Context(), principal(r), marinaID, undoID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"allocations": view.Allocations(as)})
}

// CreateBlock implements api.ServerInterface.
func (h *Handler) CreateBlock(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateBlockParams) {
	var in api.BlockCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	bi := occupancy.BlockInput{Target: placementTarget(in.Target), From: reqDate(v, "from", in.From), To: reqDate(v, "to", in.To), Reason: in.Reason}
	if in.LoaCm != nil {
		bi.LOA = geo.Cm(*in.LoaCm)
	}
	if in.BeamCm != nil {
		bi.Beam = geo.Cm(*in.BeamCm)
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	a, err := h.Occupancy.CreateBlock(r.Context(), principal(r), marinaID, bi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, a.Version)
	respond.JSON(w, http.StatusCreated, view.Allocation(a))
}

// DeleteBlock implements api.ServerInterface.
func (h *Handler) DeleteBlock(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, allocationID api.AllocationId, params api.DeleteBlockParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Occupancy.DeleteBlock(r.Context(), principal(r), marinaID, allocationID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ListReleases implements api.ServerInterface.
func (h *Handler) ListReleases(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListReleasesParams) {
	v := &auth.ValidationError{}
	f := occupancy.ReleaseFilter{
		From: dateOrZero(parseDate(v, "from", params.From)), To: dateOrZero(parseDate(v, "to", params.To)),
		ResourceID: str(params.ResourceId), Status: str(enumPtr(params.Status)),
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	items, next, err := h.Occupancy.ListReleases(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ReleasePage{Items: view.Releases(items), NextCursor: nextCursor(next)})
}

// CreateRelease implements api.ServerInterface.
func (h *Handler) CreateRelease(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateReleaseParams) {
	var in api.ReleaseCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	ri := occupancy.ReleaseInput{
		AllocationID: in.AllocationId, From: reqDate(v, "from_night", in.FromNight), To: reqDate(v, "to_night", in.ToNight),
		Source: string(in.Source), Notes: str(in.Notes),
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	rel, err := h.Occupancy.CreateRelease(r.Context(), principal(r), marinaID, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, rel.Version)
	respond.JSON(w, http.StatusCreated, view.Release(rel))
}

// RevokeRelease implements api.ServerInterface.
func (h *Handler) RevokeRelease(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, releaseID api.ReleaseId, params api.RevokeReleaseParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RevokeRequest
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	v := &auth.ValidationError{}
	from, to := parseDate(v, "from_night", in.FromNight), parseDate(v, "to_night", in.ToNight)
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	res, err := h.Occupancy.RevokeRelease(r.Context(), principal(r), marinaID, releaseID, version, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RevokeResult{Release: view.Release(&res.Release), RestoredNights: res.RestoredNights}
	if res.Remaining != nil {
		rem := view.Release(res.Remaining)
		out.Remaining = &rem
	}
	respond.SetETag(w, res.Release.Version)
	respond.JSON(w, http.StatusOK, out)
}

// ListClosures implements api.ServerInterface.
func (h *Handler) ListClosures(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListClosuresParams) {
	v := &auth.ValidationError{}
	from, to := dateOrZero(parseDate(v, "from", params.From)), dateOrZero(parseDate(v, "to", params.To))
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	cs, err := h.Occupancy.ListClosures(r.Context(), principal(r), marinaID, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.ClosureList{Items: view.Closures(cs)})
}

// CreateClosure implements api.ServerInterface.
func (h *Handler) CreateClosure(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateClosureParams) {
	var in api.ClosureCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v := &auth.ValidationError{}
	c := occupancy.Closure{
		ScopeType: string(in.ScopeType), ScopeID: str(in.ScopeId), ResourceType: str(enumPtr(in.ResourceType)),
		From: reqDate(v, "from_date", in.FromDate), To: reqDate(v, "to_date", in.ToDate), Reason: in.Reason,
	}
	if err := v.Err(); err != nil {
		h.invalid(w, r, marinaID, err)
		return
	}
	out, err := h.Occupancy.CreateClosure(r.Context(), principal(r), marinaID, c)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, out.Version)
	respond.JSON(w, http.StatusCreated, view.Closure(out))
}

// DeleteClosure implements api.ServerInterface.
func (h *Handler) DeleteClosure(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, closureID api.ClosureId, params api.DeleteClosureParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Occupancy.DeleteClosure(r.Context(), principal(r), marinaID, closureID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
