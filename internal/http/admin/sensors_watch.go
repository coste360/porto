package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/domain/sensor/watch"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// Sensors map context, alerts, history and the sensors report (M12, m12-plan §9-§10, §13). Owned by RECONCILE.
// The services authorise first (another marina's ids are 404), then validate.

// GetSensorMap implements api.ServerInterface.
func (h *Handler) GetSensorMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetSensorMapParams) {
	var date *calendar.Date
	if params.Date != nil {
		d, err := calendar.Parse(*params.Date)
		if err != nil {
			if _, aerr := h.SensorWatch.ListAlerts(r.Context(), principal(r), marinaID, watch.AlertFilter{}, "", 1); aerr != nil {
				respond.Error(w, r, aerr)
				return
			}
			respond.Problem(w, r, problem.BadRequest, problem.Fields(problem.FieldError{Field: "date", Code: "invalid"}))
			return
		}
		date = &d
	}
	mapID := ""
	if params.MapId != nil {
		mapID = *params.MapId
	}
	m, err := h.SensorWatch.Map(r.Context(), principal(r), marinaID, mapID, date)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, sensorMapJSON(m))
}

func sensorMapJSON(m *watch.Map) api.SensorMap {
	out := api.SensorMap{MapId: m.MapID, GeneratedAt: m.GeneratedAt, Entries: make([]api.SensorMapEntry, 0, len(m.Entries))}
	if m.Date != nil {
		s := m.Date.String()
		out.Date = &s
	}
	for _, e := range m.Entries {
		kinds := make([]api.SensorAlertKind, 0, len(e.OpenAlerts))
		for _, k := range e.OpenAlerts {
			kinds = append(kinds, api.SensorAlertKind(k))
		}
		je := api.SensorMapEntry{
			Code: e.Code, DeviceId: e.DeviceID, Discrepancy: e.Discrepancy, Expectation: api.SensorExpectation(e.Expectation),
			FromCm: e.FromCm, ToCm: e.ToCm, OpenAlerts: kinds, Physical: api.SensorPhysicalState(e.Physical),
			PhysicalSince: e.PhysicalSince, ResourceId: e.ResourceID, ResourceType: api.SensorResourceType(e.ResourceType),
			XCm: e.XCm, YCm: e.YCm,
		}
		if e.Health != "" {
			hv := api.SensorHealth(e.Health)
			je.Health = &hv
		}
		if e.Informational {
			t := true
			je.Informational = &t
		}
		out.Entries = append(out.Entries, je)
	}
	return out
}

func sensorAlertJSON(a *watch.AlertView) api.SensorAlert {
	refs := watchRefs(a)
	actions := make([]api.SensorAlertAction, 0, len(a.AllowedActions))
	for _, x := range a.AllowedActions {
		actions = append(actions, api.SensorAlertAction(x))
	}
	out := api.SensorAlert{
		AllowedActions: actions, ClearedAt: a.ClearedAt, DecidedAt: a.DecidedAt, DeviceCode: a.DeviceCode, DeviceId: a.DeviceID,
		Id: a.ID, Kind: api.SensorAlertKind(a.Kind), MapId: a.MapID, OpenedAt: a.OpenedAt, Refs: refs,
		ResourceCode: a.ResourceCode, ResourceId: a.ResourceID, ResourceType: api.SensorResourceType(a.ResourceType),
		Status: api.SensorAlertStatus(a.Status), Version: int64(a.Version),
	}
	if a.DecidedByUserID != "" {
		out.DecidedByUserId = &a.DecidedByUserID
	}
	if a.OutcomeRef != "" {
		out.OutcomeRef = &a.OutcomeRef
	}
	if a.Note != "" {
		out.Note = &a.Note
	}
	return out
}

func watchRefs(a *watch.AlertView) api.SensorAlertRefs {
	r := a.ParsedRefs()
	return api.SensorAlertRefs{
		AllocationId: optString(r.AllocationID), ContractId: optString(r.ContractID), PhysicalSince: r.PhysicalSince,
		PresenceId: optString(r.PresenceID), ProposedFrom: optString(r.ProposedFrom), ProposedTo: optString(r.ProposedTo),
		ReleaseId: optString(r.ReleaseID), Resold: r.Resold, VesselId: optString(r.VesselID),
	}
}

