package reconcile

import (
	"cmp"
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
)

// Contract kinds, terms and statuses the rules read (strings of the contract package, kept here so that the pure
// package imports no domain package).
const (
	contractMooring   = "mooring"
	contractTransit   = "transit"
	termAnnual        = "annual"
	statusActive      = "active"
	statusArrived     = "arrived"
	statusConfirmed   = "confirmed"
	allocBlock        = "block"
	allocMaintenance  = "maintenance"
	allocHold         = "hold"
	presenceInPort    = "in_port"
	presenceOut       = "out"
	sourceSensor      = "sensor"
	policyWithConsent = "with_consent"
)

func covers(first, last, d calendar.Date) bool { return !d.Before(first) && !d.After(last) }

// minuteOfDay is the local minute of now in loc.
func minuteOfDay(now time.Time, loc *time.Location) int {
	l := now.In(loc)
	return l.Hour()*60 + l.Minute()
}

func location(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}

// releaseCovering returns the active release of allocation covering d, if any.
func releaseCovering(f Facts, allocationID string, d calendar.Date) *Release {
	for i := range f.Releases {
		r := &f.Releases[i]
		if r.AllocationID == allocationID && covers(r.FromNight, r.ToNight, d) {
			return r
		}
	}
	return nil
}

// anyReleaseCovering returns the first active release covering d (sorted by id for determinism).
func anyReleaseCovering(f Facts, d calendar.Date) *Release {
	var out *Release
	for i := range f.Releases {
		r := &f.Releases[i]
		if covers(r.FromNight, r.ToNight, d) && (out == nil || r.ID < out.ID) {
			out = r
		}
	}
	return out
}

// stayExpectation is the expectation of one stay covering D (m12-plan §8.1).
func stayExpectation(s Stay, f Facts, d calendar.Date) string {
	if s.VesselID == "" || s.AllocationKind == allocBlock || s.AllocationKind == allocMaintenance {
		return ExpectFree
	}
	if r := releaseCovering(f, s.AllocationID, d); r != nil {
		// Released nights: free; a resale is its own stay (or ResaleStay when the loader did not list it).
		return ExpectFree
	}
	if s.AllocationKind == allocHold {
		return ExpectEither // a hold may or may not turn into an arrival
	}
	if s.ContractKind == contractTransit {
		if s.Status == statusArrived {
			return ExpectOccupied
		}
		if s.Status == "departed" || s.Status == "cancelled" || s.Status == "no_show" || s.Status == "expired" {
			return ExpectFree
		}
		return ExpectEither // confirmed or held, not arrived yet: arrival day (or late)
	}
	// Mooring contracts, berth-right owners and the other kinds with a vessel: the vessel presence decides.
	switch f.Presences[s.VesselID].State {
	case "", presenceInPort:
		return ExpectOccupied
	default:
		return ExpectFree
	}
}

func combine(exps []string) string {
	out := ExpectFree
	for _, e := range exps {
		switch e {
		case ExpectOccupied:
			return ExpectOccupied
		case ExpectEither:
			out = ExpectEither
		}
	}
	return out
}

// Expect returns the contractual expectation of a subject at now (m12-plan §8.1): occupied if any stay of D expects
// the vessel, else either (arrival day, hold, or a vessel of D - 1 leaving before departure_cutoff_minutes), else
// free. Informational devices answer none.
func Expect(s Subject, now time.Time, loc *time.Location, st Settings) string {
	if s.Informational {
		return ExpectNone
	}
	loc = location(loc)
	d := calendar.In(now, loc)
	var exps []string
	today := map[string]bool{}
	for _, stay := range s.Facts.Today {
		today[stay.AllocationID] = true
		exps = append(exps, stayExpectation(stay, s.Facts, d))
	}
	for _, r := range s.Facts.Releases {
		if r.Resold && r.ResaleStay != nil && covers(r.FromNight, r.ToNight, d) && !today[r.ResaleStay.AllocationID] {
			exps = append(exps, stayExpectation(*r.ResaleStay, s.Facts, d))
		}
	}
	if minuteOfDay(now, loc) < st.DepartureCutoffMinutes {
		for _, y := range s.Facts.Yesterday {
			if y.VesselID != "" && !today[y.AllocationID] && y.AllocationKind != allocBlock && y.AllocationKind != allocMaintenance {
				exps = append(exps, ExpectEither) // departure day
			}
		}
	}
	return combine(exps)
}

