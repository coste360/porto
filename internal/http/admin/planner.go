package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/yardplan"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/qty"
)

// Planner, assignments, workload, at-risk orders and auto-plan (M10, m10-plan §6, §13). Owned by PLANNER.

// planAppend appends a zero element to a slice of a generated anonymous struct type and returns the new slice and
// the element to fill (the generated field names are kept without restating the type).
func planAppend[T any](s []T) ([]T, *T) {
	var zero T
	s = append(s, zero)
	return s, &s[len(s)-1]
}

// planEmpty returns an empty, non-nil slice of the same type (required arrays encode as [] rather than null).
func planEmpty[T any](_ []T) []T { return []T{} }

func optIsoDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func apiAssignment(a yardplan.AssignmentView) api.Assignment {
	r := a.Row
	return api.Assignment{
		Id: r.ID, TaskId: r.TaskID, WorkOrderId: a.WorkOrderID, EmployeeId: r.EmployeeID, WorkDate: r.WorkDate.String(),
		HoursMilli: r.HoursMilli, ForcedQualification: r.ForcedQualification, Overload: r.Overload, Version: int64(r.Version),
	}
}

func apiAssignments(in []yardplan.AssignmentView) []api.Assignment {
	out := make([]api.Assignment, 0, len(in))
	for _, a := range in {
		out = append(out, apiAssignment(a))
	}
	return out
}

func apiChange(c yardplan.AssignmentChange) api.AssignmentChange {
	return api.AssignmentChange{
		TaskId: c.TaskID, EmployeeId: c.EmployeeID, WorkDate: c.Date.String(), HoursMilli: int64(c.Hours), TaskVersion: c.TaskVersion,
	}
}

