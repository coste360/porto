package schedule

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

func d(s string) calendar.Date { return calendar.MustParse(s) }

// quarterly is the M4 demo policy: deposit 30 % due in 7 days, then 3 quarterly instalments.
func quarterly() PaymentPolicy {
	return PaymentPolicy{
		Deposit: &Deposit{BP: 3000}, DepositDueDays: 7, IssueDaysBeforeDue: 15,
		Schedule: Schedule{Mode: ModeInterval, EveryMonths: 3, Count: 3, FirstDueOffsetMonths: 3},
	}
}

func TestInstallmentPlan_Scenario(t *testing.T) {
	type want struct {
		kind   string
		issue  string
		due    string
		amount money.Cents
	}
	cases := []struct {
		name string
		in   Input
		want []want
		err  error
	}{
		{
			name: "annual 30% + 3 quarters",
			in:   Input{Total: 100_001, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2025-12-01"), Payment: quarterly()},
			want: []want{
				{KindDeposit, "2025-12-01", "2025-12-08", 30_000},
				{KindInstallment, "2026-03-17", "2026-04-01", 23_333},
				{KindInstallment, "2026-06-16", "2026-07-01", 23_333},
				{KindInstallment, "2026-09-16", "2026-10-01", 23_335},
			},
		},
		{
			name: "fixed dates rolled over the year",
			in: Input{Total: 10_000, StartsOn: d("2026-06-01"), EndsOn: d("2027-06-01"), Today: d("2026-05-20"), Payment: PaymentPolicy{
				Schedule: Schedule{Mode: ModeFixedDates, Dates: []FixedDue{{MonthDay: "01-15", BP: 5000}, {MonthDay: "07-01", BP: 5000}}},
			}},
			want: []want{
				{KindInstallment, "2026-07-01", "2026-07-01", 5000},
				{KindInstallment, "2027-01-15", "2027-01-15", 5000},
			},
		},
		{
			name: "past dates merged into the first due item",
			in: Input{Total: 9_000, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-05-10"), Payment: PaymentPolicy{
				Schedule: Schedule{Mode: ModeInterval, EveryMonths: 3, Count: 3},
			}},
			want: []want{
				{KindInstallment, "2026-07-01", "2026-07-01", 9_000},
			},
		},
		{
			name: "every date past: one item due today",
			in: Input{Total: 9_000, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-12-10"), Payment: PaymentPolicy{
				Schedule: Schedule{Mode: ModeInterval, EveryMonths: 3, Count: 3},
			}},
			want: []want{{KindInstallment, "2026-12-10", "2026-12-10", 9_000}},
		},
		{
			name: "single",
			in: Input{Total: 12_345, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2025-11-01"), Payment: PaymentPolicy{
				IssueDaysBeforeDue: 10, Schedule: Schedule{Mode: ModeSingle, DueOffsetDays: 30},
			}},
			want: []want{{KindSingle, "2026-01-21", "2026-01-31", 12_345}},
		},
		{
			name: "fixed deposit and rounding unit 100",
			in: Input{Total: 100_050, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-01-01"), Payment: PaymentPolicy{
				Deposit: &Deposit{Cents: 50}, RoundingUnit: 100, Schedule: Schedule{Mode: ModeInterval, EveryMonths: 4, Count: 3},
			}},
			want: []want{
				{KindDeposit, "2026-01-01", "2026-01-01", 50},
				{KindInstallment, "2026-01-01", "2026-01-01", 33_300},
				{KindInstallment, "2026-05-01", "2026-05-01", 33_300},
				{KindInstallment, "2026-09-01", "2026-09-01", 33_400},
			},
		},
		{name: "zero total", in: Input{Total: 0, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-01-01"), Payment: quarterly()}},
		{name: "negative total", in: Input{Total: -1, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-01-01"), Payment: quarterly()}, err: ErrNegativeTotal},
		{
			name: "deposit above total",
			in: Input{Total: 10, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-01-01"), Payment: PaymentPolicy{
				Deposit: &Deposit{Cents: 11}, Schedule: Schedule{Mode: ModeSingle},
			}},
			err: ErrInvalidPolicy,
		},
		{
			name: "weights not summing to 10000",
			in: Input{Total: 10, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-01-01"), Payment: PaymentPolicy{
				Schedule: Schedule{Mode: ModeFixedDates, Dates: []FixedDue{{MonthDay: "03-01", BP: 9000}}},
			}},
			err: ErrInvalidPolicy,
		},
		{
			name: "fixed date outside the period",
			in: Input{Total: 10, StartsOn: d("2026-01-01"), EndsOn: d("2026-06-01"), Today: d("2026-01-01"), Payment: PaymentPolicy{
				Schedule: Schedule{Mode: ModeFixedDates, Dates: []FixedDue{{MonthDay: "09-01", BP: 10000}}},
			}},
			err: ErrInvalidPolicy,
		},
		{name: "unknown mode", in: Input{Total: 10, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2026-01-01")}, err: ErrInvalidPolicy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Plan(tc.in)
			if err != tc.err { //nolint:errorlint // sentinels returned as is
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d items %+v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				g := got[i]
				if g.Seq != i+1 || g.Kind != w.kind || g.IssueOn.String() != w.issue || g.DueOn.String() != w.due || g.Amount != w.amount {
					t.Errorf("item %d = %+v (issue %s due %s), want %+v", i, g, g.IssueOn, g.DueOn, w)
				}
			}
		})
	}
}