// ListSensorAlerts implements api.ServerInterface.
func (h *Handler) ListSensorAlerts(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListSensorAlertsParams) {
	f := watch.AlertFilter{}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.Kind != nil {
		f.Kind = string(*params.Kind)
	}
	if params.MapId != nil {
		f.MapID = *params.MapId
	}
	if params.DeviceId != nil {
		f.DeviceID = *params.DeviceId
	}
	cursor, limit := "", 0
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	page, err := h.SensorWatch.ListAlerts(r.Context(), principal(r), marinaID, f, cursor, limit)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.SensorAlertList{Items: make([]api.SensorAlert, 0, len(page.Items))}
	for i := range page.Items {
		out.Items = append(out.Items, sensorAlertJSON(&page.Items[i]))
	}
	if page.NextCursor != "" {
		out.NextCursor = &page.NextCursor
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetSensorAlert implements api.ServerInterface.
func (h *Handler) GetSensorAlert(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, alertID api.SensorAlertId) {
	a, err := h.SensorWatch.GetAlert(r.Context(), principal(r), marinaID, alertID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, a.Version)
	respond.JSON(w, http.StatusOK, sensorAlertJSON(a))
}

// ActOnSensorAlert implements api.ServerInterface.
func (h *Handler) ActOnSensorAlert(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, alertID api.SensorAlertId, params api.ActOnSensorAlertParams) {
	// Another marina's alert is 404 before the If-Match and body checks.
	if _, err := h.SensorWatch.GetAlert(r.Context(), principal(r), marinaID, alertID); err != nil {
		respond.Error(w, r, err)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var body api.SensorAlertActionInput
	if !respond.DecodeOrFail(w, r, &body, false) {
		return
	}
	in := watch.ActionInput{Action: string(body.Action)}
	if body.Note != nil {
		in.Note = *body.Note
	}
	if body.ConsentConfirmed != nil {
		in.ConsentConfirmed = *body.ConsentConfirmed
	}
	if body.Priority != nil {
		in.Priority = string(*body.Priority)
	}
	for _, x := range []struct {
		src   *api.IsoDate
		dst   **calendar.Date
		field string
	}{{body.From, &in.From, "from"}, {body.To, &in.To, "to"}} {
		if x.src == nil {
			continue
		}
		d, err := calendar.Parse(*x.src)
		if err != nil {
			respond.Problem(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: x.field, Code: "invalid"}))
			return
		}
		*x.dst = &d
	}
	a, err := h.SensorWatch.Act(r.Context(), principal(r), marinaID, alertID, version, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, a.Version)
	respond.JSON(w, http.StatusOK, sensorAlertJSON(a))
}

// historyQuery parses the shared query parameters of the history operations.
func historyQuery(from, to *api.IsoDate, readings *bool, cursor *api.Cursor) (watch.HistoryQuery, string) {
	q := watch.HistoryQuery{}
	for _, x := range []struct {
		src   *api.IsoDate
		dst   **calendar.Date
		field string
	}{{from, &q.From, "from"}, {to, &q.To, "to"}} {
		if x.src == nil || *x.src == "" {
			continue
		}
		d, err := calendar.Parse(*x.src)
		if err != nil {
			return q, x.field
		}
		*x.dst = &d
	}
	if readings != nil {
		q.IncludeReadings = *readings
	}
	if cursor != nil {
		q.Cursor = *cursor
	}
	return q, ""
}

func sensorHistoryJSON(hv *watch.History) api.SensorHistory {
	out := api.SensorHistory{
		Alerts: make([]api.SensorAlert, 0, len(hv.Alerts)), Changes: make([]api.SensorStateChange, 0, len(hv.Changes)),
		From: hv.From.String(), To: hv.To.String(), Nights: make([]api.SensorNight, 0, len(hv.Nights)), ResourceId: hv.ResourceID,
		ResourceType: api.SensorResourceType(hv.ResourceType), StateAtFrom: api.SensorPhysicalState(hv.StateAtFrom),
		ReadingsFrom: hv.ReadingsFrom,
	}
	if hv.DeviceIDs != nil {
		ids := append([]string{}, hv.DeviceIDs...)
		out.DeviceIds = &ids
	}
	for i := range hv.Alerts {
		out.Alerts = append(out.Alerts, sensorAlertJSON(&hv.Alerts[i]))
	}
	for _, c := range hv.Changes {
		out.Changes = append(out.Changes, api.SensorStateChange{
			At: c.At, DeviceId: c.DeviceID, From: api.SensorPhysicalState(c.FromState), Id: c.ID,
			Reason: api.SensorStateChangeReason(c.Reason), To: api.SensorPhysicalState(c.ToState),
		})
	}
	for _, n := range hv.Nights {
		disc := n.Discrepancy
		jn := api.SensorNight{Contractual: api.SensorContractual(n.Contractual), Discrepancy: &disc, Night: n.Night.String(), Physical: api.SensorPhysicalState(n.Physical)}
		if n.Presence != "" {
			p := n.Presence
			jn.Presence = &p
		}
		out.Nights = append(out.Nights, jn)
	}
	if hv.Readings != nil {
		rs := make([]api.SensorReadingRow, 0, len(hv.Readings))
		for _, x := range hv.Readings {
			rs = append(rs, api.SensorReadingRow{
				BatteryPct: x.BatteryPct, DeviceId: x.DeviceID, DistanceCm: x.DistanceCm, ObservedAt: x.ObservedAt,
				ReceivedAt: x.ReceivedAt, Rssi: x.RSSI, State: api.SensorPhysicalState(x.State),
			})
		}
		out.Readings = &rs
	}
	if hv.NextCursor != "" {
		out.NextCursor = &hv.NextCursor
	}
	return out
}

// writeHistory answers a history result (a missing required parameter is 400 validation_failed, after the
// authorisation).
func writeHistory(w http.ResponseWriter, r *http.Request, hv *watch.History, err error) {
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, sensorHistoryJSON(hv))
}

