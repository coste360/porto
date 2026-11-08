//go:build integration

package maintenance

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/maintenance/due"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// menv is the backend of the maintenance tests: contracttest's harbour (berths A1..A4, parkings P1, P2) and the
// maintenance service. The clock is 2027-06-02.
type menv struct {
	*contracttest.Env
	s     *Service
	scope store.MarinaScope
}

func ctxT() context.Context { return fixture.Ctx() }

func newMenv(t *testing.T, db *store.DB) *menv {
	t.Helper()
	e := contracttest.New(t, db)
	return &menv{Env: e, scope: e.Scope(t), s: NewService(Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Keys: e.Keys, Files: e.Files, Occupancy: e.Occupancy,
	})}
}

func d(s string) calendar.Date { return calendar.MustParse(s) }

func (m *menv) task(t *testing.T, el Element, outOfService bool) *TaskView {
	t.Helper()
	v, err := m.s.CreateTask(ctxT(), m.Owner, m.MarinaID, TaskInput{Element: &el, Title: "Verifica", OutOfService: outOfService})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return v
}

func (m *menv) invariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckUtilityInvariants(ctxT(), m.DB.DB, m.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	occ, err := store.CheckOccupancyInvariants(ctxT(), m.DB.DB, m.MarinaID, calendar.In(m.Clock.Now(), time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range append(vs, occ...) {
		t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func TestMaintenance_ScheduleJobIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		start := d("2027-01-15")
		p, err := m.s.CreatePlan(ctx, m.Owner, m.MarinaID, PlanInput{
			Name: "Ispezione subacquea", Intervention: "underwater_inspection", LeadDays: 30,
			Targets:     []Element{{Type: store.ElementBerth, ID: m.Berths[0]}, {Type: store.ElementBerth, ID: m.Berths[1]}},
			Periodicity: due.Periodicity{Kind: due.KindMonths, Every: 6, StartsOn: &start},
			Checklist:   []ChecklistEntry{{Key: "catena", Label: "Catena"}, {Key: "grillo", Label: "Grillo"}},
		})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		// Due 2027-07-15, lead 30 days: due from 2027-06-15. On 2027-06-02 nothing.
		if n, err := m.s.ScheduleMarina(ctx, m.scope); err != nil || n != 0 {
			t.Fatalf("early run: %d %v", n, err)
		}
		m.Clock.Set(time.Date(2027, 6, 20, 8, 0, 0, 0, time.UTC))
		for i := range 3 {
			if err := m.s.RunSchedule(ctx); err != nil {
				t.Fatal(err)
			}
			tasks, _, err := m.s.ListTasks(ctx, m.Owner, m.MarinaID, TaskFilter{PlanID: p.Row.ID})
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 2 {
				t.Fatalf("run %d: %d tasks", i, len(tasks))
			}
			for _, tk := range tasks {
				if tk.Row.DueOn.String() != "2027-07-15" || len(ChecklistOf(tk.Row.Checklist)) != 2 || tk.Row.Source != store.TaskSourcePlan {
					t.Fatalf("task %+v", tk.Row)
				}
			}
		}
		if n := len(m.Events(t, event.MaintenanceDue)); n != 2 {
			t.Fatalf("%d maintenance.due events", n)
		}
		var dl []store.Deadline
		if err := db.NewSelect().Model(&dl).Where("marina_id = ?", m.MarinaID).Where("source_type = ?", store.DeadlineSourceMaintenanceTask).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if len(dl) != 2 || dl[0].Kind != DeadlineKind || dl[0].Status != "open" {
			t.Fatalf("deadlines %+v", dl)
		}
		// Done: the deadline closes and the next task (2028-01-15 from the done date + 6 months) is not due yet.
		tasks, _, _ := m.s.ListTasks(ctx, m.Owner, m.MarinaID, TaskFilter{PlanID: p.Row.ID})
		done, err := m.s.Complete(ctx, m.Owner, m.MarinaID, tasks[0].Row.ID, CompleteInput{
			Outcome: "ok", Checklist: []ChecklistResult{{Key: "catena", Done: true}}, ClientRef: "c1",
		})
		if err != nil || done.Row.Status != store.TaskDone {
			t.Fatalf("complete: %v", err)
		}
		if n, err := m.s.ScheduleMarina(ctx, m.scope); err != nil || n != 0 {
			t.Fatalf("after done: %d %v", n, err)
		}
		dls, _ := store.DeadlinesOfSource(ctx, db.DB, m.scope, store.DeadlineSourceMaintenanceTask, done.Row.ID)
		if len(dls) != 1 || dls[0].Status != "done" {
			t.Fatalf("deadline after done %+v", dls)
		}
		m.invariants(t)
	})
}

func TestMaintenance_CompleteOfflineReplay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		tk := m.task(t, Element{Type: store.ElementPedestal, ID: m.pedestal(t)}, false)
		yard := m.AddUser(t, "yard@porto.test", auth.RoleYard, m.OrgID, m.MarinaID)
		in := CompleteInput{Outcome: "fixed", Notes: "sostituito differenziale", ClientRef: "op-9"}
		a, err := m.s.Complete(ctx, yard, m.MarinaID, tk.Row.ID, in)
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
		b, err := m.s.Complete(ctx, yard, m.MarinaID, tk.Row.ID, in)
		if err != nil || b.Row.Version != a.Row.Version {
			t.Fatalf("replay: %v (%d vs %d)", err, b.Row.Version, a.Row.Version)
		}
		in.ClientRef = "op-10"
		if _, err := m.s.Complete(ctx, m.Owner, m.MarinaID, tk.Row.ID, in); !errors.Is(err, ErrAlreadyDone) {
			t.Fatalf("other completion: %v", err)
		}
		if _, err := m.s.Cancel(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(b.Row.Version), "x"); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("cancel done: %v", err)
		}
		if n := m.AuditCount(t, "maintenance_task.complete"); n != 1 {
			t.Fatalf("%d completions audited", n)
		}
		// A task assigned to someone else is not the yard user's.
		other := m.task(t, Element{Type: store.ElementBerth, ID: m.Berths[0]}, false)
		uid := m.OwnerID
		if _, err := m.s.UpdateTask(ctx, m.Owner, m.MarinaID, other.Row.ID, int64(other.Row.Version), TaskPatch{AssigneeUserID: &uid}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.s.Complete(ctx, yard, m.MarinaID, other.Row.ID, in); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard completes another's task: %v", err)
		}
	})
}

