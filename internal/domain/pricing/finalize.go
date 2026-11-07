package pricing

import (
	"slices"

	"github.com/coste360/porto/internal/money"
)

// roundingStep is the step in cents of a total rounding mode (0 = none).
func roundingStep(m TotalRounding) int64 {
	switch m {
	case RoundEuro:
		return 100
	case RoundFiveEuro:
		return 500
	}
	return 0
}

// roundToStep returns the nearest multiple of step, ties to the even multiple.
func roundToStep(v money.Cents, step int64) money.Cents {
	return money.Cents(money.DivRoundHalfEven(int64(v), step) * step)
}

// finalize computes VAT, total rounding, the net/VAT split, the VAT summary and the deposit (m2-plan §2.4
// steps 9-12).
func (e *engine) finalize(bd *Breakdown) error {
	lines := bd.Lines
	gross0 := make([]money.Cents, len(lines))
	var t0 money.Cents
	for i := range lines {
		l := &lines[i]
		if l.VATRateBP < 0 || l.VATRateBP > MaxModifierBP {
			return ErrOverflow
		}
		g := l.Amount
		if !l.PriceIncludesVAT && l.VATRateBP > 0 {
			v, err := l.Amount.MulRatChecked(l.VATRateBP, 10_000)
			if err != nil {
				return ErrOverflow
			}
			if g, err = addCents(g, v); err != nil {
				return err
			}
		}
		gross0[i] = g
		var err error
		if t0, err = addCents(t0, g); err != nil {
			return err
		}
	}
	mode := e.totalRounding()
	total := t0
	if step := roundingStep(mode); step > 0 {
		total = roundToStep(t0, step)
		delta := total - t0
		if delta != 0 {
			var idx []int
			var weights []int64
			for i, g := range gross0 {
				if g > 0 {
					idx = append(idx, i)
					weights = append(weights, int64(g))
				}
			}
			shares, err := money.Allocate(delta, weights)
			if err != nil {
				return ErrOverflow
			}
			for k, i := range idx {
				lines[i].RoundingShare = shares[k]
			}
		}
		e.addTrace(TraceRoundingTotal, -1, -1, "", map[string]string{"mode": string(mode), "from_cents": itoa(int64(t0)), "to_cents": itoa(int64(total))})
	}
	bd.RoundingTotal = total - t0
	buckets := map[[2]string]*VATBucket{}
	for i := range lines {
		l := &lines[i]
		l.Gross = gross0[i] + l.RoundingShare
		rate := max(l.VATRateBP, 0)
		l.Net, l.VAT = money.VATSplit(l.Gross, rate)
		bd.Net += l.Net
		bd.VAT += l.VAT
		bd.Total += l.Gross
		if l.Kind != LineIncluded {
			e.addTrace(TraceVATLine, i, -1, "", map[string]string{"vat_rate_bp": itoa(rate), "gross_cents": itoa(int64(l.Gross))})
		}
		if l.Gross == 0 {
			continue
		}
		key := [2]string{itoa(rate), l.VATNature}
		b, ok := buckets[key]
		if !ok {
			b = &VATBucket{RateBP: rate, Nature: l.VATNature}
			buckets[key] = b
		}
		b.Net += l.Net
		b.VAT += l.VAT
		b.Gross += l.Gross
	}
	bd.VATSummary = make([]VATBucket, 0, len(buckets))
	for _, b := range buckets {
		bd.VATSummary = append(bd.VATSummary, *b)
	}
	slices.SortFunc(bd.VATSummary, func(a, b VATBucket) int {
		if a.RateBP != b.RateBP {
			if a.RateBP > b.RateBP {
				return -1
			}
			return 1
		}
		switch {
		case a.Nature < b.Nature:
			return -1
		case a.Nature > b.Nature:
			return 1
		}
		return 0
	})
	if p := e.depositPolicy(); p != nil {
		switch {
		case p.PercentBP != nil:
			d, err := bd.Total.MulRatChecked(*p.PercentBP, 10_000)
			if err != nil {
				return ErrOverflow
			}
			bd.Deposit = max(0, min(d, bd.Total))
		case p.AmountCents != nil:
			bd.Deposit = max(0, min(*p.AmountCents, bd.Total))
		}
		e.addTrace(TraceDeposit, -1, -1, "", map[string]string{"deposit_cents": itoa(int64(bd.Deposit))})
	}
	return nil
}
