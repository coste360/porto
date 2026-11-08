package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/yardplan"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Yard employees, calendars and absences (M10, m10-plan §6.1, §13). Owned by PLANNER.

// planBad answers a request whose parameters or body are malformed: another marina is still 404 first, so that the
// existence of a marina never leaks through a 400 or 422.
func (h *Handler) planBad(w http.ResponseWriter, r *http.Request, marinaID string, err error) {
	if _, merr := h.Marinas.GetMarina(r.Context(), principal(r), marinaID); merr != nil {
		respond.Error(w, r, merr)
		return
	}
	respond.Error(w, r, err)
}

// planDates parses optional query dates; false when one is malformed (the answer is already written).
func (h *Handler) planDates(w http.ResponseWriter, r *http.Request, marinaID string, fields map[string]*string) (map[string]calendar.Date, bool) {
	v := &auth.ValidationError{}
	out := map[string]calendar.Date{}
	for name, s := range fields {
		if d := parseDate(v, name, s); d != nil {
			out[name] = *d
		}
	}
	if err := v.Err(); err != nil {
		h.planBad(w, r, marinaID, err)
		return nil, false
	}
	return out, true
}

func apiEmployee(e *yardplan.EmployeeView) api.Employee {
	r := e.Row
	cal := make([]api.EmployeeCalendarDay, 0, 7)
	for i, m := range e.Week {
		cal = append(cal, api.EmployeeCalendarDay{Weekday: i + 1, Minutes: m})
	}
	return api.Employee{
		Id: r.ID, Code: r.Code, FirstName: r.FirstName, LastName: r.LastName, UserId: view.String(r.UserID),
		Department: r.Department, HourlyCostCents: r.HourlyCostCents, Active: r.Active, QualificationIds: e.QualificationIDs,
		Calendar: cal, Version: int64(r.Version), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func writeEmployee(w http.ResponseWriter, e *yardplan.EmployeeView, status int) {
	respond.SetETag(w, e.Row.Version)
	respond.JSON(w, status, apiEmployee(e))
}

func calendarDays(in []api.EmployeeCalendarDay) []yardplan.CalendarDay {
	out := make([]yardplan.CalendarDay, 0, len(in))
	for _, d := range in {
		out = append(out, yardplan.CalendarDay{Weekday: d.Weekday, Minutes: d.Minutes})
	}
	return out
}

func apiAbsence(a *yardplan.Absence) api.Absence {
	return api.Absence{
		Id: a.ID, EmployeeId: a.EmployeeID, FromOn: a.FromOn.String(), ToOn: a.ToOn.String(), Kind: api.AbsenceKind(a.Kind),
		MinutesPerDay: a.MinutesPerDay, Note: a.Note, CreatedAt: a.CreatedAt.UTC(),
	}
}

// ListEmployees implements api.ServerInterface.
func (h *Handler) ListEmployees(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListEmployeesParams) {
	f := yardplan.EmployeeFilter{Active: params.Active, Department: str(params.Department), QualificationID: str(params.QualificationId)}
	items, next, err := h.YardPlan.ListEmployees(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.Employee, 0, len(items))
	for i := range items {
		out = append(out, apiEmployee(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.EmployeePage{Items: out, NextCursor: nextCursor(next)})
}

// CreateEmployee implements api.ServerInterface.
func (h *Handler) CreateEmployee(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateEmployeeParams) {
	var in api.EmployeeInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	ein := yardplan.EmployeeInput{
		Code: in.Code, FirstName: in.FirstName, LastName: in.LastName, UserID: str(in.UserId), Department: str(in.Department),
		HourlyCostCents: in.HourlyCostCents, Active: in.Active,
	}
	if in.QualificationIds != nil {
		ein.QualificationIDs = *in.QualificationIds
	}
	if in.Calendar != nil {
		ein.Calendar = calendarDays(*in.Calendar)
	}
	e, err := h.YardPlan.CreateEmployee(r.Context(), principal(r), marinaID, ein)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEmployee(w, e, http.StatusCreated)
}

// GetEmployee implements api.ServerInterface.
func (h *Handler) GetEmployee(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, employeeID api.EmployeeId) {
	e, err := h.YardPlan.GetEmployee(r.Context(), principal(r), marinaID, employeeID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEmployee(w, e, http.StatusOK)
}

// UpdateEmployee implements api.ServerInterface.
func (h *Handler) UpdateEmployee(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, employeeID api.EmployeeId, params api.UpdateEmployeeParams) {
	var in api.EmployeePatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := yardplan.EmployeePatch{
		Code: in.Code, FirstName: in.FirstName, LastName: in.LastName, UserID: in.UserId, ClearUser: respond.IsNull(fields, "user_id"),
		Department: in.Department, HourlyCostCents: in.HourlyCostCents, Active: in.Active, QualificationIDs: in.QualificationIds,
	}
	e, err := h.YardPlan.UpdateEmployee(r.Context(), principal(r), marinaID, employeeID, int64(version), patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEmployee(w, e, http.StatusOK)
}

// PutEmployeeCalendar implements api.ServerInterface.
func (h *Handler) PutEmployeeCalendar(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, employeeID api.EmployeeId, params api.PutEmployeeCalendarParams) {
	var in api.EmployeeCalendarInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	e, err := h.YardPlan.PutEmployeeCalendar(r.Context(), principal(r), marinaID, employeeID, int64(version), calendarDays(in.Days))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeEmployee(w, e, http.StatusOK)
}

// ListAbsences implements api.ServerInterface.
func (h *Handler) ListAbsences(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, employeeID api.EmployeeId, params api.ListAbsencesParams) {
	d, ok := h.planDates(w, r, marinaID, map[string]*string{"from": params.From, "to": params.To})
	if !ok {
		return
	}
	rows, err := h.YardPlan.ListAbsences(r.Context(), principal(r), marinaID, employeeID, d["from"], d["to"])
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.Absence, 0, len(rows))
	for i := range rows {
		out = append(out, apiAbsence(&rows[i]))
	}
	respond.JSON(w, http.StatusOK, api.AbsenceList{Items: out})
}

// CreateAbsence implements api.ServerInterface.
func (h *Handler) CreateAbsence(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, employeeID api.EmployeeId, _ api.CreateAbsenceParams) {
	var in api.AbsenceInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	from, to := in.FromOn, in.ToOn
	d, ok := h.planDates(w, r, marinaID, map[string]*string{"from_on": &from, "to_on": &to})
	if !ok {
		return
	}
	a, err := h.YardPlan.CreateAbsence(r.Context(), principal(r), marinaID, employeeID, yardplan.AbsenceInput{
		From: d["from_on"], To: d["to_on"], Kind: string(in.Kind), MinutesPerDay: in.MinutesPerDay, Note: str(in.Note),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, apiAbsence(a))
}

// DeleteAbsence implements api.ServerInterface.
func (h *Handler) DeleteAbsence(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, employeeID api.EmployeeId, absenceID api.AbsenceId) {
	if err := h.YardPlan.DeleteAbsence(r.Context(), principal(r), marinaID, employeeID, absenceID); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