func (m *menv) pedestal(t *testing.T) string {
	t.Helper()
	now := store.UTC(m.Clock.Now())
	p := &store.Pedestal{
		ID: store.NewID(), MarinaID: m.MarinaID, MapID: m.MapID, Code: "C1", XCm: 100, YCm: 100, Sockets: store.JSON(`[]`),
		Meters: store.JSON(`[]`), CreatedAt: now, UpdatedAt: now,
	}
	m.Insert(t, p)
	return p.ID
}

func TestMaintenance_ReportNeverOccupies(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		rental := m.AddUser(t, "rental@porto.test", auth.RoleRental, m.OrgID, m.MarinaID)
		x, y := int64(1200), int64(800)
		in := ReportInput{ClientRef: "rep-1", MapID: m.MapID, X: &x, Y: &y, Title: "Bitta allentata", Priority: "high"}
		a, err := m.s.Report(ctx, rental, m.MarinaID, in)
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		b, err := m.s.Report(ctx, rental, m.MarinaID, in)
		if err != nil || a.Row.ID != b.Row.ID {
			t.Fatalf("replay: %v", err)
		}
		if a.Row.Status != store.TaskOpen || a.Row.Source != store.TaskSourceReport || len(AllocationIDsOf(&a.Row)) != 0 {
			t.Fatalf("report task %+v", a.Row)
		}
		n, err := db.NewSelect().TableExpr("allocations").Where("marina_id = ?", m.MarinaID).Count(ctx)
		if err != nil || n != 0 {
			t.Fatalf("%d allocations", n)
		}
		if _, err := m.s.Report(ctx, rental, m.MarinaID, ReportInput{ClientRef: "rep-2", Title: "x"}); err == nil {
			t.Fatal("report without element nor map accepted")
		}
		if _, err := m.s.CreateTask(ctx, rental, m.MarinaID, TaskInput{Title: "x", MapID: m.MapID}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("rental creates office task: %v", err)
		}
		wl, day, err := m.s.Worklist(ctx, rental, m.MarinaID)
		if err != nil || day.IsZero() {
			t.Fatalf("worklist: %v", err)
		}
		_ = wl
	})
}

