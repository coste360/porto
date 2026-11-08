package auto

import (
	"fmt"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/yardplan/load"
	"github.com/coste360/porto/internal/qty"
)

var monday = calendar.MustParse("2026-06-01")

func emp(id string, quals ...string) load.Employee {
	return load.Employee{ID: id, Qualifications: quals, WeekMinutes: [7]int{480, 480, 480, 480, 480, 0, 0}}
}

func TestPropose_GreedyLowestLoadAndFS(t *testing.T) {
	in := Input{
		From: monday, HorizonDays: 14,
		Employees: []load.Employee{emp("e2", "PAINT"), emp("e1", "PAINT"), emp("e3", "MECH")},
		Existing:  []load.Assignment{{EmployeeID: "e1", TaskID: "x", WorkOrderID: "o9", Date: monday, Hours: 4000}},
		Tasks: []Task{
			{
				ID: "t2", WorkOrderID: "o1", OrderNumber: "C2026-000001", PlannedDelivery: monday.AddDays(5), Seq: 2,
				QualificationID: "PAINT", Remaining: 6000, DependsOn: []string{"t1"},
			},
			{
				ID: "t1", WorkOrderID: "o1", OrderNumber: "C2026-000001", PlannedDelivery: monday.AddDays(5), Seq: 1,
				QualificationID: "PAINT", Remaining: 10000,
			},
		},
	}
	p, err := Propose(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{TaskID: "t1", EmployeeID: "e2", Date: monday, Hours: 8000},            // e1 already 50 % loaded
		{TaskID: "t1", EmployeeID: "e1", Date: monday.AddDays(1), Hours: 2000}, // both empty: lowest id
		{TaskID: "t2", EmployeeID: "e1", Date: monday.AddDays(2), Hours: 6000}, // after t1's last day
	}
	if !reflect.DeepEqual(p.Changes, want) || len(p.Unplaced) != 0 {
		t.Fatalf("proposal %+v", p)
	}
}

func TestPropose_StepsWeekendAndUnplaced(t *testing.T) {
	e := emp("e1", "Q")
	e.WeekMinutes[0] = 450 // Monday 7.5 h
	e.WeekMinutes[1] = 100 // Tuesday 1.667 h: the 0.5 h step leaves 1.5 h
	in := Input{From: monday, HorizonDays: 3, Employees: []load.Employee{e}, Tasks: []Task{
		{ID: "t", WorkOrderID: "o", Seq: 1, QualificationID: "Q", Remaining: 20000},
		{ID: "u", WorkOrderID: "o", Seq: 2, QualificationID: "Q", Remaining: 1000, DependsOn: []string{"t"}},
		{ID: "v", WorkOrderID: "p", Seq: 1, QualificationID: "NONE", Remaining: 1000},
	}}
	p, err := Propose(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{TaskID: "t", EmployeeID: "e1", Date: monday, Hours: 7500},
		{TaskID: "t", EmployeeID: "e1", Date: monday.AddDays(1), Hours: 1500},
		{TaskID: "t", EmployeeID: "e1", Date: monday.AddDays(2), Hours: 8000},
	}
	if !reflect.DeepEqual(p.Changes, want) {
		t.Fatalf("changes %+v", p.Changes)
	}
	wantU := []Unplaced{{TaskID: "t", Remaining: 3000}, {TaskID: "u", Remaining: 1000}, {TaskID: "v", Remaining: 1000}}
	if !reflect.DeepEqual(p.Unplaced, wantU) {
		t.Fatalf("unplaced %+v", p.Unplaced)
	}
}

