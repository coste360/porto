package chargeplan

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Adjustment kinds understood by ContractTarget (the contract package's PriceAdjustment kinds, plus the addendum
// sub-kinds of m5-plan §3.4). Unknown kinds are distributed over every line.
const (
	AdjustMove          = "move"
	AdjustMoveUndo      = "move_undo"
	AdjustVesselChange  = "vessel_change"
	AdjustEarlyPayment  = "early_payment"
	AdjustAddendum      = "addendum"
	AdjustServiceAdd    = "service_add"
	AdjustServiceRemove = "service_remove"
)

// SnapKey is the plan key of a snapshot breakdown line.
func SnapKey(seq int) string { return "snap:" + strconv.Itoa(seq) }

// AddKey is the plan key of a line added by an addendum.
func AddKey(addendumID string) string { return "add:" + addendumID }

// FromBreakdownLine converts an engine line into a target line (amounts as computed by the engine).
func FromBreakdownLine(l pricing.Line) Line {
	return Line{
		Key: SnapKey(l.Seq), ItemID: l.ItemID, ItemCode: l.ItemCode, RevenueClass: l.RevenueClass,
		ExternalCode: l.ExternalCode, Qty: l.Qty, VATRateBP: max(l.VATRateBP, 0), VATNature: l.VATNature,
		PriceIncludesVAT: l.PriceIncludesVAT, Net: l.Net, VAT: l.VAT, Gross: l.Gross,
		ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo,
	}
}

// ContractTarget returns the billable target of a contract or transit: the breakdown lines with a non-zero gross
// (key snap:<seq>) plus every adjustment distributed over the lines it concerns with money.Allocate on gross
// weights. A line whose gross changed is re-split with money.VATSplit, so net + vat = gross per line (INV-25);
// untouched lines keep the engine amounts, so with no adjustment Totals(target) equals the breakdown VAT summary.
// Σ target gross must equal total, else ErrTargetMismatch.
func ContractTarget(b pricing.Breakdown, adj []Adjustment, total money.Cents) (Target, error) {
	lines := make([]Line, 0, len(b.Lines))
	for _, l := range b.Lines {
		if l.Gross == 0 {
			continue
		}
		lines = append(lines, FromBreakdownLine(l))
	}
	touched := make([]bool, len(lines))
	for _, a := range adj {
		if a.DeltaCents == 0 && a.Added == nil {
			continue
		}
		if a.Added != nil {
			add := *a.Added
			if add.Key == "" {
				add.Key = AddKey(a.AddendumID)
			}
			add.Gross = a.DeltaCents
			lines = append(lines, add)
			touched = append(touched, true)
			continue
		}
		idx := selectLines(lines, a.LineKeys)
		if len(idx) == 0 {
			idx = selectLines(lines, nil)
		}
		if len(idx) == 0 {
			return Target{}, fmt.Errorf("%w: adjustment without lines", ErrTargetMismatch)
		}
		weights := make([]int64, len(idx))
		var sum int64
		for k, i := range idx {
			weights[k] = abs(int64(lines[i].Gross))
			sum += weights[k]
		}
		if sum == 0 {
			for k := range weights {
				weights[k] = 1
			}
		}
		shares, err := money.Allocate(a.DeltaCents, weights)
		if err != nil {
			return Target{}, err
		}
		for k, i := range idx {
			if shares[k] != 0 {
				lines[i].Gross += shares[k]
				touched[i] = true
			}
		}
	}
	var sum money.Cents
	out := lines[:0]
	for i := range lines {
		l := lines[i]
		if touched[i] {
			l.Net, l.VAT = money.VATSplit(l.Gross, l.VATRateBP)
		}
		sum += l.Gross
		if l.Gross == 0 && touched[i] {
			continue // fully removed: nothing left to charge on it
		}
		out = append(out, l)
	}
	if sum != total {
		return Target{}, fmt.Errorf("%w: target %d, total %d", ErrTargetMismatch, sum, total)
	}
	return Target{Lines: out}, nil
}

// selectLines returns the indexes of the lines with one of keys (every line when keys is empty).
func selectLines(lines []Line, keys []string) []int {
	var idx []int
	for i, l := range lines {
		if len(keys) == 0 || slices.Contains(keys, l.Key) {
			idx = append(idx, i)
		}
	}
	return idx
}

// Remaining returns, per target line, the gross and VAT still to charge given the lines already charged (signed).
// Charged keys that are no longer in the target come last, with a negative remainder (described by their
// ChargedLine.Line when given).
func Remaining(t Target, charged []ChargedLine) []Remainder {
	type sum struct {
		gross, vat money.Cents
		line       *Line
	}
	byKey := map[string]*sum{}
	var order []string
	for _, c := range charged {
		s := byKey[c.Key]
		if s == nil {
			s = &sum{}
			byKey[c.Key] = s
			order = append(order, c.Key)
		}
		s.gross += c.GrossCents
		s.vat += c.VATCents
		if s.line == nil && c.Line != nil {
			s.line = c.Line
		}
	}
	out := make([]Remainder, 0, len(t.Lines))
	seen := map[string]bool{}
	for _, l := range t.Lines {
		r := Remainder{Line: l, Gross: l.Gross, VAT: l.VAT}
		if s := byKey[l.Key]; s != nil {
			r.Gross -= s.gross
			r.VAT -= s.vat
		}
		seen[l.Key] = true
		out = append(out, r)
	}
	for _, k := range order {
		if seen[k] {
			continue
		}
		s := byKey[k]
		if s.gross == 0 && s.vat == 0 {
			continue
		}
		l := Line{Key: k}
		if s.line != nil {
			l = *s.line
			l.Key = k
		}
		l.Gross, l.VAT, l.Net = 0, 0, 0
		out = append(out, Remainder{Line: l, Gross: -s.gross, VAT: -s.vat})
	}
	return out
}

// SumGross is Σ remaining gross.
func SumGross(rem []Remainder) money.Cents {
	var s money.Cents
	for _, r := range rem {
		s += r.Gross
	}
	return s
}

// Split splits an instalment amount over the remainders: weights are the positive remaining gross per line and
// parts are money.Allocate(amount, weights), never above a remainder. When amount exceeds Σ positive remainders
// the excess is allocated over every line by target gross (and corrected later by the true-up). The VAT of a part
// is computed by cumulative rounding per line (vat_cum = vat_line × gross_cum / gross_line, half-even), so once a
// line's gross is fully charged its VAT is fully charged too (INV-57). Parts of zero gross are omitted.
func Split(rem []Remainder, amount money.Cents) ([]Part, error) {
	if amount < 0 {
		return nil, fmt.Errorf("chargeplan: negative instalment amount %d", amount)
	}
	if amount == 0 {
		return nil, nil
	}
	shares := make([]money.Cents, len(rem))
	weights := make([]int64, len(rem))
	var pos money.Cents
	for i, r := range rem {
		if r.Gross > 0 {
			weights[i] = int64(r.Gross)
			pos += r.Gross
		}
	}
	base := min(amount, pos)
	if base > 0 {
		a, err := money.Allocate(base, weights)
		if err != nil {
			return nil, err
		}
		copy(shares, a)
	}
	if excess := amount - base; excess > 0 {
		var sum int64
		for i, r := range rem {
			weights[i] = abs(int64(r.Line.Gross))
			sum += weights[i]
		}
		if sum == 0 {
			return nil, ErrNoLines
		}
		a, err := money.Allocate(excess, weights)
		if err != nil {
			return nil, err
		}
		for i := range shares {
			shares[i] += a[i]
		}
	}
	parts := make([]Part, 0, len(rem))
	for i, r := range rem {
		if shares[i] == 0 {
			continue
		}
		parts = append(parts, part(r, shares[i]))
	}
	return parts, nil
}

// part computes one part of gross g on remainder r with cumulative VAT.
func part(r Remainder, g money.Cents) Part {
	l := r.Line
	var vat money.Cents
	switch {
	case g == r.Gross:
		vat = r.VAT // the line is fully charged: its VAT too
	case l.Gross == 0:
		_, vat = money.VATSplit(g, l.VATRateBP)
	default:
		chargedGross := l.Gross - r.Gross
		chargedVAT := l.VAT - r.VAT
		cum := chargedGross + g
		vatCum := l.VAT.MulRat(int64(cum), int64(l.Gross))
		vat = vatCum - chargedVAT
	}
	p := Part{Line: l, Gross: g, VAT: vat, Net: g - vat}
	p.Qty, p.Unit = partQty(l, p)
	return p
}

// partQty derives the informative quantity and unit price of a part: target qty × part gross / line gross
// (half-even); when that is 0 the part carries qty 1 and the whole amount as unit price. A negative part has a
// negative quantity and a positive unit price.
func partQty(l Line, p Part) (qty.Milli, money.Cents) {
	amount := p.Gross
	if !l.PriceIncludesVAT {
		amount = p.Net
	}
	sign := qty.Milli(1)
	if p.Gross < 0 {
		sign, amount = -1, -amount
	}
	var q qty.Milli
	if l.Gross != 0 && l.Qty != 0 {
		q = qty.Milli(money.Cents(l.Qty).MulRat(abs(int64(p.Gross)), abs(int64(l.Gross))))
	}
	if q <= 0 {
		return 1000 * sign, amount
	}
	return q * sign, amount.MulRat(1000, int64(q))
}

// All turns every non-zero remainder into a part (true-ups, syncs and reversals charge the whole remainder).
func All(rem []Remainder) []Part {
	parts := make([]Part, 0, len(rem))
	for _, r := range rem {
		if r.Gross == 0 && r.VAT == 0 {
			continue
		}
		p := Part{Line: r.Line, Gross: r.Gross, VAT: r.VAT, Net: r.Gross - r.VAT}
		p.Qty, p.Unit = partQty(r.Line, p)
		parts = append(parts, p)
	}
	return parts
}

// PartLines returns the parts as lines carrying the part amounts (for Totals).
func PartLines(parts []Part) []Line {
	out := make([]Line, len(parts))
	for i, p := range parts {
		l := p.Line
		l.Net, l.VAT, l.Gross, l.Qty = p.Net, p.VAT, p.Gross, p.Qty
		out[i] = l
	}
	return out
}

// Totals sums lines per (rate, nature), sorted like pricing (rate descending, then nature). Lines of zero gross
// are skipped, as the engine does.
func Totals(lines []Line) []VATBucket {
	type key struct {
		rate   int64
		nature string
	}
	buckets := map[key]*VATBucket{}
	for _, l := range lines {
		if l.Gross == 0 && l.VAT == 0 && l.Net == 0 {
			continue
		}
		k := key{max(l.VATRateBP, 0), l.VATNature}
		b := buckets[k]
		if b == nil {
			b = &VATBucket{RateBP: k.rate, Nature: k.nature}
			buckets[k] = b
		}
		b.Net += l.Net
		b.VAT += l.VAT
		b.Gross += l.Gross
	}
	out := make([]VATBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, *b)
	}
	SortBuckets(out)
	return out
}

// SortBuckets sorts VAT buckets like pricing.Breakdown.VATSummary (rate descending, then nature).
func SortBuckets(b []VATBucket) {
	slices.SortFunc(b, func(x, y VATBucket) int {
		if x.RateBP != y.RateBP {
			if x.RateBP > y.RateBP {
				return -1
			}
			return 1
		}
		switch {
		case x.Nature < y.Nature:
			return -1
		case x.Nature > y.Nature:
			return 1
		}
		return 0
	})
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
