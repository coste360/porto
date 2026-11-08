package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/optimizer"
	"github.com/coste360/porto/internal/domain/storage"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Storage areas, plan, accessibility, preset, storage contracts and movements (M9, m9-plan §3–§7). Owned by STORAGE.

// ---- views ----

func stDate(d calendar.Date) api.IsoDate { return d.String() }

func stDatePtr(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func stCmPtr(v *int64) *api.Cm {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

func stRotPtr(v *int64) *api.SurfaceRotationDd {
	if v == nil {
		return nil
	}
	r := api.SurfaceRotationDd(*v)
	return &r
}

func stStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stFitCodes(codes []string) *[]api.FitCode {
	if len(codes) == 0 {
		return nil
	}
	out := make([]api.FitCode, len(codes))
	for i, c := range codes {
		out[i] = api.FitCode(c)
	}
	return &out
}

func stSupport(s string) *api.StorageSupport {
	if s == "" {
		return nil
	}
	v := api.StorageSupport(s)
	return &v
}

func stVessel(v *storage.Vessel) *api.VesselBrief {
	if v == nil || v.ID == "" {
		return nil
	}
	b := &api.VesselBrief{Id: v.ID, Name: v.Name, LoaCm: v.LOACm, BeamCm: v.BeamCm, HullType: api.HullType(v.HullType)}
	if v.DraftCm > 0 {
		d := v.DraftCm
		b.DraftCm = &d
	}
	return b
}

func stContractBrief(it *storage.SnapshotItem) *api.ContractBrief {
	if it.ContractID == "" {
		return nil
	}
	b := &api.ContractBrief{Id: it.ContractID, Kind: api.ContractKind(it.ContractKind), Status: api.ContractStatus(it.ContractStatus), Code: it.ContractCode}
	if it.ContractTerm != "" {
		t := api.ContractTerm(it.ContractTerm)
		b.Term = &t
	}
	return b
}

func stIndicators(in *storage.Indicators) api.StorageIndicators {
	out := api.StorageIndicators{
		ArrivalsBlocked: in.ArrivalsBlocked, ExitsBlocked: in.ExitsBlocked, LargestFreeRectCm2: in.LargestFreeRectCm2,
		Moves: in.Moves, Overlaps: in.Overlaps, Unplaced: in.Unplaced, UsableCm2: in.UsableCm2,
		UsedFootprintCm2: in.UsedFootprintCm2, UsedHullCm2: in.UsedHullCm2,
	}
	out.BlockedByDate = make([]struct {
		ArrivalsBlocked *int        `json:"arrivals_blocked,omitempty"`
		Date            api.IsoDate `json:"date"`
		ExitsBlocked    int         `json:"exits_blocked"`
	}, len(in.BlockedByDate))
	for i, d := range in.BlockedByDate {
		a := d.ArrivalsBlocked
		out.BlockedByDate[i].ArrivalsBlocked, out.BlockedByDate[i].Date, out.BlockedByDate[i].ExitsBlocked = &a, stDate(d.Date), d.ExitsBlocked
	}
	return out
}

func stAccess(list []storage.Access) []api.StorageAccessEntry {
	out := make([]api.StorageAccessEntry, 0, len(list))
	for _, a := range list {
		e := api.StorageAccessEntry{
			Accessible: a.Accessible, AllocationId: a.ID, Blocking: a.Blocking, Event: api.StorageAccessEntryEvent(a.Event),
			On: stDate(a.On), VesselId: stStrPtr(a.VesselID),
		}
		if e.Blocking == nil {
			e.Blocking = []string{}
		}
		if a.Order > 0 {
			o := a.Order
			e.Order = &o
		}
		out = append(out, e)
	}
	return out
}

func stIssues(list []storage.ItemIssue) []api.StorageItemIssue {
	out := make([]api.StorageItemIssue, 0, len(list))
	for _, is := range list {
		i := api.StorageItemIssue{AllocationId: is.AllocationID, Reason: api.StorageItemIssueReason(is.Reason), FitCodes: stFitCodes(is.FitCodes)}
		if len(is.WithAllocationID) > 0 {
			w := is.WithAllocationID
			i.WithAllocationIds = &w
		}
		out = append(out, i)
	}
	return out
}

func stLayoutItems(list []optimizer.ItemResult) []api.StorageLayoutItem {
	out := make([]api.StorageLayoutItem, 0, len(list))
	for _, it := range list {
		li := api.StorageLayoutItem{AllocationId: it.ID, FitCodes: stFitCodes(it.FitCodes)}
		if it.At != nil {
			x, y, r := int64(it.At.X), int64(it.At.Y), api.SurfaceRotationDd(it.At.Rotation)
			li.XCm, li.YCm, li.RotationDd = &x, &y, &r
		} else if it.UnplacedReason != "" {
			u := api.StorageLayoutItemUnplacedReason(it.UnplacedReason)
			li.UnplacedReason = &u
		}
		out = append(out, li)
	}
	return out
}

func stArea(s *storage.AreaSummary) api.StorageArea {
	a := &s.Area
	return api.StorageArea{
		BoatsToday: s.BoatsToday, CellCm: int64(s.CellCm), Code: a.Code, Cols: s.Cols, DraftScenarios: s.DraftScenarios,
		ExitsBlockedNext30: s.ExitsBlockedNext30, GatesValid: s.GatesValid, Id: a.ID, MapId: a.MapID, MapKind: api.MapKind(s.MapKind),
		OccupiedCellsToday: s.OccupiedCellsToday, PriceClass: stStrPtr(a.PriceClass), Rows: s.Rows, UsableCells: s.UsableCells,
		UsableCm2: s.UsableCm2,
	}
}

func stPlacement(a *storage.Allocation) *api.StoragePlacement {
	if a == nil {
		return nil
	}
	return &api.StoragePlacement{
		AllocationId: a.ID, FirstNight: stDate(a.FirstNight), LastNight: stDate(a.LastNight), MapId: stStrPtr(a.MapID),
		ResourceId: a.ResourceID, ResourceType: api.ResourceType(a.ResourceType), RotationDd: stRotPtr(a.RotationDd),
		XCm: stCmPtr(a.XCm), YCm: stCmPtr(a.YCm),
	}
}

func stContract(v *storage.ContractView) api.StorageContract {
	d := &v.Details
	out := api.StorageContract{
		Contract: view.ContractM4(v.Contract), Placement: stPlacement(v.Placement), Cradle: stPlacement(v.Cradle),
		Details: api.StorageContractDetails{
			ArrivalOn: stDate(v.ArrivalOn), ContractId: d.ContractID, ExitOn: stDate(v.ExitOn), HaulOutOn: stDatePtr(d.HaulOutOn),
			LaunchOn: stDatePtr(d.LaunchOn), NotesHandling: stStrPtr(d.NotesHandling), Support: api.StorageSupport(d.Support),
			SupportOwner: api.StorageSupportOwner(d.SupportOwner), UpdatedAt: d.UpdatedAt, Version: int64(d.Version),
		},
	}
	if v.Fit != nil {
		f := view.FitResult(*v.Fit)
		out.Fit = &f
	}
	return out
}

func writeStorageContract(w http.ResponseWriter, v *storage.ContractView, status int) {
	respond.SetETag(w, v.Details.Version)
	respond.JSON(w, status, stContract(v))
}

func stMoveEnd(rt, rid string, x, y, rot *int64) *api.StorageMoveEnd {
	if rt == "" {
		return nil
	}
	return &api.StorageMoveEnd{ResourceType: api.ResourceType(rt), ResourceId: rid, XCm: stCmPtr(x), YCm: stCmPtr(y), RotationDd: stRotPtr(rot)}
}

func stMovement(m *storage.MovementView) api.StorageMovement {
	out := api.StorageMovement{
		AllocationId: m.AllocationID, AreaId: m.AreaID, EquipmentHint: api.StorageEquipmentHint(m.EquipmentHint),
		From: stMoveEnd(m.FromResourceType, m.FromResourceID, m.FromXCm, m.FromYCm, m.FromRotationDd), Id: m.ID,
		MoveOn: stDate(m.MoveOn), ScenarioId: stStrPtr(m.ScenarioID), Seq: m.Seq, Status: api.StorageMovementStatus(m.Status),
		Temporary: m.TemporaryPosition, Version: int64(m.Version), Vessel: stVessel(m.Vessel),
		DoneByUserId: stStrPtr(m.DoneByUserID),
	}
	out.To = *stMoveEnd(m.ToResourceType, m.ToResourceID, m.ToXCm, m.ToYCm, m.ToRotationDd)
	if !m.DoneAt.IsZero() {
		t := m.DoneAt
		out.DoneAt = &t
	}
	return out
}

func stMovements(list []storage.MovementView) []api.StorageMovement {
	out := make([]api.StorageMovement, 0, len(list))
	for i := range list {
		out = append(out, stMovement(&list[i]))
	}
	return out
}

// stLayoutIn converts layout item inputs (a position needs x, y and rotation).
func stLayoutIn(in []api.StorageLayoutItemInput) ([]storage.LayoutItem, bool) {
	out := make([]storage.LayoutItem, 0, len(in))
	for _, it := range in {
		li := storage.LayoutItem{AllocationID: it.AllocationId, Pinned: it.Pinned != nil && *it.Pinned}
		switch {
		case it.XCm != nil && it.YCm != nil:
			p := optimizer.Placement{X: geo.Cm(*it.XCm), Y: geo.Cm(*it.YCm)}
			if it.RotationDd != nil {
				p.Rotation = geo.Angle(*it.RotationDd)
			}
			li.At = &p
		case it.XCm != nil || it.YCm != nil:
			return nil, false
		}
		out = append(out, li)
	}
	return out, true
}

// ---- handlers ----

// ListStorageAreas implements api.ServerInterface.
func (h *Handler) ListStorageAreas(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	list, err := h.Storage.ListAreas(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.StorageAreaList{Items: make([]api.StorageArea, 0, len(list))}
	for i := range list {
		out.Items = append(out.Items, stArea(&list[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetStoragePlan implements api.ServerInterface.
func (h *Handler) GetStoragePlan(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, areaID api.AreaId, params api.GetStoragePlanParams) {
	d := newCtrDates()
	from, to := d.ptr(params.From), d.ptr(params.To)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	pv, err := h.Storage.Plan(r.Context(), principal(r), marinaID, areaID, from, to)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	sn := pv.Snapshot
	out := api.StoragePlan{
		Access: stAccess(pv.Access), Area: stArea(&pv.Summary), From: stDate(sn.From), To: stDate(sn.To),
		Gates: []api.StorageGate{}, Items: []api.StoragePlanItem{}, Obstacles: []api.StorageObstacle{},
	}
	issues := map[int]string{}
	for _, g := range sn.Gates {
		issues[g.Gate] = g.Reason
	}
	for i, cells := range sn.Grid.Gates {
		g := api.StorageGate{Index: i, Cells: cells, Valid: true}
		if reason, ok := issues[i]; ok {
			is := api.StorageGateIssue(reason)
			g.Issue, g.Valid = &is, false
		}
		out.Gates = append(out.Gates, g)
	}
	for i := range sn.Items {
		it := &sn.Items[i]
		if !it.OnArea {
			continue
		}
		a := &it.Allocation
		pi := api.StoragePlanItem{
			AllocationId: a.ID, ArrivalOn: stDate(it.ArrivalOn), Cells: it.Cells, Contract: stContractBrief(it),
			DisplacementKg: it.Displacement, ExitOn: stDate(it.ExitOn), FirstNight: stDate(a.FirstNight),
			Kind: api.AllocationKind(a.Kind), LastNight: stDate(a.LastNight), Support: stSupport(it.Support),
			Version: int64(a.Version), Vessel: stVessel(&it.Vessel),
		}
		if pi.Cells == nil {
			pi.Cells = []int{}
		}
		if a.XCm != nil {
			pi.XCm = *a.XCm
		}
		if a.YCm != nil {
			pi.YCm = *a.YCm
		}
		if a.RotationDd != nil {
			pi.RotationDd = api.SurfaceRotationDd(*a.RotationDd)
		}
		out.Items = append(out.Items, pi)
	}
	for _, o := range sn.Obstacles {
		cells := o.Cells
		if cells == nil {
			cells = []int{}
		}
		out.Obstacles = append(out.Obstacles, api.StorageObstacle{
			AllocationId: o.ID, Cells: cells, FirstNight: stDate(o.First), LastNight: stDate(o.Last), Kind: api.AllocationKind(o.Kind),
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetStorageAccessibility implements api.ServerInterface.
func (h *Handler) GetStorageAccessibility(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, areaID api.AreaId, params api.GetStorageAccessibilityParams) {
	d := newCtrDates()
	date := d.ptr(params.Date)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	v, err := h.Storage.Accessibility(r.Context(), principal(r), marinaID, areaID, date)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.StorageAccessibility{
		AreaId: v.AreaID, Date: stDate(v.Date), Entries: stAccess(v.Entries), ExtractionOrder: v.ExtractionOrder,
	})
}

// ArrangeStorageArea implements api.ServerInterface.
func (h *Handler) ArrangeStorageArea(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, areaID api.AreaId) {
	var in api.StorageArrangeRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ai := storage.ArrangeInput{From: d.ptr(in.FromOn), To: d.ptr(in.ToOn)}
	if in.Items != nil {
		items, ok := stLayoutIn(*in.Items)
		if !ok {
			d.ok = false
		}
		ai.Draft = items
	}
	if in.Seed != nil {
		sd := uint64(*in.Seed) //nolint:gosec // the seed travels as its int64 bit pattern
		ai.Seed = &sd
	}
	if in.MaxIterations != nil {
		ai.MaxIter = *in.MaxIterations
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	res, err := h.Storage.Arrange(r.Context(), principal(r), marinaID, areaID, ai)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.StorageArrangeResult{
		Access: stAccess(res.Access), Indicators: stIndicators(&res.Indicators), Items: stLayoutItems(res.Items),
		Iterations: res.Iterations, Seed: int64(res.Seed), StoppedBy: api.StorageArrangeResultStoppedBy(res.StoppedBy), //nolint:gosec // bit pattern
	})
}

// EvaluateStorageLayout implements api.ServerInterface.
func (h *Handler) EvaluateStorageLayout(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, areaID api.AreaId) {
	var in api.StorageEvaluateRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	from, to := d.ptr(in.FromOn), d.ptr(in.ToOn)
	items, ok := stLayoutIn(in.Items)
	if !ok || !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	ev, err := h.Storage.Evaluate(r.Context(), principal(r), marinaID, areaID, from, to, items)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.StorageEvaluation{
		Access: stAccess(ev.Access), Indicators: stIndicators(&ev.Indicators), Issues: stIssues(ev.Issues),
	})
}

// CreateStorageContract implements api.ServerInterface.
func (h *Handler) CreateStorageContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateStorageContractParams) {
	var in api.StorageContractCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ci := storage.ContractInput{
		Term: string(in.Term), CustomerID: in.CustomerId, BillingCustomerID: str(in.BillingCustomerId), VesselID: in.VesselId,
		StartsOn: d.parse(in.StartsOn), EndsOn: d.ptr(in.EndsOn), AreaID: str(in.AreaId), TrailerSpotID: str(in.TrailerSpotId),
		CradleResourceID: str(in.CradleResourceId), Services: ctrServices(in.Services), Policy: ctrPolicyIn(in.Policy),
		Notes: str(in.Notes), FitOverride: ctrFit(in.FitOverride),
		Details: contract.StorageDetailsInput{
			Support: string(in.Support), HaulOutOn: d.ptr(in.HaulOutOn), LaunchOn: d.ptr(in.LaunchOn), NotesHandling: str(in.NotesHandling),
		},
	}
	if in.SupportOwner != nil {
		ci.Details.SupportOwner = string(*in.SupportOwner)
	}
	if in.Activate != nil {
		ci.Activate = *in.Activate
	}
	if in.Placement != nil {
		ci.Placement = &optimizer.Placement{X: geo.Cm(in.Placement.XCm), Y: geo.Cm(in.Placement.YCm), Rotation: geo.Angle(in.Placement.RotationDd)}
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	v, err := h.Storage.CreateContract(r.Context(), principal(r), marinaID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeStorageContract(w, v, http.StatusCreated)
}

// GetStorageContract implements api.ServerInterface.
func (h *Handler) GetStorageContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId) {
	v, err := h.Storage.GetContract(r.Context(), principal(r), marinaID, contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeStorageContract(w, v, http.StatusOK)
}

// UpdateStorageContract implements api.ServerInterface.
func (h *Handler) UpdateStorageContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.UpdateStorageContractParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.StorageContractPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	patch := contract.StorageDetailsPatch{NotesHandling: in.NotesHandling}
	if in.Support != nil {
		s := string(*in.Support)
		patch.Support = &s
	}
	if in.SupportOwner != nil {
		s := string(*in.SupportOwner)
		patch.SupportOwner = &s
	}
	// An empty string clears a planned date.
	if in.HaulOutOn != nil {
		if *in.HaulOutOn == "" {
			patch.ClearHaulOut = true
		} else {
			patch.HaulOutOn = d.ptr(in.HaulOutOn)
		}
	}
	if in.LaunchOn != nil {
		if *in.LaunchOn == "" {
			patch.ClearLaunch = true
		} else {
			patch.LaunchOn = d.ptr(in.LaunchOn)
		}
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	v, err := h.Storage.UpdateContract(r.Context(), principal(r), marinaID, contractID, int64(ifMatch), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeStorageContract(w, v, http.StatusOK)
}

// ListStorageMovements implements api.ServerInterface.
func (h *Handler) ListStorageMovements(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListStorageMovementsParams) {
	d := newCtrDates()
	f := storage.MovementFilter{Date: d.ptr(params.Date), AreaID: str(params.AreaId), Status: str(enumPtr(params.Status))}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	list, err := h.Storage.ListMovements(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.StorageMovementList{Date: stDate(list.Date), Items: stMovements(list.Items), Warnings: stAccess(list.Warnings)})
}

// CompleteStorageMovement implements api.ServerInterface.
func (h *Handler) CompleteStorageMovement(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, movementID api.MovementId, params api.CompleteStorageMovementParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	m, err := h.Storage.CompleteMovement(r.Context(), principal(r), marinaID, movementID, int64(ifMatch))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, m.Version)
	respond.JSON(w, http.StatusOK, stMovement(m))
}
