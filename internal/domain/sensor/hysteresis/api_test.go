package hysteresis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
	"time"

	"pgregory.net/rapid"
)

var day = time.Date(2027, 6, 12, 0, 0, 0, 0, time.UTC)

func at(hhmm string) time.Time {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(err)
	}
	return day.Add(time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute)
}

func def(n int) Params { return Params{Readings: n, Window: 5 * time.Minute} }

func TestHysteresis_Table(t *testing.T) {
	cases := []struct {
		name   string
		s      State
		p      Params
		rs     []Reading
		want   State
		change *Change
		out    Outcome
	}{
		{
			name: "H1 first reading becomes candidate", s: Initial(), p: def(2),
			rs:   []Reading{{at("10:00"), Occupied}},
			want: State{Confirmed: Unknown, Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")},
		},
		{
			name: "H2 second agreeing reading confirms at the first",
			s:    State{Confirmed: Unknown, Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")},
			p:    def(2), rs: []Reading{{at("10:01"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:00"), LastObservedAt: at("10:01")},
			change: &Change{From: Unknown, To: Occupied, At: at("10:00"), Reason: ReasonReadings},
		},
		{
			name: "H3 dissenting reading becomes candidate",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("09:00"), LastObservedAt: at("10:04")},
			p:    def(2), rs: []Reading{{at("10:05"), Free}},
			want: State{Confirmed: Occupied, ConfirmedSince: at("09:00"), Candidate: Free, CandidateSince: at("10:05"), CandidateCount: 1, LastObservedAt: at("10:05")},
		},
		{
			name: "H4 wave clears the candidate",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("09:00"), Candidate: Free, CandidateSince: at("10:05"), CandidateCount: 1, LastObservedAt: at("10:05")},
			p:    def(2), rs: []Reading{{at("10:06"), Occupied}},
			want: State{Confirmed: Occupied, ConfirmedSince: at("09:00"), LastObservedAt: at("10:06")},
		},
		{
			name: "H5 second free reading confirms free at the first",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("09:00"), Candidate: Free, CandidateSince: at("10:05"), CandidateCount: 1, LastObservedAt: at("10:05")},
			p:    def(2), rs: []Reading{{at("10:06"), Free}},
			want:   State{Confirmed: Free, ConfirmedSince: at("10:05"), LastObservedAt: at("10:06")},
			change: &Change{From: Occupied, To: Free, At: at("10:05"), Reason: ReasonReadings},
		},
		{
			name: "H6 unknown replaces a free candidate",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("09:00"), Candidate: Free, CandidateSince: at("10:05"), CandidateCount: 1, LastObservedAt: at("10:05")},
			p:    def(2), rs: []Reading{{at("10:06"), Unknown}},
			want: State{Confirmed: Occupied, ConfirmedSince: at("09:00"), Candidate: Unknown, CandidateSince: at("10:06"), CandidateCount: 1, LastObservedAt: at("10:06")},
		},
		{
			name: "H7 older reading is out of order",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("09:00"), LastObservedAt: at("10:06")},
			p:    def(2), rs: []Reading{{at("09:59"), Free}},
			want: State{Confirmed: Occupied, ConfirmedSince: at("09:00"), LastObservedAt: at("10:06")},
			out:  OutOfOrder,
		},
		{
			name: "H7b equal instant is out of order",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("09:00"), LastObservedAt: at("10:06")},
			p:    def(2), rs: []Reading{{at("10:06"), Free}},
			want: State{Confirmed: Occupied, ConfirmedSince: at("09:00"), LastObservedAt: at("10:06")},
			out:  OutOfOrder,
		},
		{
			name: "H8 N=3 duration confirms",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")},
			p:    def(3), rs: []Reading{{at("10:06"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:00"), LastObservedAt: at("10:06")},
			change: &Change{From: Free, To: Occupied, At: at("10:00"), Reason: ReasonDuration},
		},
		{
			name: "H8b N=3 just under T does not confirm",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")},
			p:    def(3), rs: []Reading{{at("10:04"), Occupied}},
			want: State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 2, LastObservedAt: at("10:04")},
		},
		{
			name: "H8c N=3 exactly T confirms",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")},
			p:    def(3), rs: []Reading{{at("10:05"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:00"), LastObservedAt: at("10:05")},
			change: &Change{From: Free, To: Occupied, At: at("10:00"), Reason: ReasonDuration},
		},
		{
			name: "H9 N=3 count wins over duration",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 2, LastObservedAt: at("10:01")},
			p:    def(3), rs: []Reading{{at("10:02"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:00"), LastObservedAt: at("10:02")},
			change: &Change{From: Free, To: Occupied, At: at("10:00"), Reason: ReasonReadings},
		},
		{
			name: "H9b readings wins when both hold",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 2, LastObservedAt: at("10:01")},
			p:    def(3), rs: []Reading{{at("10:30"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:00"), LastObservedAt: at("10:30")},
			change: &Change{From: Free, To: Occupied, At: at("10:00"), Reason: ReasonReadings},
		},
		{
			name: "H10 N=1 confirms at once",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), LastObservedAt: at("09:00")},
			p:    def(1), rs: []Reading{{at("10:00"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:00"), LastObservedAt: at("10:00")},
			change: &Change{From: Free, To: Occupied, At: at("10:00"), Reason: ReasonReadings},
		},
		{
			name: "H12 after silence two readings confirm",
			s:    State{Confirmed: Unknown, ConfirmedSince: at("10:36"), LastObservedAt: at("10:06")},
			p:    def(2), rs: []Reading{{at("10:40"), Occupied}, {at("10:41"), Occupied}},
			want:   State{Confirmed: Occupied, ConfirmedSince: at("10:40"), LastObservedAt: at("10:41")},
			change: &Change{From: Unknown, To: Occupied, At: at("10:40"), Reason: ReasonReadings},
		},
		{
			name: "H14 back to confirmed clears",
			s:    State{Confirmed: Free, ConfirmedSince: at("08:00"), Candidate: Occupied, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")},
			p:    def(2), rs: []Reading{{at("10:01"), Free}},
			want: State{Confirmed: Free, ConfirmedSince: at("08:00"), LastObservedAt: at("10:01")},
		},
		{
			name: "unknown readings confirm unknown",
			s:    State{Confirmed: Occupied, ConfirmedSince: at("08:00"), LastObservedAt: at("09:00")},
			p:    def(2), rs: []Reading{{at("10:00"), Unknown}, {at("10:01"), Unknown}},
			want:   State{Confirmed: Unknown, ConfirmedSince: at("10:00"), LastObservedAt: at("10:01")},
			change: &Change{From: Occupied, To: Unknown, At: at("10:00"), Reason: ReasonReadings},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.s
			var got *Change
			var out Outcome
			for _, r := range c.rs {
				var ch *Change
				s, ch, out = Step(s, r, c.p)
				if ch != nil {
					got = ch
				}
			}
			if s != c.want {
				t.Fatalf("state\n got %+v\nwant %+v", s, c.want)
			}
			if (got == nil) != (c.change == nil) || (got != nil && *got != *c.change) {
				t.Fatalf("change got %+v want %+v", got, c.change)
			}
			want := c.out
			if want == 0 {
				want = Applied
			}
			if out != want {
				t.Fatalf("outcome %v", out)
			}
		})
	}
	t.Run("H13 a duplicate instant in one batch is never counted", func(t *testing.T) {
		s := State{Confirmed: Free, ConfirmedSince: at("08:00"), LastObservedAt: at("09:00")}
		got, ch := Fold(s, []Reading{{at("10:00"), Occupied}, {at("10:00"), Occupied}}, def(2))
		if len(ch) != 0 || got.CandidateCount != 1 {
			t.Fatalf("%+v %+v", got, ch)
		}
	})
	t.Run("H15 device moved", func(t *testing.T) {
		s := State{Confirmed: Occupied, ConfirmedSince: at("08:00"), Candidate: Free, CandidateSince: at("10:00"), CandidateCount: 1, LastObservedAt: at("10:00")}
		got, ch := Moved(s, at("11:00"))
		if ch == nil || *ch != (Change{From: Occupied, To: Unknown, At: at("11:00"), Reason: ReasonDeviceMoved}) {
			t.Fatalf("change %+v", ch)
		}
		if got != (State{Confirmed: Unknown, ConfirmedSince: at("11:00"), LastObservedAt: at("10:00")}) {
			t.Fatalf("state %+v", got)
		}
		if _, ch := Moved(Initial(), at("11:00")); ch != nil {
			t.Fatal("an unknown device emits no change")
		}
		if _, _, out := Step(got, Reading{at("10:30"), Free}, def(2)); out != OutOfOrder {
			t.Fatal("a reading from before the move must be out of order")
		}
	})
}

func TestHysteresis_Silence(t *testing.T) {
	s := State{Confirmed: Occupied, ConfirmedSince: at("09:00"), Candidate: Free, CandidateSince: at("10:06"), CandidateCount: 1, LastObservedAt: at("10:06")}
	if _, ch := Silence(s, at("10:35"), 30*time.Minute); ch != nil {
		t.Fatal("silence before the threshold")
	}
	// H11.
	got, ch := Silence(s, at("10:36"), 30*time.Minute)
	if ch == nil || *ch != (Change{From: Occupied, To: Unknown, At: at("10:36"), Reason: ReasonSilence}) {
		t.Fatalf("change %+v", ch)
	}
	if got != (State{Confirmed: Unknown, ConfirmedSince: at("10:36"), LastObservedAt: at("10:06")}) {
		t.Fatalf("state %+v", got)
	}
	// The instant is LastObservedAt + offline even when the job runs late.
	if _, ch := Silence(s, at("12:00"), 30*time.Minute); ch == nil || !ch.At.Equal(at("10:36")) {
		t.Fatalf("late job %+v", ch)
	}
	// A late reading observed before the silence instant cannot rewrite it.
	if _, _, out := Step(got, Reading{at("10:20"), Free}, def(2)); out != OutOfOrder {
		t.Fatal("reading before the silence instant must be out of order")
	}
	if _, _, out := Step(got, Reading{at("10:40"), Free}, def(2)); out != Applied {
		t.Fatal("reading after the silence instant applies")
	}
	if _, ch := Silence(got, at("13:00"), 30*time.Minute); ch != nil {
		t.Fatal("unknown stays unknown")
	}
	if _, ch := Silence(Initial(), at("13:00"), 30*time.Minute); ch != nil {
		t.Fatal("never read")
	}
}

func TestHysteresis_Purity(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "api.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path != "sort" && path != "time" {
			t.Errorf("api.go imports %s", path)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "time" && (x.Sel.Name == "Now" || x.Sel.Name == "Since" || x.Sel.Name == "Until") {
				t.Errorf("api.go uses time.%s", x.Sel.Name)
			}
		case *ast.Ident:
			if x.Name == "float32" || x.Name == "float64" {
				t.Error("api.go uses floats")
			}
		}
		return true
	})
}

var states = []string{Occupied, Free, Unknown}

// genReadings draws readings over a few hours with long agreeing runs, single dissenting waves, duplicate instants
// and out-of-order arrivals.
func genReadings(t *rapid.T) []Reading {
	n := rapid.IntRange(0, 60).Draw(t, "n")
	cur := rapid.SampledFrom(states).Draw(t, "start")
	minute := 0
	var rs []Reading
	for range n {
		minute += rapid.IntRange(0, 4).Draw(t, "gap") // 0 = duplicate instant
		st := cur
		switch rapid.IntRange(0, 9).Draw(t, "kind") {
		case 0:
			cur = rapid.SampledFrom(states).Draw(t, "flip")
			st = cur
		case 1:
			st = rapid.SampledFrom(states).Draw(t, "wave")
		}
		rs = append(rs, Reading{ObservedAt: day.Add(time.Duration(minute) * time.Minute), State: st})
	}
	// Shuffle a few to arrive out of order.
	for k := rapid.IntRange(0, 5).Draw(t, "swaps"); k > 0 && len(rs) > 1; k-- {
		i := rapid.IntRange(0, len(rs)-1).Draw(t, "i")
		j := rapid.IntRange(0, len(rs)-1).Draw(t, "j")
		rs[i], rs[j] = rs[j], rs[i]
	}
	return rs
}

type stepped struct {
	r      Reading
	change *Change
}

func TestRapid_HysteresisFold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := Params{Readings: rapid.IntRange(1, 5).Draw(t, "N"), Window: time.Duration(rapid.IntRange(1, 30).Draw(t, "T")) * time.Minute}
		rs := genReadings(t)
		sorted := append([]Reading(nil), rs...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ObservedAt.Before(sorted[j].ObservedAt) })

		// Step-by-step on the arrival order, keeping only applied readings.
		s := Initial()
		var applied []stepped
		var changes []Change
		for _, r := range rs {
			var ch *Change
			var out Outcome
			s, ch, out = Step(s, r, p)
			if out == Applied {
				applied = append(applied, stepped{r, ch})
			}
			if ch != nil {
				changes = append(changes, *ch)
			}
		}

		// P1: a change to X at At is backed by consecutive applied X readings from At to the emitting reading, with
		// count >= N or span >= T.
		for i, a := range applied {
			if a.change == nil {
				continue
			}
			x := a.change.To
			count := 0
			for j := i; j >= 0 && !applied[j].r.ObservedAt.Before(a.change.At); j-- {
				if applied[j].r.State != x {
					t.Fatalf("P1: change %+v backed by a %s reading", a.change, applied[j].r.State)
				}
				count++
			}
			if count < p.Readings && a.r.ObservedAt.Sub(a.change.At) < p.Window {
				t.Fatalf("P1: change %+v with %d readings over %v", a.change, count, a.r.ObservedAt.Sub(a.change.At))
			}
		}

		// P3: changes alternate, At non-decreasing and not after the last observed reading.
		prev := Unknown
		var last time.Time
		for _, c := range changes {
			if c.From != prev || c.From == c.To {
				t.Fatalf("P3: %+v after %s", c, prev)
			}
			if c.At.Before(last) || c.At.After(s.LastObservedAt) {
				t.Fatalf("P3: instant %v", c.At)
			}
			prev, last = c.To, c.At
		}
		if s.Confirmed != prev {
			t.Fatalf("P3: confirmed %s, last change to %s", s.Confirmed, prev)
		}

		// P4: one batch, or any split of the sorted readings into consecutive batches, gives the same result.
		whole, wholeCh := Fold(Initial(), sorted, p)
		cut := rapid.IntRange(0, len(sorted)).Draw(t, "cut")
		s1, c1 := Fold(Initial(), sorted[:cut], p)
		s2, c2 := Fold(s1, sorted[cut:], p)
		if s2 != whole || !sameChanges(append(c1, c2...), wholeCh) {
			t.Fatalf("P4: split at %d differs", cut)
		}
		// Fold sorts: folding the arrival order equals folding the sorted order.
		if fs, fc := Fold(Initial(), rs, p); fs != whole || !sameChanges(fc, wholeCh) {
			t.Fatal("P4: Fold is not order independent")
		}

		// P5: removing duplicate instants changes nothing.
		var dedup []Reading
		for i, r := range sorted {
			if i > 0 && r.ObservedAt.Equal(sorted[i-1].ObservedAt) {
				continue
			}
			dedup = append(dedup, r)
		}
		if ds, dc := Fold(Initial(), dedup, p); ds != whole || !sameChanges(dc, wholeCh) {
			t.Fatal("P5: duplicates changed the fold")
		}

		// P6: N = 1 means the confirmed state is the last reading's.
		if p.Readings == 1 && len(dedup) > 0 && whole.Confirmed != dedup[len(dedup)-1].State {
			t.Fatalf("P6: confirmed %s, last %s", whole.Confirmed, dedup[len(dedup)-1].State)
		}

		// P2: with N >= 2 a single dissenting reading between agreeing ones never confirms its state.
		if p.Readings >= 2 {
			for i := 1; i+1 < len(applied); i++ {
				w := applied[i].r.State
				if applied[i-1].r.State == w || applied[i+1].r.State == w {
					continue
				}
				for _, k := range []int{i, i + 1} {
					if c := applied[k].change; c != nil && c.To == w {
						t.Fatalf("P2: wave %s at %v confirmed", w, applied[i].r.ObservedAt)
					}
				}
			}
		}
	})
}

func sameChanges(a, b []Change) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func BenchmarkHysteresis_Fold(b *testing.B) {
	rs := make([]Reading, 500)
	for i := range rs {
		st := Free
		if (i/7)%2 == 1 {
			st = Occupied
		}
		rs[i] = Reading{ObservedAt: day.Add(time.Duration(i) * time.Minute), State: st}
	}
	p := def(2)
	b.ReportAllocs()
	for b.Loop() {
		Fold(Initial(), rs, p)
	}
}
