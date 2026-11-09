package reconcile

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
)

var rome = func() *time.Location {
	l, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		panic(err)
	}
	return l
}()

// D is the night of the tests; now is 2027-06-12 15:00 local unless a case says otherwise.
var (
	day   = calendar.MustParse("2027-06-12")
	now   = time.Date(2027, 6, 12, 15, 0, 0, 0, rome)
	defSt = Settings{
		UnexpectedAfterMinutes: 15, DepartureCutoffMinutes: 720, SilentAlertMinutes: 180, BatteryLowPct: 20,
		PresenceOutAfterHours: 6, ReleaseProposalAfterDays: 3, ReleaseProposalNights: 7,
	}
)

func localAt(h, m int) time.Time { return time.Date(2027, 6, 12, h, m, 0, 0, rome) }

func mooring(term string) Stay {
	return Stay{
		AllocationID: "al-m", ContractID: "c-m", ContractKind: "mooring", ContractTerm: term, Status: "active", VesselID: "v-m",
		FirstNight: calendar.MustParse("2027-01-01"), LastNight: calendar.MustParse("2027-12-31"), AllocationKind: "contract",
		ReleasePolicy: "with_consent",
	}
}

func transit(status string, first calendar.Date) Stay {
	return Stay{
		AllocationID: "al-t", ContractID: "c-t", ContractKind: "transit", Status: status, VesselID: "v-t",
		FirstNight: first, LastNight: first.AddDays(3), AllocationKind: "transit",
	}
}

func subject(physical string, since time.Time, f Facts) Subject {
	contact := now.Add(-time.Minute)
	b := 80
	return Subject{
		DeviceID: "dev-1", ResourceType: "berth", ResourceID: "b-1", MapID: "m-1", Active: true, CreatedAt: now.Add(-30 * 24 * time.Hour),
		Physical: physical, PhysicalSince: since, LastContactAt: &contact, BatteryPct: &b, Facts: f,
	}
}

func presences(vessel, state, source string, since time.Time) map[string]Presence {
	return map[string]Presence{vessel: {VesselID: vessel, State: state, LastSource: source, PresenceID: "pr-" + vessel, Since: since}}
}

