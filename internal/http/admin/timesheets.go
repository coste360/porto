package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/workorder"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/qty"
)

// Time entries (M10, m10-plan §7, §13): timers, manual entries, approval, my work (phone). Owned by ORDERS.

func woEntry(v *workorder.EntryView) api.Timesheet {
	t := v.Row
	out := api.Timesheet{
		Id: t.ID, EmployeeId: t.EmployeeID, WorkOrderId: t.WorkOrderID, WorkOrderNumber: view.String(v.OrderNumber),
		TaskId: view.String(t.TaskID), WorkDate: t.WorkDate.String(), StartedAt: view.Time(t.StartedAt), EndedAt: view.Time(t.EndedAt),
		HoursMilli: t.HoursMilli, Source: api.TimesheetSource(t.Source), Status: api.TimesheetStatus(t.Status),
		CostRateCents: t.CostRateCents, CostCents: t.CostCents, Note: t.Note, Late: t.Late, ApprovedAt: view.Time(t.ApprovedAt),
		RejectReason: view.String(t.RejectReason), VoidsId: view.String(t.VoidsID), Version: int64(t.Version),
		CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC(),
	}
	if len(v.Warnings) > 0 {
		ws := v.Warnings
		out.Warnings = &ws
	}
	return out
}

func writeEntry(w http.ResponseWriter, v *workorder.EntryView, status int) {
	respond.SetETag(w, v.Row.Version)
	respond.JSON(w, status, woEntry(v))
}

