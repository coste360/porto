// Package auto is the deterministic greedy auto-plan of the yard planner (m10-plan §6.4, D23): orders by planned
// delivery then number, tasks by seq respecting finish-to-start dependencies, each day the qualified employee with
// the lowest load (ties by id), in 0.5 h steps, never above capacity. It proposes and never writes. No store, no
// clock, no randomness, no float.
package auto

import (
	"errors"
	"sort"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/yardplan/load"
	"github.com/coste360/porto/internal/qty"
)

// ErrNotImplemented is kept for the contract skeleton callers; nothing returns it any more.
var ErrNotImplemented = errors.New("yardplan/auto: not implemented")

// ErrInvalid reports an invalid input (horizon out of 1..MaxHorizon, negative remaining hours).
var ErrInvalid = errors.New("yardplan/auto: invalid input")

// MaxHorizon is the longest horizon in days.
const MaxHorizon = 90

// Step is the granularity of a proposed assignment (0.5 h).
const Step qty.Milli = 500

// Task is a task with unplanned remaining hours.
type Task struct {
	ID              string
	WorkOrderID     string
	OrderNumber     string
	PlannedDelivery calendar.Date // zero = none (last)
	Seq             int
	QualificationID string
	Remaining       qty.Milli
	DependsOn       []string
}

// Input of Propose.
type Input struct {
	From        calendar.Date
	HorizonDays int
	Tasks       []Task
	Employees   []load.Employee
	Existing    []load.Assignment
}

// Change is one proposed assignment: Hours is the new total of (task, employee, date), existing hours included,
// so that it can be confirmed as is through putAssignments.
type Change struct {
	TaskID     string
	EmployeeID string
	Date       calendar.Date
	Hours      qty.Milli
}

// Unplaced is a task whose remaining hours did not fit in the horizon.
type Unplaced struct {
	TaskID    string
	Remaining qty.Milli
}

// Proposal is the output of Propose (deterministic for equal input).
type Proposal struct {
	Changes  []Change
	Unplaced []Unplaced
}

type key struct {
	task, emp string
	date      calendar.Date
}

type dayKey struct {
	emp  string
	date calendar.Date
}

// order sorts tasks: planned delivery (none last), order number, then a topological order of each order's tasks
// that prefers the lowest seq (Kahn). Dependencies on tasks outside the input are ignored for ordering.
func order(tasks []Task) []Task {
	byOrder := map[string][]Task{}
	var orders []Task // one representative per order
	for _, t := range tasks {
		if _, ok := byOrder[t.WorkOrderID]; !ok {
			orders = append(orders, t)
		}
		byOrder[t.WorkOrderID] = append(byOrder[t.WorkOrderID], t)
	}
	sort.SliceStable(orders, func(i, j int) bool {
		a, b := orders[i], orders[j]
		if a.PlannedDelivery.IsZero() != b.PlannedDelivery.IsZero() {
			return !a.PlannedDelivery.IsZero()
		}
		if c := a.PlannedDelivery.Compare(b.PlannedDelivery); c != 0 {
			return c < 0
		}
		if a.OrderNumber != b.OrderNumber {
			return a.OrderNumber < b.OrderNumber
		}
		return a.WorkOrderID < b.WorkOrderID
	})
	out := make([]Task, 0, len(tasks))
	for _, o := range orders {
		out = append(out, topo(byOrder[o.WorkOrderID])...)
	}
	return out
}

func less(a, b Task) bool {
	if a.Seq != b.Seq {
		return a.Seq < b.Seq
	}
	return a.ID < b.ID
}

func topo(ts []Task) []Task {
	in := map[string]bool{}
	for _, t := range ts {
		in[t.ID] = true
	}
	indeg := map[string]int{}
	succ := map[string][]string{}
	byID := map[string]Task{}
	for _, t := range ts {
		byID[t.ID] = t
		for _, d := range t.DependsOn {
			if in[d] && d != t.ID {
				indeg[t.ID]++
				succ[d] = append(succ[d], t.ID)
			}
		}
	}
	var ready []Task
	for _, t := range ts {
		if indeg[t.ID] == 0 {
			ready = append(ready, t)
		}
	}
	out := make([]Task, 0, len(ts))
	done := map[string]bool{}
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
		t := ready[0]
		ready = ready[1:]
		out = append(out, t)
		done[t.ID] = true
		for _, s := range succ[t.ID] {
			indeg[s]--
			if indeg[s] == 0 {
				ready = append(ready, byID[s])
			}
		}
	}
	// A cycle (never stored: dependencies are acyclic) leaves tasks behind: append them by seq.
	var rest []Task
	for _, t := range ts {
		if !done[t.ID] {
			rest = append(rest, t)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return less(rest[i], rest[j]) })
	return append(out, rest...)
}

