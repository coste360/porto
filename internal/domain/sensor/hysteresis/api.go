package hysteresis

import (
	"sort"
	"time"
)

// States.
const (
	Occupied = "occupied"
	Free     = "free"
	Unknown  = "unknown"
)

// Change reasons.
const (
	ReasonReadings    = "readings"
	ReasonDuration    = "duration"
	ReasonSilence     = "silence"
	ReasonDeviceMoved = "device_moved"
)

// State is the hysteresis state of a device (sensor_device_states).
type State struct {
	Confirmed      string // occupied | free | unknown
	ConfirmedSince time.Time
	Candidate      string // "" | occupied | free | unknown
	CandidateSince time.Time
	CandidateCount int
	LastObservedAt time.Time
}

// Params are N (Readings >= 1) and T (Window >= 1 minute).
type Params struct {
	Readings int
	Window   time.Duration
}

// Reading is one observed state.
type Reading struct {
	ObservedAt time.Time
	State      string
}

// Change is a confirmed change.
type Change struct {
	From, To string
	At       time.Time
	Reason   string // readings | duration | silence | device_moved
}

// Outcome of a Step.
type Outcome int

// Outcomes.
const (
	Applied Outcome = iota + 1
	OutOfOrder
)

// Initial is the state of a device never read (unknown).
func Initial() State { return State{Confirmed: Unknown} }

// valid reports whether st is a physical state.
func valid(st string) bool { return st == Occupied || st == Free || st == Unknown }

// Step applies one reading (m12-plan §5.2). Readings at or before LastObservedAt are out of order and leave the
// state unchanged (equal instants never reach Step: the database dedupes them); so are readings observed before
// ConfirmedSince (a late reading from before a silence or a move cannot rewrite the state confirmed after it, which
// keeps the change instants non-decreasing); those still advance LastObservedAt. A reading with a state that is not
// occupied, free or unknown is treated as unknown (the ingestion rejects such items before folding).
func Step(s State, r Reading, p Params) (State, *Change, Outcome) {
	if !s.LastObservedAt.IsZero() && !r.ObservedAt.After(s.LastObservedAt) {
		return s, nil, OutOfOrder
	}
	if r.ObservedAt.Before(s.ConfirmedSince) {
		// Observed, so LastObservedAt advances, but it cannot change a state confirmed after it.
		s.LastObservedAt = r.ObservedAt
		return s, nil, OutOfOrder
	}
	if !valid(s.Confirmed) {
		s.Confirmed = Unknown
	}
	st := r.State
	if !valid(st) {
		st = Unknown
	}
	n := max(p.Readings, 1)
	window := max(p.Window, time.Minute)
	s.LastObservedAt = r.ObservedAt
	if st == s.Confirmed {
		s.Candidate, s.CandidateSince, s.CandidateCount = "", time.Time{}, 0
		return s, nil, Applied
	}
	if st == s.Candidate {
		s.CandidateCount++
	} else {
		s.Candidate, s.CandidateSince, s.CandidateCount = st, r.ObservedAt, 1
	}
	reason := ""
	switch {
	case s.CandidateCount >= n:
		reason = ReasonReadings
	case r.ObservedAt.Sub(s.CandidateSince) >= window:
		reason = ReasonDuration
	}
	if reason == "" {
		return s, nil, Applied
	}
	c := &Change{From: s.Confirmed, To: s.Candidate, At: s.CandidateSince, Reason: reason}
	s.Confirmed, s.ConfirmedSince = s.Candidate, s.CandidateSince
	s.Candidate, s.CandidateSince, s.CandidateCount = "", time.Time{}, 0
	return s, c, Applied
}

// Fold applies rs in ObservedAt order (repeated Step); rs is not modified. Out-of-order readings (at or before the
// state's LastObservedAt) are ignored.
func Fold(s State, rs []Reading, p Params) (State, []Change) {
	sorted := append([]Reading(nil), rs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ObservedAt.Before(sorted[j].ObservedAt) })
	var out []Change
	for _, r := range sorted {
		var c *Change
		s, c, _ = Step(s, r, p)
		if c != nil {
			out = append(out, *c)
		}
	}
	return s, out
}

// Silence forces unknown when now - LastObservedAt >= offlineAfter (m12-plan §5.3): the change is at
// LastObservedAt + offlineAfter, reason silence, and the candidate is cleared. A device never read, or already
// unknown, is unchanged.
func Silence(s State, now time.Time, offlineAfter time.Duration) (State, *Change) {
	if s.Confirmed == Unknown || s.LastObservedAt.IsZero() || now.Sub(s.LastObservedAt) < offlineAfter {
		return s, nil
	}
	at := s.LastObservedAt.Add(offlineAfter)
	c := &Change{From: s.Confirmed, To: Unknown, At: at, Reason: ReasonSilence}
	s.Confirmed, s.ConfirmedSince = Unknown, at
	s.Candidate, s.CandidateSince, s.CandidateCount = "", time.Time{}, 0
	return s, c
}

// Moved resets the state of a reassigned device to unknown at now (reason device_moved, H15). ConfirmedSince
// becomes now, so readings of the old position observed before the move stay out of order. The change is nil when
// the device was already unknown (the reset still clears the candidate).
func Moved(s State, now time.Time) (State, *Change) {
	var c *Change
	if s.Confirmed != Unknown && s.Confirmed != "" {
		c = &Change{From: s.Confirmed, To: Unknown, At: now, Reason: ReasonDeviceMoved}
	}
	s.Confirmed, s.ConfirmedSince = Unknown, now
	s.Candidate, s.CandidateSince, s.CandidateCount = "", time.Time{}, 0
	return s, c
}
