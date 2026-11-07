package contract

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Termination amounts (spec 13.4; m4-plan §3.5, §15 D10): the refund (negative) or penalty (positive) of an
// early termination, from the contract's cancellation clause. It is stored on the contract and in the audit
// diff, never charged in M4 (the adjustment charge is M5).

// TerminationAmount returns penalty − refund for a contract of total cents spread over nights nights, of which
// unused are after the termination date, when paid cents are already settled:
//   - refund pro_rata = the unused nights' share of what was paid (paid × unused / nights, half-even); none = 0;
//   - penalty = penalty_bp of the remaining total (total × unused / nights, half-even, then bp half-even).
func TerminationAmount(total, paid money.Cents, nights, unused int, c CancellationPolicy) money.Cents {
	if nights <= 0 || unused <= 0 {
		return 0
	}
	unused = min(unused, nights)
	var refund money.Cents
	if c.Refund == RefundProRata && paid > 0 {
		refund = paid.MulRat(int64(unused), int64(nights))
	}
	var penalty money.Cents
	if c.PenaltyBP > 0 && total > 0 {
		penalty = total.MulRat(int64(unused), int64(nights)).PercentBP(c.PenaltyBP)
	}
	return penalty - refund
}

// terminationAmountTx computes the amount of terminating c on effectiveOn from its paid instalments.
func (s *Service) terminationAmountTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, effectiveOn calendar.Date) (int64, error) {
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return 0, err
	}
	pol, err := DecodePolicy(c.Policy)
	if err != nil {
		return 0, err
	}
	_, _, can := effective(pol, cs)
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return 0, err
	}
	var paid money.Cents
	for _, r := range live {
		if r.Status == store.InstallmentPaid {
			paid += money.Cents(r.AmountCents)
		}
	}
	nights := c.StartsOn.DaysUntil(c.EndsOn)
	unused := effectiveOn.DaysUntil(c.EndsOn)
	return int64(TerminationAmount(money.Cents(c.TotalCents), paid, nights, unused, can)), nil
}
