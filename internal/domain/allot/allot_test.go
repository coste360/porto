package allot

import (
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// drawMillis draws n non-negative weights summing to exactly 1000 (zeros allowed).
func drawMillis(t *rapid.T, n int, label string) []int64 {
	cuts := make([]int64, n-1)
	for i := range cuts {
		cuts[i] = rapid.Int64Range(0, Whole).Draw(t, label)
	}
	// sort the cut points and take the gaps
	for i := 1; i < len(cuts); i++ {
		for j := i; j > 0 && cuts[j] < cuts[j-1]; j-- {
			cuts[j], cuts[j-1] = cuts[j-1], cuts[j]
		}
	}
	out := make([]int64, n)
	prev := int64(0)
	for i, c := range cuts {
		out[i] = c - prev
		prev = c
	}
	out[n-1] = Whole - prev
	return out
}

// drawShares draws 1..4 holder shares, each ≥ 1, summing to 1000.
func drawShares(t *rapid.T, berth int) []Share {
	n := rapid.IntRange(1, 4).Draw(t, "holders")
	w := make([]int64, n)
	left := int64(Whole)
	for i := range n - 1 {
		hi := left - int64(n-1-i)
		w[i] = rapid.Int64Range(1, hi).Draw(t, "share")
		left -= w[i]
	}
	w[n-1] = left
	out := make([]Share, n)
	for i := range w {
		out[i] = Share{CustomerID: "c" + strconv.Itoa(berth) + "-" + strconv.Itoa(i), Millis: w[i]}
	}
	return out
}

// TestCommonCharges_AllocationSumsExactly is INV-42: for random tables (1..400 berths, zeros allowed), items
// (−10^8..10^9 cents) and holders (1..4 per berth, some berths without a right), Σ per item = item amount, Σ run
// = Σ items, Σ per berth over holders = berth share, and every berth share is within 1 cent of the exact share.
func TestCommonCharges_AllocationSumsExactly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nb := rapid.IntRange(1, 400).Draw(t, "berths")
		millis := drawMillis(t, nb, "millis")
		holders := map[string]BerthHolders{}
		for b := range nb {
			id := "b" + strconv.Itoa(b)
			if rapid.IntRange(0, 9).Draw(t, "has_right") > 1 {
				holders[id] = BerthHolders{BerthID: id, RightID: "r" + id, Shares: drawShares(t, b)}
			}
		}
		entries := make([]Entry, nb)
		for b := range nb {
			entries[b] = Entry{BerthID: "b" + strconv.Itoa(b), Millis: millis[b]}
		}
		ni := rapid.IntRange(1, 12).Draw(t, "items")
		items := make([]Item, ni)
		var sumItems money.Cents
		for i := range items {
			items[i] = Item{ID: "i" + strconv.Itoa(i), Amount: money.Cents(rapid.Int64Range(-100_000_000, 1_000_000_000).Draw(t, "amount")), Entries: entries}
			sumItems += items[i].Amount
		}
		allocs, err := Common(items, holders)
		if err != nil {
			t.Fatal(err)
		}
		perItem := map[string]money.Cents{}
		perBerth := map[string]money.Cents{} // item|berth → Σ holders
		share := map[string]money.Cents{}
		var run money.Cents
		for _, a := range allocs {
			perItem[a.ItemID] += a.Amount
			k := a.ItemID + "|" + a.BerthID
			perBerth[k] += a.Amount
			if s, ok := share[k]; ok && s != a.BerthShare {
				t.Fatalf("berth share differs between rows of %s", k)
			}
			share[k] = a.BerthShare
			run += a.Amount
			if _, has := holders[a.BerthID]; has == (a.CustomerID == "") {
				t.Fatalf("customer of %s: %q", a.BerthID, a.CustomerID)
			}
		}
		for _, it := range items {
			if perItem[it.ID] != it.Amount {
				t.Fatalf("item %s: Σ %d, amount %d", it.ID, perItem[it.ID], it.Amount)
			}
			for b, e := range entries {
				k := it.ID + "|" + e.BerthID
				if perBerth[k] != share[k] {
					t.Fatalf("%s: Σ holders %d, berth share %d", k, perBerth[k], share[k])
				}
				// |share − amount × millis / 1000| < 1
				exact := int64(it.Amount) * millis[b] // ×1000 scale
				diff := int64(share[k])*Whole - exact
				if diff <= -Whole || diff >= Whole {
					t.Fatalf("%s: share %d too far from %d/1000", k, share[k], exact)
				}
			}
		}
		if run != sumItems {
			t.Fatalf("run %d, items %d", run, sumItems)
		}
	})
}

