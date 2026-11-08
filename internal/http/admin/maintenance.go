package admin

import (
	"io"
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/domain/maintenance/due"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Maintenance plans, tasks, field reports, worklist and map lens (M9, m9-plan §8). Owned by UTILITIES.

func mntDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func mntChecklist(list []maintenance.ChecklistEntry) []api.ChecklistEntry {
	out := make([]api.ChecklistEntry, 0, len(list))
	for _, c := range list {
		out = append(out, api.ChecklistEntry{Key: c.Key, Label: c.Label})
	}
	return out
}

func mntChecklistIn(in *[]api.ChecklistEntry) []maintenance.ChecklistEntry {
	if in == nil {
		return nil
	}
	out := make([]maintenance.ChecklistEntry, 0, len(*in))
	for _, c := range *in {
		out = append(out, maintenance.ChecklistEntry{Key: c.Key, Label: c.Label})
	}
	return out
}

func mntTask(v *maintenance.TaskView) api.MaintenanceTask {
	t := v.Row
	results := maintenance.ResultsOf(t.ChecklistResults)
	cr := make([]api.ChecklistResult, 0, len(results))
	for _, r := range results {
		cr = append(cr, api.ChecklistResult{Key: r.Key, Done: r.Done, Note: view.String(r.Note)})
	}
	files := make([]api.MaintenanceFile, 0, len(v.Files))
	for _, f := range v.Files {
		files = append(files, api.MaintenanceFile{
			FileId: f.FileID, Kind: api.MaintenanceFileKind(f.Kind), ContentType: f.ContentType, CreatedAt: f.CreatedAt.UTC(),
		})
	}
	ids := maintenance.AllocationIDsOf(&t)
	if ids == nil {
		ids = []string{}
	}
	out := api.MaintenanceTask{
		Id: t.ID, Source: api.MaintenanceSource(t.Source), Title: t.Title, Description: view.String(t.Description),
		Priority: api.MaintenancePriority(t.Priority), Status: api.MaintenanceTaskStatus(t.Status), DueOn: mntDate(t.DueOn),
		ScheduledFrom: mntDate(t.ScheduledFrom), ScheduledTo: mntDate(t.ScheduledTo), OutOfService: t.OutOfService,
		MapId: view.String(t.MapID), PositionXCm: t.PositionXCm, PositionYCm: t.PositionYCm,
		Checklist: mntChecklist(maintenance.ChecklistOf(t.Checklist)), ChecklistResults: cr, OutcomeNotes: view.String(t.OutcomeNotes),
		ExternalCostCents: t.ExternalCostCents, SupplierRef: view.String(t.SupplierRef), AssigneeUserId: view.String(t.AssigneeUserID),
		ReportedByUserId: view.String(t.ReportedByUserID), AllocationIds: ids, FollowsTaskId: view.String(t.FollowsTaskID),
		PlanId: view.String(t.PlanID), DoneAt: view.Time(t.DoneAt), Files: files, Overdue: v.Overdue, Version: int64(t.Version),
		CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC(),
	}
	if t.ElementID != "" {
		out.Element = &api.MaintenanceElementRef{ElementType: api.MaintenanceElementType(t.ElementType), ElementId: t.ElementID, Code: view.String(v.Code)}
	}
	if t.Intervention != "" {
		iv := api.MaintenanceIntervention(t.Intervention)
		out.Intervention = &iv
	}
	if t.Outcome != "" {
		o := api.MaintenanceOutcome(t.Outcome)
		out.Outcome = &o
	}
	if t.InternalMinutes != nil {
		m := int(*t.InternalMinutes)
		out.InternalMinutes = &m
	}
	return out
}

func writeTask(w http.ResponseWriter, v *maintenance.TaskView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, mntTask(v))
}

