package load

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/qty"
)

func ptr(v int) *int { return &v }

var monday = calendar.MustParse("2026-06-01")

func emp(id string, quals ...string) Employee {
	return Employee{ID: id, Qualifications: quals, WeekMinutes: [7]int{480, 480, 480, 480, 480, 0, 0}}
}

func TestLoad_CapacityWithAbsences(t *testing.T) {
	e := emp("e1")
	e.Absences = []Absence{
		{From: monday.AddDays(1), To: monday.AddDays(1)},                    // whole Tuesday
		{From: monday.AddDays(2), To: monday.AddDays(3), Minutes: ptr(90)},  // 1.5 h Wed-Thu
		{From: monday.AddDays(3), To: monday.AddDays(3), Minutes: ptr(600)}, // Thursday: more than the day
	}
	want := map[int]qty.Milli{0: 8000, 1: 0, 2: 6500, 3: 0, 4: 8000, 5: 0, 6: 0}
	for off, w := range want {
		got, err := Capacity(e, monday.AddDays(off))
		if err != nil || got != w {
			t.Fatalf("day %d: capacity %v, %v; want %v", off, got, err, w)
		}
	}
	e.WeekMinutes[0] = 450 // 7.5 h
	if c, _ := Capacity(e, monday); c != 7500 {
		t.Fatalf("7.5 h = %v", c)
	}
	e.WeekMinutes[0] = 100 // 1.667 h half-even
	if c, _ := Capacity(e, monday); c != 1667 {
		t.Fatalf("100 min = %v", c)
	}
}

func TestLoad_CellAndOverload(t *testing.T) {
	c := NewCell(8000, 6000, 0)
	if c.LoadBP == nil || *c.LoadBP != 7500 || c.Overload {
		t.Fatalf("cell %+v", c)
	}
	c = NewCell(8000, 9000, 0)
	if *c.LoadBP != 11250 || !c.Overload {
		t.Fatalf("overload %+v", c)
	}
	c = NewCell(0, 1000, 0)
	if c.LoadBP != nil || !c.Overload {
		t.Fatalf("zero capacity %+v", c)
	}
	c = NewCell(0, 0, 0)
	if c.LoadBP != nil || c.Overload {
		t.Fatalf("empty %+v", c)
	}
	// half-even: 1/3 of capacity = 3333.33 bp -> 3333; 1/16 = 625; 1/32 = 312.5 -> 312
	if v := *BP(1000, 3000); v != 3333 {
		t.Fatal(v)
	}
	if v := *BP(250, 8000); v != 312 {
		t.Fatal(v)
	}
}

func TestLoad_AggregateIsRatioOfSums(t *testing.T) {
	// mean of ratios would be (10000 + 0)/2 = 5000; ratio of sums is 1000/9000.
	agg, err := Aggregate([]Cell{NewCell(1000, 1000, 500), NewCell(8000, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Capacity != 9000 || agg.Assigned != 1000 || agg.Actual != 500 || *agg.LoadBP != 1111 || agg.Overload {
		t.Fatalf("agg %+v", agg)
	}
	if _, err := Aggregate([]Cell{{Capacity: -1}}); err == nil {
		t.Fatal("negative accepted")
	}
}

func TestLoad_Periods(t *testing.T) {
	d := calendar.MustParse("2026-12-31") // Thursday
	if PeriodStart(d, PeriodWeek).String() != "2026-12-28" || PeriodStart(d, PeriodMonth).String() != "2026-12-01" ||
		PeriodStart(d, PeriodDay) != d {
		t.Fatal("period start")
	}
	f, l := MonthOf(d)
	if f.String() != "2026-12-01" || l.String() != "2026-12-31" {
		t.Fatal("month of december", f, l)
	}
	f, l = MonthOf(calendar.MustParse("2028-02-10"))
	if l.String() != "2028-02-29" || f.String() != "2028-02-01" {
		t.Fatal("leap february")
	}
	f, l = WeekOf(d)
	if f.String() != "2026-12-28" || l.String() != "2027-01-03" {
		t.Fatal("week")
	}
}

func TestLoad_AtRisk(t *testing.T) {
	today := monday
	emps := []Employee{emp("a", "PAINT"), emp("b", "MECH")}
	tasks := []RiskTask{{QualificationID: "PAINT", EstHours: 20000, ProgressBP: 5000}} // 10 h remaining
	// Mon-Tue: 16 h of PAINT capacity, 8 h used by another order on Monday -> 8 h free < 10 h.
	in := RiskInput{
		WorkOrderID: "o1", Today: today, Delivery: today.AddDays(1), Tasks: tasks, Employees: emps,
		Assignments: []Assignment{
			{EmployeeID: "a", WorkOrderID: "o2", Date: today, Hours: 8000},
			{EmployeeID: "a", WorkOrderID: "o1", Date: today.AddDays(1), Hours: 8000}, // own order: not subtracted
		},
	}
	v, err := AtRisk(in)
	if err != nil || v.Late || len(v.Risks) != 1 || v.Risks[0].Remaining != 10000 || v.Risks[0].Free != 8000 {
		t.Fatalf("verdict %+v %v", v, err)
	}
	in.Delivery = today.AddDays(2)
	if v, _ = AtRisk(in); len(v.Risks) != 0 {
		t.Fatalf("three days are enough: %+v", v)
	}
	in.Delivery = today.AddDays(-1)
	if v, _ = AtRisk(in); !v.Late {
		t.Fatal("late")
	}
	in.Completed = true
	if v, _ = AtRisk(in); v.Late || len(v.Risks) != 0 {
		t.Fatal("completed is never at risk")
	}
	in.Completed, in.Delivery = false, calendar.Date{}
	if v, _ = AtRisk(in); v.Late || len(v.Risks) != 0 {
		t.Fatal("no delivery: no verdict")
	}
}

func TestRapid_LoadAggregate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 20).Draw(t, "n")
		cells := make([]Cell, n)
		var c, a int64
		for i := range cells {
			cp := qty.Milli(rapid.Int64Range(0, 16000).Draw(t, "c"))
			as := qty.Milli(rapid.Int64Range(0, 20000).Draw(t, "a"))
			cells[i] = NewCell(cp, as, 0)
			c += int64(cp)
			a += int64(as)
		}
		agg, err := Aggregate(cells)
		if err != nil {
			t.Fatal(err)
		}
		if int64(agg.Capacity) != c || int64(agg.Assigned) != a || agg.Overload != (a > c) {
			t.Fatalf("agg %+v", agg)
		}
		if c > 0 && (*agg.LoadBP < a*10000/c || *agg.LoadBP > a*10000/c+1) {
			t.Fatalf("load bp %d", *agg.LoadBP)
		}
	})
}