// TestRight_HolderSharesSumTo1000 is INV-40: valid shares sum to 1000 and split any amount exactly; invalid ones
// are rejected with ErrSharesInvalid.
func TestRight_HolderSharesSumTo1000(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		shares := drawShares(t, 0)
		if sum, err := CheckShares(shares); err != nil || sum != Whole {
			t.Fatalf("valid shares: %d %v", sum, err)
		}
		amount := money.Cents(rapid.Int64Range(-1_000_000_000, 1_000_000_000).Draw(t, "amount"))
		parts, err := Holders(amount, shares)
		if err != nil {
			t.Fatal(err)
		}
		var sum money.Cents
		for _, p := range parts {
			sum += p.Amount
		}
		if sum != amount {
			t.Fatalf("Σ %d, amount %d", sum, amount)
		}
		// breaking the sum is always rejected
		bad := append([]Share(nil), shares...)
		bad[0].Millis += rapid.Int64Range(1, 5).Draw(t, "delta")
		if bad[0].Millis <= Whole {
			if _, err := CheckShares(bad); !errors.Is(err, ErrSharesInvalid) {
				t.Fatalf("Σ %d accepted", Whole+1)
			}
		}
	})
	cases := map[string][]Share{
		"empty":     nil,
		"zero":      {{CustomerID: "a", Millis: 0}, {CustomerID: "b", Millis: 1000}},
		"999":       {{CustomerID: "a", Millis: 333}, {CustomerID: "b", Millis: 666}},
		"duplicate": {{CustomerID: "a", Millis: 500}, {CustomerID: "a", Millis: 500}},
		"no id":     {{Millis: 1000}},
	}
	for name, s := range cases {
		if _, err := CheckShares(s); !errors.Is(err, ErrSharesInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCommon_Scenario(t *testing.T) {
	entries := []Entry{{"b1", 333}, {"b2", 333}, {"b3", 334}, {"b4", 0}}
	holders := map[string]BerthHolders{
		"b1": {BerthID: "b1", RightID: "r1", Shares: []Share{{"c1", 500}, {"c2", 500}}},
		"b3": {BerthID: "b3", RightID: "r3", Shares: []Share{{"c3", 1000}}},
	}
	allocs, err := Common([]Item{{ID: "i", Amount: 101, Entries: entries}}, holders)
	if err != nil {
		t.Fatal(err)
	}
	// 101 × {333, 333, 334} = 33.633, 33.633, 33.734 → 34, 33, 34 (largest remainders: b3 .734, then b1 .633)
	want := []Allocation{
		{ItemID: "i", BerthID: "b1", RightID: "r1", CustomerID: "c1", BerthShare: 34, Amount: 17},
		{ItemID: "i", BerthID: "b1", RightID: "r1", CustomerID: "c2", BerthShare: 34, Amount: 17},
		{ItemID: "i", BerthID: "b2", BerthShare: 33, Amount: 33},
		{ItemID: "i", BerthID: "b3", RightID: "r3", CustomerID: "c3", BerthShare: 34, Amount: 34},
	}
	if len(allocs) != len(want) {
		t.Fatalf("got %+v", allocs)
	}
	for i := range want {
		if allocs[i] != want[i] {
			t.Fatalf("row %d: got %+v, want %+v", i, allocs[i], want[i])
		}
	}
	if _, err := Common([]Item{{ID: "i", Amount: 1, Entries: []Entry{{"b1", 999}}}}, nil); !errors.Is(err, ErrTableIncomplete) {
		t.Fatalf("Σ 999: %v", err)
	}
	if got := AdvanceAmount(1_234_567, 5000); got != 617_284 { // 617 283.5 → even
		t.Fatalf("advance %d", got)
	}
}

func TestFeeAmount_Scenario(t *testing.T) {
	rows := []FeeRow{
		{CategoryID: "cat-a", Amount: 120_000},
		{CategoryID: "cat-b", Amount: 99_999},
	}
	bands := []FeeRow{{FromCm: 0, ToCm: 1000, Amount: 50_000}, {FromCm: 1000, ToCm: 1500, Amount: 80_000}, {FromCm: 1500, Amount: 150_000}}
	m2 := []FeeRow{{RateCentsPerM2: 2_550}}
	tests := []struct {
		name string
		in   FeeInput
		want FeeResult
	}{
		{
			"per category",
			FeeInput{Basis: BasisPerCategory, Rows: rows, BerthCategoryID: "cat-a", DaysCovered: 365, DaysInYear: 365},
			FeeResult{Base: 120_000, Updated: 120_000, Amount: 120_000},
		},
		{
			"per category + istat 1.7 %",
			FeeInput{Basis: BasisPerCategory, Rows: rows, IstatBP: 170, BerthCategoryID: "cat-b", DaysCovered: 365, DaysInYear: 365},
			// 99 999 × 1.017 = 101 698.983 → 101 699
			FeeResult{Base: 99_999, Updated: 101_699, Amount: 101_699},
		},
		{"per category no category", FeeInput{Basis: BasisPerCategory, Rows: rows, DaysCovered: 365, DaysInYear: 365}, FeeResult{Excluded: ExcludedNoCategory}},
		{"per category no rate", FeeInput{Basis: BasisPerCategory, Rows: rows, BerthCategoryID: "cat-z", DaysCovered: 365, DaysInYear: 365}, FeeResult{Excluded: ExcludedNoRate}},
		{
			"per m2",
			FeeInput{Basis: BasisPerM2Berth, Rows: m2, BerthLengthCm: 1200, BerthWidthCm: 410, DaysCovered: 366, DaysInYear: 366},
			// 12 × 4.10 = 49.2 m² × 25.50 € = 1 254.60 €
			FeeResult{Base: 125_460, Updated: 125_460, Amount: 125_460},
		},
		{
			"per m2 half-even",
			FeeInput{Basis: BasisPerM2Berth, Rows: []FeeRow{{RateCentsPerM2: 1}}, BerthLengthCm: 50, BerthWidthCm: 100, DaysCovered: 365, DaysInYear: 365},
			// 0.5 m² × 1 cent = 0.5 → 0
			FeeResult{Base: 0, Updated: 0, Amount: 0},
		},
		{
			"band lower bound inclusive",
			FeeInput{Basis: BasisPerLengthBand, Rows: bands, BerthLengthCm: 1000, DaysCovered: 365, DaysInYear: 365},
			FeeResult{Base: 80_000, Updated: 80_000, Amount: 80_000},
		},
		{
			"open band",
			FeeInput{Basis: BasisPerLengthBand, Rows: bands, BerthLengthCm: 2400, DaysCovered: 365, DaysInYear: 365},
			FeeResult{Base: 150_000, Updated: 150_000, Amount: 150_000},
		},
		{
			"pro rata half a year",
			FeeInput{Basis: BasisPerCategory, Rows: rows, BerthCategoryID: "cat-a", DaysCovered: 181, DaysInYear: 365},
			// 120 000 × 181 / 365 = 59 506.849 → 59 507
			FeeResult{Base: 120_000, Updated: 120_000, Amount: 59_507},
		},
		{"outside the year", FeeInput{Basis: BasisPerCategory, Rows: rows, BerthCategoryID: "cat-a", DaysCovered: 0, DaysInYear: 365}, FeeResult{Excluded: ExcludedOutsideYear}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FeeAmount(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
	if _, err := FeeAmount(FeeInput{Basis: "nope", DaysCovered: 1, DaysInYear: 365}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown basis: %v", err)
	}
	if DaysInYear(2028) != 366 || DaysInYear(2027) != 365 {
		t.Fatal("days in year")
	}
	if got := DaysCovered(calendar.MustParse("2027-07-01"), calendar.MustParse("2030-01-01"), 2027); got != 184 {
		t.Fatalf("days covered %d", got)
	}
	if got := DaysCovered(calendar.MustParse("2028-01-01"), calendar.MustParse("2030-01-01"), 2027); got != 0 {
		t.Fatalf("days covered %d", got)
	}
}

// TestAllot_Purity checks the package stays pure (no clock, no randomness, no I/O).
func TestAllot_Purity(t *testing.T) {
	allowed := map[string]bool{"github.com/coste360/porto/internal/calendar": true, "github.com/coste360/porto/internal/money": true}
	forbidden := map[string]bool{"time": true, "math/rand": true, "math/rand/v2": true, "crypto/rand": true, "os": true, "net": true, "net/http": true, "database/sql": true, "io/fs": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if (strings.Contains(path, ".") && !allowed[path]) || forbidden[path] {
				t.Errorf("%s imports %q", name, path)
			}
		}
	}
}