func TestExpect_Table(t *testing.T) {
	yesterday := day.AddDays(-1)
	m := mooring("annual")
	departing := Stay{AllocationID: "al-y", ContractKind: "transit", Status: "arrived", VesselID: "v-y", FirstNight: yesterday.AddDays(-2), LastNight: yesterday, AllocationKind: "transit"}
	cases := []struct {
		name string
		f    Facts
		at   time.Time
		info bool
		want string
	}{
		{"nothing", Facts{}, now, false, ExpectFree},
		{"transit arrived", Facts{Today: []Stay{transit("arrived", yesterday)}}, now, false, ExpectOccupied},
		{"transit confirmed arrival day", Facts{Today: []Stay{transit("confirmed", day)}}, now, false, ExpectEither},
		{"transit held arrival day", Facts{Today: []Stay{transit("held", day)}}, now, false, ExpectEither},
		{"mooring no presence row", Facts{Today: []Stay{m}}, now, false, ExpectOccupied},
		{"mooring in port", Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", now)}, now, false, ExpectOccupied},
		{"mooring out", Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "staff", now)}, now, false, ExpectFree},
		{"mooring ashore", Facts{Today: []Stay{m}, Presences: presences("v-m", "ashore", "staff", now)}, now, false, ExpectFree},
		{"mooring yard", Facts{Today: []Stay{m}, Presences: presences("v-m", "yard", "staff", now)}, now, false, ExpectFree},
		{"departure day 11:59", Facts{Yesterday: []Stay{departing}}, localAt(11, 59), false, ExpectEither},
		{"departure day 12:00", Facts{Yesterday: []Stay{departing}}, localAt(12, 0), false, ExpectFree},
		{"stay continuing is not a departure", Facts{Today: []Stay{m}, Yesterday: []Stay{m}, Presences: presences("v-m", "out", "staff", now)}, localAt(9, 0), false, ExpectFree},
		{"release not resold", Facts{Today: []Stay{m}, Releases: []Release{{ID: "r1", AllocationID: "al-m", FromNight: day, ToNight: day.AddDays(2)}}}, now, false, ExpectFree},
		{"release resold arrived", Facts{
			Today:    []Stay{m, transit("arrived", day)},
			Releases: []Release{{ID: "r1", AllocationID: "al-m", FromNight: day, ToNight: day.AddDays(2), Resold: true, ResaleStatus: "arrived"}},
		}, now, false, ExpectOccupied},
		{"release resold confirmed (resale only in ResaleStay)", Facts{
			Today: []Stay{m},
			Releases: []Release{{
				ID: "r1", AllocationID: "al-m", FromNight: day, ToNight: day.AddDays(2), Resold: true,
				ResaleStay: func() *Stay { s := transit("confirmed", day); return &s }(), ResaleStatus: "confirmed",
			}},
		}, now, false, ExpectEither},
		{"block", Facts{Today: []Stay{{AllocationID: "al-b", AllocationKind: "block"}}}, now, false, ExpectFree},
		{"maintenance", Facts{Today: []Stay{{AllocationID: "al-x", AllocationKind: "maintenance", VesselID: ""}}}, now, false, ExpectFree},
		{"closure only", Facts{Closed: true}, now, false, ExpectFree},
		{"segment two placements one occupied", Facts{Today: []Stay{transit("confirmed", day), m}}, now, false, ExpectOccupied},
		{"segment two placements either and free", Facts{Today: []Stay{transit("confirmed", day), m}, Presences: presences("v-m", "out", "staff", now)}, now, false, ExpectEither},
		{"pedestal informational", Facts{Today: []Stay{m}}, now, true, ExpectNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := subject(Occupied, now, c.f)
			s.Informational = c.info
			if got := Expect(s, c.at, rome, defSt); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestDiscrepancy_Table(t *testing.T) {
	u := 15 * time.Minute
	cases := []struct {
		phys, exp string
		d         time.Duration
		want      bool
	}{
		{Occupied, ExpectFree, u, true},
		{Occupied, ExpectFree, u - time.Minute, false},
		{Free, ExpectOccupied, u, true},
		{Occupied, ExpectOccupied, u, false},
		{Free, ExpectFree, u, false},
		{Unknown, ExpectFree, u, false},
		{Occupied, ExpectEither, u, false},
		{Free, ExpectEither, u, false},
		{Occupied, ExpectNone, u, false},
	}
	for _, c := range cases {
		if got := Discrepancy(c.phys, c.d, c.exp, defSt); got != c.want {
			t.Fatalf("%v: got %v", c, got)
		}
	}
}

// kinds returns "open:<kind>", "clear:<kind>" and "presence:<event>" labels of the actions.
func labels(as []Action) []string {
	var out []string
	for _, a := range as {
		switch {
		case a.Open != nil:
			out = append(out, "open:"+a.Open.Kind)
		case a.Clear != nil:
			out = append(out, "clear:"+a.Clear.Kind)
		case a.Presence != nil:
			out = append(out, "presence:"+a.Presence.Event)
		}
	}
	return out
}

func TestReconcile_Table(t *testing.T) {
	u := 15 * time.Minute
	m := mooring("annual")
	seasonal := mooring("seasonal")
	enabled := defSt
	enabled.PresenceEffectEnabled = true
	staffSince := now.Add(-10 * 24 * time.Hour)
	rel := Release{ID: "r1", AllocationID: "al-m", FromNight: day.AddDays(-1), ToNight: day.AddDays(3)}
	type tc struct {
		name   string
		st     Settings
		s      Subject
		alerts []AlertKey
		want   []string
	}
	withBattery := func(s Subject, b int) Subject { s.BatteryPct = &b; return s }
	withContact := func(s Subject, c *time.Time) Subject { s.LastContactAt = c; return s }
	info := func(s Subject) Subject { s.Informational = true; return s }
	cases := []tc{
		// R1
		{"R1 at threshold", defSt, subject(Occupied, now.Add(-u), Facts{}), nil, []string{"open:unexpected_occupied"}},
		{"R1 one minute before", defSt, subject(Occupied, now.Add(-u+time.Minute), Facts{}), nil, nil},
		{"R1 already recorded", defSt, subject(Occupied, now.Add(-u), Facts{}), []AlertKey{{DeviceID: "dev-1", Kind: KindUnexpectedOccupied, ConditionKey: "occ:" + timeKey(now.Add(-u))}}, nil},
		{"R1 clears when free", defSt, subject(Free, now, Facts{}), []AlertKey{{DeviceID: "dev-1", Kind: KindUnexpectedOccupied, ConditionKey: "occ:x", Open: true}}, []string{"clear:unexpected_occupied"}},
		{"R1 new occupation replaces the old key", defSt, subject(Occupied, now.Add(-u), Facts{}), []AlertKey{{DeviceID: "dev-1", Kind: KindUnexpectedOccupied, ConditionKey: "occ:old"}}, []string{"clear:unexpected_occupied", "open:unexpected_occupied"}},
		{"R1 not on arrival day", defSt, subject(Occupied, now.Add(-time.Hour), Facts{Today: []Stay{transit("held", day)}}), nil, nil},
		{"R1 unknown physical", defSt, subject(Unknown, now.Add(-time.Hour), Facts{}), nil, nil},
		{"R1 informational", defSt, info(subject(Occupied, now.Add(-time.Hour), Facts{})), nil, nil},
		// R6 wins over R1
		{"R6 not resold", defSt, subject(Occupied, now.Add(-u), Facts{Today: []Stay{m}, Releases: []Release{rel}}), nil, []string{"open:early_return"}},
		{"R6 one minute before", defSt, subject(Occupied, now.Add(-u+time.Minute), Facts{Today: []Stay{m}, Releases: []Release{rel}}), nil, nil},
		{"R6 resold not arrived", defSt, subject(Occupied, now.Add(-u), Facts{
			Today: []Stay{m, transit("confirmed", day)}, Releases: []Release{{ID: "r1", AllocationID: "al-m", FromNight: day, ToNight: day, Resold: true, ResaleStatus: "confirmed"}},
		}), nil, []string{"open:early_return"}},
		{"R6 resale arrived clears", defSt, subject(Occupied, now.Add(-u), Facts{
			Today: []Stay{m, transit("arrived", day)}, Releases: []Release{{ID: "r1", AllocationID: "al-m", FromNight: day, ToNight: day, Resold: true, ResaleStatus: "arrived"}},
		}), []AlertKey{{DeviceID: "dev-1", Kind: KindEarlyReturn, ConditionKey: "release:r1"}}, []string{"clear:early_return"}},
		// R2
		{"R2 transit first night today", defSt, subject(Occupied, localAt(10, 0), Facts{Today: []Stay{transit("confirmed", day)}}), nil, []string{"open:transit_arrival"}},
		{"R2 transit first night yesterday", defSt, subject(Occupied, localAt(1, 0), Facts{Today: []Stay{transit("confirmed", day.AddDays(-1))}}), nil, []string{"open:transit_arrival"}},
		{"R2 occupied before the first night", defSt, subject(Occupied, localAt(0, 0).Add(-time.Minute), Facts{Today: []Stay{transit("confirmed", day)}}), nil, nil},
		{"R2 arrived transit", defSt, subject(Occupied, localAt(10, 0), Facts{Today: []Stay{transit("arrived", day)}}), []AlertKey{{DeviceID: "dev-1", Kind: KindTransitArrival, ConditionKey: "contract:c-t"}}, []string{"clear:transit_arrival"}},
		{"R2 free for U clears", defSt, subject(Free, now.Add(-u), Facts{Today: []Stay{transit("confirmed", day)}}), []AlertKey{{DeviceID: "dev-1", Kind: KindTransitArrival, ConditionKey: "contract:c-t"}}, []string{"clear:transit_arrival"}},
		{"R2 free less than U keeps", defSt, subject(Free, now.Add(-u+time.Minute), Facts{Today: []Stay{transit("confirmed", day)}}), []AlertKey{{DeviceID: "dev-1", Kind: KindTransitArrival, ConditionKey: "contract:c-t"}}, nil},
		// R3
		{"R3 annual free 6 h", enabled, subject(Free, now.Add(-6*time.Hour), Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", staffSince)}), nil, []string{"presence:departed"}},
		{"R3 one minute before", enabled, subject(Free, now.Add(-6*time.Hour+time.Minute), Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", staffSince)}), nil, nil},
		{"R3 no presence row", enabled, subject(Free, now.Add(-6*time.Hour), Facts{Today: []Stay{m}}), nil, []string{"presence:departed"}},
		{"R3 disabled", defSt, subject(Free, now.Add(-6*time.Hour), Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", staffSince)}), nil, nil},
		{"R3 seasonal never", enabled, subject(Free, now.Add(-6*time.Hour), Facts{Today: []Stay{seasonal}}), nil, nil},
		{"R3 transit never", enabled, subject(Free, now.Add(-6*time.Hour), Facts{Today: []Stay{transit("arrived", day.AddDays(-2))}}), nil, nil},
		{"R3 berth emptied before the staff arrival", enabled, subject(Free, now.Add(-7*time.Hour), Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", now.Add(-time.Hour))}), nil, nil},
		// R3b / R4
		{"R3b return after sensor departure", enabled, subject(Occupied, now.Add(-u), Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "sensor", staffSince)}), nil, []string{"presence:arrived"}},
		{"R3b one minute before", enabled, subject(Occupied, now.Add(-u+time.Minute), Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "sensor", staffSince)}), nil, nil},
		{"R4 return after staff departure", enabled, subject(Occupied, now.Add(-u), Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "staff", staffSince)}), nil, []string{"open:return_detected"}},
		{"R4 effect disabled after sensor departure", defSt, subject(Occupied, now.Add(-u), Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "sensor", staffSince)}), nil, []string{"open:return_detected"}},
		{"R4 clears on presence in port", defSt, subject(Occupied, now.Add(-u), Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", now)}), []AlertKey{{DeviceID: "dev-1", Kind: KindReturnDetected, ConditionKey: "presence:pr-v-m"}}, []string{"clear:return_detected"}},
		// R5
		{"R5 three days", defSt, subject(Free, now.Add(-72*time.Hour), Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "staff", now.Add(-80*time.Hour))}), nil, []string{"open:release_proposal"}},
		{"R5 one minute before", defSt, subject(Free, now.Add(-72*time.Hour+time.Minute), Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "staff", now.Add(-80*time.Hour))}), nil, nil},
		{"R5 policy never", defSt, subject(Free, now.Add(-72*time.Hour), Facts{Today: []Stay{func() Stay { s := m; s.ReleasePolicy = "never"; return s }()}, Presences: presences("v-m", "out", "staff", now.Add(-80*time.Hour))}), nil, nil},
		{"R5 release tomorrow exists", defSt, subject(Free, now.Add(-72*time.Hour), Facts{Today: []Stay{m}, Releases: []Release{{ID: "r2", AllocationID: "al-m", FromNight: day.AddDays(1), ToNight: day.AddDays(2)}}, Presences: presences("v-m", "out", "staff", now.Add(-80*time.Hour))}), nil, nil},
		{"R5 allocation ends today", defSt, subject(Free, now.Add(-72*time.Hour), Facts{Today: []Stay{func() Stay { s := m; s.LastNight = day; return s }()}, Presences: presences("v-m", "out", "staff", now.Add(-80*time.Hour))}), nil, nil},
		{"R5 presence in port", defSt, subject(Free, now.Add(-72*time.Hour), Facts{Today: []Stay{m}, Presences: presences("v-m", "in_port", "staff", now.Add(-80*time.Hour))}), nil, nil},
		// R7
		{"R7 silent at threshold", defSt, withContact(subject(Unknown, now, Facts{}), func() *time.Time { c := now.Add(-180 * time.Minute); return &c }()), nil, []string{"open:device_silent"}},
		{"R7 one minute before", defSt, withContact(subject(Unknown, now, Facts{}), func() *time.Time { c := now.Add(-179 * time.Minute); return &c }()), nil, nil},
		{"R7 never contacted", defSt, withContact(subject(Unknown, time.Time{}, Facts{}), nil), nil, []string{"open:device_silent"}},
		{"R7 new contact clears", defSt, subject(Unknown, now, Facts{}), []AlertKey{{DeviceID: "dev-1", Kind: KindDeviceSilent, ConditionKey: "silent:x"}}, []string{"clear:device_silent"}},
		{"R7 informational too", defSt, info(withContact(subject(Unknown, now, Facts{}), nil)), nil, []string{"open:device_silent"}},
		// R8
		{"R8 battery under", defSt, withBattery(subject(Unknown, now, Facts{}), 19), nil, []string{"open:battery_low"}},
		{"R8 battery at threshold", defSt, withBattery(subject(Unknown, now, Facts{}), 20), nil, nil},
		{"R8 recovery band keeps", defSt, withBattery(subject(Unknown, now, Facts{}), 24), []AlertKey{{DeviceID: "dev-1", Kind: KindBatteryLow, ConditionKey: "battery"}}, nil},
		{"R8 recovered", defSt, withBattery(subject(Unknown, now, Facts{}), 25), []AlertKey{{DeviceID: "dev-1", Kind: KindBatteryLow, ConditionKey: "battery"}}, []string{"clear:battery_low"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := labels(Evaluate(Input{Now: now, Loc: rome, Settings: c.st, Subjects: []Subject{c.s}, Alerts: c.alerts}))
			if !slices.Equal(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestReconcile_R5RangeClamps(t *testing.T) {
	m := mooring("annual")
	ret := day.AddDays(4)
	f := Facts{Today: []Stay{m}, Presences: presences("v-m", "out", "staff", now.Add(-80*time.Hour))}
	run := func(f Facts) *OpenAlert {
		for _, a := range Evaluate(Input{Now: now, Loc: rome, Settings: defSt, Subjects: []Subject{subject(Free, now.Add(-73*time.Hour), f)}}) {
			if a.Open != nil {
				return a.Open
			}
		}
		return nil
	}
	o := run(f)
	if o == nil || *o.Refs.ProposedFrom != day.AddDays(1) || *o.Refs.ProposedTo != day.AddDays(7) {
		t.Fatalf("default range: %+v", o)
	}
	p := f.Presences["v-m"]
	p.ExpectedReturnOn = &ret
	f.Presences = map[string]Presence{"v-m": p}
	if o := run(f); o == nil || *o.Refs.ProposedTo != day.AddDays(3) {
		t.Fatalf("expected return clamp: %+v", o)
	}
	m.LastNight = day.AddDays(2)
	f.Today = []Stay{m}
	if o := run(f); o == nil || *o.Refs.ProposedTo != day.AddDays(2) {
		t.Fatalf("last night clamp: %+v", o)
	}
	ret = day.AddDays(1)
	p.ExpectedReturnOn = &ret
	f.Presences = map[string]Presence{"v-m": p}
	if o := run(f); o != nil {
		t.Fatalf("back tomorrow: no proposal, got %+v", o)
	}
}

func TestReconcile_DismissSuppresses(t *testing.T) {
	s := subject(Occupied, now.Add(-time.Hour), Facts{})
	key := "occ:" + timeKey(s.PhysicalSince)
	in := Input{Now: now, Loc: rome, Settings: defSt, Subjects: []Subject{s}, Alerts: []AlertKey{{DeviceID: "dev-1", Kind: KindUnexpectedOccupied, ConditionKey: key}}}
	if got := Evaluate(in); len(got) != 0 {
		t.Fatalf("dismissed condition reopened: %v", labels(got))
	}
	if Stale(in, s, KindUnexpectedOccupied, key) {
		t.Fatal("holding condition is not stale")
	}
	s.Physical, s.PhysicalSince = Free, now
	if !Stale(in, s, KindUnexpectedOccupied, key) {
		t.Fatal("free berth: stale")
	}
}

func TestReconcile_Purity(t *testing.T) {
	s := subject(Occupied, now.Add(-time.Hour), Facts{})
	in := Input{Now: now, Loc: rome, Settings: defSt, Subjects: []Subject{s, s}}
	a, b := Evaluate(in), Evaluate(in)
	if fmt.Sprint(labels(a)) != fmt.Sprint(labels(b)) || len(a) != 1 {
		t.Fatalf("not deterministic or duplicate subject counted: %v", labels(a))
	}
}

func TestRapid_ReconcileDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 12).Draw(t, "n")
		states := []string{Occupied, Free, Unknown}
		var subs []Subject
		var alerts []AlertKey
		ids := map[string]bool{}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("dev-%02d", i)
			ids[id] = true
			since := now.Add(-time.Duration(rapid.IntRange(0, 6000).Draw(t, "since")) * time.Minute)
			var f Facts
			switch rapid.IntRange(0, 4).Draw(t, "facts") {
			case 1:
				f.Today = []Stay{mooring("annual")}
				f.Presences = presences("v-m", rapid.SampledFrom([]string{"", "in_port", "out"}).Draw(t, "pr"), rapid.SampledFrom([]string{"staff", "sensor"}).Draw(t, "src"), now.Add(-200*time.Hour))
			case 2:
				f.Today = []Stay{transit(rapid.SampledFrom([]string{"confirmed", "arrived"}).Draw(t, "ts"), day.AddDays(-rapid.IntRange(0, 1).Draw(t, "tf")))}
			case 3:
				f.Today = []Stay{mooring("annual")}
				f.Releases = []Release{{ID: "r-" + id, AllocationID: "al-m", FromNight: day, ToNight: day.AddDays(2), Resold: rapid.Bool().Draw(t, "resold")}}
			}
			s := subject(rapid.SampledFrom(states).Draw(t, "phys"), since, f)
			s.DeviceID = id
			b := rapid.IntRange(0, 100).Draw(t, "battery")
			s.BatteryPct = &b
			s.Informational = rapid.IntRange(0, 9).Draw(t, "info") == 0
			subs = append(subs, s)
			if rapid.Bool().Draw(t, "alert") {
				alerts = append(alerts, AlertKey{DeviceID: id, Kind: rapid.SampledFrom(kindOrder).Draw(t, "kind"), ConditionKey: "k"})
			}
		}
		st := defSt
		st.PresenceEffectEnabled = rapid.Bool().Draw(t, "effect")
		in := Input{Now: now, Loc: rome, Settings: st, Subjects: subs, Alerts: alerts}
		want := Evaluate(in)
		perm := rapid.Permutation(subs).Draw(t, "perm")
		aperm := rapid.Permutation(alerts).Draw(t, "aperm")
		got := Evaluate(Input{Now: now, Loc: rome, Settings: st, Subjects: perm, Alerts: aperm})
		if fmt.Sprintf("%+v", flatten(got)) != fmt.Sprintf("%+v", flatten(want)) {
			t.Fatalf("order dependent:\n%v\n%v", flatten(got), flatten(want))
		}
		for _, a := range got {
			var dev string
			switch {
			case a.Open != nil:
				dev = a.Open.DeviceID
			case a.Clear != nil:
				dev = a.Clear.DeviceID
			case a.Presence != nil:
				dev = a.Presence.DeviceID
				if a.Presence.VesselID != "v-m" {
					t.Fatalf("presence for a non-mooring vessel: %+v", a.Presence)
				}
			}
			if !ids[dev] {
				t.Fatalf("action outside the input: %s", dev)
			}
		}
	})
}

