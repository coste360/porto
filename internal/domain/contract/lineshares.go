package contract

import (
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// withLineShares returns the lines with their informative share of the contract total (contract_lines.amount_cents
// is never written: the price lives in the snapshot, which moves, addenda and changes of vessel adjust after the
// lines are created, so the share is derived on read). Breakdown lines go to the contract line of their item
// (services, land resources); berth lines and lines without a matching item go to the berth and area segments (resource
// lines without an item), split evenly; a land resource without an item is not priced (0). What adjustments add (total − Σ breakdown) is spread over the
// shares by weight. The shares sum to total exactly; without snapshot the whole total goes to the first line.
func withLineShares(lines []store.ContractLine, snap *PriceSnapshot, total int64) []store.ContractLine {
	if len(lines) == 0 {
		return lines
	}
	out := make([]store.ContractLine, len(lines))
	copy(out, lines)
	shares := make([]money.Cents, len(out))
	var segments []int
	for i, l := range out {
		if l.LineKind == "resource" && l.ItemID == "" && l.ResourceType != store.ResourceLand {
			segments = append(segments, i)
		}
	}
	if len(segments) == 0 {
		segments = []int{0}
	}
	if snap != nil {
		var bucket money.Cents
		for _, b := range snap.Breakdown.Lines {
			if b.Gross == 0 {
				continue
			}
			idx := -1
			if b.Kind != pricing.LineBerth && b.ItemID != "" {
				for i, l := range out {
					if l.ItemID == b.ItemID {
						idx = i
						break
					}
				}
			}
			if idx < 0 {
				bucket += b.Gross
				continue
			}
			shares[idx] += b.Gross
		}
		addShares(shares, segments, bucket, nil)
	}
	var sum money.Cents
	for _, s := range shares {
		sum += s
	}
	if rest := money.Cents(total) - sum; rest != 0 {
		all := make([]int, len(out))
		weights := make([]int64, len(out))
		for i := range out {
			all[i] = i
			weights[i] = int64(shares[i])
			if weights[i] < 0 {
				weights[i] = -weights[i]
			}
		}
		addShares(shares, all, rest, weights)
	}
	for i := range out {
		out[i].AmountCents = int64(shares[i])
	}
	return out
}

// addShares adds amount to shares[idx...] in proportion to weights (evenly when nil or all zero).
func addShares(shares []money.Cents, idx []int, amount money.Cents, weights []int64) {
	if amount == 0 || len(idx) == 0 {
		return
	}
	var wsum int64
	for _, w := range weights {
		wsum += w
	}
	if wsum == 0 {
		weights = make([]int64, len(idx))
		for k := range weights {
			weights[k] = 1
		}
	}
	parts, err := money.Allocate(amount, weights)
	if err != nil { // only on invalid weights, which cannot happen here
		shares[idx[0]] += amount
		return
	}
	for k, i := range idx {
		shares[i] += parts[k]
	}
}