// Propose computes the proposal (pure).
func Propose(in Input) (Proposal, error) {
	if in.HorizonDays < 1 || in.HorizonDays > MaxHorizon || in.From.IsZero() {
		return Proposal{}, ErrInvalid
	}
	emps := append([]load.Employee(nil), in.Employees...)
	sort.Slice(emps, func(i, j int) bool { return emps[i].ID < emps[j].ID })
	last := in.From.AddDays(in.HorizonDays - 1)

	assigned := map[dayKey]qty.Milli{}
	triple := map[key]qty.Milli{}
	lastDay := map[string]calendar.Date{} // task -> last assigned day (existing or proposed)
	for _, a := range in.Existing {
		if a.Hours < 0 {
			return Proposal{}, ErrInvalid
		}
		assigned[dayKey{a.EmployeeID, a.Date}] += a.Hours
		triple[key{a.TaskID, a.EmployeeID, a.Date}] += a.Hours
		if d, ok := lastDay[a.TaskID]; !ok || a.Date.After(d) {
			lastDay[a.TaskID] = a.Date
		}
	}
	capOf := map[dayKey]qty.Milli{}
	capacity := func(e load.Employee, d calendar.Date) (qty.Milli, error) {
		k := dayKey{e.ID, d}
		if c, ok := capOf[k]; ok {
			return c, nil
		}
		c, err := load.Capacity(e, d)
		capOf[k] = c
		return c, err
	}

	var p Proposal
	changed := map[key]int{} // index in p.Changes
	unfinished := map[string]bool{}
	for _, t := range order(in.Tasks) {
		if t.Remaining < 0 {
			return Proposal{}, ErrInvalid
		}
		if t.Remaining == 0 {
			continue
		}
		start := in.From
		blocked := false
		for _, dep := range t.DependsOn {
			if unfinished[dep] {
				blocked = true
			}
			if d, ok := lastDay[dep]; ok && !d.Before(start) {
				start = d.AddDays(1)
			}
		}
		remaining := t.Remaining
		for d := start; !blocked && remaining > 0 && !d.After(last); d = d.AddDays(1) {
			var best *load.Employee
			var bestFree, bestCap, bestAssigned qty.Milli
			for i := range emps {
				e := &emps[i]
				if !e.Holds(t.QualificationID) {
					continue
				}
				c, err := capacity(*e, d)
				if err != nil {
					return Proposal{}, err
				}
				a := assigned[dayKey{e.ID, d}]
				free := c - a
				if free <= 0 {
					continue
				}
				// lowest load a/c first (cross-multiplied, no division); ties keep the lower id (sorted).
				if best == nil || int64(a)*int64(bestCap) < int64(bestAssigned)*int64(c) {
					best, bestFree, bestCap, bestAssigned = e, free, c, a
				}
			}
			if best == nil {
				continue
			}
			amount := remaining
			if amount > bestFree {
				amount = bestFree - bestFree%Step
			}
			if amount <= 0 {
				continue
			}
			k := key{t.ID, best.ID, d}
			assigned[dayKey{best.ID, d}] += amount
			triple[k] += amount
			remaining -= amount
			lastDay[t.ID] = d
			if i, ok := changed[k]; ok {
				p.Changes[i].Hours = triple[k]
			} else {
				changed[k] = len(p.Changes)
				p.Changes = append(p.Changes, Change{TaskID: t.ID, EmployeeID: best.ID, Date: d, Hours: triple[k]})
			}
		}
		if remaining > 0 {
			unfinished[t.ID] = true
			p.Unplaced = append(p.Unplaced, Unplaced{TaskID: t.ID, Remaining: remaining})
		}
	}
	return p, nil
}