func flatten(as []Action) []string {
	var out []string
	for _, a := range as {
		switch {
		case a.Open != nil:
			out = append(out, fmt.Sprintf("open %s %s %s", a.Open.DeviceID, a.Open.Kind, a.Open.ConditionKey))
		case a.Clear != nil:
			out = append(out, fmt.Sprintf("clear %s %s", a.Clear.DeviceID, a.Clear.Kind))
		case a.Presence != nil:
			out = append(out, fmt.Sprintf("presence %s %s %s", a.Presence.DeviceID, a.Presence.Event, a.Presence.At))
		}
	}
	return out
}

// BenchmarkReconcile_1000 evaluates the rules on 1,000 subjects (m12-plan §8.4 budget: the pure part of a run).
func BenchmarkReconcile_1000(b *testing.B) {
	subs := make([]Subject, 1000)
	for i := range subs {
		f := Facts{}
		switch i % 4 {
		case 1:
			f.Today = []Stay{mooring("annual")}
			f.Presences = presences("v-m", "in_port", "staff", now.Add(-100*time.Hour))
		case 2:
			f.Today = []Stay{transit("confirmed", day)}
		}
		s := subject([]string{Occupied, Free, Unknown}[i%3], now.Add(-time.Duration(i)*time.Minute), f)
		s.DeviceID = fmt.Sprintf("dev-%04d", i)
		subs[i] = s
	}
	in := Input{Now: now, Loc: rome, Settings: defSt, Subjects: subs}
	b.ResetTimer()
	for b.Loop() {
		_ = Evaluate(in)
	}
}