func mntPlan(v *maintenance.PlanView) api.MaintenancePlan {
	p := v.Row
	targets := make([]api.MaintenanceElementRef, 0, len(v.Targets))
	for _, t := range v.Targets {
		targets = append(targets, api.MaintenanceElementRef{ElementType: api.MaintenanceElementType(t.ElementType), ElementId: t.ElementID})
	}
	per := api.MaintenancePeriodicity{
		Kind: api.MaintenancePeriodicityKind(p.PeriodKind), Every: p.PeriodEvery, MeterId: view.String(p.PeriodMeterID),
		MonthDay: view.String(p.PeriodMonthDay), StartsOn: mntDate(p.PeriodStartsOn),
	}
	return api.MaintenancePlan{
		Id: p.ID, Name: p.Name, Intervention: api.MaintenanceIntervention(p.Intervention), InterventionLabel: view.String(p.InterventionLabel),
		Targets: targets, Periodicity: per, LeadDays: p.LeadDays, AssigneeUserId: view.String(p.AssigneeUserID),
		SupplierName: view.String(p.SupplierName), Checklist: mntChecklist(maintenance.ChecklistOf(p.Checklist)),
		RequiresFree: p.RequiresFree, Nights: p.Nights, Active: p.Active, Version: int64(p.Version),
		CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
}

func writePlan(w http.ResponseWriter, v *maintenance.PlanView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, mntPlan(v))
}

func mntElements(in []api.MaintenanceElementRef) []maintenance.Element {
	out := make([]maintenance.Element, 0, len(in))
	for _, e := range in {
		out = append(out, maintenance.Element{Type: string(e.ElementType), ID: e.ElementId})
	}
	return out
}

func mntElement(in *api.MaintenanceElementRef) *maintenance.Element {
	if in == nil {
		return nil
	}
	return &maintenance.Element{Type: string(in.ElementType), ID: in.ElementId}
}

// mntPeriodicity converts the wire periodicity; ok is false on an invalid date.
func mntPeriodicity(in api.MaintenancePeriodicity) (due.Periodicity, string, bool) {
	p := due.Periodicity{Kind: string(in.Kind), MonthDay: str(in.MonthDay)}
	if in.Every != nil {
		p.Every = *in.Every
	}
	d := newCtrDates()
	p.StartsOn = d.ptr(in.StartsOn)
	return p, str(in.MeterId), d.ok
}