func TestInstallmentPlan_EarlyPaymentAlternative(t *testing.T) {
	p := quarterly()
	p.EarlyPayment = &EarlyPayment{DiscountBP: 300, PayByOffsetDays: 15}
	alt, err := EarlyPaymentPlan(Input{Total: 100_050, StartsOn: d("2026-01-01"), EndsOn: d("2027-01-01"), Today: d("2025-12-01"), Payment: p})
	if err != nil {
		t.Fatal(err)
	}
	// 100 050 × 3 % = 3 001.5 → 3 002 (half-even).
	if alt.Discount != 3_002 || alt.Total != 97_048 || len(alt.Items) != 1 || alt.Items[0].Amount != 97_048 ||
		alt.Items[0].DueOn.String() != "2025-12-16" || alt.Items[0].Kind != KindSingle {
		t.Fatalf("alt = %+v", alt)
	}
	none, err := EarlyPaymentPlan(Input{Total: 1, Today: d("2025-12-01"), Payment: quarterly()})
	if err != nil || none != nil {
		t.Fatalf("no early payment = %+v, %v", none, err)
	}
}

func TestInstallmentPlan_SumsToTotal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := calendar.New(2024, 1, 1).AddDays(rapid.IntRange(0, 1500).Draw(t, "start"))
		end := start.AddDays(rapid.IntRange(1, 400).Draw(t, "nights"))
		today := start.AddDays(rapid.IntRange(-120, 420).Draw(t, "today"))
		p := PaymentPolicy{
			DepositDueDays:     rapid.IntRange(0, 30).Draw(t, "dep_days"),
			IssueDaysBeforeDue: rapid.IntRange(0, 60).Draw(t, "issue_days"),
			RoundingUnit:       money.Cents(rapid.SampledFrom([]int64{0, 1, 100, 1000}).Draw(t, "unit")),
		}
		total := money.Cents(rapid.Int64Range(0, 1_000_000_000).Draw(t, "total"))
		switch rapid.IntRange(0, 2).Draw(t, "deposit") {
		case 1:
			p.Deposit = &Deposit{BP: rapid.Int64Range(0, 10_000).Draw(t, "dep_bp")}
		case 2:
			p.Deposit = &Deposit{Cents: money.Cents(rapid.Int64Range(0, int64(total)).Draw(t, "dep_cents"))}
		}
		switch rapid.SampledFrom([]string{ModeSingle, ModeInterval, ModeFixedDates}).Draw(t, "mode") {
		case ModeSingle:
			p.Schedule = Schedule{Mode: ModeSingle, DueOffsetDays: rapid.IntRange(0, 90).Draw(t, "offset")}
		case ModeInterval:
			p.Schedule = Schedule{
				Mode: ModeInterval, Count: rapid.IntRange(1, 24).Draw(t, "count"),
				EveryMonths: rapid.IntRange(1, 12).Draw(t, "every"), FirstDueOffsetMonths: rapid.IntRange(0, 6).Draw(t, "first"),
			}
		default:
			n := rapid.IntRange(1, 12).Draw(t, "n_dates")
			weights := make([]int64, n)
			left := int64(WeightTotal)
			for i := range n - 1 {
				weights[i] = rapid.Int64Range(1, left-int64(n-1-i)).Draw(t, "w")
				left -= weights[i]
			}
			weights[n-1] = left
			span := start.DaysUntil(end)
			for i := range n {
				p.Schedule.Dates = append(p.Schedule.Dates, FixedDue{Date: start.AddDays(rapid.IntRange(0, span-1).Draw(t, "date")), BP: weights[i]})
			}
			p.Schedule.Mode = ModeFixedDates
		}
		items, err := Plan(Input{Total: total, StartsOn: start, EndsOn: end, Today: today, Payment: p})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		var sum money.Cents
		for i, it := range items {
			sum += it.Amount
			if it.Amount < 0 {
				t.Fatalf("negative amount %+v", it)
			}
			if it.Seq != i+1 {
				t.Fatalf("seq %d at %d", it.Seq, i)
			}
			if it.IssueOn.After(it.DueOn) || it.IssueOn.Before(today) || it.DueOn.Before(today) {
				t.Fatalf("dates of %+v, today %s", it, today)
			}
		}
		if sum != total {
			t.Fatalf("Σ = %d, total %d (%+v)", sum, total, items)
		}
		if total == 0 && len(items) != 0 {
			t.Fatalf("zero total gave %+v", items)
		}
	})
}

func TestAddMonths_Clamp(t *testing.T) {
	cases := map[string]string{"2026-01-31+1": "2026-02-28", "2024-01-31+1": "2024-02-29", "2026-11-30+3": "2027-02-28", "2026-05-15+12": "2027-05-15"}
	for in, want := range cases {
		parts := strings.Split(in, "+")
		n, _ := strconv.Atoi(parts[1])
		if got := AddMonths(d(parts[0]), n).String(); got != want {
			t.Errorf("%s = %s, want %s", in, got, want)
		}
	}
}

// TestSchedule_Purity checks the package stays pure: standard library plus calendar and money, no clock (time is
// used only for calendar arithmetic), no randomness, no I/O.
func TestSchedule_Purity(t *testing.T) {
	allowed := []string{"github.com/coste360/porto/internal/calendar", "github.com/coste360/porto/internal/money"}
	forbidden := []string{"math/rand", "math/rand/v2", "crypto/rand", "os", "net", "net/http", "database/sql", "io/fs"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if (strings.Contains(path, ".") && !slices.Contains(allowed, path)) || slices.Contains(forbidden, path) {
				t.Errorf("%s imports %q", name, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since") {
					t.Errorf("%s calls time.%s", fset.Position(sel.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
}
