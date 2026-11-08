package admin

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/coste360/porto/internal/domain/utility"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Utility meters, readings, consumptions, charging and settings (M9, m9-plan §9). Owned by UTILITIES.

func utlMeter(v *utility.MeterView) api.UtilityMeter {
	m := v.Row
	out := api.UtilityMeter{
		Id: m.ID, Code: m.Code, Kind: api.MeterKind(m.Kind), Unit: api.UtilityUnit(m.Unit), PedestalId: view.String(m.PedestalID),
		SocketCode: view.String(m.SocketCode), Serial: view.String(m.Serial), ResourceId: view.String(m.ResourceID),
		EquipmentId: view.String(m.EquipmentID), ItemId: view.String(m.ItemID), MultiplierMilli: m.MultiplierMilli,
		RolloverMilli: m.RolloverMilli, DailyThresholdMilli: m.DailyThresholdMilli, Active: m.Active, Version: int64(m.Version),
		CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
	if m.ResourceType != "" {
		rt := api.ResourceType(m.ResourceType)
		out.ResourceType = &rt
	}
	if v.Last != nil {
		out.LastReading = &api.MeterReadingBrief{
			Id: v.Last.ID, ReadAt: v.Last.ReadAt.UTC(), ReadOn: v.Last.ReadOn.String(), ValueMilli: v.Last.ValueMilli,
		}
	}
	return out
}

func writeMeter(w http.ResponseWriter, v *utility.MeterView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, utlMeter(v))
}

func utlReading(v *utility.ReadingView) api.MeterReading {
	r := v.Row
	codes := utility.AnomalyCodesOf(&r)
	ac := make([]api.MeterAnomalyCode, 0, len(codes))
	for _, c := range codes {
		ac = append(ac, api.MeterAnomalyCode(c))
	}
	out := api.MeterReading{
		Id: r.ID, MeterId: r.MeterID, ReadAt: r.ReadAt.UTC(), ReadOn: r.ReadOn.String(), ValueMilli: r.ValueMilli,
		Kind: api.MeterReadingKind(r.Kind), Source: api.MeterReadingSource(r.Source), PhotoFileId: view.String(r.PhotoFileID),
		UserId: view.String(r.UserID), Note: view.String(r.Note), Status: api.MeterReadingStatus(r.Status), AnomalyCodes: ac,
		ClientRef: view.String(r.ClientRef), Version: int64(r.Version), CreatedAt: r.CreatedAt.UTC(),
	}
	if v.Delta != nil {
		d := int64(*v.Delta)
		out.DeltaMilli = &d
	}
	return out
}

func writeReading(w http.ResponseWriter, v *utility.ReadingView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, utlReading(v))
}

func utlConsumption(v *utility.ConsumptionView) api.UtilityConsumption {
	c := v.Row
	out := api.UtilityConsumption{
		Id: c.ID, MeterId: c.MeterID, MeterCode: view.String(v.MeterCode), Kind: api.MeterKind(v.Kind), FromReadingId: c.FromReadingID,
		ToReadingId: c.ToReadingID, AllocationId: view.String(c.AllocationID), CustomerId: view.String(c.CustomerID),
		ResourceId: view.String(c.ResourceID), FirstNight: c.FirstNight.String(), LastNight: c.LastNight.String(), QtyMilli: c.QtyMilli,
		Status: api.UtilityConsumptionStatus(c.Status), ChargeId: view.String(c.ChargeID), BillingId: view.String(c.BillingID),
		ExcludeReason: view.String(c.ExcludeReason), Version: int64(c.Version), Contract: view.ContractBrief(v.Contract),
	}
	if c.ResourceType != "" {
		rt := api.ResourceType(c.ResourceType)
		out.ResourceType = &rt
	}
	return out
}

func writeConsumption(w http.ResponseWriter, v *utility.ConsumptionView) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, http.StatusOK, utlConsumption(v))
}

