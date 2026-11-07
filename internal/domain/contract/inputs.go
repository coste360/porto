package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Segment is one placement of a stay: a resource (with a position on dynamic areas) for a range of nights.
type Segment struct {
	ResourceType string
	ResourceID   string
	X, Y         *int64 // dynamic areas only
	RotationDd   *int64
	First, Last  calendar.Date // inclusive nights
}

// spec returns the allocation spec of a segment for [first, last].
func (g Segment) spec(first, last calendar.Date) engine.AllocSpec {
	sp := engine.AllocSpec{ResourceType: engine.ResourceType(g.ResourceType), ResourceID: g.ResourceID, First: first, Last: last}
	if g.ResourceType == store.ResourceArea {
		pl := &engine.Placement{}
		if g.X != nil {
			pl.X = geo.Cm(*g.X)
		}
		if g.Y != nil {
			pl.Y = geo.Cm(*g.Y)
		}
		if g.RotationDd != nil {
			pl.Rotation = geo.Angle(*g.RotationDd)
		}
		sp.Placement = pl
	}
	return sp
}

// segmentOf returns the segment an allocation occupies.
func segmentOf(a *store.Allocation) Segment {
	return Segment{
		ResourceType: a.ResourceType, ResourceID: a.ResourceID, X: a.XCm, Y: a.YCm, RotationDd: a.RotationDd,
		First: a.FirstNight, Last: a.LastNight,
	}
}

// ServiceRequest is a service priced with a stay (an item and its quantity in thousandths).
type ServiceRequest struct {
	ItemID   string
	QtyMilli int64
}

// FitOverrideInput accepts the blocking dimensional fit codes of an allocation (occupancy.override_fit); the
// reason is audited.
type FitOverrideInput struct {
	Reason string
}

var resourceTypes = []string{store.ResourceBerth, store.ResourceArea, store.ResourceLand}

// checkSegments validates 1..max segments that cover the nights [arrival, departure) contiguously.
func checkSegments(v *auth.ValidationError, field string, segs []Segment, arrival, departure calendar.Date, maxSegs int) {
	if len(segs) == 0 {
		v.Add(field, "required")
		return
	}
	if len(segs) > maxSegs {
		v.Add(field, "too_many")
		return
	}
	next := arrival
	for i, g := range segs {
		f := fmt.Sprintf("%s[%d]", field, i)
		if !slices.Contains(resourceTypes, g.ResourceType) {
			v.Add(f+".resource_type", "invalid")
		}
		if !store.ValidID(g.ResourceID) {
			v.Add(f+".resource_id", "invalid")
		}
		if g.ResourceType == store.ResourceArea && g.X == nil {
			v.Add(f+".x_cm", "required")
		}
		if g.ResourceType != store.ResourceArea && (g.X != nil || g.Y != nil || g.RotationDd != nil) {
			v.Add(f+".x_cm", "not_allowed")
		}
		if g.First.IsZero() || g.Last.IsZero() || g.Last.Before(g.First) {
			v.Add(f+".last_night", "invalid")
			continue
		}
		if !g.First.Equal(next) {
			v.Add(f+".first_night", "not_contiguous")
		}
		next = g.Last.AddDays(1)
	}
	if !next.Equal(departure) {
		v.Add(field, "not_covering")
	}
}

// checkStay validates an arrival and a departure (exclusive), at most maxNights apart.
func checkStay(v *auth.ValidationError, arrival, departure calendar.Date, arrivalField, departureField string, maxNights int) {
	if arrival.IsZero() {
		v.Add(arrivalField, "required")
	}
	if departure.IsZero() {
		v.Add(departureField, "required")
	}
	if arrival.IsZero() || departure.IsZero() {
		return
	}
	if !departure.After(arrival) {
		v.Add(departureField, "before_arrival")
		return
	}
	if arrival.DaysUntil(departure) > maxNights {
		v.Add(departureField, "too_long")
	}
}

// fitOverride builds the recorded override (codes are filled by the engine from the fit result).
func fitOverride(a actor, in *FitOverrideInput) *occupancy.FitOverride {
	if in == nil {
		return nil
	}
	return occupancy.NewFitOverride(a.p, strings.TrimSpace(in.Reason))
}

func overrideReason(in *FitOverrideInput) *string {
	if in == nil {
		return nil
	}
	r := in.Reason
	return &r
}

// cleanText trims s and reports whether it has at most maxRunes runes.
func cleanText(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= maxRunes
}

// defaultPolicy returns the contract policy defaults of the marina settings (m3-plan §4.7).
func defaultPolicy(st *store.OccupancySettings) Policy {
	return Policy{Version: PolicyVersion, RepriceOnMove: st.RepriceOnMove, ReleasePolicy: st.ReleasePolicy, AllowSegments: true}
}

// PolicyInput are the policy fields given by a client (nil = default).
type PolicyInput struct {
	Version       *int
	RepriceOnMove *bool
	ReleasePolicy *string
	AllowSegments *bool
}

// mergePolicy applies in on base and validates the result.
func mergePolicy(v *auth.ValidationError, base Policy, in *PolicyInput) Policy {
	if in == nil {
		return base
	}
	if in.Version != nil && *in.Version != PolicyVersion {
		v.Add("policy.version", "invalid")
	}
	if in.RepriceOnMove != nil {
		base.RepriceOnMove = *in.RepriceOnMove
	}
	if in.ReleasePolicy != nil {
		if *in.ReleasePolicy != ReleaseNever && *in.ReleasePolicy != ReleaseWithConsent {
			v.Add("policy.release_policy", "invalid")
		}
		base.ReleasePolicy = *in.ReleasePolicy
	}
	if in.AllowSegments != nil {
		base.AllowSegments = *in.AllowSegments
	}
	return base
}

// DecodePolicy reads a stored policy, rejecting unknown keys (m3-plan §4.7). An empty document is the zero
// policy.
func DecodePolicy(raw store.JSON) (Policy, error) {
	var p Policy
	if len(raw) == 0 || string(raw) == "{}" {
		return p, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("contract policy: %w", err)
	}
	return p, nil
}

// endOfDay returns the last second of a local date as an instant (quote expiries, m3-plan §3.3).
func endOfDay(d calendar.Date, loc *time.Location) time.Time {
	return store.UTC(d.AddDays(1).Time(loc).Add(-time.Second))
}

// DefaultEndsOn returns the default end (exclusive) of a mooring contract by term (m3-plan §13 D27): annual
// +1 year (Feb 29 → Feb 28), monthly +1 calendar month with end-of-month clamp. Seasonal and period have no
// date-only default (seasonal comes from the price list).
func DefaultEndsOn(term string, starts calendar.Date) (calendar.Date, bool) {
	switch term {
	case TermAnnual:
		return addMonthsClamped(starts, 12), true
	case TermMonthly:
		return addMonthsClamped(starts, 1), true
	}
	return calendar.Date{}, false
}

func addMonthsClamped(d calendar.Date, months int) calendar.Date {
	y, m := d.Year(), int(d.Month())-1+months
	y += m / 12
	m = m%12 + 1
	last := calendar.New(y, time.Month(m)+1, 1).AddDays(-1).Day()
	if m == 12 {
		last = 31
	}
	day := min(d.Day(), last)
	return calendar.New(y, time.Month(m), day)
}
