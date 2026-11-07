package allot

import (
	"errors"
	"fmt"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// Errors of the allocation rules.
var (
	// ErrSharesInvalid means holder shares are not 1..1000 each, repeat a customer or do not sum to 1000 (INV-40).
	ErrSharesInvalid = errors.New("allot: shares do not sum to 1000")
	// ErrTableIncomplete means the millis of a table do not sum to 1000 (INV-41).
	ErrTableIncomplete = errors.New("allot: millesimal table does not sum to 1000")
	// ErrInvalidInput means an input cannot be computed (negative measures, empty periods, unknown basis).
	ErrInvalidInput = errors.New("allot: invalid input")
)

// Whole is the sum of the shares of a right and of the millis of a millesimal table.
const Whole = 1000

// Share is a holder's share of a right in thousandths.
type Share struct {
	CustomerID string
	Millis     int64
}

// HolderAmount is the part of an amount due by one holder.
type HolderAmount struct {
	CustomerID string
	Millis     int64
	Amount     money.Cents
}

// CheckShares validates holder shares (INV-40): at least one, each 1..1000, distinct customers, Σ = 1000. It
// returns the sum and ErrSharesInvalid when the rule is broken.
func CheckShares(shares []Share) (int64, error) {
	var sum int64
	seen := make(map[string]bool, len(shares))
	bad := len(shares) == 0
	for _, s := range shares {
		if s.Millis < 1 || s.Millis > Whole || s.CustomerID == "" || seen[s.CustomerID] {
			bad = true
		}
		seen[s.CustomerID] = true
		sum += s.Millis
	}
	if bad || sum != Whole {
		return sum, ErrSharesInvalid
	}
	return sum, nil
}

// Holders splits amount among the holders by share with the largest remainder (Σ = amount exactly). Ties go to
// the earlier holder.
func Holders(amount money.Cents, shares []Share) ([]HolderAmount, error) {
	if _, err := CheckShares(shares); err != nil {
		return nil, err
	}
	w := make([]int64, len(shares))
	for i, s := range shares {
		w[i] = s.Millis
	}
	parts, err := money.Allocate(amount, w)
	if err != nil {
		return nil, fmt.Errorf("allot holders: %w", err)
	}
	out := make([]HolderAmount, len(shares))
	for i, s := range shares {
		out[i] = HolderAmount{CustomerID: s.CustomerID, Millis: s.Millis, Amount: parts[i]}
	}
	return out, nil
}

// Entry is the millesimal weight of a berth in a table.
type Entry struct {
	BerthID string
	Millis  int64
}

// SumMillis returns Σ millis of entries and ErrTableIncomplete unless it is 1000 with no negative entry (INV-41).
func SumMillis(entries []Entry) (int64, error) {
	var sum int64
	neg := false
	for _, e := range entries {
		if e.Millis < 0 {
			neg = true
		}
		sum += e.Millis
	}
	if neg || sum != Whole {
		return sum, ErrTableIncomplete
	}
	return sum, nil
}

// BerthHolders are the holders of the right active on a berth at the reference date (none: the marina's share).
type BerthHolders struct {
	BerthID string
	RightID string
	Shares  []Share
}

// Item is a budget item to allocate on its table.
type Item struct {
	ID      string
	Amount  money.Cents // advance: share of the budget; balance: actual − advances (may be negative)
	Entries []Entry
}

// Allocation is one row of a common charge run. CustomerID is empty for the marina's own share (§15 D18).
type Allocation struct {
	ItemID     string
	BerthID    string
	RightID    string
	CustomerID string
	BerthShare money.Cents
	Amount     money.Cents
}

// Common allocates every item over its table's berths (largest remainder over the millis) and every berth share
// over the holders of the berth (largest remainder over the shares): Σ per item = item amount and Σ run = Σ items,
// exactly (INV-42). Berths with 0 millis get no row; a berth without holders gets one row with an empty
// CustomerID (the marina's own share). The output follows the order of items, entries and shares.
func Common(items []Item, holders map[string]BerthHolders) ([]Allocation, error) {
	var out []Allocation
	for _, it := range items {
		if _, err := SumMillis(it.Entries); err != nil {
			return nil, fmt.Errorf("item %s: %w", it.ID, err)
		}
		var entries []Entry
		var w []int64
		for _, e := range it.Entries {
			if e.Millis > 0 {
				entries, w = append(entries, e), append(w, e.Millis)
			}
		}
		shares, err := money.Allocate(it.Amount, w)
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", it.ID, err)
		}
		for i, e := range entries {
			bh, ok := holders[e.BerthID]
			if !ok || len(bh.Shares) == 0 {
				out = append(out, Allocation{ItemID: it.ID, BerthID: e.BerthID, BerthShare: shares[i], Amount: shares[i]})
				continue
			}
			parts, err := Holders(shares[i], bh.Shares)
			if err != nil {
				return nil, fmt.Errorf("berth %s: %w", e.BerthID, err)
			}
			for _, p := range parts {
				out = append(out, Allocation{
					ItemID: it.ID, BerthID: e.BerthID, RightID: bh.RightID, CustomerID: p.CustomerID,
					BerthShare: shares[i], Amount: p.Amount,
				})
			}
		}
	}
	return out, nil
}

