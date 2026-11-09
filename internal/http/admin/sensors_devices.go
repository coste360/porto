package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/sensor"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Sensor devices, gateways, sources and settings (M12, m12-plan §3, §6, §13). Owned by INGEST. Thin handlers: the
// service authorises first (another marina's ids are 404). Responses that carry a key set Cache-Control: no-store;
// keys are never logged.

func sensorDeviceView(v *sensor.DeviceView) api.SensorDevice {
	d := v.SensorDevice
	out := api.SensorDevice{
		Id: d.ID, ExternalId: d.ExternalID, Code: d.Code, Kind: api.SensorKind(d.Kind), Model: d.Model, Firmware: d.Firmware,
		ResourceType: api.SensorResourceType(d.ResourceType), ResourceId: d.ResourceID, ResourceCode: v.ResourceCode,
		FromCm: d.FromCm, ToCm: d.ToCm, MapId: d.MapID, XCm: d.XCm, YCm: d.YCm, Active: d.Active,
		Health: api.SensorHealth(v.Health), KeyStatus: api.SensorKeyStatus(v.KeyStatus), KeyIssuedAt: d.KeyIssuedAt,
		Physical: api.SensorPhysicalState(sensor.PhysicalUnknown), Version: int64(d.Version), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	if d.PedestalSocket != "" {
		s := d.PedestalSocket
		out.PedestalSocket = &s
	}
	if d.GatewayID != "" {
		s := d.GatewayID
		out.GatewayId = &s
	}
	if d.SourceID != "" {
		s := d.SourceID
		out.SourceId = &s
	}
	if d.KeyID != "" {
		s := d.KeyID
		out.KeyId = &s
	}
	if d.PrevKeyID != "" {
		out.PrevValidUntil = d.PrevValidUntil
	}
	if d.ResourceType == "pedestal" {
		info := v.Informational
		out.Informational = &info
	}
	if st := v.State; st != nil {
		out.Physical = api.SensorPhysicalState(st.Physical)
		out.PhysicalSince, out.LastObservedAt, out.LastContactAt = st.PhysicalSince, st.LastObservedAt, st.LastContactAt
		out.BatteryPct, out.Rssi, out.DistanceCm = st.BatteryPct, st.RSSI, st.DistanceCm
		if st.Firmware != "" {
			out.Firmware = st.Firmware
		}
	}
	return out
}

func sensorGatewayView(v *sensor.GatewayView) api.SensorGateway {
	g := v.SensorGateway
	out := api.SensorGateway{
		Id: g.ID, Code: g.Code, Name: g.Name, Kind: api.SensorGatewayKind(g.Kind), Active: g.Active, DeviceCount: v.DeviceCount,
		KeyStatus: api.SensorKeyStatus(v.KeyStatus), KeyIssuedAt: g.KeyIssuedAt, LastContactAt: g.LastContactAt,
		Version: int64(g.Version), CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
	if g.KeyID != "" {
		s := g.KeyID
		out.KeyId = &s
	}
	if g.PrevKeyID != "" {
		out.PrevValidUntil = g.PrevValidUntil
	}
	return out
}

func sensorSourceView(v *sensor.SourceView) api.SensorSource {
	s := v.SensorSource
	out := api.SensorSource{
		Id: s.ID, Code: s.Code, Name: s.Name, PollSeconds: s.PollSeconds, Enabled: s.Enabled, ConfigSet: v.ConfigSet,
		ConfigPublic: v.ConfigPublic, DeviceCount: v.DeviceCount, LastPolledAt: s.LastPolledAt, Version: int64(s.Version),
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if out.ConfigPublic == nil {
		out.ConfigPublic = map[string]string{}
	}
	if s.LastErrorCode != "" {
		c := api.SensorSourceErrorCode(s.LastErrorCode)
		out.LastErrorCode = &c
	}
	return out
}

func sensorSettingsView(s *sensor.Settings) api.SensorSettings {
	return api.SensorSettings{
		HysteresisReadings: s.HysteresisReadings, HysteresisMinutes: s.HysteresisMinutes, OfflineAfterMinutes: s.OfflineAfterMinutes,
		SilentAlertMinutes: s.SilentAlertMinutes, BatteryLowPct: s.BatteryLowPct, UnexpectedAfterMinutes: s.UnexpectedAfterMinutes,
		DepartureCutoffMinutes: s.DepartureCutoffMinutes, PresenceEffectEnabled: s.PresenceEffectEnabled,
		PresenceOutAfterHours: s.PresenceOutAfterHours, ReleaseProposalAfterDays: s.ReleaseProposalAfterDays,
		ReleaseProposalNights: s.ReleaseProposalNights, RawRetentionDays: s.RawRetentionDays, Version: int64(s.Version),
	}
}

func writeDevice(w http.ResponseWriter, status int, v *sensor.DeviceView) {
	respond.SetETag(w, v.Version)
	respond.JSON(w, status, sensorDeviceView(v))
}

func writeDeviceKey(w http.ResponseWriter, status int, v *sensor.DeviceWithKey) {
	w.Header().Set("Cache-Control", "no-store")
	respond.SetETag(w, v.Device.Version)
	respond.JSON(w, status, api.SensorDeviceWithKey{Device: sensorDeviceView(&v.Device), Key: v.Key})
}

func writeGateway(w http.ResponseWriter, v *sensor.GatewayView) {
	respond.SetETag(w, v.Version)
	respond.JSON(w, http.StatusOK, sensorGatewayView(v))
}

func writeGatewayKey(w http.ResponseWriter, status int, v *sensor.GatewayWithKey) {
	w.Header().Set("Cache-Control", "no-store")
	respond.SetETag(w, v.Gateway.Version)
	respond.JSON(w, status, api.SensorGatewayWithKey{Gateway: sensorGatewayView(&v.Gateway), Key: v.Key})
}

func writeSource(w http.ResponseWriter, status int, v *sensor.SourceView) {
	respond.SetETag(w, v.Version)
	respond.JSON(w, status, sensorSourceView(v))
}

func graceOf(in api.SensorKeyRotateInput) int {
	if in.GraceHours == nil {
		return 24
	}
	return *in.GraceHours
}

// ListSensorDevices implements api.ServerInterface.
func (h *Handler) ListSensorDevices(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListSensorDevicesParams) {
	f := sensor.DeviceFilter{MapID: str(params.MapId), ResourceID: str(params.ResourceId), Active: params.Active}
	if params.ResourceType != nil {
		f.ResourceType = string(*params.ResourceType)
	}
	if params.Health != nil {
		f.Health = string(*params.Health)
	}
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	page, err := h.Sensors.ListDevices(r.Context(), principal(r), marinaID, f, str(params.Cursor), limit)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.SensorDeviceList{Items: make([]api.SensorDevice, 0, len(page.Items))}
	for i := range page.Items {
		out.Items = append(out.Items, sensorDeviceView(&page.Items[i]))
	}
	if page.NextCursor != "" {
		c := page.NextCursor
		out.NextCursor = &c
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateSensorDevice implements api.ServerInterface.
func (h *Handler) CreateSensorDevice(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateSensorDeviceParams) {
	var in api.SensorDeviceInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v, err := h.Sensors.CreateDevice(r.Context(), principal(r), marinaID, sensor.DeviceInput{
		ExternalID: in.ExternalId, Code: in.Code, Kind: string(in.Kind), Model: str(in.Model), Firmware: str(in.Firmware),
		ResourceType: string(in.ResourceType), ResourceID: in.ResourceId, FromCm: in.FromCm, ToCm: in.ToCm,
		PedestalSocket: str(in.PedestalSocket), XCm: in.XCm, YCm: in.YCm, GatewayID: str(in.GatewayId), SourceID: str(in.SourceId),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDeviceKey(w, http.StatusCreated, v)
}

// GetSensorDevice implements api.ServerInterface.
func (h *Handler) GetSensorDevice(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deviceID api.SensorDeviceId) {
	v, err := h.Sensors.GetDevice(r.Context(), principal(r), marinaID, deviceID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDevice(w, http.StatusOK, v)
}

// UpdateSensorDevice implements api.ServerInterface.
func (h *Handler) UpdateSensorDevice(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deviceID api.SensorDeviceId, params api.UpdateSensorDeviceParams) {
	var in api.SensorDevicePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p := sensor.DevicePatch{
		Code: in.Code, Model: in.Model, Firmware: in.Firmware, ResourceID: in.ResourceId, FromCm: in.FromCm, ToCm: in.ToCm,
		PedestalSocket: in.PedestalSocket, XCm: in.XCm, YCm: in.YCm, Active: in.Active,
	}
	if in.Kind != nil {
		k := string(*in.Kind)
		p.Kind = &k
	}
	if in.ResourceType != nil {
		t := string(*in.ResourceType)
		p.ResourceType = &t
	}
	if in.Binding != nil {
		p.Binding = &sensor.Binding{GatewayID: str(in.Binding.GatewayId), SourceID: str(in.Binding.SourceId)}
	}
	v, err := h.Sensors.UpdateDevice(r.Context(), principal(r), marinaID, deviceID, ifMatch, p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDevice(w, http.StatusOK, v)
}

// RotateSensorDeviceKey implements api.ServerInterface.
func (h *Handler) RotateSensorDeviceKey(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deviceID api.SensorDeviceId, params api.RotateSensorDeviceKeyParams) {
	var in api.SensorKeyRotateInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Sensors.RotateDeviceKey(r.Context(), principal(r), marinaID, deviceID, ifMatch, graceOf(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDeviceKey(w, http.StatusOK, v)
}

// RevokeSensorDeviceKey implements api.ServerInterface.
func (h *Handler) RevokeSensorDeviceKey(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deviceID api.SensorDeviceId, params api.RevokeSensorDeviceKeyParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Sensors.RevokeDeviceKey(r.Context(), principal(r), marinaID, deviceID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeDevice(w, http.StatusOK, v)
}

// ListSensorGateways implements api.ServerInterface.
func (h *Handler) ListSensorGateways(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	rows, err := h.Sensors.ListGateways(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.SensorGatewayList{Items: make([]api.SensorGateway, 0, len(rows))}
	for i := range rows {
		out.Items = append(out.Items, sensorGatewayView(&rows[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateSensorGateway implements api.ServerInterface.
func (h *Handler) CreateSensorGateway(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateSensorGatewayParams) {
	var in api.SensorGatewayInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	v, err := h.Sensors.CreateGateway(r.Context(), principal(r), marinaID, sensor.GatewayInput{Code: in.Code, Name: in.Name, Kind: string(in.Kind)})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeGatewayKey(w, http.StatusCreated, v)
}

// UpdateSensorGateway implements api.ServerInterface.
func (h *Handler) UpdateSensorGateway(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, gatewayID api.SensorGatewayId, params api.UpdateSensorGatewayParams) {
	var in api.SensorGatewayPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p := sensor.GatewayPatch{Name: in.Name, Active: in.Active}
	if in.Kind != nil {
		k := string(*in.Kind)
		p.Kind = &k
	}
	v, err := h.Sensors.UpdateGateway(r.Context(), principal(r), marinaID, gatewayID, ifMatch, p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeGateway(w, v)
}

// RotateSensorGatewayKey implements api.ServerInterface.
func (h *Handler) RotateSensorGatewayKey(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, gatewayID api.SensorGatewayId, params api.RotateSensorGatewayKeyParams) {
	var in api.SensorKeyRotateInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Sensors.RotateGatewayKey(r.Context(), principal(r), marinaID, gatewayID, ifMatch, graceOf(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeGatewayKey(w, http.StatusOK, v)
}

// RevokeSensorGatewayKey implements api.ServerInterface.
func (h *Handler) RevokeSensorGatewayKey(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, gatewayID api.SensorGatewayId, params api.RevokeSensorGatewayKeyParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Sensors.RevokeGatewayKey(r.Context(), principal(r), marinaID, gatewayID, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeGateway(w, v)
}

// ListSensorSources implements api.ServerInterface.
func (h *Handler) ListSensorSources(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	rows, err := h.Sensors.ListSources(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.SensorSourceList{Items: make([]api.SensorSource, 0, len(rows)), Drivers: []api.SensorSourceDriver{}}
	for i := range rows {
		out.Items = append(out.Items, sensorSourceView(&rows[i]))
	}
	for _, d := range h.Sensors.Drivers() {
		pub, sec := d.PublicKeys, d.Secret
		if pub == nil {
			pub = []string{}
		}
		if sec == nil {
			sec = []string{}
		}
		out.Drivers = append(out.Drivers, api.SensorSourceDriver{Code: d.Code, PublicKeys: pub, SecretKeys: sec})
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateSensorSource implements api.ServerInterface.
func (h *Handler) CreateSensorSource(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateSensorSourceParams) {
	var in api.SensorSourceInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	si := sensor.SourceInput{Code: in.Code, Name: in.Name, Enabled: in.Enabled}
	if in.Config != nil {
		si.Config = *in.Config
	}
	if in.PollSeconds != nil {
		si.PollSeconds = *in.PollSeconds
	}
	v, err := h.Sensors.CreateSource(r.Context(), principal(r), marinaID, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeSource(w, http.StatusCreated, v)
}

// UpdateSensorSource implements api.ServerInterface.
func (h *Handler) UpdateSensorSource(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, sourceID api.SensorSourceId, params api.UpdateSensorSourceParams) {
	var in api.SensorSourcePatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p := sensor.SourcePatch{Name: in.Name, PollSeconds: in.PollSeconds, Enabled: in.Enabled}
	if in.Config != nil {
		p.Config = *in.Config
		if p.Config == nil {
			p.Config = map[string]string{}
		}
	}
	v, err := h.Sensors.UpdateSource(r.Context(), principal(r), marinaID, sourceID, ifMatch, p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeSource(w, http.StatusOK, v)
}

// PollSensorSourceNow implements api.ServerInterface.
func (h *Handler) PollSensorSourceNow(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, sourceID api.SensorSourceId) {
	res, err := h.Sensors.PollSourceNow(r.Context(), principal(r), marinaID, sourceID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.SensorSourcePollResult{Accepted: res.Accepted, Duplicates: res.Duplicates, Rejected: res.Rejected}
	if res.ErrorCode != "" {
		c := api.SensorSourceErrorCode(res.ErrorCode)
		out.ErrorCode = &c
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetSensorSettings implements api.ServerInterface.
func (h *Handler) GetSensorSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Sensors.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, sensorSettingsView(s))
}

// PutSensorSettings implements api.ServerInterface (If-Match "0" for the first save).
func (h *Handler) PutSensorSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.PutSensorSettingsParams) {
	var in api.SensorSettingsInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ifMatch, ok := ifMatchOrZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	s, err := h.Sensors.PutSettings(r.Context(), principal(r), marinaID, ifMatch, sensor.SettingsInput{
		HysteresisReadings: in.HysteresisReadings, HysteresisMinutes: in.HysteresisMinutes, OfflineAfterMinutes: in.OfflineAfterMinutes,
		SilentAlertMinutes: in.SilentAlertMinutes, BatteryLowPct: in.BatteryLowPct, UnexpectedAfterMinutes: in.UnexpectedAfterMinutes,
		DepartureCutoffMinutes: in.DepartureCutoffMinutes, PresenceEffectEnabled: in.PresenceEffectEnabled,
		PresenceOutAfterHours: in.PresenceOutAfterHours, ReleaseProposalAfterDays: in.ReleaseProposalAfterDays,
		ReleaseProposalNights: in.ReleaseProposalNights,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, sensorSettingsView(s))
}