func TestPropose_InvalidInput(t *testing.T) {
	for _, in := range []Input{
		{From: monday},
		{From: monday, HorizonDays: 91},
		{HorizonDays: 3},
		{From: monday, HorizonDays: 3, Tasks: []Task{{ID: "t", Remaining: -1}}},
	} {
		if _, err := Propose(in); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
}

func TestRapid_ProposeInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		quals := []string{"A", "B", ""}
		ne := rapid.IntRange(1, 5).Draw(t, "employees")
		emps := make([]load.Employee, ne)
		for i := range emps {
			e := load.Employee{ID: fmt.Sprintf("e%d", i)}
			for d := range 7 {
				e.WeekMinutes[d] = rapid.SampledFrom([]int{0, 240, 450, 480, 600}).Draw(t, "min")
			}
			if rapid.Bool().Draw(t, "qa") {
				e.Qualifications = append(e.Qualifications, "A")
			}
			if rapid.Bool().Draw(t, "qb") {
				e.Qualifications = append(e.Qualifications, "B")
			}
			if rapid.Bool().Draw(t, "abs") {
				off := rapid.IntRange(0, 10).Draw(t, "aoff")
				e.Absences = []load.Absence{{From: monday.AddDays(off), To: monday.AddDays(off + 1)}}
			}
			emps[i] = e
		}
		nt := rapid.IntRange(0, 12).Draw(t, "tasks")
		tasks := make([]Task, nt)
		for i := range tasks {
			order := rapid.IntRange(0, 3).Draw(t, "order")
			tasks[i] = Task{
				ID: fmt.Sprintf("t%02d", i), WorkOrderID: fmt.Sprintf("o%d", order), OrderNumber: fmt.Sprintf("N%d", order),
				Seq: i + 1, QualificationID: rapid.SampledFrom(quals).Draw(t, "q"),
				Remaining:       qty.Milli(rapid.Int64Range(0, 40).Draw(t, "rem") * 250),
				PlannedDelivery: monday.AddDays(rapid.IntRange(0, 20).Draw(t, "del")),
			}
			if i > 0 && rapid.Bool().Draw(t, "dep") {
				j := rapid.IntRange(0, i-1).Draw(t, "depOn")
				if tasks[j].WorkOrderID == tasks[i].WorkOrderID {
					tasks[i].DependsOn = []string{tasks[j].ID}
				}
			}
		}
		var existing []load.Assignment
		for range rapid.IntRange(0, 5).Draw(t, "existing") {
			existing = append(existing, load.Assignment{
				EmployeeID: emps[rapid.IntRange(0, ne-1).Draw(t, "xe")].ID, TaskID: "x", WorkOrderID: "ox",
				Date: monday.AddDays(rapid.IntRange(0, 10).Draw(t, "xd")), Hours: qty.Milli(rapid.Int64Range(1, 8).Draw(t, "xh") * 1000),
			})
		}
		in := Input{From: monday, HorizonDays: rapid.IntRange(1, 21).Draw(t, "h"), Tasks: tasks, Employees: emps, Existing: existing}
		p, err := Propose(in)
		if err != nil {
			t.Fatal(err)
		}
		p2, _ := Propose(in)
		if !reflect.DeepEqual(p, p2) {
			t.Fatal("not deterministic")
		}
		byID := map[string]load.Employee{}
		for _, e := range emps {
			byID[e.ID] = e
		}
		taskOf := map[string]Task{}
		for _, tk := range tasks {
			taskOf[tk.ID] = tk
		}
		perDay := map[string]qty.Milli{}
		proposedBefore := map[string]qty.Milli{}
		for _, a := range existing {
			proposedBefore[a.EmployeeID+a.Date.String()] += a.Hours
		}
		placed := map[string]qty.Milli{}
		first, lastD := map[string]calendar.Date{}, map[string]calendar.Date{}
		for _, c := range p.Changes {
			tk := taskOf[c.TaskID]
			if !byID[c.EmployeeID].Holds(tk.QualificationID) {
				t.Fatalf("qualification violated %+v", c)
			}
			if c.Date.Before(in.From) || c.Date.After(in.From.AddDays(in.HorizonDays-1)) {
				t.Fatalf("outside horizon %+v", c)
			}
			perDay[c.EmployeeID+c.Date.String()] += c.Hours
			placed[c.TaskID] += c.Hours
			if f, ok := first[c.TaskID]; !ok || c.Date.Before(f) {
				first[c.TaskID] = c.Date
			}
			if l, ok := lastD[c.TaskID]; !ok || c.Date.After(l) {
				lastD[c.TaskID] = c.Date
			}
		}
		for k, h := range perDay {
			for _, e := range emps {
				for d := range in.HorizonDays {
					day := in.From.AddDays(d)
					if e.ID+day.String() == k {
						cp, _ := load.Capacity(e, day)
						if h+proposedBefore[k] > cp && h > 0 {
							t.Fatalf("capacity exceeded %s: %v + %v > %v", k, h, proposedBefore[k], cp)
						}
					}
				}
			}
		}
		unplaced := map[string]qty.Milli{}
		for _, u := range p.Unplaced {
			unplaced[u.TaskID] = u.Remaining
		}
		for _, tk := range tasks {
			if placed[tk.ID]+unplaced[tk.ID] != tk.Remaining {
				t.Fatalf("task %s: placed %v + unplaced %v != %v", tk.ID, placed[tk.ID], unplaced[tk.ID], tk.Remaining)
			}
			for _, dep := range tk.DependsOn {
				if f, ok := first[tk.ID]; ok {
					if l, ok := lastD[dep]; ok && !f.After(l) {
						t.Fatalf("FS violated: %s starts %v, %s ends %v", tk.ID, f, dep, l)
					}
				}
			}
		}
	})
}