// GetPlanner implements api.ServerInterface.
func (h *Handler) GetPlanner(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetPlannerParams) {
	d, ok := h.planDates(w, r, marinaID, map[string]*string{"from": params.From, "to": params.To})
	if !ok {
		return
	}
	pv, err := h.YardPlan.GetPlanner(r.Context(), principal(r), marinaID, d["from"], d["to"], str(params.Department))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.Planner{
		From: pv.From.String(), To: pv.To.String(), Today: pv.Today.String(), Rows: []api.PlannerRow{},
		Assignments: apiAssignments(pv.Assignments), Tasks: []api.PlannerTask{},
	}
	for _, row := range pv.Rows {
		e := row.Employee
		ar := api.PlannerRow{
			EmployeeId: e.ID, Code: e.Code, DisplayName: e.FirstName + " " + e.LastName, Department: e.Department,
			QualificationIds: row.QualificationIDs, Days: make([]api.PlannerDay, 0, len(row.Days)),
		}
		for _, day := range row.Days {
			actual := int64(day.Cell.Actual)
			pd := api.PlannerDay{
				Date: day.Date.String(), CapacityMilli: int64(day.Cell.Capacity), AssignedMilli: int64(day.Cell.Assigned),
				ActualMilli: &actual, LoadBp: day.Cell.LoadBP, Overload: day.Cell.Overload,
			}
			if day.Absence != "" {
				k := api.AbsenceKind(day.Absence)
				pd.Absence = &k
			}
			ar.Days = append(ar.Days, pd)
		}
		out.Rows = append(out.Rows, ar)
	}
	for _, t := range pv.Tasks {
		out.Tasks = append(out.Tasks, api.PlannerTask{
			TaskId: t.Task.ID, WorkOrderId: t.Task.WorkOrderID, WorkOrderNumber: t.Number, VesselName: view.String(t.Task.VesselName),
			Title: t.Task.Title, QualificationId: view.String(t.Task.QualificationID), EstimatedHoursMilli: t.Task.EstimatedHoursMilli,
			RemainingMilli: int64(t.Remaining), AssignedMilli: int64(t.Assigned), ProgressBp: int64(t.Task.ProgressBP),
			PlannedDeliveryOn: optIsoDate(t.Task.PlannedDeliveryOn), DependsOn: t.DependsOn, Version: int64(t.Task.Version),
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

// PutAssignments implements api.ServerInterface.
func (h *Handler) PutAssignments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.AssignmentBatchInput
	if err := respond.Decode(r, &in, false); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	v := &auth.ValidationError{}
	batch := yardplan.BatchInput{
		ForceQualification: in.ForceQualification != nil && *in.ForceQualification,
		AllowOverload:      in.AllowOverload != nil && *in.AllowOverload,
	}
	for _, c := range in.Changes {
		wd := c.WorkDate
		d := parseDate(v, "changes.work_date", &wd)
		if d == nil {
			continue
		}
		batch.Changes = append(batch.Changes, yardplan.AssignmentChange{
			TaskID: c.TaskId, EmployeeID: c.EmployeeId, Date: *d, Hours: qty.Milli(c.HoursMilli), TaskVersion: c.TaskVersion,
		})
	}
	if err := v.Err(); err != nil {
		h.planBad(w, r, marinaID, err)
		return
	}
	res, err := h.YardPlan.PutAssignments(r.Context(), principal(r), marinaID, batch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.AssignmentBatchResult{Assignments: apiAssignments(res.Assignments)}
	out.Tasks = planEmpty(out.Tasks)
	for _, t := range res.Tasks {
		items, x := planAppend(out.Tasks)
		out.Tasks = items
		x.TaskId, x.Version = t.TaskID, int64(t.Version)
	}
	respond.JSON(w, http.StatusOK, out)
}

// DeleteAssignment implements api.ServerInterface.
func (h *Handler) DeleteAssignment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, assignmentID api.AssignmentId, params api.DeleteAssignmentParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.YardPlan.DeleteAssignment(r.Context(), principal(r), marinaID, assignmentID, int64(version)); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// GetYardLoad implements api.ServerInterface.
func (h *Handler) GetYardLoad(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.GetYardLoadParams) {
	d, ok := h.planDates(w, r, marinaID, map[string]*string{"from": params.From, "to": params.To})
	if !ok {
		return
	}
	groupBy, period := "", ""
	if params.GroupBy != nil {
		groupBy = string(*params.GroupBy)
	}
	if params.Period != nil {
		period = string(*params.Period)
	}
	lv, err := h.YardPlan.GetYardLoad(r.Context(), principal(r), marinaID, groupBy, period, d["from"], d["to"])
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.YardLoad{
		From: lv.From.String(), To: lv.To.String(), GroupBy: api.YardLoadGroupBy(lv.GroupBy), Period: api.YardLoadPeriod(lv.Period),
		Rows: make([]api.YardLoadRow, 0, len(lv.Rows)),
	}
	for _, row := range lv.Rows {
		ar := api.YardLoadRow{
			Key: row.Key, Label: row.Label, CapacityMilli: int64(row.Total.Capacity), AssignedMilli: int64(row.Total.Assigned),
			LoadBp: row.Total.LoadBP, Cells: make([]api.YardLoadCell, 0, len(row.Cells)),
		}
		for _, c := range row.Cells {
			ar.Cells = append(ar.Cells, api.YardLoadCell{
				PeriodStart: c.PeriodStart.String(), CapacityMilli: int64(c.Cell.Capacity), AssignedMilli: int64(c.Cell.Assigned),
				ActualMilli: int64(c.Cell.Actual), LoadBp: c.Cell.LoadBP, Overload: c.Cell.Overload,
			})
		}
		out.Rows = append(out.Rows, ar)
	}
	respond.JSON(w, http.StatusOK, out)
}

// GetAtRiskOrders implements api.ServerInterface.
func (h *Handler) GetAtRiskOrders(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	v, err := h.YardPlan.GetAtRiskOrders(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.AtRiskOrderList{Today: v.Today.String(), Items: make([]api.AtRiskOrder, 0, len(v.Items))}
	for _, o := range v.Items {
		ao := api.AtRiskOrder{
			WorkOrderId: o.WorkOrderID, Number: o.Number, VesselName: view.String(o.VesselName), PlannedDeliveryOn: optIsoDate(o.Delivery),
		}
		ao.Reasons = planEmpty(ao.Reasons)
		if o.Late {
			items, x := planAppend(ao.Reasons)
			ao.Reasons = items
			x.Code = api.Late
		}
		for _, rk := range o.Risks {
			free, rem := int64(rk.Free), int64(rk.Remaining)
			items, x := planAppend(ao.Reasons)
			ao.Reasons = items
			x.Code, x.QualificationId, x.FreeMilli, x.RemainingMilli = api.Capacity, view.String(rk.QualificationID), &free, &rem
		}
		out.Items = append(out.Items, ao)
	}
	respond.JSON(w, http.StatusOK, out)
}

// ProposeYardPlan implements api.ServerInterface.
func (h *Handler) ProposeYardPlan(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in *api.YardPlanProposalInput
	if err := respond.Decode(r, &in, true); err != nil {
		h.planBad(w, r, marinaID, respond.ErrBadJSON)
		return
	}
	pin := yardplan.ProposalInput{}
	if in != nil {
		d, ok := h.planDates(w, r, marinaID, map[string]*string{"from": in.From})
		if !ok {
			return
		}
		pin.From = d["from"]
		if in.HorizonDays != nil {
			pin.HorizonDays = *in.HorizonDays
		}
		if in.WorkOrderIds != nil {
			pin.WorkOrderIDs = *in.WorkOrderIds
		}
	}
	pv, err := h.YardPlan.ProposeYardPlan(r.Context(), principal(r), marinaID, pin)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.YardPlanProposal{From: pv.From.String(), To: pv.To.String(), Changes: make([]api.AssignmentChange, 0, len(pv.Changes))}
	for _, c := range pv.Changes {
		out.Changes = append(out.Changes, apiChange(c))
	}
	out.Unplaced = planEmpty(out.Unplaced)
	for _, u := range pv.Unplaced {
		items, x := planAppend(out.Unplaced)
		out.Unplaced = items
		x.RemainingMilli, x.TaskId = int64(u.Remaining), u.TaskID
	}
	respond.JSON(w, http.StatusOK, out)
}
