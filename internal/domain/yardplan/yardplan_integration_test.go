//go:build integration

package yardplan

import (
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/yardplan/yardtest"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// The clock of yardtest is 2027-06-02 (a Wednesday); the week is 2027-05-31..2027-06-06.

type penv struct {
	*yardtest.Env
	s     *Service
	paint string
	mech  string
}

func newPenv(t *testing.T, db *store.DB) *penv {
	t.Helper()
	e := yardtest.New(t, db)
	p := &penv{Env: e, s: NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})}
	p.paint = e.Qualification(t, "PAINT", false)
	p.mech = e.Qualification(t, "MECH", true)
	return p
}

func d(s string) calendar.Date { return calendar.MustParse(s) }

func (p *penv) employee(t *testing.T, code string, quals ...string) *EmployeeView {
	t.Helper()
	cost := int64(2200)
	e, err := p.s.CreateEmployee(fixture.Ctx(), p.Owner, p.MarinaID, EmployeeInput{
		Code: code, FirstName: "Nome" + code, LastName: "Cognome", Department: "verniciatura", HourlyCostCents: &cost,
		QualificationIDs: quals,
	})
	if err != nil {
		t.Fatalf("employee %s: %v", code, err)
	}
	return e
}

func TestYardPlan_EmployeesCRUD(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		p := newPenv(t, db)
		ctx := fixture.Ctx()
		e := p.employee(t, "E01", p.paint)
		if e.Week != [7]int{480, 480, 480, 480, 480, 0, 0} || len(e.QualificationIDs) != 1 || e.Row.Version != 1 {
			t.Fatalf("employee %+v", e)
		}
		if _, err := p.s.CreateEmployee(ctx, p.Owner, p.MarinaID, EmployeeInput{Code: "E01", FirstName: "A", LastName: "B"}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, err := p.s.CreateEmployee(ctx, p.Owner, p.MarinaID, EmployeeInput{
			Code: "bad code", FirstName: "", LastName: "B",
			QualificationIDs: []string{store.NewID()},
		}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("validation: %v", err)
		}
		// user link unique per marina
		uid := p.OwnerID
		if _, err := p.s.UpdateEmployee(ctx, p.Owner, p.MarinaID, e.Row.ID, 1, EmployeePatch{UserID: &uid}); err != nil {
			t.Fatal(err)
		}
		e2 := p.employee(t, "E02", p.mech)
		if _, err := p.s.UpdateEmployee(ctx, p.Owner, p.MarinaID, e2.Row.ID, 1, EmployeePatch{UserID: &uid}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("user taken: %v", err)
		}
		if _, err := p.s.UpdateEmployee(ctx, p.Owner, p.MarinaID, e.Row.ID, 1, EmployeePatch{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		quals := []string{p.paint, p.mech}
		got, err := p.s.UpdateEmployee(ctx, p.Owner, p.MarinaID, e.Row.ID, 2, EmployeePatch{QualificationIDs: &quals, ClearUser: true})
		if err != nil || len(got.QualificationIDs) != 2 || got.Row.UserID != "" || got.Row.Version != 3 {
			t.Fatalf("update %+v %v", got, err)
		}
		got, err = p.s.PutEmployeeCalendar(ctx, p.Owner, p.MarinaID, e.Row.ID, 3, []CalendarDay{{1, 450}, {6, 240}})
		if err != nil || got.Week != [7]int{450, 0, 0, 0, 0, 240, 0} || got.Row.Version != 4 {
			t.Fatalf("calendar %+v %v", got, err)
		}
		if _, err := p.s.PutEmployeeCalendar(ctx, p.Owner, p.MarinaID, e.Row.ID, 4, []CalendarDay{{8, 10}}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("bad weekday: %v", err)
		}
		list, _, err := p.s.ListEmployees(ctx, p.Owner, p.MarinaID, store.EmployeeFilter{QualificationID: p.mech}, "", 0)
		if err != nil || len(list) != 2 {
			t.Fatalf("list by qualification: %d %v", len(list), err)
		}
		list, next, err := p.s.ListEmployees(ctx, p.Owner, p.MarinaID, store.EmployeeFilter{}, "", 1)
		if err != nil || len(list) != 1 || list[0].Row.Code != "E01" || next == "" {
			t.Fatalf("page 1 %v %q", err, next)
		}
		list, _, err = p.s.ListEmployees(ctx, p.Owner, p.MarinaID, store.EmployeeFilter{}, next, 1)
		if err != nil || len(list) != 1 || list[0].Row.Code != "E02" {
			t.Fatalf("page 2 %v", err)
		}
		// absences
		half := 240
		a, err := p.s.CreateAbsence(ctx, p.Owner, p.MarinaID, e.Row.ID, AbsenceInput{From: d("2027-06-07"), To: d("2027-06-07"), Kind: "leave", MinutesPerDay: &half})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.s.CreateAbsence(ctx, p.Owner, p.MarinaID, e.Row.ID, AbsenceInput{From: d("2027-06-08"), To: d("2027-06-07"), Kind: "x"}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("bad absence: %v", err)
		}
		abs, err := p.s.ListAbsences(ctx, p.Owner, p.MarinaID, e.Row.ID, d("2027-06-01"), d("2027-06-30"))
		if err != nil || len(abs) != 1 {
			t.Fatalf("absences %v", err)
		}
		if err := p.s.DeleteAbsence(ctx, p.Owner, p.MarinaID, e2.Row.ID, a.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("absence of another employee: %v", err)
		}
		if err := p.s.DeleteAbsence(ctx, p.Owner, p.MarinaID, e.Row.ID, a.ID); err != nil {
			t.Fatal(err)
		}
		// cross marina and roles
		_, otherMarina, other := p.OtherOrganization(t)
		if _, err := p.s.GetEmployee(ctx, other, otherMarina, e.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina: %v", err)
		}
		if _, err := p.s.GetEmployee(ctx, other, p.MarinaID, e.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("cross marina: %v", err)
		}
		worker := p.AddUser(t, "yard@porto.test", auth.RoleYard, p.OrgID, p.MarinaID)
		if _, _, err := p.s.ListEmployees(ctx, worker, p.MarinaID, store.EmployeeFilter{}, "", 0); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard worker reads employees: %v", err)
		}
		mgr := p.AddUser(t, "mgr@porto.test", auth.RoleYardMgr, p.OrgID, p.MarinaID)
		if _, err := p.s.GetEmployee(ctx, mgr, p.MarinaID, e.Row.ID); err != nil {
			t.Fatal(err)
		}
		for _, ev := range p.Events(t, event.YardPlanChanged) {
			if containsAny(string(ev.Payload), "NomeE01", "Cognome") {
				t.Fatalf("PII in event %s", ev.Payload)
			}
		}
	})
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}

