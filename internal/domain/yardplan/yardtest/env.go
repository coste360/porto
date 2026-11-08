// Package yardtest builds the yard rows the planner and lifting tests need without the catalogue and work order
// services: labour items, qualifications, work orders with tasks and dependencies (inserted as rows), on top of
// contracttest's harbour. Test support only (PLANNER, m10-plan §16.4).
package yardtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
)

// Env is contracttest's backend plus helpers for yard rows.
type Env struct {
	*contracttest.Env
	orders int64
}

// New builds the environment.
func New(t testing.TB, db *store.DB) *Env {
	t.Helper()
	return &Env{Env: contracttest.New(t, db)}
}

// Qualification creates a labour item and a yard qualification (liftOperator for lifting operators) and returns
// its id.
func (e *Env) Qualification(t testing.TB, code string, liftOperator bool) string {
	t.Helper()
	ctx := fixture.Ctx()
	classes, err := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
	if err != nil || len(classes) == 0 {
		t.Fatalf("revenue classes: %v", err)
	}
	it, err := e.Catalog.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: "LAB-" + strings.ToUpper(code), Name: catalog.I18nText{It: code}, ItemType: "labour", Unit: "hour",
		RevenueClassID: classes[0].ID,
	})
	if err != nil {
		t.Fatalf("labour item: %v", err)
	}
	now := store.UTC(e.Clock.Now())
	q := &store.YardQualification{
		ID: store.NewID(), MarinaID: e.MarinaID, Code: code, Name: store.JSON(`{"it":"` + code + `"}`), LabourItemID: it.ID,
		DefaultCostCents: 2500, LiftOperator: liftOperator, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, q)
	return q.ID
}

// TaskSpec describes a task of WorkOrder.
type TaskSpec struct {
	Title           string
	QualificationID string
	EstHoursMilli   int64
	ProgressBP      int
	Status          string // default todo
	DependsOn       []int  // indexes of earlier tasks of the same order
}

// Order is a work order with its tasks.
type Order struct {
	ID    string
	Tasks []string
}

// WorkOrder inserts a work order (status, planned delivery) for the fixture vessel and customer with its tasks.
func (e *Env) WorkOrder(t testing.TB, status string, delivery calendar.Date, tasks ...TaskSpec) Order {
	t.Helper()
	e.orders++
	now := store.UTC(e.Clock.Now())
	o := &store.WorkOrder{
		ID: store.NewID(), MarinaID: e.MarinaID, Number: e.orders, Year: 2027, CustomerID: e.Customer, VesselID: e.Vessel,
		PlannedDeliveryOn: delivery, Status: status, BillingMode: "actual", RateSnapshot: store.JSON(`{}`), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	switch status {
	case store.WorkOrderCancelled:
		o.CancelledAt = now
	case store.WorkOrderInvoiced:
		o.InvoicedAt = now
	}
	e.Insert(t, o)
	out := Order{ID: o.ID}
	for i, ts := range tasks {
		st := ts.Status
		if st == "" {
			st = store.WorkTaskTodo
		}
		if st == store.WorkTaskDone {
			ts.ProgressBP = 10000
		}
		wt := &store.WorkTask{
			ID: store.NewID(), MarinaID: e.MarinaID, WorkOrderID: o.ID, Seq: i + 1, Title: ts.Title, QualificationID: ts.QualificationID,
			EstimatedHoursMilli: ts.EstHoursMilli, Status: st, ProgressBP: ts.ProgressBP, Checklist: store.JSON(`[]`), Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		e.Insert(t, wt)
		out.Tasks = append(out.Tasks, wt.ID)
		for _, d := range ts.DependsOn {
			e.Insert(t, &store.WorkTaskDep{TaskID: wt.ID, DependsOnTaskID: out.Tasks[d], MarinaID: e.MarinaID})
		}
	}
	return out
}

// TaskVersion reads the version of a task.
func (e *Env) TaskVersion(t testing.TB, id string) int64 {
	t.Helper()
	var v int64
	if err := e.DB.NewSelect().TableExpr("work_tasks").Column("version").Where("id = ?", id).Scan(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// SetDisplacement sets the displacement of the fixture vessel.
func (e *Env) SetDisplacement(t testing.TB, kg int64) {
	t.Helper()
	if _, err := e.DB.NewUpdate().TableExpr("vessels").Set("displacement_kg = ?", kg).Where("id = ?", e.Vessel).
		Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// InsertQualification inserts a yard qualification row on an existing labour item of a marina (handler tests that
// run on the full app without the catalogue service).
func InsertQualification(t testing.TB, db *store.DB, now time.Time, marinaID, labourItemID, code string, liftOperator bool) string {
	t.Helper()
	now = store.UTC(now)
	q := &store.YardQualification{
		ID: store.NewID(), MarinaID: marinaID, Code: code, Name: store.JSON(`{"it":"` + code + `"}`), LabourItemID: labourItemID,
		LiftOperator: liftOperator, Active: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(q).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	return q.ID
}

// InsertPlannedOrder inserts a planned work order of a marina with its tasks (all with the same qualification) and
// the dependencies of TaskSpec.DependsOn.
func InsertPlannedOrder(t testing.TB, db *store.DB, now time.Time, marinaID, customerID, vesselID string, delivery calendar.Date,
	tasks ...TaskSpec,
) Order {
	t.Helper()
	ctx := context.Background()
	now = store.UTC(now)
	o := &store.WorkOrder{
		ID: store.NewID(), MarinaID: marinaID, Number: 1, Year: 2027, CustomerID: customerID, VesselID: vesselID,
		PlannedDeliveryOn: delivery, Status: store.WorkOrderPlanned, BillingMode: "actual", RateSnapshot: store.JSON(`{}`),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := db.NewInsert().Model(o).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	out := Order{ID: o.ID}
	for i, ts := range tasks {
		wt := &store.WorkTask{
			ID: store.NewID(), MarinaID: marinaID, WorkOrderID: o.ID, Seq: i + 1, Title: ts.Title, QualificationID: ts.QualificationID,
			EstimatedHoursMilli: ts.EstHoursMilli, Status: store.WorkTaskTodo, Checklist: store.JSON(`[]`), Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if _, err := db.NewInsert().Model(wt).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		out.Tasks = append(out.Tasks, wt.ID)
		for _, d := range ts.DependsOn {
			if _, err := db.NewInsert().Model(&store.WorkTaskDep{TaskID: wt.ID, DependsOnTaskID: out.Tasks[d], MarinaID: marinaID}).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	return out
}
