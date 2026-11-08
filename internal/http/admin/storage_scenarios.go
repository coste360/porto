package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/storage"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Storage scenarios: working copies, optimize, compare and the transactional apply (M9, m9-plan §7). Owned by STORAGE.

func stScenario(sc *storage.Scenario, ind *storage.Indicators) api.StorageScenario {
	out := api.StorageScenario{
		AreaId: sc.AreaID, AuthorUserId: sc.AuthorUserID, CreatedAt: sc.CreatedAt, FromOn: stDate(sc.FromOn), Id: sc.ID,
		Name: sc.Name, Seed: sc.Seed, Status: api.StorageScenarioStatus(sc.Status), ToOn: stDate(sc.ToOn),
		UpdatedAt: sc.UpdatedAt, Version: int64(sc.Version), AppliedByUserId: stStrPtr(sc.AppliedByUserID),
	}
	if !sc.AppliedAt.IsZero() {
		t := sc.AppliedAt
		out.AppliedAt = &t
	}
	if ind != nil {
		i := stIndicators(ind)
		out.Indicators, out.Stale = &i, ind.Stale
	}
	return out
}

func stScenarioDetail(d *storage.ScenarioDetail) api.StorageScenarioDetail {
	out := api.StorageScenarioDetail{
		Access: stAccess(d.Access), Issues: stIssues(d.Issues), Items: []api.StorageScenarioItem{}, Moves: []api.StorageScenarioMove{},
		Scenario: stScenario(&d.Scenario, d.Indicators),
	}
	for _, it := range d.Items {
		si := api.StorageScenarioItem{
			AllocationId: it.AllocationID, BaseVersion: int64(it.BaseVersion), Origin: api.StorageScenarioItemOrigin(it.Origin),
			Pinned: it.Pinned, XCm: stCmPtr(it.XCm), YCm: stCmPtr(it.YCm), RotationDd: stRotPtr(it.RotationDd),
		}
		if sn := it.Snapshot; sn != nil {
			cv := int64(it.CurrentVersion)
			a := &sn.Allocation
			si.ArrivalOn, si.ExitOn, si.FirstNight, si.LastNight = stDate(sn.ArrivalOn), stDate(sn.ExitOn), stDate(a.FirstNight), stDate(a.LastNight)
			si.Contract, si.CurrentVersion, si.Kind = stContractBrief(sn), &cv, api.AllocationKind(a.Kind)
			si.Support, si.Vessel = stSupport(sn.Support), stVessel(&sn.Vessel)
		}
		out.Items = append(out.Items, si)
	}
	for _, m := range d.Moves {
		out.Moves = append(out.Moves, api.StorageScenarioMove{
			AllocationId: m.AllocationID, From: stMoveEnd(m.FromResourceType, m.FromResourceID, m.FromXCm, m.FromYCm, m.FromRotationDd),
			Kind: api.StorageScenarioMoveKind(m.Kind), MoveOn: stDate(m.MoveOn), Seq: m.Seq,
			To: *stMoveEnd(m.ToResourceType, m.ToResourceID, m.ToXCm, m.ToYCm, m.ToRotationDd), VesselId: stStrPtr(m.VesselID),
		})
	}
	if d.Run != nil {
		out.Run = &struct {
			Iterations int                                   `json:"iterations"`
			Seed       int64                                 `json:"seed"`
			StoppedBy  api.StorageScenarioDetailRunStoppedBy `json:"stopped_by"`
		}{Iterations: d.Run.Iterations, Seed: int64(d.Run.Seed), StoppedBy: api.StorageScenarioDetailRunStoppedBy(d.Run.StoppedBy)} //nolint:gosec // bit pattern
	}
	return out
}

func writeScenario(w http.ResponseWriter, d *storage.ScenarioDetail, status int) {
	respond.SetETag(w, d.Scenario.Version)
	respond.JSON(w, status, stScenarioDetail(d))
}

func stEnd(e *storage.MoveEnd) *api.StorageMoveEnd {
	if e == nil {
		return nil
	}
	out := &api.StorageMoveEnd{ResourceType: api.ResourceType(e.ResourceType), ResourceId: e.ResourceID}
	if e.At != nil {
		x, y, r := int64(e.At.X), int64(e.At.Y), api.SurfaceRotationDd(e.At.Rotation)
		out.XCm, out.YCm, out.RotationDd = &x, &y, &r
	}
	return out
}