func TestMaintenance_OccupancyConflicts(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		tr := m.Transit(t, m.Berths[0], "2027-06-04", "2027-06-07", true)
		tk := m.task(t, Element{Type: store.ElementBerth, ID: m.Berths[0]}, true)
		_, err := m.s.Schedule(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(tk.Row.Version), ScheduleInput{From: d("2027-06-03"), To: d("2027-06-05")})
		var ce *ConflictsError
		if !errors.As(err, &ce) || len(ce.Conflicts) != 1 {
			t.Fatalf("schedule over a transit: %v", err)
		}
		c := ce.Conflicts[0]
		if c.ContractID != tr.Row.ID || c.From.String() != "2027-06-04" || c.To.String() != "2027-06-05" || c.VesselID == "" {
			t.Fatalf("conflict %+v", c)
		}
		if len(c.Alternatives) == 0 || len(c.Alternatives) > 3 {
			t.Fatalf("%d alternatives", len(c.Alternatives))
		}
		after, _ := m.s.GetTask(ctx, m.Owner, m.MarinaID, tk.Row.ID)
		if after.Row.Version != tk.Row.Version || after.Row.Status != store.TaskOpen {
			t.Fatal("a failed schedule wrote the task")
		}
		// Free nights: maintenance allocation booked, berth not sellable on them.
		v, err := m.s.Schedule(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(tk.Row.Version), ScheduleInput{From: d("2027-06-08"), To: d("2027-06-09")})
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if v.Row.Status != store.TaskScheduled || len(AllocationIDsOf(&v.Row)) != 1 {
			t.Fatalf("scheduled %+v", v.Row)
		}
		m.invariants(t)
	})
}

func TestMaintenance_OutOfServiceNotSellable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		tk := m.task(t, Element{Type: store.ElementLandResource, ID: m.Lands[0]}, true)
		v, err := m.s.Schedule(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(tk.Row.Version), ScheduleInput{From: d("2027-06-02"), To: d("2027-06-04")})
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		vessel := m.AddVessel(t, "Grecale", 400, 200, "rib")
		err = m.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			_, err := m.Occupancy.AllocateTx(ctx, tx, m.scope, m.Clock.Now(), occupancy.NewAllocation{
				VesselID: &vessel, Kind: occupancy.KindTransit,
				Spec: engine.AllocSpec{ResourceType: engine.ResourceType(store.ResourceLand), ResourceID: m.Lands[0], First: d("2027-06-03"), Last: d("2027-06-03")},
			})
			return err
		})
		if !errors.Is(err, occupancy.ErrResourceTaken) {
			t.Fatalf("sold a maintenance night: %v", err)
		}
		_, _, lens, err := m.s.Map(ctx, m.Owner, m.MarinaID, m.MapID, nil)
		if err != nil || len(lens) != 1 || !lens[0].OutOfService {
			t.Fatalf("lens %+v %v", lens, err)
		}
		// Completing keeps the nights; cancelling a done task is refused; release frees them.
		v, err = m.s.Complete(ctx, m.Owner, m.MarinaID, v.Row.ID, CompleteInput{Outcome: "ok"})
		if err != nil {
			t.Fatal(err)
		}
		if n := m.Rows(t, AllocationIDsOf(&v.Row)[0]); n != 3 {
			t.Fatalf("%d rows after completion", n)
		}
		v, err = m.s.Release(ctx, m.Owner, m.MarinaID, v.Row.ID, int64(v.Row.Version), nil)
		if err != nil {
			t.Fatal(err)
		}
		if n := m.Rows(t, AllocationIDsOf(&v.Row)[0]); n != 0 {
			t.Fatalf("%d rows after release", n)
		}
		m.invariants(t)
	})
}