// AdvanceAmount is the advance of an item: amount × share_bp / 10 000, half-even.
func AdvanceAmount(budget money.Cents, shareBP int64) money.Cents { return budget.PercentBP(shareBP) }

// Fee bases (management_fee_tables.basis).
const (
	BasisPerCategory   = "per_category"
	BasisPerM2Berth    = "per_m2_berth"
	BasisPerLengthBand = "per_length_band"
)

// Bases lists the fee bases.
var Bases = []string{BasisPerCategory, BasisPerM2Berth, BasisPerLengthBand}

// Exclusion reasons of a fee line.
const (
	ExcludedNoRate      = "no_rate"
	ExcludedNoCategory  = "no_category"
	ExcludedNoHolders   = "no_holders"
	ExcludedOutsideYear = "outside_year"
)

// FeeRow is a row of a fee table.
type FeeRow struct {
	CategoryID     string
	FromCm, ToCm   int64 // per_length_band: [FromCm, ToCm); ToCm 0 = no upper bound
	Amount         money.Cents
	RateCentsPerM2 money.Cents
}

// FeeInput is a right's data for the yearly fee.
type FeeInput struct {
	Basis           string
	Rows            []FeeRow
	IstatBP         int64
	BerthCategoryID string
	BerthLengthCm   int64
	BerthWidthCm    int64
	DaysCovered     int // days of the year covered by the right
	DaysInYear      int
}

// FeeResult is the fee of a right for the year: the base, the ISTAT-updated base and the pro-rated amount.
type FeeResult struct {
	Base     money.Cents
	Updated  money.Cents
	Amount   money.Cents
	Excluded string // no_rate, no_category, outside_year: the right is excluded with this reason
}

// FeeAmount computes the yearly fee of a right (m4-plan §4.4): base by basis (per_m2_berth = length × width cm²
// × rate / 10 000 half-even), × (10 000 + istat_bp) / 10 000 half-even, × days covered / days in year half-even.
// A right without a matching row is excluded with a reason and amounts 0.
func FeeAmount(in FeeInput) (FeeResult, error) {
	if in.DaysInYear < 365 || in.DaysInYear > 366 || in.DaysCovered < 0 || in.DaysCovered > in.DaysInYear ||
		in.BerthLengthCm < 0 || in.BerthWidthCm < 0 || in.IstatBP <= -10_000 {
		return FeeResult{}, ErrInvalidInput
	}
	if in.DaysCovered == 0 {
		return FeeResult{Excluded: ExcludedOutsideYear}, nil
	}
	var base money.Cents
	switch in.Basis {
	case BasisPerCategory:
		if in.BerthCategoryID == "" {
			return FeeResult{Excluded: ExcludedNoCategory}, nil
		}
		i := slices.IndexFunc(in.Rows, func(r FeeRow) bool { return r.CategoryID == in.BerthCategoryID })
		if i < 0 {
			return FeeResult{Excluded: ExcludedNoRate}, nil
		}
		base = in.Rows[i].Amount
	case BasisPerM2Berth:
		i := slices.IndexFunc(in.Rows, func(r FeeRow) bool { return r.RateCentsPerM2 > 0 })
		if i < 0 {
			return FeeResult{Excluded: ExcludedNoRate}, nil
		}
		cm2 := in.BerthLengthCm * in.BerthWidthCm
		var err error
		if base, err = in.Rows[i].RateCentsPerM2.MulRatChecked(cm2, 10_000); err != nil {
			return FeeResult{}, err
		}
	case BasisPerLengthBand:
		i := slices.IndexFunc(in.Rows, func(r FeeRow) bool {
			return in.BerthLengthCm >= r.FromCm && (r.ToCm == 0 || in.BerthLengthCm < r.ToCm)
		})
		if i < 0 {
			return FeeResult{Excluded: ExcludedNoRate}, nil
		}
		base = in.Rows[i].Amount
	default:
		return FeeResult{}, ErrInvalidInput
	}
	updated := base.MulRat(10_000+in.IstatBP, 10_000)
	amount := updated
	if in.DaysCovered != in.DaysInYear {
		amount = updated.MulRat(int64(in.DaysCovered), int64(in.DaysInYear))
	}
	return FeeResult{Base: base, Updated: updated, Amount: amount}, nil
}

// DaysInYear returns 365 or 366.
func DaysInYear(year int) int {
	return calendar.New(year, 1, 1).DaysUntil(calendar.New(year+1, 1, 1))
}

// DaysCovered returns the days of year inside the inclusive period [from, to] (0 when they do not meet).
func DaysCovered(from, to calendar.Date, year int) int {
	lo, hi := calendar.New(year, 1, 1), calendar.New(year, 12, 31)
	if from.After(lo) {
		lo = from
	}
	if to.Before(hi) {
		hi = to
	}
	if hi.Before(lo) {
		return 0
	}
	return lo.DaysUntil(hi) + 1
}
