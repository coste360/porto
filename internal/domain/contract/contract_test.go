package contract

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

var (
	mooringStatuses = []Status{StatusDraft, StatusAwaitingSignature, StatusActive, StatusExpired, StatusRenewed, StatusTerminated, StatusCancelled, StatusSuspended}
	transitStatuses = []Status{StatusHeld, StatusPendingPayment, StatusAwaitingApproval, StatusConfirmed, StatusArrived, StatusDeparted, StatusCancelled, StatusExpired, StatusNoShow}
	quoteStatuses   = []Status{QuoteDraft, QuoteSent, QuoteAccepted, QuoteDeclined, QuoteExpired}
	allActions      = []Action{
		ActionCreate, ActionCreateActive, ActionActivate, ActionCancel, ActionTerminate, ActionExpire,
		ActionConfirm, ActionNoShow, ActionArrive, ActionDepart, ActionChangeStay, ActionUpdate, ActionSend, ActionAccept,
		ActionDecline, ActionRequestSignature, ActionSign, ActionSignatureFailed, ActionSuspend, ActionResume,
		ActionExpireRenewed, ActionTacitRenew, ActionAddendum, ActionChangeVessel,
	}
)

// TestContract_TransitionTable checks every (state, action) pair of the three machines: the pairs of m3-plan §4
// lead where the table says, every other pair is ErrInvalidTransition with the state and the action named.
func TestContract_TransitionTable(t *testing.T) {
	want := map[string]map[[2]string]Status{
		// m3-plan §4.1 + m4-plan §3.1.
		"mooring": {
			{"", "create"}: StatusDraft, {"", "create_active"}: StatusActive, {"draft", "activate"}: StatusActive,
			{"draft", "cancel"}: StatusCancelled, {"draft", "update"}: StatusDraft, {"active", "update"}: StatusActive,
			{"active", "terminate"}: StatusTerminated, {"active", "expire"}: StatusExpired,
			{"draft", "request_signature"}: StatusAwaitingSignature, {"awaiting_signature", "sign"}: StatusActive,
			{"awaiting_signature", "activate"}: StatusActive, {"awaiting_signature", "signature_failed"}: StatusDraft,
			{"awaiting_signature", "cancel"}: StatusCancelled, {"active", "suspend"}: StatusSuspended,
			{"suspended", "resume"}: StatusActive, {"suspended", "terminate"}: StatusTerminated,
			{"active", "expire_renewed"}: StatusRenewed, {"active", "tacit_renew"}: StatusRenewed,
			{"awaiting_signature", "update"}: StatusAwaitingSignature, {"suspended", "update"}: StatusSuspended,
			{"draft", "addendum"}: StatusDraft, {"awaiting_signature", "addendum"}: StatusAwaitingSignature,
			{"active", "addendum"}: StatusActive, {"suspended", "addendum"}: StatusSuspended,
			{"draft", "change_vessel"}: StatusDraft, {"awaiting_signature", "change_vessel"}: StatusAwaitingSignature,
			{"active", "change_vessel"}: StatusActive, {"suspended", "change_vessel"}: StatusSuspended,
		},
		"transit": {
			{"", "create"}: StatusHeld, {"held", "confirm"}: StatusConfirmed,
			{"held", "cancel"}: StatusCancelled, {"held", "expire"}: StatusExpired, {"held", "arrive"}: StatusHeld,
			{"held", "change_stay"}: StatusHeld, {"held", "update"}: StatusHeld, {"confirmed", "cancel"}: StatusCancelled,
			{"confirmed", "no_show"}: StatusNoShow, {"confirmed", "arrive"}: StatusArrived,
			{"confirmed", "change_stay"}: StatusConfirmed, {"confirmed", "update"}: StatusConfirmed,
			{"arrived", "depart"}: StatusDeparted, {"arrived", "change_stay"}: StatusArrived, {"arrived", "update"}: StatusArrived,
		},
		"quote": {
			{"", "create"}: QuoteDraft, {"draft", "update"}: QuoteDraft, {"draft", "send"}: QuoteSent,
			{"draft", "accept"}: QuoteAccepted, {"sent", "accept"}: QuoteAccepted, {"draft", "decline"}: QuoteDeclined,
			{"sent", "decline"}: QuoteDeclined, {"draft", "expire"}: QuoteExpired, {"sent", "expire"}: QuoteExpired,
		},
	}
	machines := []struct {
		name     string
		table    []Transition
		statuses []Status
	}{
		{"mooring", MooringTransitions, mooringStatuses},
		{"transit", TransitTransitions, transitStatuses},
		{"quote", QuoteTransitions, quoteStatuses},
	}
	for _, m := range machines {
		if len(m.table) != len(want[m.name]) {
			t.Errorf("%s: %d rows, want %d", m.name, len(m.table), len(want[m.name]))
		}
		for _, from := range append([]Status{StatusNone}, m.statuses...) {
			for _, a := range allActions {
				tr, err := Next(m.table, from, a)
				to, ok := want[m.name][[2]string{string(from), string(a)}]
				switch {
				case ok && err != nil:
					t.Errorf("%s %s/%s: %v", m.name, from, a, err)
				case ok && tr.To != to:
					t.Errorf("%s %s/%s → %s, want %s", m.name, from, a, tr.To, to)
				case !ok && !errors.Is(err, ErrInvalidTransition):
					t.Errorf("%s %s/%s allowed (→ %s)", m.name, from, a, tr.To)
				case !ok:
					var te *TransitionError
					if !errors.As(err, &te) || te.From != string(from) || te.Action != a {
						t.Errorf("%s %s/%s: error %v", m.name, from, a, err)
					}
				}
			}
		}
		for _, tr := range m.table {
			for _, typ := range tr.Events {
				if _, ok := event.Lookup(typ); !ok {
					t.Errorf("%s %s/%s: unknown event %s", m.name, tr.From, tr.Action, typ)
				}
			}
		}
	}
	// Statuses reserved for later milestones are never reached (M5 pending_payment, M8 awaiting_approval).
	for _, s := range []Status{StatusPendingPayment, StatusAwaitingApproval} {
		for _, table := range [][]Transition{MooringTransitions, TransitTransitions} {
			for _, tr := range table {
				if tr.To == s || tr.From == s {
					t.Errorf("status %s appears in a transition", s)
				}
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// TestContract_ConfirmedOnlyInConfirmTransit is the static half of INV-09: no code under internal/ sets a
// transit to confirmed except confirmTx (confirm.go), and the confirm transitions are looked up only there
// (states.go defines them).
func TestContract_ConfirmedOnlyInConfirmTransit(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal")
	assign := regexp.MustCompile(`(?i)\bstatus\s*(=|:)\s*(string\()?(contract\.)?(StatusConfirmed|"confirmed")`)
	sqlSet := regexp.MustCompile(`(?i)status\s*=\s*'confirmed'`)
	lookup := regexp.MustCompile(`\bActionConfirm\b`)
	// The table itself: the only way into confirmed is confirm from held (INV-09).
	for _, tr := range TransitTransitions {
		if tr.To == StatusConfirmed && tr.From != StatusConfirmed && (tr.From != StatusHeld || tr.Action != ActionConfirm) {
			t.Errorf("transition %s/%s reaches confirmed outside confirmTx", tr.From, tr.Action)
		}
	}
	allowed := map[string]bool{
		filepath.Join(root, "domain", "contract", "states.go"):  true,
		filepath.Join(root, "domain", "contract", "confirm.go"): true,
		filepath.Join(root, "domain", "event", "payloads.go"):   true, // sample event payloads
	}
	err := filepath.WalkDir(root, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(line), "//") || allowed[path] {
				continue
			}
			if assign.MatchString(line) || sqlSet.MatchString(line) || lookup.MatchString(line) {
				t.Errorf("%s:%d sets or looks up the confirmed status outside confirmTx: %s", path, n, strings.TrimSpace(line))
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestContractCode_Scenario(t *testing.T) {
	cases := []struct {
		series string
		year   int
		n      int64
		want   string
	}{
		{SeriesMooring, 2026, 1, "ORM-2026-0001"},
		{SeriesTransit, 2027, 42, "TRN-2027-0042"},
		{SeriesQuote, 2027, 12345, "PRV-2027-12345"},
		{"storage", 2027, 3, "CTR-2027-0003"},
	}
	for _, c := range cases {
		if got := ContractCode(c.series, c.year, c.n); got != c.want {
			t.Errorf("%s %d %d: %s, want %s", c.series, c.year, c.n, got, c.want)
		}
	}
	if got := Code(&store.Contract{Kind: KindTransit, Year: 2027, Number: 7}); got != "TRN-2027-0007" {
		t.Errorf("Code: %s", got)
	}
}

func TestDefaultEndsOn_Scenario(t *testing.T) {
	cases := []struct {
		term, start, want string
		ok                bool
	}{
		{TermAnnual, "2027-03-15", "2028-03-15", true},
		{TermAnnual, "2028-02-29", "2029-02-28", true},
		{TermMonthly, "2027-01-31", "2027-02-28", true},
		{TermMonthly, "2027-12-10", "2028-01-10", true},
		{TermMonthly, "2028-01-31", "2028-02-29", true},
		{TermSeasonal, "2027-05-01", "", false},
		{TermPeriod, "2027-05-01", "", false},
	}
	for _, c := range cases {
		got, ok := DefaultEndsOn(c.term, calendar.MustParse(c.start))
		if ok != c.ok || (ok && got.String() != c.want) {
			t.Errorf("%s %s: %s %v, want %s %v", c.term, c.start, got, ok, c.want, c.ok)
		}
	}
}

func TestContractTypeOf_Scenario(t *testing.T) {
	cases := []struct {
		kind, term string
		want       pricing.ContractType
	}{
		{KindTransit, "", pricing.ContractTransit},
		{KindMooring, TermAnnual, pricing.ContractAnnual},
		{KindMooring, TermSeasonal, pricing.ContractSeasonal},
		{KindMooring, TermMonthly, pricing.ContractMonthly},
		{KindMooring, TermPeriod, pricing.ContractMonthly},
	}
	for _, c := range cases {
		if got := PricingType(c.kind, c.term); got != c.want {
			t.Errorf("%s/%s: %s", c.kind, c.term, got)
		}
	}
}

func TestDecodePolicy_RejectsUnknownKeys(t *testing.T) {
	p, err := DecodePolicy(store.JSON(`{"version":1,"reprice_on_move":true,"release_policy":"never","allow_segments":false}`))
	if err != nil || !p.RepriceOnMove || p.ReleasePolicy != ReleaseNever || p.AllowSegments {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := DecodePolicy(store.JSON(`{"version":1,"cancellation":"x"}`)); err == nil {
		t.Fatal("unknown key accepted")
	}
	if p, err := DecodePolicy(nil); err != nil || p.Version != 0 {
		t.Fatalf("empty: %+v %v", p, err)
	}
	v := &auth.ValidationError{}
	bad := "sometimes"
	ver := 3
	mergePolicy(v, Policy{Version: 1}, &PolicyInput{ReleasePolicy: &bad, Version: &ver})
	if len(v.Fields) != 2 {
		t.Fatalf("fields %+v", v.Fields)
	}
}

func TestCheckSegments_Scenario(t *testing.T) {
	x := int64(100)
	b := store.NewID()
	seg := func(typ string, x *int64, first, last string) Segment {
		return Segment{ResourceType: typ, ResourceID: b, X: x, First: calendar.MustParse(first), Last: calendar.MustParse(last)}
	}
	arr, dep := calendar.MustParse("2027-06-10"), calendar.MustParse("2027-06-13")
	cases := []struct {
		name string
		segs []Segment
		max  int
		want []string // field codes
	}{
		{"one berth", []Segment{seg("berth", nil, "2027-06-10", "2027-06-12")}, 1, nil},
		{"two contiguous", []Segment{seg("berth", nil, "2027-06-10", "2027-06-10"), seg("dynamic_area", &x, "2027-06-11", "2027-06-12")}, 2, nil},
		{"none", nil, 1, []string{"p:required"}},
		{"too many", []Segment{seg("berth", nil, "2027-06-10", "2027-06-10"), seg("berth", nil, "2027-06-11", "2027-06-12")}, 1, []string{"p:too_many"}},
		{"gap", []Segment{seg("berth", nil, "2027-06-10", "2027-06-10"), seg("berth", nil, "2027-06-12", "2027-06-12")}, 2, []string{"p[1].first_night:not_contiguous"}},
		{"short", []Segment{seg("berth", nil, "2027-06-10", "2027-06-11")}, 1, []string{"p:not_covering"}},
		{"area without x", []Segment{seg("dynamic_area", nil, "2027-06-10", "2027-06-12")}, 1, []string{"p[0].x_cm:required"}},
		{"berth with x", []Segment{seg("berth", &x, "2027-06-10", "2027-06-12")}, 1, []string{"p[0].x_cm:not_allowed"}},
		{"bad type", []Segment{seg("buoy", nil, "2027-06-10", "2027-06-12")}, 1, []string{"p[0].resource_type:invalid"}},
	}
	for _, c := range cases {
		v := &auth.ValidationError{}
		checkSegments(v, "p", c.segs, arr, dep, c.max)
		var got []string
		for _, f := range v.Fields {
			got = append(got, f.Field+":"+f.Code)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDecodeSnapshot_Absent(t *testing.T) {
	if DecodeSnapshot(nil) != nil || DecodeSnapshot(store.JSON(`{"x":1}`)) != nil {
		t.Fatal("snapshot without version decoded")
	}
}
