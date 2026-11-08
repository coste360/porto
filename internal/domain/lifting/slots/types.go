// Package slots converts between wall-clock times and the 15-minute slot index 0..95 of a local day and checks
// booking windows (m10-plan §11, D28). Slots are wall-clock labels: a DST change day keeps 96 labels and the lift
// window never includes 02:00-03:00. Pure: no store, no clock, no randomness.
package slots

import (
	"errors"
	"time"
)

// PerDay is the number of 15-minute slots of a day.
const PerDay = 96

// Minutes is the length of a slot.
const Minutes = 15

// Default lift window 07:00-19:00 (yard_settings.lift_from_slot / lift_to_slot defaults).
const (
	DefaultLiftFrom = 28
	DefaultLiftTo   = 76
)

// dstGap is the hour of the wall-clock day that may not exist (spring) or exist twice (autumn) on a DST change
// day in the European zones: 02:00-03:00, slots 8..11. No booking may touch it.
var dstGap = Window{From: 8, To: 12}

// ErrInvalid reports an hour, minute or slot outside its range.
var ErrInvalid = errors.New("lifting/slots: invalid time or slot")

// Window is a half-open slot range [From, To).
type Window struct {
	From, To int
}

// Valid reports whether w is a non-empty range inside the day.
func (w Window) Valid() bool {
	return w.From >= 0 && w.To <= PerDay && w.To > w.From
}

// Len is the number of slots of w (0 when invalid).
func (w Window) Len() int {
	if !w.Valid() {
		return 0
	}
	return w.To - w.From
}

// Overlaps reports whether two valid windows share a slot.
func (w Window) Overlaps(o Window) bool {
	return w.Valid() && o.Valid() && w.From < o.To && o.From < w.To
}

// FromClock returns the slot of a wall-clock hour and minute (minute rounded down to the slot).
func FromClock(hour, minute int) (int, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, ErrInvalid
	}
	return hour*4 + minute/Minutes, nil
}

// Clock returns the wall-clock hour and minute where slot starts; slot 96 is the end of the day (24:00).
func Clock(slot int) (hour, minute int, err error) {
	if slot < 0 || slot > PerDay {
		return 0, 0, ErrInvalid
	}
	return slot / 4, (slot % 4) * Minutes, nil
}

// Label is "HH:MM" of the start of slot (slot 96 = "24:00"), "" when invalid.
func Label(slot int) string {
	h, m, err := Clock(slot)
	if err != nil {
		return ""
	}
	return string([]byte{byte('0' + h/10), byte('0' + h%10), ':', byte('0' + m/10), byte('0' + m%10)})
}

// Of returns the slot of the wall-clock time of t in its own location (t carries the marina zone).
func Of(t time.Time) int {
	return t.Hour()*4 + t.Minute()/Minutes
}

// SafeWindow reports whether a lift window is valid and avoids the DST gap 02:00-03:00.
func SafeWindow(lift Window) bool {
	return lift.Valid() && !lift.Overlaps(dstGap)
}

// Within reports whether w lies inside the lift window and avoids the DST gap.
func Within(w, lift Window) bool {
	if !w.Valid() || !lift.Valid() || w.Overlaps(dstGap) {
		return false
	}
	return w.From >= lift.From && w.To <= lift.To
}

// Range returns the slots of w (nil when invalid).
func Range(w Window) []int {
	if !w.Valid() {
		return nil
	}
	out := make([]int, 0, w.To-w.From)
	for s := w.From; s < w.To; s++ {
		out = append(out, s)
	}
	return out
}

// Duration returns the window starting at from covering minutes rounded up to whole slots.
func Duration(from, minutes int) Window {
	n := (minutes + Minutes - 1) / Minutes
	if n < 1 {
		n = 1
	}
	return Window{From: from, To: from + n}
}