// ListMaintenancePlans implements api.ServerInterface.
func (h *Handler) ListMaintenancePlans(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListMaintenancePlansParams) {
	et := ""
	if params.ElementType != nil {
		et = string(*params.ElementType)
	}
	items, next, err := h.Maintenance.ListPlans(r.Context(), principal(r), marinaID, params.Active, et, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.MaintenancePlan, 0, len(items))
	for i := range items {
		out = append(out, mntPlan(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.MaintenancePlanPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateMaintenancePlan implements api.ServerInterface.
func (h *Handler) CreateMaintenancePlan(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateMaintenancePlanParams) {
	var in api.MaintenancePlanInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	per, meter, ok := mntPeriodicity(in.Periodicity)
	if !ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	pi := maintenance.PlanInput{
		Name: in.Name, Intervention: string(in.Intervention), InterventionLabel: str(in.InterventionLabel), Targets: mntElements(in.Targets),
		Periodicity: per, MeterID: meter, LeadDays: intOr(in.LeadDays, 0), AssigneeUserID: str(in.AssigneeUserId),
		SupplierName: str(in.SupplierName), Checklist: mntChecklistIn(in.Checklist), RequiresFree: boolOr(in.RequiresFree, false),
		Nights: intOr(in.Nights, 1), Active: in.Active,
	}
	v, err := h.Maintenance.CreatePlan(r.Context(), principal(r), marinaID, pi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePlan(w, v, http.StatusCreated)
}

// GetMaintenancePlan implements api.ServerInterface.
func (h *Handler) GetMaintenancePlan(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PlanId) {
	v, err := h.Maintenance.GetPlan(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePlan(w, v, http.StatusOK)
}

// UpdateMaintenancePlan implements api.ServerInterface.
func (h *Handler) UpdateMaintenancePlan(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.PlanId, params api.UpdateMaintenancePlanParams) {
	var in api.MaintenancePlanPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	set := map[string]bool{}
	for _, k := range maintenance.PlanFields {
		if _, present := fields[k]; present {
			set[k] = true
		}
	}
	pi := maintenance.PlanInput{
		Name: str(in.Name), InterventionLabel: str(in.InterventionLabel), AssigneeUserID: str(in.AssigneeUserId),
		SupplierName: str(in.SupplierName), Checklist: mntChecklistIn(in.Checklist), RequiresFree: boolOr(in.RequiresFree, false),
		LeadDays: intOr(in.LeadDays, 0), Nights: intOr(in.Nights, 0), Active: in.Active,
	}
	if in.Intervention != nil {
		pi.Intervention = string(*in.Intervention)
	}
	if in.Targets != nil {
		pi.Targets = mntElements(*in.Targets)
	}
	if in.Periodicity != nil {
		per, meter, ok := mntPeriodicity(*in.Periodicity)
		if !ok {
			h.badPricingBody(w, r, marinaID)
			return
		}
		pi.Periodicity, pi.MeterID = per, meter
	}
	v, err := h.Maintenance.UpdatePlan(r.Context(), principal(r), marinaID, id, int64(version), pi, set)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writePlan(w, v, http.StatusOK)
}

// ListMaintenanceTasks implements api.ServerInterface.
func (h *Handler) ListMaintenanceTasks(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListMaintenanceTasksParams) {
	f := maintenance.TaskFilter{
		ElementID: str(params.ElementId), PlanID: str(params.PlanId), AssigneeUserID: str(params.AssigneeUserId),
		OverdueOnly: boolOr(params.OverdueOnly, false), Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.Priority != nil {
		f.Priority = string(*params.Priority)
	}
	if params.ElementType != nil {
		f.ElementType = string(*params.ElementType)
	}
	items, next, err := h.Maintenance.ListTasks(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.MaintenanceTask, 0, len(items))
	for i := range items {
		out = append(out, mntTask(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.MaintenanceTaskPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateMaintenanceTask implements api.ServerInterface.
func (h *Handler) CreateMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateMaintenanceTaskParams) {
	var in api.MaintenanceTaskCreate
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	ti := maintenance.TaskInput{
		Element: mntElement(in.Element), Title: in.Title, Description: str(in.Description), DueOn: d.ptr(in.DueOn),
		AssigneeUserID: str(in.AssigneeUserId), OutOfService: boolOr(in.OutOfService, false), FollowsTaskID: str(in.FollowsTaskId),
		MapID: str(in.MapId), X: in.PositionXCm, Y: in.PositionYCm,
	}
	if in.Priority != nil {
		ti.Priority = string(*in.Priority)
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	v, err := h.Maintenance.CreateTask(r.Context(), principal(r), marinaID, ti)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusCreated)
}

// GetMaintenanceTask implements api.ServerInterface.
func (h *Handler) GetMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId) {
	v, err := h.Maintenance.GetTask(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// UpdateMaintenanceTask implements api.ServerInterface.
func (h *Handler) UpdateMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, params api.UpdateMaintenanceTaskParams) {
	var in api.MaintenanceTaskPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := maintenance.TaskPatch{
		Title: in.Title, Description: in.Description, SupplierRef: in.SupplierRef, OutOfService: in.OutOfService,
		ClearAssignee: respond.IsNull(fields, "assignee_user_id") || (in.AssigneeUserId != nil && *in.AssigneeUserId == ""),
		ClearDue:      respond.IsNull(fields, "due_on") || (in.DueOn != nil && *in.DueOn == ""),
	}
	if in.AssigneeUserId != nil && *in.AssigneeUserId != "" {
		patch.AssigneeUserID = in.AssigneeUserId
	}
	if in.Priority != nil {
		p := string(*in.Priority)
		patch.Priority = &p
	}
	if in.DueOn != nil && *in.DueOn != "" {
		d := newCtrDates()
		patch.DueOn = d.ptr(in.DueOn)
		if !d.ok {
			h.badPricingBody(w, r, marinaID)
			return
		}
	}
	v, err := h.Maintenance.UpdateTask(r.Context(), principal(r), marinaID, id, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// ScheduleMaintenanceTask implements api.ServerInterface.
func (h *Handler) ScheduleMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, params api.ScheduleMaintenanceTaskParams) {
	var in api.MaintenanceScheduleInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	si := maintenance.ScheduleInput{From: d.parse(in.FromNight), To: d.parse(in.ToNight), OutOfService: in.OutOfService}
	if in.OccupyBerthIds != nil {
		si.OccupyBerthIDs = *in.OccupyBerthIds
	}
	if !d.ok || si.From.IsZero() || si.To.IsZero() {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Maintenance.Schedule(r.Context(), principal(r), marinaID, id, int64(version), si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// StartMaintenanceTask implements api.ServerInterface.
func (h *Handler) StartMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, params api.StartMaintenanceTaskParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Maintenance.Start(r.Context(), principal(r), marinaID, id, int64(version))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// CompleteMaintenanceTask implements api.ServerInterface (offline kind maintenance.outcome: no If-Match).
func (h *Handler) CompleteMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, _ api.CompleteMaintenanceTaskParams) {
	var in api.MaintenanceCompleteInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	ci := maintenance.CompleteInput{
		Outcome: string(in.Outcome), Notes: str(in.Notes), ExternalCostCents: in.ExternalCostCents, SupplierRef: str(in.SupplierRef),
		ClientRef: str(in.ClientRef),
	}
	if in.InternalMinutes != nil {
		m := int64(*in.InternalMinutes)
		ci.InternalMinutes = &m
	}
	if in.ChecklistResults != nil {
		for _, c := range *in.ChecklistResults {
			ci.Checklist = append(ci.Checklist, maintenance.ChecklistResult{Key: c.Key, Done: c.Done, Note: str(c.Note)})
		}
	}
	v, err := h.Maintenance.Complete(r.Context(), principal(r), marinaID, id, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// ReleaseMaintenanceTask implements api.ServerInterface.
func (h *Handler) ReleaseMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, params api.ReleaseMaintenanceTaskParams) {
	var in api.MaintenanceReleaseInput
	if !h.mntDecode(w, r, marinaID, &in, true) {
		return
	}
	d := newCtrDates()
	last := d.ptr(in.LastNight)
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Maintenance.Release(r.Context(), principal(r), marinaID, id, int64(version), last)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// CancelMaintenanceTask implements api.ServerInterface.
func (h *Handler) CancelMaintenanceTask(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, params api.CancelMaintenanceTaskParams) {
	var in api.MaintenanceCancelInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.Maintenance.Cancel(r.Context(), principal(r), marinaID, id, int64(version), in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusOK)
}

// UploadMaintenancePhoto implements api.ServerInterface (raw body: JPEG, PNG or PDF).
func (h *Handler) UploadMaintenancePhoto(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, _ api.UploadMaintenancePhotoParams) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maintenance.MaxFileBytes+1))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	f, err := h.Maintenance.UploadFile(r.Context(), principal(r), marinaID, id, maintenance.FileUpload{
		ContentType: r.Header.Get("Content-Type"), Data: data,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.MaintenanceFile{
		FileId: f.FileID, Kind: api.MaintenanceFileKind(f.Kind), ContentType: f.ContentType, CreatedAt: f.CreatedAt.UTC(),
	})
}

// GetMaintenanceFileLink implements api.ServerInterface.
func (h *Handler) GetMaintenanceFileLink(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.TaskId, fileID api.FileId) {
	l, err := h.Maintenance.FileLink(r.Context(), principal(r), marinaID, id, fileID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.FileLink{Url: l.URL, ExpiresAt: l.ExpiresAt.UTC()})
}

// ReportMaintenance implements api.ServerInterface (offline kind maintenance.report).
func (h *Handler) ReportMaintenance(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.ReportMaintenanceParams) {
	var in api.MaintenanceReportInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	ri := maintenance.ReportInput{
		ClientRef: in.ClientRef, Element: mntElement(in.Element), MapID: str(in.MapId), X: in.PositionXCm, Y: in.PositionYCm,
		Title: in.Title, Description: str(in.Description),
	}
	if in.Priority != nil {
		ri.Priority = string(*in.Priority)
	}
	v, err := h.Maintenance.Report(r.Context(), principal(r), marinaID, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeTask(w, v, http.StatusCreated)
}

// GetMaintenanceWorklist implements api.ServerInterface.
func (h *Handler) GetMaintenanceWorklist(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	items, day, err := h.Maintenance.Worklist(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.MaintenanceTask, 0, len(items))
	for i := range items {
		out = append(out, mntTask(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.MaintenanceWorklist{Date: day.String(), Items: out})
}

// GetMaintenanceMap implements api.ServerInterface.
func (h *Handler) GetMaintenanceMap(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetMaintenanceMapParams) {
	d := newCtrDates()
	date := d.ptr(params.Date)
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	mapID, day, entries, err := h.Maintenance.Map(r.Context(), principal(r), marinaID, str(params.MapId), date)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.MaintenanceMapEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, api.MaintenanceMapEntry{
			ElementType: api.MaintenanceElementType(e.ElementType), ElementId: e.ElementID, Open: e.Open, Overdue: e.Overdue,
			OutOfService: e.OutOfService,
		})
	}
	respond.JSON(w, http.StatusOK, api.MaintenanceMap{MapId: mapID, Date: day.String(), Items: out})
}