// ListTimesheets implements api.ServerInterface.
func (h *Handler) ListTimesheets(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListTimesheetsParams) {
	d := newCtrDates()
	f := workorder.EntryFilter{
		EmployeeID: str(params.EmployeeId), WorkOrderID: str(params.WorkOrderId), From: d.parse(str(params.From)),
		To: d.parse(str(params.To)), Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	items, next, err := h.WorkOrders.ListEntries(r.Context(), principal(r), marinaID, f)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.Timesheet, 0, len(items))
	for i := range items {
		out = append(out, woEntry(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.TimesheetPage{Items: out, NextCursor: nextCursor(next)})
}

// CreateTimeEntry implements api.ServerInterface (offline kind yard.timesheet).
func (h *Handler) CreateTimeEntry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateTimeEntryParams) {
	var in api.TimeEntryInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	d := newCtrDates()
	ei := workorder.EntryInput{
		ClientRef: in.ClientRef, EmployeeID: str(in.EmployeeId), TaskID: str(in.TaskId), WorkOrderID: str(in.WorkOrderId),
		WorkDate: d.parse(str(in.WorkDate)), StartedAt: in.StartedAt, EndedAt: in.EndedAt, Note: str(in.Note),
	}
	if !d.ok || (in.StartedAt == nil) != (in.EndedAt == nil) {
		if d.ok {
			respond.Error(w, r, workorderInvalid("ended_at", "required"))
			return
		}
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.HoursMilli != nil {
		hm := qty.Milli(*in.HoursMilli)
		ei.Hours = &hm
	}
	v, err := h.WorkOrders.CreateEntry(r.Context(), principal(r), marinaID, ei)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEntry(w, v, http.StatusCreated)
}

// StartTimer implements api.ServerInterface (offline kind yard.timesheet).
func (h *Handler) StartTimer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.StartTimerParams) {
	var in api.TimerStartInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	v, err := h.WorkOrders.StartTimer(r.Context(), principal(r), marinaID, workorder.TimerStart{
		ClientRef: in.ClientRef, EmployeeID: str(in.EmployeeId), TaskID: str(in.TaskId), WorkOrderID: str(in.WorkOrderId),
		StartedAt: in.StartedAt,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEntry(w, v, http.StatusCreated)
}

// StopTimer implements api.ServerInterface (offline kind yard.timesheet, dependent on startTimer).
func (h *Handler) StopTimer(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, timesheetID api.TimesheetId, _ api.StopTimerParams) {
	var in *api.TimerStopInput
	if !h.mntDecode(w, r, marinaID, &in, true) {
		return
	}
	ts := workorder.TimerStop{}
	if in != nil {
		ts = workorder.TimerStop{ClientRef: str(in.ClientRef), EndedAt: in.EndedAt, Note: in.Note}
	}
	v, err := h.WorkOrders.StopTimer(r.Context(), principal(r), marinaID, timesheetID, ts)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEntry(w, v, http.StatusOK)
}

// UpdateTimeEntry implements api.ServerInterface.
func (h *Handler) UpdateTimeEntry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, timesheetID api.TimesheetId, params api.UpdateTimeEntryParams) {
	var in api.TimeEntryPatch
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	d := newCtrDates()
	patch := workorder.EntryPatch{TaskID: in.TaskId, WorkDate: d.ptr(in.WorkDate), StartedAt: in.StartedAt, EndedAt: in.EndedAt, Note: in.Note}
	if !d.ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if (in.StartedAt == nil) != (in.EndedAt == nil) {
		respond.Error(w, r, workorderInvalid("ended_at", "required"))
		return
	}
	if in.HoursMilli != nil {
		hm := qty.Milli(*in.HoursMilli)
		patch.Hours = &hm
	}
	v, err := h.WorkOrders.UpdateEntry(r.Context(), principal(r), marinaID, timesheetID, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEntry(w, v, http.StatusOK)
}

// ApproveTimeEntries implements api.ServerInterface.
func (h *Handler) ApproveTimeEntries(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.TimesheetApproveInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	entries := make([]workorder.EntryVersion, 0, len(in.Entries))
	for _, e := range in.Entries {
		entries = append(entries, workorder.EntryVersion{ID: e.Id, Version: e.Version})
	}
	items, err := h.WorkOrders.Approve(r.Context(), principal(r), marinaID, entries)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.Timesheet, 0, len(items))
	for i := range items {
		out = append(out, woEntry(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.TimesheetList{Items: out})
}

// RejectTimeEntry implements api.ServerInterface.
func (h *Handler) RejectTimeEntry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, timesheetID api.TimesheetId, params api.RejectTimeEntryParams) {
	var in api.TimesheetRejectInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	v, err := h.WorkOrders.Reject(r.Context(), principal(r), marinaID, timesheetID, int64(version), in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEntry(w, v, http.StatusOK)
}

// VoidTimeEntry implements api.ServerInterface.
func (h *Handler) VoidTimeEntry(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, timesheetID api.TimesheetId, params api.VoidTimeEntryParams) {
	var in *api.TimesheetVoidInput
	if !h.mntDecode(w, r, marinaID, &in, true) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	reason := ""
	if in != nil {
		reason = str(in.Reason)
	}
	v, err := h.WorkOrders.Void(r.Context(), principal(r), marinaID, timesheetID, int64(version), reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEntry(w, v, http.StatusOK)
}

// GetMyYardWork implements api.ServerInterface.
func (h *Handler) GetMyYardWork(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	m, err := h.WorkOrders.MyWork(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.MyYardWork{Today: m.Today.String(), EmployeeId: view.String(m.EmployeeID), Assignments: make([]api.MyYardAssignment, 0, len(m.Assignments))}
	for _, a := range m.Assignments {
		cl := woChecklist(a.Checklist)
		tv := a.Row.TaskVersion
		out.Assignments = append(out.Assignments, api.MyYardAssignment{
			AssignmentId: a.Row.AssignmentID, WorkDate: a.Row.WorkDate.String(), HoursMilli: a.Row.HoursMilli, TaskId: a.Row.TaskID,
			TaskTitle: a.Row.TaskTitle, TaskStatus: api.WorkTaskStatus(a.Row.TaskStatus), TaskVersion: &tv, ProgressBp: a.Row.ProgressBP,
			Checklist: &cl, WorkOrderId: a.Row.WorkOrderID, WorkOrderNumber: a.OrderNumber, VesselName: view.String(a.Row.VesselName),
			Position: view.String(a.Position),
		})
	}
	if m.RunningTimer != nil {
		t := woEntry(m.RunningTimer)
		out.RunningTimer = &t
	}
	respond.JSON(w, http.StatusOK, out)
}

// workorderInvalid is a validation error of one field (400 validation_failed).
func workorderInvalid(field, code string) error {
	return workorder.Invalid(field, code)
}