// Discrepancy reports whether the physical state disagrees with the expectation for at least
// UnexpectedAfterMinutes (occupied vs free, free vs occupied; unknown and either never).
func Discrepancy(physical string, stableFor time.Duration, expectation string, st Settings) bool {
	if stableFor < time.Duration(st.UnexpectedAfterMinutes)*time.Minute {
		return false
	}
	return (physical == Occupied && expectation == ExpectFree) || (physical == Free && expectation == ExpectOccupied)
}

// vesselStays returns the stays of D with a vessel (blocks and maintenance excluded).
func vesselStays(f Facts) []Stay {
	var out []Stay
	for _, s := range f.Today {
		if s.VesselID != "" && s.AllocationKind != allocBlock && s.AllocationKind != allocMaintenance {
			out = append(out, s)
		}
	}
	return out
}

// singleStay is the only stay with a vessel of D (rules that act on one vessel), or nil.
func singleStay(f Facts) *Stay {
	vs := vesselStays(f)
	if len(vs) != 1 {
		return nil
	}
	return &vs[0]
}

func annualMooring(s *Stay) bool {
	return s != nil && s.ContractKind == contractMooring && s.ContractTerm == termAnnual && s.Status == statusActive && s.AllocationKind != allocHold
}

func timeKey(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func ptrDate(d calendar.Date) *calendar.Date { return &d }

func ptrTime(t time.Time) *time.Time { return &t }

func ptrBool(b bool) *bool { return &b }

// verdict is the state of one (device, kind) condition: open with key, explicitly cleared, or neither.
type verdict struct {
	holds bool
	key   string
	refs  Refs
	clear bool
}

// subjectEval evaluates one subject.
type subjectEval struct {
	in      Input
	s       Subject
	now     time.Time
	loc     *time.Location
	d       calendar.Date
	physDur time.Duration // time since the physical state was confirmed
}

func (e *subjectEval) unexpected() time.Duration {
	return time.Duration(e.in.Settings.UnexpectedAfterMinutes) * time.Minute
}

func (e *subjectEval) occupiedFor(atLeast time.Duration) bool {
	return e.s.Physical == Occupied && !e.s.PhysicalSince.IsZero() && e.physDur >= atLeast
}

func (e *subjectEval) freeFor(atLeast time.Duration) bool {
	return e.s.Physical == Free && !e.s.PhysicalSince.IsZero() && e.physDur >= atLeast
}

// skipOccupancyRules: R1-R6 never open for an informational pedestal, an inactive device or an unknown state.
func (e *subjectEval) occupancyRules() bool { return !e.s.Informational && e.s.Active }

// r6 is the early return on a released berth.
func (e *subjectEval) r6() verdict {
	if !e.occupancyRules() {
		return verdict{clear: true}
	}
	r := anyReleaseCovering(e.s.Facts, e.d)
	if r == nil || (r.Resold && r.ResaleStatus == statusArrived) || e.s.Physical == Free {
		return verdict{clear: true}
	}
	if !e.occupiedFor(e.unexpected()) {
		return verdict{}
	}
	resold := r.Resold
	refs := Refs{ReleaseID: r.ID, AllocationID: r.AllocationID, Resold: ptrBool(resold), PhysicalSince: ptrTime(e.s.PhysicalSince)}
	for _, st := range e.s.Facts.Today {
		if st.AllocationID == r.AllocationID {
			refs.ContractID, refs.VesselID = st.ContractID, st.VesselID
		}
	}
	return verdict{holds: true, key: "release:" + r.ID, refs: refs}
}

// r1 is the occupied berth without an allocation.
func (e *subjectEval) r1(exp string, r6 verdict) verdict {
	if !e.occupancyRules() || e.s.Physical != Occupied || exp == ExpectOccupied || exp == ExpectEither || r6.holds || e.vesselReturning() {
		return verdict{clear: true}
	}
	if !e.occupiedFor(e.unexpected()) || exp != ExpectFree {
		return verdict{}
	}
	return verdict{holds: true, key: "occ:" + timeKey(e.s.PhysicalSince), refs: Refs{PhysicalSince: ptrTime(e.s.PhysicalSince)}}
}

// r2 is the arrival of a confirmed transit.
func (e *subjectEval) r2() verdict {
	if !e.occupancyRules() {
		return verdict{clear: true}
	}
	vs := vesselStays(e.s.Facts)
	var transit *Stay
	if len(vs) == 1 && vs[0].ContractKind == contractTransit && vs[0].Status == statusConfirmed &&
		(vs[0].FirstNight.Equal(e.d) || vs[0].FirstNight.Equal(e.d.AddDays(-1))) {
		transit = &vs[0]
	}
	if transit == nil {
		return verdict{clear: true}
	}
	if e.freeFor(e.unexpected()) {
		return verdict{clear: true}
	}
	midnight := transit.FirstNight.Time(e.loc)
	if e.s.Physical != Occupied || e.s.PhysicalSince.IsZero() || e.s.PhysicalSince.Before(midnight) {
		return verdict{}
	}
	return verdict{holds: true, key: "contract:" + transit.ContractID, refs: Refs{
		AllocationID: transit.AllocationID, ContractID: transit.ContractID, VesselID: transit.VesselID,
		PhysicalSince: ptrTime(e.s.PhysicalSince),
	}}
}

// presenceStay returns the single active mooring stay of D (annual only when annual is set) with its vessel
// presence, unless a release covers D.
func (e *subjectEval) presenceStay(annual bool) (*Stay, Presence) {
	st := singleStay(e.s.Facts)
	if st == nil || st.ContractKind != contractMooring || st.Status != statusActive || st.AllocationKind == allocHold ||
		(annual && !annualMooring(st)) || releaseCovering(e.s.Facts, st.AllocationID, e.d) != nil {
		return nil, Presence{}
	}
	return st, e.s.Facts.Presences[st.VesselID]
}

// vesselReturning reports whether the occupied berth is the return of its own mooring vessel recorded out (R3b or
// R4 territory, never R1).
func (e *subjectEval) vesselReturning() bool {
	st, pr := e.presenceStay(false)
	return st != nil && pr.State == presenceOut && e.s.Physical == Occupied
}

// r3 is the automatic departure (presence_effect_enabled): free for presence_out_after_hours since after the last
// presence.
func (e *subjectEval) r3() *RecordPresence {
	if !e.in.Settings.PresenceEffectEnabled || !e.occupancyRules() {
		return nil
	}
	st, pr := e.presenceStay(true)
	if st == nil || (pr.State != "" && pr.State != presenceInPort) {
		return nil
	}
	if !e.freeFor(time.Duration(e.in.Settings.PresenceOutAfterHours) * time.Hour) {
		return nil
	}
	if pr.State != "" && !e.s.PhysicalSince.After(pr.Since) {
		return nil // the berth emptied before the last recorded presence: the staff record wins
	}
	return &RecordPresence{DeviceID: e.s.DeviceID, VesselID: st.VesselID, AllocationID: st.AllocationID, Event: EventDeparted, At: e.s.PhysicalSince}
}

// r3b is the automatic return after a sensor departure (annual contracts); r4 the proposal after a staff
// departure (any active mooring contract, or annual ones when the effect is disabled).
func (e *subjectEval) r3bR4() (*RecordPresence, verdict) {
	if !e.occupancyRules() {
		return nil, verdict{clear: true}
	}
	st, pr := e.presenceStay(false)
	if st == nil || pr.State != presenceOut || e.s.Physical == Free {
		return nil, verdict{clear: true}
	}
	if !e.occupiedFor(e.unexpected()) || !e.s.PhysicalSince.After(pr.Since) {
		return nil, verdict{}
	}
	if e.in.Settings.PresenceEffectEnabled && pr.LastSource == sourceSensor && annualMooring(st) {
		return &RecordPresence{DeviceID: e.s.DeviceID, VesselID: st.VesselID, AllocationID: st.AllocationID, Event: EventArrived, At: e.s.PhysicalSince},
			verdict{clear: true}
	}
	return nil, verdict{holds: true, key: "presence:" + pr.PresenceID, refs: Refs{
		AllocationID: st.AllocationID, ContractID: st.ContractID, VesselID: st.VesselID, PresenceID: pr.PresenceID,
		PhysicalSince: ptrTime(e.s.PhysicalSince),
	}}
}

// r5 is the release proposal of a mooring contract whose vessel is away (release_policy with_consent).
func (e *subjectEval) r5() verdict {
	if !e.occupancyRules() {
		return verdict{clear: true}
	}
	st := singleStay(e.s.Facts)
	tomorrow := e.d.AddDays(1)
	if st == nil || st.ContractKind != contractMooring || st.Status != statusActive || st.ReleasePolicy != policyWithConsent ||
		st.LastNight.Before(tomorrow) || releaseCovering(e.s.Facts, st.AllocationID, tomorrow) != nil {
		return verdict{clear: true}
	}
	pr := e.s.Facts.Presences[st.VesselID]
	if pr.State == presenceInPort || e.s.Physical == Occupied {
		return verdict{clear: true}
	}
	if pr.State != presenceOut || !e.freeFor(time.Duration(e.in.Settings.ReleaseProposalAfterDays)*24*time.Hour) {
		return verdict{}
	}
	to := st.LastNight
	if lim := e.d.AddDays(e.in.Settings.ReleaseProposalNights); lim.Before(to) {
		to = lim
	}
	if pr.ExpectedReturnOn != nil {
		if lim := pr.ExpectedReturnOn.AddDays(-1); lim.Before(to) {
			to = lim
		}
	}
	if to.Before(tomorrow) {
		return verdict{}
	}
	return verdict{holds: true, key: "alloc:" + st.AllocationID + ":" + timeKey(e.s.PhysicalSince), refs: Refs{
		AllocationID: st.AllocationID, ContractID: st.ContractID, VesselID: st.VesselID,
		ProposedFrom: ptrDate(tomorrow), ProposedTo: ptrDate(to), PhysicalSince: ptrTime(e.s.PhysicalSince),
	}}
}

// r7 is the silent device.
func (e *subjectEval) r7() verdict {
	if !e.s.Active {
		return verdict{clear: true}
	}
	ref, key := e.s.CreatedAt, "silent:never"
	if e.s.LastContactAt != nil {
		ref, key = *e.s.LastContactAt, "silent:"+timeKey(*e.s.LastContactAt)
	}
	if e.now.Sub(ref) < time.Duration(e.in.Settings.SilentAlertMinutes)*time.Minute {
		return verdict{clear: true}
	}
	return verdict{holds: true, key: key}
}

// r8 is the low battery (recovery at battery_low_pct + 5).
func (e *subjectEval) r8() verdict {
	if !e.s.Active || e.s.BatteryPct == nil {
		return verdict{clear: !e.s.Active}
	}
	b := *e.s.BatteryPct
	switch {
	case b < e.in.Settings.BatteryLowPct:
		return verdict{holds: true, key: "battery"}
	case b >= e.in.Settings.BatteryLowPct+5:
		return verdict{clear: true}
	}
	return verdict{}
}

// Verdicts returns the condition of every alert kind for a subject and the automatic presence, if any. Evaluate
// and the alert actions (staleness) share it.
func verdicts(in Input, s Subject) (map[string]verdict, *RecordPresence) {
	loc := location(in.Loc)
	e := &subjectEval{in: in, s: s, now: in.Now, loc: loc, d: calendar.In(in.Now, loc)}
	if !s.PhysicalSince.IsZero() {
		e.physDur = in.Now.Sub(s.PhysicalSince)
	}
	exp := Expect(s, in.Now, loc, in.Settings)
	v := map[string]verdict{}
	r6 := e.r6()
	v[KindEarlyReturn] = r6
	v[KindUnexpectedOccupied] = e.r1(exp, r6)
	v[KindTransitArrival] = e.r2()
	pres, r4 := e.r3bR4()
	v[KindReturnDetected] = r4
	v[KindReleaseProposal] = e.r5()
	v[KindDeviceSilent] = e.r7()
	v[KindBatteryLow] = e.r8()
	if pres == nil {
		pres = e.r3()
	}
	return v, pres
}

// kindOrder is the deterministic order of the kinds in the output.
var kindOrder = []string{
	KindUnexpectedOccupied, KindTransitArrival, KindReturnDetected, KindReleaseProposal, KindEarlyReturn,
	KindDeviceSilent, KindBatteryLow,
}

// Evaluate applies the rules R1-R8 to every subject (m12-plan §8.2) and returns the actions in a deterministic
// order (subjects by device id, then clears, opens and the automatic presence by kind), independent of the order of
// the subjects and of the alerts.
//
// For each (device, kind): when the condition holds with key K, every uncleared alert with another key is cleared
// and K is opened unless it is already recorded (a dismissed or confirmed K stays suppressed until it clears);
// when the clear condition holds, the uncleared alerts are cleared; otherwise nothing changes (hysteresis between
// the opening threshold and the clear condition).
func Evaluate(in Input) []Action {
	subjects := slices.Clone(in.Subjects)
	slices.SortStableFunc(subjects, func(a, b Subject) int { return cmp.Compare(a.DeviceID, b.DeviceID) })
	recorded := map[[2]string][]string{}
	for _, a := range in.Alerts {
		k := [2]string{a.DeviceID, a.Kind}
		if !slices.Contains(recorded[k], a.ConditionKey) {
			recorded[k] = append(recorded[k], a.ConditionKey)
		}
	}
	seen := map[string]bool{}
	var out []Action
	for _, s := range subjects {
		if seen[s.DeviceID] {
			continue
		}
		seen[s.DeviceID] = true
		v, pres := verdicts(in, s)
		var clears, opens []Action
		for _, kind := range kindOrder {
			vd := v[kind]
			keys := recorded[[2]string{s.DeviceID, kind}]
			switch {
			case vd.holds:
				other := false
				for _, k := range keys {
					if k != vd.key {
						other = true
					}
				}
				if other {
					clears = append(clears, Action{Clear: &ClearCondition{DeviceID: s.DeviceID, Kind: kind}})
				}
				if other || !slices.Contains(keys, vd.key) {
					opens = append(opens, Action{Open: &OpenAlert{DeviceID: s.DeviceID, Kind: kind, ConditionKey: vd.key, Refs: vd.refs}})
				}
			case vd.clear && len(keys) > 0:
				clears = append(clears, Action{Clear: &ClearCondition{DeviceID: s.DeviceID, Kind: kind}})
			}
		}
		out = append(out, clears...)
		out = append(out, opens...)
		if pres != nil {
			out = append(out, Action{Presence: pres})
		}
	}
	return out
}

// Stale reports whether the recorded condition (kind, key) of a subject no longer holds: its clear condition holds
// or the rule now holds with another key (m12-plan §8.3, 409 alert_stale). Between the opening threshold and the
// clear condition the alert is still actionable.
func Stale(in Input, s Subject, kind, key string) bool {
	v, _ := verdicts(in, s)
	vd := v[kind]
	if vd.holds {
		return vd.key != key
	}
	return vd.clear
}

// ExpectedPhysical is the expectation and discrepancy of a subject at in.Now (map, anomaly).
func ExpectedPhysical(in Input, s Subject) (expectation string, discrepancy bool) {
	exp := Expect(s, in.Now, in.Loc, in.Settings)
	var dur time.Duration
	if !s.PhysicalSince.IsZero() {
		dur = in.Now.Sub(s.PhysicalSince)
	}
	return exp, s.Active && exp != ExpectNone && Discrepancy(s.Physical, dur, exp, in.Settings)
}