// TestConcurrency_MaintenanceScheduleVsAllocation: 100 goroutines race a maintenance schedule against desk
// allocations of the same berth night: exactly one wins.
func TestConcurrency_MaintenanceScheduleVsAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		tk := m.task(t, Element{Type: store.ElementBerth, ID: m.Berths[2]}, true)
		probe, err := m.s.Schedule(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(tk.Row.Version), ScheduleInput{From: d("2027-06-20"), To: d("2027-06-20")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.s.Cancel(ctx, m.Owner, m.MarinaID, probe.Row.ID, int64(probe.Row.Version), "probe"); err != nil {
			t.Fatal(err)
		}
		tasks := make([]*TaskView, 50)
		for i := range tasks {
			tasks[i] = m.task(t, Element{Type: store.ElementBerth, ID: m.Berths[2]}, true)
		}
		vessels := make([]string, 50)
		for i := range vessels {
			vessels[i] = m.AddVessel(t, "V", 800, 300, "monohull_sail")
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%2 == 0 {
					tk := tasks[i/2]
					_, err = m.s.Schedule(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(tk.Row.Version), ScheduleInput{From: d("2027-06-10"), To: d("2027-06-10")})
				} else {
					vessel := vessels[i/2]
					err = m.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
						_, err := m.Occupancy.AllocateTx(ctx, tx, m.scope, m.Clock.Now(), occupancy.NewAllocation{
							VesselID: &vessel, Kind: occupancy.KindTransit,
							Spec: engine.AllocSpec{ResourceType: engine.ResourceType(store.ResourceBerth), ResourceID: m.Berths[2], First: d("2027-06-10"), Last: d("2027-06-10")},
						})
						return err
					})
				}
				if err == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("%d winners", wins)
		}
		m.invariants(t)
	})
}

func TestMaintenance_PlansAndEquipment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		eq, err := m.s.CreateEquipment(ctx, m.Owner, m.MarinaID, EquipmentInput{Code: "TL-1", Name: "Travel lift 50 t", Kind: "travel_lift"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.s.CreateEquipment(ctx, m.Owner, m.MarinaID, EquipmentInput{Code: "TL-1", Name: "Altro", Kind: "crane"}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("duplicate code: %v", err)
		}
		x := int64(10)
		if _, err := m.s.CreateEquipment(ctx, m.Owner, m.MarinaID, EquipmentInput{Code: "F", Name: "F", Kind: "forklift", X: &x, Y: &x}); err == nil {
			t.Fatal("position without map accepted")
		}
		name := "Travel lift 70 t"
		up, err := m.s.UpdateEquipment(ctx, m.Owner, m.MarinaID, eq.Row.ID, int64(eq.Row.Version), EquipmentPatch{Name: &name})
		if err != nil || up.Row.Name != name || up.Row.Version != 2 {
			t.Fatalf("update: %v", err)
		}
		if _, err := m.s.CreatePlan(ctx, m.Owner, m.MarinaID, PlanInput{
			Name: "Custom", Intervention: "custom", Targets: []Element{{Type: store.ElementEquipment, ID: eq.Row.ID}},
			Periodicity: due.Periodicity{Kind: due.KindFixedDate, MonthDay: "03-01"},
		}); err == nil {
			t.Fatal("custom plan without label accepted")
		}
		if _, err := m.s.CreatePlan(ctx, m.Owner, m.MarinaID, PlanInput{
			Name: "Foreign", Intervention: "general", Targets: []Element{{Type: store.ElementBerth, ID: store.NewID()}},
			Periodicity: due.Periodicity{Kind: due.KindMonths, Every: 1},
		}); !errors.Is(err, ErrElementInvalid) {
			t.Fatalf("unknown element: %v", err)
		}
		p, err := m.s.CreatePlan(ctx, m.Owner, m.MarinaID, PlanInput{
			Name: "Collaudo annuale", Intervention: "general", Targets: []Element{{Type: store.ElementEquipment, ID: eq.Row.ID}},
			Periodicity: due.Periodicity{Kind: due.KindFixedDate, MonthDay: "03-01"}, Nights: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		lead := 60
		up2, err := m.s.UpdatePlan(ctx, m.Owner, m.MarinaID, p.Row.ID, int64(p.Row.Version), PlanInput{LeadDays: lead}, map[string]bool{"lead_days": true})
		if err != nil || up2.Row.LeadDays != 60 || up2.Row.Name != "Collaudo annuale" || len(up2.Targets) != 1 {
			t.Fatalf("update plan: %v %+v", err, up2)
		}
		list, _, err := m.s.ListPlans(ctx, m.Owner, m.MarinaID, nil, store.ElementEquipment, "", 0)
		if err != nil || len(list) != 1 {
			t.Fatalf("plans by element type %d %v", len(list), err)
		}
		_, otherMarina, other := m.OtherOrganization(t)
		if _, err := m.s.GetPlan(ctx, other, otherMarina, p.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign plan: %v", err)
		}
	})
}

// TestMaintenance_RescheduleKeepsPastNights: a reschedule replaces the nights from today on; the nights already
// held stay on the task's allocation.
func TestMaintenance_RescheduleKeepsPastNights(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		ctx := ctxT()
		tk := m.task(t, Element{Type: store.ElementLandResource, ID: m.Lands[0]}, true)
		v, err := m.s.Schedule(ctx, m.Owner, m.MarinaID, tk.Row.ID, int64(tk.Row.Version), ScheduleInput{From: d("2027-06-02"), To: d("2027-06-04")})
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		first := AllocationIDsOf(&v.Row)[0]
		m.Clock.Set(m.Clock.Now().AddDate(0, 0, 2)) // 2027-06-04
		v, err = m.s.Schedule(ctx, m.Owner, m.MarinaID, v.Row.ID, int64(v.Row.Version), ScheduleInput{From: d("2027-06-05"), To: d("2027-06-06")})
		if err != nil {
			t.Fatalf("reschedule: %v", err)
		}
		if n := m.Rows(t, first); n != 2 {
			t.Fatalf("past nights of the first schedule: %d rows, want 2 (06-02, 06-03)", n)
		}
		ids := AllocationIDsOf(&v.Row)
		if len(ids) != 2 || m.Rows(t, ids[1]) != 2 {
			t.Fatalf("allocations %v", ids)
		}
		m.invariants(t)
	})
}