func TestYardPlan_AssignmentRules(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		p := newPenv(t, db)
		ctx := fixture.Ctx()
		painter := p.employee(t, "P1", p.paint)
		mech := p.employee(t, "M1", p.mech)
		o := p.WorkOrder(t, store.WorkOrderPlanned, d("2027-06-11"),
			yardtest.TaskSpec{Title: "Carteggiatura", QualificationID: p.paint, EstHoursMilli: 12000},
			yardtest.TaskSpec{Title: "Antivegetativa", QualificationID: p.paint, EstHoursMilli: 6000, DependsOn: []int{0}},
		)
		sand, paint := o.Tasks[0], o.Tasks[1]
		ch := func(task, emp, date string, h qty.Milli) AssignmentChange {
			return AssignmentChange{TaskID: task, EmployeeID: emp, Date: d(date), Hours: h, TaskVersion: p.TaskVersion(t, task)}
		}
		// qualification mismatch, then forced (audited)
		_, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(sand, mech.Row.ID, "2027-06-03", 4000)}})
		if !errors.Is(err, ErrQualificationMismatch) {
			t.Fatalf("mismatch: %v", err)
		}
		res, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{
			Changes: []AssignmentChange{ch(sand, mech.Row.ID, "2027-06-03", 4000)}, ForceQualification: true,
		})
		if err != nil || len(res.Assignments) != 1 || !res.Assignments[0].Row.ForcedQualification || res.Tasks[0].Version != 2 {
			t.Fatalf("forced %+v %v", res, err)
		}
		// stale task version
		stale := ch(sand, painter.Row.ID, "2027-06-03", 4000)
		stale.TaskVersion = 1
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{stale}}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		// capacity: 8 h on Thursday, 9 h refused with details, allowed with overload
		_, err = p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(sand, painter.Row.ID, "2027-06-03", 9000)}})
		var ce *CapacityError
		if !errors.As(err, &ce) || ce.Capacity != 8000 || ce.Assigned != 9000 || ce.EmployeeID != painter.Row.ID {
			t.Fatalf("capacity: %v", err)
		}
		res, err = p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{
			Changes: []AssignmentChange{ch(sand, painter.Row.ID, "2027-06-03", 9000)}, AllowOverload: true,
		})
		if err != nil || len(res.Assignments) != 2 {
			t.Fatalf("overload %+v %v", res, err)
		}
		var over bool
		for _, a := range res.Assignments {
			over = over || (a.Row.EmployeeID == painter.Row.ID && a.Row.Overload)
		}
		if !over {
			t.Fatal("overload flag")
		}
		// reducing an overloaded day is always accepted
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(sand, painter.Row.ID, "2027-06-03", 8500)}}); err != nil {
			t.Fatalf("reduce overload: %v", err)
		}
		// FS: painting before the last sanding day is refused, the same day and later accepted
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(paint, painter.Row.ID, "2027-06-02", 2000)}}); !errors.Is(err, ErrDependencyViolation) {
			t.Fatalf("dependency: %v", err)
		}
		res, err = p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(paint, painter.Row.ID, "2027-06-04", 6000)}})
		if err != nil {
			t.Fatal(err)
		}
		// moving the predecessor after the successor is refused too
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(sand, painter.Row.ID, "2027-06-07", 2000)}}); !errors.Is(err, ErrDependencyViolation) {
			t.Fatalf("predecessor after successor: %v", err)
		}
		// planner grid
		pv, err := p.s.GetPlanner(ctx, p.Owner, p.MarinaID, calendar.Date{}, calendar.Date{}, "")
		if err != nil {
			t.Fatal(err)
		}
		if pv.From.String() != "2027-05-31" || pv.To.String() != "2027-06-06" || len(pv.Rows) != 2 || len(pv.Assignments) != 3 || len(pv.Tasks) != 2 {
			t.Fatalf("planner %+v", pv)
		}
		for _, r := range pv.Rows {
			if r.Employee.ID == painter.Row.ID {
				thu := r.Days[3]
				if thu.Cell.Capacity != 8000 || thu.Cell.Assigned != 8500 || !thu.Cell.Overload || *thu.Cell.LoadBP != 10625 {
					t.Fatalf("thursday %+v", thu)
				}
			}
		}
		for _, tk := range pv.Tasks {
			if tk.Task.ID == sand && (tk.Remaining != 12000 || tk.Assigned != 12500 || tk.Number != "C2027-000001") {
				t.Fatalf("tray %+v", tk)
			}
			if tk.Task.ID == paint && (len(tk.DependsOn) != 1 || tk.DependsOn[0] != sand) {
				t.Fatalf("deps %+v", tk)
			}
		}
		// delete with If-Match; a deleted task assignment refreshes the task cache
		var a AssignmentView
		for _, x := range res.Assignments {
			if x.Row.TaskID == paint {
				a = x
			}
		}
		if err := p.s.DeleteAssignment(ctx, p.Owner, p.MarinaID, a.Row.ID, 9); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("delete stale: %v", err)
		}
		if err := p.s.DeleteAssignment(ctx, p.Owner, p.MarinaID, a.Row.ID, int64(a.Row.Version)); err != nil {
			t.Fatal(err)
		}
		var wt store.WorkTask
		if err := db.NewSelect().Model(&wt).Where("id = ?", paint).Scan(ctx); err != nil || !wt.PlannedFrom.IsZero() {
			t.Fatalf("task cache %+v %v", wt, err)
		}
		if err := db.NewSelect().Model(&wt).Where("id = ?", sand).Scan(ctx); err != nil || wt.PlannedFrom.String() != "2027-06-03" {
			t.Fatalf("task cache %+v %v", wt, err)
		}
		// closed order refuses
		closed := p.WorkOrder(t, store.WorkOrderCancelled, calendar.Date{}, yardtest.TaskSpec{Title: "X", EstHoursMilli: 1000})
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{ch(closed.Tasks[0], painter.Row.ID, "2027-06-03", 1000)}}); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed: %v", err)
		}
		if n := p.AuditCount(t, "task.assignments"); n < 4 {
			t.Fatalf("audit rows %d", n)
		}
	})
}