// ListStorageScenarios implements api.ServerInterface.
func (h *Handler) ListStorageScenarios(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListStorageScenariosParams) {
	f := storage.ScenarioFilter{AreaID: str(params.AreaId), Status: str(enumPtr(params.Status)), Cursor: str(params.Cursor)}
	if params.Limit != nil {
		f.Limit = *params.Limit
	}
	list, next, err := h.Storage.ListScenarios(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.StorageScenarioPage{Items: make([]api.StorageScenario, 0, len(list)), NextCursor: stStrPtr(next)}
	for i := range list {
		out.Items = append(out.Items, stScenario(&list[i].Scenario, list[i].Indicators))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateStorageScenario implements api.ServerInterface.
func (h *Handler) CreateStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateStorageScenarioParams) {
	var in api.StorageScenarioCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	si := storage.ScenarioInput{AreaID: in.AreaId, Name: in.Name, From: d.parse(in.FromOn), To: d.parse(in.ToOn)}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	res, err := h.Storage.CreateScenario(r.Context(), principal(r), marinaID, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusCreated)
}

// GetStorageScenario implements api.ServerInterface.
func (h *Handler) GetStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId) {
	res, err := h.Storage.GetScenario(r.Context(), principal(r), marinaID, scenarioID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusOK)
}

// UpdateStorageScenario implements api.ServerInterface.
func (h *Handler) UpdateStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.UpdateStorageScenarioParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.StorageScenarioPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	patch := storage.ScenarioPatch{Name: in.Name, From: d.ptr(in.FromOn), To: d.ptr(in.ToOn)}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	res, err := h.Storage.UpdateScenario(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusOK)
}

// DiscardStorageScenario implements api.ServerInterface.
func (h *Handler) DiscardStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.DiscardStorageScenarioParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Storage.DiscardScenario(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch)); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SaveStorageScenarioItems implements api.ServerInterface.
func (h *Handler) SaveStorageScenarioItems(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.SaveStorageScenarioItemsParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.StorageScenarioItemsPut
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	items, ok := stLayoutIn(in.Items)
	if !ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	res, err := h.Storage.SaveItems(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch), items)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusOK)
}

// AddStorageScenarioItem implements api.ServerInterface.
func (h *Handler) AddStorageScenarioItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.AddStorageScenarioItemParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.StorageScenarioItemAdd
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Storage.AddTrayItem(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch), in.AllocationId)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusOK)
}

// RemoveStorageScenarioItem implements api.ServerInterface.
func (h *Handler) RemoveStorageScenarioItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, allocationID api.AllocationId, params api.RemoveStorageScenarioItemParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	res, err := h.Storage.RemoveTrayItem(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch), allocationID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusOK)
}

// OptimizeStorageScenario implements api.ServerInterface.
func (h *Handler) OptimizeStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.OptimizeStorageScenarioParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in *api.StorageOptimizeRequest
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	var seed *uint64
	maxIter := 0
	if in != nil {
		if in.Seed != nil {
			sd := uint64(*in.Seed) //nolint:gosec // the seed travels as its int64 bit pattern
			seed = &sd
		}
		if in.MaxIterations != nil {
			maxIter = *in.MaxIterations
		}
	}
	res, err := h.Storage.Optimize(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch), seed, maxIter)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeScenario(w, res, http.StatusOK)
}

// CompareStorageScenario implements api.ServerInterface.
func (h *Handler) CompareStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.CompareStorageScenarioParams) {
	d := newCtrDates()
	date := d.ptr(params.Date)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, err := h.Storage.Compare(r.Context(), principal(r), marinaID, scenarioID, str(params.With), date)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.StorageScenarioComparison{
		Date: stDate(c.Date), Left: stIndicators(&c.Left), Right: stIndicators(&c.Right), ScenarioId: c.ScenarioID, With: c.With,
	}
	out.Diffs = make([]struct {
		AllocationId string              `json:"allocation_id"` //nolint:revive // generated field name
		Left         *api.StorageMoveEnd `json:"left,omitempty"`
		Right        *api.StorageMoveEnd `json:"right,omitempty"`
		VesselId     *string             `json:"vessel_id,omitempty"` //nolint:revive // generated field name
	}, len(c.Diffs))
	for i, df := range c.Diffs {
		out.Diffs[i].AllocationId, out.Diffs[i].Left, out.Diffs[i].Right, out.Diffs[i].VesselId = df.AllocationID, stEnd(df.Left), stEnd(df.Right), stStrPtr(df.VesselID)
	}
	respond.JSON(w, http.StatusOK, out)
}

// ListStorageTrayCandidates implements api.ServerInterface.
func (h *Handler) ListStorageTrayCandidates(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId) {
	list, err := h.Storage.TrayCandidates(r.Context(), principal(r), marinaID, scenarioID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.StorageTrayCandidateList{Items: make([]api.StorageTrayCandidate, 0, len(list))}
	for i := range list {
		c := &list[i]
		out.Items = append(out.Items, api.StorageTrayCandidate{
			AllocationId: c.Allocation.ID, Vessel: stVessel(c.Vessel), ResourceType: api.ResourceType(c.Allocation.ResourceType),
			ResourceId: c.Allocation.ResourceID, ResourceCode: c.ResourceCode, FirstNight: stDate(c.Allocation.FirstNight),
			LastNight: stDate(c.Allocation.LastNight),
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

// ApplyStorageScenario implements api.ServerInterface.
func (h *Handler) ApplyStorageScenario(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, scenarioID api.ScenarioId, params api.ApplyStorageScenarioParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	res, err := h.Storage.Apply(r.Context(), principal(r), marinaID, scenarioID, int64(ifMatch))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Scenario.Version)
	respond.JSON(w, http.StatusOK, api.StorageApplyResult{Movements: stMovements(res.Movements), Scenario: stScenario(&res.Scenario, nil), PriceEffect: view.PriceEffect(res.PriceEffect)})
}