// ListUtilityMeters implements api.ServerInterface.
func (h *Handler) ListUtilityMeters(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListUtilityMetersParams) {
	f := utility.MeterFilter{
		PedestalID: str(params.PedestalId), ResourceID: str(params.ResourceId), Active: params.Active, Q: str(params.Q),
		Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if params.Kind != nil {
		f.Kind = string(*params.Kind)
	}
	if params.Route != nil {
		f.Route = string(*params.Route)
	}
	items, next, err := h.Utilities.ListMeters(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.UtilityMeter, 0, len(items))
	for i := range items {
		out = append(out, utlMeter(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.UtilityMeterPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateUtilityMeter implements api.ServerInterface.
func (h *Handler) CreateUtilityMeter(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateUtilityMeterParams) {
	var in api.UtilityMeterInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	mi := utility.MeterInput{
		Code: in.Code, Kind: string(in.Kind), Unit: string(in.Unit), PedestalID: str(in.PedestalId), SocketCode: str(in.SocketCode),
		Serial: str(in.Serial), ResourceID: str(in.ResourceId), EquipmentID: str(in.EquipmentId), ItemID: str(in.ItemId),
		RolloverMilli: in.RolloverMilli, DailyThresholdMilli: in.DailyThresholdMilli, Active: in.Active,
	}
	if in.ResourceType != nil {
		mi.ResourceType = string(*in.ResourceType)
	}
	if in.MultiplierMilli != nil {
		mi.MultiplierMilli = *in.MultiplierMilli
		if mi.MultiplierMilli == 0 {
			mi.MultiplierMilli = -1 // an explicit 0 is invalid, not the default
		}
	}
	v, err := h.Utilities.CreateMeter(r.Context(), principal(r), marinaID, mi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeMeter(w, v, http.StatusCreated)
}

// GetUtilityMeter implements api.ServerInterface.
func (h *Handler) GetUtilityMeter(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MeterId) {
	v, err := h.Utilities.GetMeter(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeMeter(w, v, http.StatusOK)
}

// UpdateUtilityMeter implements api.ServerInterface.
func (h *Handler) UpdateUtilityMeter(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.MeterId, params api.UpdateUtilityMeterParams) {
	var in api.UtilityMeterPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	set := map[string]any{}
	strs := map[string]*string{
		utility.PatchCode: in.Code, utility.PatchPedestalID: in.PedestalId, utility.PatchSocketCode: in.SocketCode,
		utility.PatchSerial: in.Serial, utility.PatchResourceType: in.ResourceType, utility.PatchResourceID: in.ResourceId,
		utility.PatchEquipmentID: in.EquipmentId, utility.PatchItemID: in.ItemId,
	}
	for k, v := range strs {
		if _, present := fields[k]; present {
			set[k] = v
		}
	}
	ints := map[string]*int64{
		utility.PatchMultiplierMilli: in.MultiplierMilli, utility.PatchRolloverMilli: in.RolloverMilli,
		utility.PatchDailyThresholdMilli: in.DailyThresholdMilli,
	}
	for k, v := range ints {
		if _, present := fields[k]; present {
			set[k] = v
		}
	}
	if in.Active != nil {
		set[utility.PatchActive] = *in.Active
	}
	v, err := h.Utilities.UpdateMeter(r.Context(), principal(r), marinaID, id, int64(version), utility.MeterPatch{Set: set})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeMeter(w, v, http.StatusOK)
}

// ListMeterReadings implements api.ServerInterface.
func (h *Handler) ListMeterReadings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListMeterReadingsParams) {
	d := newCtrDates()
	f := utility.ReadingFilter{
		MeterID: str(params.MeterId), From: d.ptr(params.From), To: d.ptr(params.To), Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	items, next, err := h.Utilities.ListReadings(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.MeterReading, 0, len(items))
	for i := range items {
		out = append(out, utlReading(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.MeterReadingPage{Items: out, NextCursor: nextCursor(next)})
}

// RecordMeterReading implements api.ServerInterface (offline kind utility.reading).
func (h *Handler) RecordMeterReading(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.RecordMeterReadingParams) {
	var in api.MeterReadingCreate
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	ri := utility.ReadingInput{
		MeterID: in.MeterId, Value: in.Value, Note: str(in.Note), ReadAt: in.ReadAt, ClientRef: str(in.ClientRef),
	}
	if in.Kind != nil {
		ri.Kind = string(*in.Kind)
	}
	if params.PortoOfflineQueuedAt != nil {
		ri.QueuedAt = *params.PortoOfflineQueuedAt
	}
	v, err := h.Utilities.RecordReading(r.Context(), principal(r), marinaID, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeReading(w, v, http.StatusCreated)
}

// UploadMeterReadingPhoto implements api.ServerInterface (raw body: JPEG or PNG).
func (h *Handler) UploadMeterReadingPhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReadingId, _ api.UploadMeterReadingPhotoParams) {
	data, err := io.ReadAll(io.LimitReader(r.Body, utility.MaxPhotoBytes+1))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fileID, err := h.Utilities.UploadPhoto(r.Context(), principal(r), marinaID, id, utility.PhotoUpload{
		ContentType: r.Header.Get("Content-Type"), Data: data,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.MeterReadingPhoto{FileId: fileID})
}

// GetMeterReadingPhotoLink implements api.ServerInterface.
func (h *Handler) GetMeterReadingPhotoLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReadingId) {
	l, err := h.Utilities.PhotoLink(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.FileLink{Url: l.URL, ExpiresAt: l.ExpiresAt.UTC()})
}

// ConfirmMeterReading implements api.ServerInterface.
func (h *Handler) ConfirmMeterReading(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReadingId, params api.ConfirmMeterReadingParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Utilities.ConfirmReading(r.Context(), principal(r), marinaID, id, int64(version))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeReading(w, v, http.StatusOK)
}

// VoidMeterReading implements api.ServerInterface.
func (h *Handler) VoidMeterReading(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ReadingId, params api.VoidMeterReadingParams) {
	var in api.MeterReadingVoidInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Utilities.VoidReading(r.Context(), principal(r), marinaID, id, int64(version), in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeReading(w, v, http.StatusOK)
}

// ListUtilityConsumptions implements api.ServerInterface.
func (h *Handler) ListUtilityConsumptions(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListUtilityConsumptionsParams) {
	d := newCtrDates()
	f := utility.ConsumptionFilter{
		ContractID: str(params.ContractId), MeterID: str(params.MeterId), From: d.ptr(params.From), To: d.ptr(params.To),
		Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.Kind != nil {
		f.Kind = string(*params.Kind)
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	items, next, err := h.Utilities.ListConsumptions(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.UtilityConsumption, 0, len(items))
	for i := range items {
		out = append(out, utlConsumption(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.UtilityConsumptionPage{Items: out, NextCursor: nextCursor(next)})
}

// ExcludeUtilityConsumption implements api.ServerInterface.
func (h *Handler) ExcludeUtilityConsumption(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ConsumptionId, params api.ExcludeUtilityConsumptionParams) {
	var in api.UtilityConsumptionExclude
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Utilities.ExcludeConsumption(r.Context(), principal(r), marinaID, id, int64(version), in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeConsumption(w, v)
}

// AssignUtilityConsumption implements api.ServerInterface.
func (h *Handler) AssignUtilityConsumption(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ConsumptionId, params api.AssignUtilityConsumptionParams) {
	var in api.UtilityConsumptionAssign
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Utilities.AssignConsumption(r.Context(), principal(r), marinaID, id, int64(version), in.ContractId)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeConsumption(w, v)
}

// RunUtilityCharges implements api.ServerInterface.
func (h *Handler) RunUtilityCharges(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.RunUtilityChargesParams) {
	res, err := h.Utilities.Run(r.Context(), principal(r), marinaID, str(params.ContractId))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	// The anonymous item types of the generated result are filled through JSON to keep the handler thin.
	type charge struct {
		Adjustment bool   `json:"adjustment,omitempty"`
		BillingID  string `json:"billing_id"`
		ChargeID   string `json:"charge_id"`
		ContractID string `json:"contract_id"`
		Lines      int    `json:"lines"`
		TotalCents int64  `json:"total_cents"`
	}
	type skip struct {
		ConsumptionID *string `json:"consumption_id,omitempty"`
		ContractID    *string `json:"contract_id,omitempty"`
		Reason        string  `json:"reason"`
	}
	charges := make([]charge, 0, len(res.Charges))
	for _, c := range res.Charges {
		charges = append(charges, charge{c.Adjustment, c.BillingID, c.ChargeID, c.ContractID, c.Lines, c.TotalCents})
	}
	skips := make([]skip, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		skips = append(skips, skip{view.String(s.ConsumptionID), view.String(s.ContractID), s.Reason})
	}
	raw, err := json.Marshal(map[string]any{"charges": charges, "skipped": skips})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	var out api.UtilityChargeRunResult
	if err := json.Unmarshal(raw, &out); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, out)
}

func utlSettings(s *utility.Settings) api.UtilitySettings {
	return api.UtilitySettings{
		Mode: api.UtilityChargingMode(s.Mode), PowerItemId: view.String(s.PowerItemID), WaterItemId: view.String(s.WaterItemID),
		PowerDailyThresholdMilli: s.PowerDailyThresholdMilli, WaterDailyThresholdMilli: s.WaterDailyThresholdMilli, Version: int64(s.Version),
	}
}

// GetUtilitySettings implements api.ServerInterface.
func (h *Handler) GetUtilitySettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Utilities.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, utlSettings(s))
}

// UpdateUtilitySettings implements api.ServerInterface.
func (h *Handler) UpdateUtilitySettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdateUtilitySettingsParams) {
	var in api.UtilitySettingsInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	s, err := h.Utilities.UpdateSettings(r.Context(), principal(r), marinaID, int64(version), utility.SettingsInput{
		PowerItemID: in.PowerItemId, WaterItemID: in.WaterItemId, Mode: string(in.Mode),
		PowerDailyThresholdMilli: in.PowerDailyThresholdMilli, WaterDailyThresholdMilli: in.WaterDailyThresholdMilli,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, utlSettings(s))
}