// GetSensorHistory implements api.ServerInterface.
func (h *Handler) GetSensorHistory(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetSensorHistoryParams) {
	q, bad := historyQuery(params.From, params.To, params.IncludeReadings, params.Cursor)
	if params.ResourceType != nil {
		q.ResourceType = string(*params.ResourceType)
	}
	if params.ResourceId != nil {
		q.ResourceID = *params.ResourceId
	}
	if bad != "" {
		if _, err := h.SensorWatch.ListAlerts(r.Context(), principal(r), marinaID, watch.AlertFilter{}, "", 1); err != nil {
			respond.Error(w, r, err)
			return
		}
		respond.Problem(w, r, problem.BadRequest, problem.Fields(problem.FieldError{Field: bad, Code: "invalid"}))
		return
	}
	hv, err := h.SensorWatch.History(r.Context(), principal(r), marinaID, q)
	writeHistory(w, r, hv, err)
}

// GetSensorDeviceHistory implements api.ServerInterface.
func (h *Handler) GetSensorDeviceHistory(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, deviceID api.SensorDeviceId, params api.GetSensorDeviceHistoryParams) {
	q, bad := historyQuery(params.From, params.To, params.IncludeReadings, params.Cursor)
	if bad != "" {
		q = watch.HistoryQuery{}
	}
	hv, err := h.SensorWatch.DeviceHistory(r.Context(), principal(r), marinaID, deviceID, q)
	if err == nil && bad != "" {
		respond.Problem(w, r, problem.BadRequest, problem.Fields(problem.FieldError{Field: bad, Code: "invalid"}))
		return
	}
	writeHistory(w, r, hv, err)
}

// GetSensorsReport implements api.ServerInterface (GET .../reports/sensors).
func (h *Handler) GetSensorsReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetSensorsReportParams) {
	if _, err := report.ParseParams(calc.KeySensors, r.URL.Query()); err != nil {
		// Authorise first: another marina is 404, a role without the report permissions 403, never 422.
		if cerr := h.Reports.CheckSensors(r.Context(), principal(r), marinaID); cerr != nil {
			respond.Error(w, r, cerr)
			return
		}
		respond.Error(w, r, err)
		return
	}
	serveReport(h, w, r, marinaID, calc.KeySensors, h.Reports.Sensors, resultView(
		func(x *calc.SensorsResult) *calc.SensorsResult { return x },
		func(x *calc.SensorsResult) any { return sensorsReportJSON(x) },
		export.Sensors))
}

func sensorsReportJSON(x *calc.SensorsResult) api.SensorsReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.SensorsTotals) api.SensorsReportTotals {
		return api.SensorsReportTotals{
			Records: t.Records, OccupiedUnexpected: t.OccupiedUnexpected.Ptr(), Absent: t.Absent.Ptr(),
			ResourcesWithDiscrepancy: t.ResourcesWithDiscrepancy.Ptr(), NightsCompared: t.NightsCompared.Ptr(), CoverageBp: t.CoverageBP.Ptr(),
		}
	}
	return api.SensorsReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals), View: api.SensorsReportView(calc.SensorsViewDiscrepancies),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.SensorsTotals]) api.SensorsReportGroup {
			return api.SensorsReportGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.SensorsRecord) api.SensorsReportRecord {
			ids := r.AlertIDs
			if ids == nil {
				ids = []string{}
			}
			return api.SensorsReportRecord{
				AlertIds: ids, Contractual: api.SensorContractual(r.Contractual), FromCm: r.FromCm, ToCm: r.ToCm,
				Kind: api.SensorDiscrepancyKind(r.Kind), MapId: rpS(r.MapID), MapName: rpS(r.MapName), Night: r.Night.String(),
				Physical: api.SensorPhysicalState(r.Physical), Presence: rpS(r.Presence), Ref: refJSON(r.Ref),
				ResourceCode: r.ResourceCode, ResourceId: r.ResourceID, ResourceType: api.SensorResourceType(r.ResourceType),
			}
		}),
	}
}