// TestConcurrency_OfflineReplay_MaintenanceReport (INV-83): 100 parallel replays of one phone report (same
// client_ref): one task, the same task in every response, one audit row and one event.
func TestConcurrency_OfflineReplay_MaintenanceReport(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		rental := m.AddUser(t, "rental@porto.test", auth.RoleRental, m.OrgID, m.MarinaID)
		x, y := int64(300), int64(400)
		in := ReportInput{ClientRef: "rep-par", MapID: m.MapID, X: &x, Y: &y, Title: "Cima sfilacciata", Priority: "normal"}
		before := len(m.Events(t, event.MaintenanceStatusChanged))
		ids := replay100(t, func() (string, int, error) {
			v, err := m.s.Report(ctxT(), rental, m.MarinaID, in)
			if err != nil {
				return "", 0, err
			}
			return v.Row.ID, v.Row.Version, nil
		})
		if len(ids) != 1 {
			t.Fatalf("responses name %d tasks", len(ids))
		}
		n, err := db.NewSelect().TableExpr("maintenance_tasks").Where("marina_id = ?", m.MarinaID).Count(ctxT())
		if err != nil || n != 1 {
			t.Fatalf("%d tasks (%v)", n, err)
		}
		if a := m.AuditCount(t, "maintenance_task.report"); a != 1 {
			t.Fatalf("%d audit rows", a)
		}
		if ev := len(m.Events(t, event.MaintenanceStatusChanged)) - before; ev != 1 {
			t.Fatalf("%d events", ev)
		}
	})
}

// TestConcurrency_OfflineReplay_MaintenanceOutcome (INV-83): 100 parallel replays of one queued outcome (same
// client_ref): done once, the same version in every response, one audit row and one event.
func TestConcurrency_OfflineReplay_MaintenanceOutcome(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		m := newMenv(t, db)
		tk := m.task(t, Element{Type: store.ElementPedestal, ID: m.pedestal(t)}, false)
		yard := m.AddUser(t, "yard@porto.test", auth.RoleYard, m.OrgID, m.MarinaID)
		in := CompleteInput{Outcome: "ok", ClientRef: "out-par"}
		before := len(m.Events(t, event.MaintenanceStatusChanged))
		ids := replay100(t, func() (string, int, error) {
			v, err := m.s.Complete(ctxT(), yard, m.MarinaID, tk.Row.ID, in)
			if err != nil {
				return "", 0, err
			}
			if v.Row.Status != store.TaskDone {
				return "", 0, errors.New("not done: " + v.Row.Status)
			}
			return v.Row.ID, v.Row.Version, nil
		})
		if len(ids) != 1 {
			t.Fatalf("responses differ: %v", ids)
		}
		if a := m.AuditCount(t, "maintenance_task.complete"); a != 1 {
			t.Fatalf("%d audit rows", a)
		}
		if ev := len(m.Events(t, event.MaintenanceStatusChanged)) - before; ev != 1 {
			t.Fatalf("%d events", ev)
		}
	})
}

// replay100 runs op from 100 goroutines and returns the distinct (id, version) responses; any error fails.
func replay100(t *testing.T, op func() (string, int, error)) map[string]int {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := map[string]int{}
	var errs []error
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, v, err := op()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			out[id+"@"+strconv.Itoa(v)]++
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("replay: %v", errs[0])
	}
	return out
}
