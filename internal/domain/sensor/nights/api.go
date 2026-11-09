// Package nights is the pure physical classification of nights from the history of a sensor (m12-plan §10.2): a
// night D is occupied when the confirmed state is occupied for at least 240 minutes of [D 22:00, D+1 06:00) local,
// free when free for at least 240, otherwise unknown (ties occupied). DST-safe: the window is measured in real
// minutes between the two local instants (420 or 540 minutes on the nights of the clock change), the threshold
// stays 240. No store, no clock, no float.
package nights

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
)

// States.
const (
	Occupied = "occupied"
	Free     = "free"
	Unknown  = "unknown"
)

// ThresholdMinutes is the majority of the nominal 480-minute window.
const ThresholdMinutes = 240

// Change is one confirmed change of the history (ascending At).
type Change struct {
	To string
	At time.Time
}

// Window returns the instants [D 22:00, D+1 06:00) local of night d.
func Window(d calendar.Date, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	n := d.AddDays(1)
	start = time.Date(d.Year(), d.Month(), d.Day(), 22, 0, 0, 0, loc)
	end = time.Date(n.Year(), n.Month(), n.Day(), 6, 0, 0, 0, loc)
	return start, end
}

// Classify returns the physical state of each night of [from, to] given the state before the first change and the
// changes in ascending order of At (sensor_state_changes guarantees it, Q41).
func Classify(stateBefore string, changes []Change, from, to calendar.Date, loc *time.Location) map[calendar.Date]string {
	out := map[calendar.Date]string{}
	for d := from; !d.After(to); d = d.AddDays(1) {
		start, end := Window(d, loc)
		occ, free := Minutes(stateBefore, changes, start, end)
		switch {
		case occ >= ThresholdMinutes:
			out[d] = Occupied
		case free >= ThresholdMinutes:
			out[d] = Free
		default:
			out[d] = Unknown
		}
	}
	return out
}

// Minutes returns the whole minutes of [start, end) spent occupied and free.
func Minutes(stateBefore string, changes []Change, start, end time.Time) (occupied, free int) {
	st := StateAt(stateBefore, changes, start)
	cur := start
	var occD, freeD time.Duration
	add := func(state string, d time.Duration) {
		switch state {
		case Occupied:
			occD += d
		case Free:
			freeD += d
		}
	}
	for _, c := range changes {
		if !c.At.After(start) {
			continue
		}
		if !c.At.Before(end) {
			break
		}
		add(st, c.At.Sub(cur))
		cur, st = c.At, c.To
	}
	add(st, end.Sub(cur))
	return int(occD / time.Minute), int(freeD / time.Minute)
}

// StateAt returns the confirmed state at instant t (state at 02:00 for a past map date).
func StateAt(stateBefore string, changes []Change, t time.Time) string {
	st := stateBefore
	if st == "" {
		st = Unknown
	}
	for _, c := range changes {
		if c.At.After(t) {
			break
		}
		st = c.To
	}
	return st
}
