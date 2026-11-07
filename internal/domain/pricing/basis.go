package pricing

import (
	"math"
	"strconv"

	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// dims are the dimensions used by the bases (m2-plan §2.4 step 3).
type dims struct {
	ok     bool
	source DimensionSource
	l, b   geo.Cm
}

func (e *engine) resolveDims() dims {
	v := e.in.Vessel
	hasVessel := v != nil && v.LOA > 0
	hasBerth := e.in.Resource.Length > 0
	src := e.settings.DimensionSource
	switch src {
	case DimensionBerth:
		if !hasBerth {
			return dims{source: src}
		}
		return dims{ok: true, source: src, l: e.in.Resource.Length, b: e.in.Resource.Width}
	case DimensionMax:
		switch {
		case hasVessel && hasBerth:
			return dims{ok: true, source: src, l: max(v.LOA, e.in.Resource.Length), b: max(v.Beam, e.in.Resource.Width)}
		case hasVessel:
			return dims{ok: true, source: DimensionVessel, l: v.LOA, b: v.Beam}
		case hasBerth:
			return dims{ok: true, source: DimensionBerth, l: e.in.Resource.Length, b: e.in.Resource.Width}
		}
		return dims{source: src}
	default:
		if !hasVessel {
			return dims{source: DimensionVessel}
		}
		return dims{ok: true, source: DimensionVessel, l: v.LOA, b: v.Beam}
	}
}

// roundLength applies the list's length rounding: up = ceil to a multiple of the step, half_even = nearest
// multiple with ties to the even multiple. A step below 1 means 1 cm.
func roundLength(l geo.Cm, lr LengthRounding) geo.Cm {
	step := int64(lr.StepCm)
	if step < 1 {
		step = 1
	}
	v := int64(l)
	if lr.Mode == LengthRoundHalfEven {
		return geo.Cm(money.DivRoundHalfEven(v, step) * step)
	}
	q := v / step
	if v%step > 0 {
		q++
	}
	return geo.Cm(q * step)
}

// Reasons a rule cannot price an input (internal; mapped to errors when no rule can).
const (
	failNone = iota
	failNoAmount
	failNoBand
	failDims
)

// unitPrice is the amount of one unit of a winning rule, with what the trace shows about it.
type unitPrice struct {
	rule       *Rule
	amount     money.Cents
	candidates int
	band       string
	bandLength geo.Cm
	rounded    [][3]int64 // from, to, step
	minBeam    *[2]geo.Cm // beam, min
}

// unitAmount computes the amount of one unit of rule r (m2-plan §2.4 step 4). q is the quantity of a metered
// service (1 for everything else). It returns a fail reason when the rule cannot price the input.
func (e *engine) unitAmount(r *Rule, q qty.Milli) (unitPrice, int, error) {
	up := unitPrice{rule: r}
	switch r.Basis {
	case BasisFlat:
		if r.AmountCents == nil {
			return up, failNoAmount, nil
		}
		up.amount = *r.AmountCents
	case BasisPerBerth:
		if a, ok := r.Amounts[e.in.Resource.Category]; ok && e.in.Resource.Category != "" {
			up.amount = a
		} else if r.AmountCents != nil {
			up.amount = *r.AmountCents
		} else {
			return up, failNoAmount, nil
		}
	case BasisLengthBand:
		if !e.dims.ok {
			if r.AmountCents == nil {
				return up, failDims, nil
			}
			up.amount = *r.AmountCents
			break
		}
		band, ok := e.bandFor(e.dims.l)
		if !ok {
			if r.AmountCents == nil {
				return up, failNoBand, nil
			}
			up.amount = *r.AmountCents
			break
		}
		up.band, up.bandLength = band, e.dims.l
		if a, ok := r.Amounts[band]; ok {
			up.amount = a
		} else if r.AmountCents != nil {
			up.amount = *r.AmountCents
		} else {
			return up, failNoAmount, nil
		}
	case BasisPerMeter:
		if r.UnitCents == nil {
			return up, failNoAmount, nil
		}
		if !e.dims.ok {
			return up, failDims, nil
		}
		l := e.round(&up, e.dims.l)
		a, err := r.UnitCents.MulRatChecked(int64(l), 100)
		if err != nil {
			return up, failNone, ErrOverflow
		}
		up.amount = a
	case BasisPerM2:
		if r.UnitCents == nil {
			return up, failNoAmount, nil
		}
		if !e.dims.ok {
			return up, failDims, nil
		}
		l := e.round(&up, e.dims.l)
		b := e.dims.b
		if mr := e.settings.MinBeamRatio; mr != nil && mr.Den > 0 && mr.Num > 0 {
			if int64(l) > math.MaxInt64/mr.Num {
				return up, failNone, ErrOverflow
			}
			minB := geo.Cm(money.DivRoundHalfEven(int64(l)*mr.Num, mr.Den))
			if minB > b {
				up.minBeam = &[2]geo.Cm{b, minB}
				b = minB
			}
		}
		b = e.round(&up, b)
		a, err := area(*r.UnitCents, l, b)
		if err != nil {
			return up, failNone, err
		}
		up.amount = a
	case BasisPerM2Berth:
		if r.UnitCents == nil {
			return up, failNoAmount, nil
		}
		if e.in.Resource.Length <= 0 || e.in.Resource.Width <= 0 {
			return up, failDims, nil
		}
		l := e.round(&up, e.in.Resource.Length)
		w := e.round(&up, e.in.Resource.Width)
		a, err := area(*r.UnitCents, l, w)
		if err != nil {
			return up, failNone, err
		}
		up.amount = a
	case BasisMetered:
		if r.UnitCents == nil {
			return up, failNoAmount, nil
		}
		a, err := q.MulCents(*r.UnitCents)
		if err != nil {
			return up, failNone, ErrOverflow
		}
		up.amount = a
	default:
		return up, failNoAmount, nil
	}
	return up, failNone, nil
}

// round applies the length rounding and records it for the trace.
func (e *engine) round(up *unitPrice, v geo.Cm) geo.Cm {
	r := roundLength(v, e.settings.LengthRounding)
	if r != v {
		up.rounded = append(up.rounded, [3]int64{int64(v), int64(r), int64(max(e.settings.LengthRounding.StepCm, 1))})
	}
	return r
}

// area returns half_even(unit × l × w / 10 000) with a 128-bit intermediate.
func area(unit money.Cents, l, w geo.Cm) (money.Cents, error) {
	if l < 0 || w < 0 || (l > 0 && int64(w) > math.MaxInt64/int64(l)) {
		return 0, ErrOverflow
	}
	a, err := unit.MulRatChecked(int64(l)*int64(w), 10_000)
	if err != nil {
		return 0, ErrOverflow
	}
	return a, nil
}

// bandFor returns the first band whose inclusive upper bound is >= l (raw length); an open band catches the
// rest.
func (e *engine) bandFor(l geo.Cm) (string, bool) {
	for _, b := range e.cat.PriceList.Bands {
		if b.MaxCm == nil || l <= *b.MaxCm {
			return b.Code, true
		}
	}
	return "", false
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