func TestYardPlan_LoadAtRiskAndProposal(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		p := newPenv(t, db)
		ctx := fixture.Ctx()
		p1 := p.employee(t, "P1", p.paint)
		p2 := p.employee(t, "P2", p.paint)
		m1 := p.employee(t, "M1", p.mech)
		// P2 absent all week, M1 half day on Thursday.
		if _, err := p.s.CreateAbsence(ctx, p.Owner, p.MarinaID, p2.Row.ID, AbsenceInput{From: d("2027-05-31"), To: d("2027-06-06"), Kind: "vacation"}); err != nil {
			t.Fatal(err)
		}
		half := 240
		if _, err := p.s.CreateAbsence(ctx, p.Owner, p.MarinaID, m1.Row.ID, AbsenceInput{From: d("2027-06-03"), To: d("2027-06-03"), Kind: "leave", MinutesPerDay: &half}); err != nil {
			t.Fatal(err)
		}
		late := p.WorkOrder(t, store.WorkOrderInProgress, d("2027-06-01"), yardtest.TaskSpec{Title: "Lucidatura", QualificationID: p.paint, EstHoursMilli: 2000})
		tight := p.WorkOrder(t, store.WorkOrderPlanned, d("2027-06-03"),
			yardtest.TaskSpec{Title: "Carena", QualificationID: p.paint, EstHoursMilli: 40000, ProgressBP: 2500},
			yardtest.TaskSpec{Title: "Motore", QualificationID: p.mech, EstHoursMilli: 4000})
		_ = late
		v, err := p.s.GetAtRiskOrders(ctx, p.Owner, p.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Items) != 2 || !v.Items[0].Late || v.Items[1].WorkOrderID != tight.ID {
			t.Fatalf("at risk %+v", v.Items)
		}
		// tight: 30 h of paint remaining, P1 free Wed+Thu = 16 h -> risk; mech 4 h, M1 12 h -> no risk.
		r := v.Items[1].Risks
		if len(r) != 1 || r[0].QualificationID != p.paint || r[0].Remaining != 30000 || r[0].Free != 16000 {
			t.Fatalf("risks %+v", r)
		}
		// load by department and qualification are ratios of sums
		ver := p.TaskVersion(t, tight.Tasks[0])
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: []AssignmentChange{
			{TaskID: tight.Tasks[0], EmployeeID: p1.Row.ID, Date: d("2027-06-02"), Hours: 8000, TaskVersion: ver},
			{TaskID: tight.Tasks[0], EmployeeID: p1.Row.ID, Date: d("2027-06-03"), Hours: 4000, TaskVersion: ver},
		}}); err != nil {
			t.Fatal(err)
		}
		lv, err := p.s.GetYardLoad(ctx, p.Owner, p.MarinaID, GroupQualification, "week", calendar.Date{}, calendar.Date{})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range lv.Rows {
			if row.Key == p.paint {
				// P1 40 h + P2 0 h capacity, 12 h assigned
				if row.Label != "PAINT" || row.Total.Capacity != 40000 || row.Total.Assigned != 12000 || *row.Total.LoadBP != 3000 || len(row.Cells) != 1 {
					t.Fatalf("paint row %+v", row)
				}
			}
			if row.Key == p.mech && row.Total.Capacity != 36000 {
				t.Fatalf("mech row %+v", row)
			}
		}
		bays, err := p.s.GetYardLoad(ctx, p.Owner, p.MarinaID, GroupBay, "day", d("2027-06-01"), d("2027-06-05"))
		if err != nil || len(bays.Rows) != 1 || bays.Rows[0].Key != NoBay || bays.Rows[0].Total.LoadBP != nil || bays.Rows[0].Total.Assigned != 12000 {
			t.Fatalf("bays %+v %v", bays, err)
		}
		if _, err := p.s.GetYardLoad(ctx, p.Owner, p.MarinaID, "x", "", calendar.Date{}, calendar.Date{}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("bad group: %v", err)
		}
		// proposal from tomorrow (Thursday): never above capacity, deterministic, confirmable as is
		prop, err := p.s.ProposeYardPlan(ctx, p.Owner, p.MarinaID, ProposalInput{HorizonDays: 10})
		if err != nil {
			t.Fatal(err)
		}
		if prop.From.String() != "2027-06-03" || len(prop.Changes) == 0 {
			t.Fatalf("proposal %+v", prop)
		}
		again, _ := p.s.ProposeYardPlan(ctx, p.Owner, p.MarinaID, ProposalInput{HorizonDays: 10})
		if len(again.Changes) != len(prop.Changes) {
			t.Fatal("not deterministic")
		}
		for _, c := range prop.Changes {
			if c.TaskID == tight.Tasks[0] && c.Date.String() == "2027-06-03" && (c.EmployeeID != p1.Row.ID || c.Hours != 6000) {
				t.Fatalf("thursday: 4 h assigned + 2 h left after the late order: %+v", c)
			}
		}
		if _, err := p.s.PutAssignments(ctx, p.Owner, p.MarinaID, BatchInput{Changes: prop.Changes}); err != nil {
			t.Fatalf("confirm proposal: %v", err)
		}
		next, err := p.s.ProposeYardPlan(ctx, p.Owner, p.MarinaID, ProposalInput{HorizonDays: 10})
		if err != nil || len(next.Changes) != 0 {
			t.Fatalf("after confirm nothing left: %+v %v", next, err)
		}
		if _, err := p.s.ProposeYardPlan(ctx, p.Owner, p.MarinaID, ProposalInput{HorizonDays: 91}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("horizon: %v", err)
		}
	})
}
