package account

import (
	"errors"

	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/money"
)

// Billing of a work order through the M5 charge planner (m10-plan §10): every billing charges parts of the
// remainder of a target (Σ target − Σ charged per plan key), so that after the final billing Σ charged = target per
// line and per VAT bucket (INV-94, TestRapid_BillingSequenceSums).

// ErrAmountAboveRemaining means an explicit amount exceeds the positive remaining gross.
var ErrAmountAboveRemaining = errors.New("workorder/account: amount above the remaining gross")

// PlanTarget converts target lines into a chargeplan target.
func PlanTarget(lines []TargetLine) chargeplan.Target {
	t := chargeplan.Target{Lines: make([]chargeplan.Line, 0, len(lines))}
	for _, l := range lines {
		t.Lines = append(t.Lines, chargeplan.Line{
			Key: l.PlanKey, ItemID: l.ItemID, Description: l.Description, Qty: l.Qty, VATRateBP: l.VAT.RateBP,
			VATNature: l.VAT.Nature, PriceIncludesVAT: l.VAT.PriceIncludesVAT, Net: l.Net, VAT: l.VATCents, Gross: l.Gross,
		})
	}
	return t
}

// TargetGross is Σ gross of target lines.
func TargetGross(lines []TargetLine) money.Cents {
	var s money.Cents
	for _, l := range lines {
		s += l.Gross
	}
	return s
}

// PositiveRemaining is Σ of the positive remainders (what a partial billing may still charge).
func PositiveRemaining(rem []chargeplan.Remainder) money.Cents {
	var s money.Cents
	for _, r := range rem {
		if r.Gross > 0 {
			s += r.Gross
		}
	}
	return s
}

// Partial plans an advance or progress billing of amount over the remainder of target: the parts never exceed a
// positive remainder line (chargeplan.Split weights by remaining gross). amount must be > 0 and ≤ the positive
// remaining gross.
func Partial(target []TargetLine, charged []chargeplan.ChargedLine, amount money.Cents) ([]chargeplan.Part, error) {
	rem := chargeplan.Remaining(PlanTarget(target), charged)
	if amount <= 0 {
		return nil, ErrAmountAboveRemaining
	}
	if amount > PositiveRemaining(rem) {
		return nil, ErrAmountAboveRemaining
	}
	return chargeplan.Split(onlyPositive(rem), amount)
}

// onlyPositive keeps the positive remainders (a partial billing never charges a negative line).
func onlyPositive(rem []chargeplan.Remainder) []chargeplan.Remainder {
	out := make([]chargeplan.Remainder, 0, len(rem))
	for _, r := range rem {
		if r.Gross > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Final plans the final (or adjustment) billing: every non-zero remainder becomes a part, negative lines included,
// so that after it Σ charged = target exactly per line (and per VAT bucket).
func Final(target []TargetLine, charged []chargeplan.ChargedLine) []chargeplan.Part {
	return chargeplan.All(chargeplan.Remaining(PlanTarget(target), charged))
}

// SumParts is Σ gross of parts.
func SumParts(parts []chargeplan.Part) money.Cents {
	var s money.Cents
	for _, p := range parts {
		s += p.Gross
	}
	return s
}
