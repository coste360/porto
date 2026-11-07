package pricing

import (
	"errors"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Engine limits (m2-plan §2.4 step 14). Validate reports catalogs beyond them; Quote refuses stays that are too
// long and reports arithmetic beyond int64 as ErrOverflow.
const (
	MaxNights       = 3660
	MaxAmountCents  = 100_000_000
	MaxModifierBP   = 100_000
	MaxDiscountBP   = 10_000
	MaxLengthCm     = 20_000
	MaxQtyMilli     = 1_000_000_000
	MaxRules        = 2000
	MaxPeriods      = 50
	MaxBands        = 60
	MaxPriority     = 1000
	DefaultHullType = "monohull_motor"
)

// engine holds one quote in progress. It is created by Quote and never shared.
type engine struct {
	in       Input
	cat      Catalog
	settings ListSettings
	items    map[string]*Item
	periods  periodIndex
	channel  Channel
	nights   int
	dims     dims
	rules    []*Rule // sorted by ruleCmp (winner order), so every scan is order independent
	trace    []TraceStep
}

func newEngine(in Input, cat Catalog) *engine {
	e := &engine{in: in, cat: cat, settings: normalizeSettings(cat.PriceList.Settings)}
	e.items = make(map[string]*Item, len(cat.Items))
	for i := range cat.Items {
		it := &cat.Items[i]
		// Duplicate ids: the smallest code wins so the input order never matters.
		if prev, ok := e.items[it.ID]; !ok || it.Code < prev.Code {
			e.items[it.ID] = it
		}
	}
	e.periods = newPeriodIndex(cat.PriceList.Periods)
	e.rules = make([]*Rule, len(cat.Rules))
	for i := range cat.Rules {
		e.rules[i] = &cat.Rules[i]
	}
	slices.SortStableFunc(e.rules, ruleCmp)
	e.channel = in.Channel
	if e.channel == "" {
		e.channel = ChannelDesk
	}
	return e
}

// normalizeSettings fills the defaults of m2-plan §2.3 for missing values.
func normalizeSettings(s ListSettings) ListSettings {
	if s.DimensionSource == "" {
		s.DimensionSource = DimensionVessel
	}
	if s.LengthRounding.StepCm == 0 && s.LengthRounding.Mode == "" {
		s.LengthRounding = LengthRounding{StepCm: 10, Mode: LengthRoundUp}
	}
	return s
}

func (e *engine) addTrace(code string, line, block int, ruleID string, params map[string]string) {
	e.trace = append(e.trace, TraceStep{Code: code, Line: line, Block: block, RuleID: ruleID, Params: params})
}

func (e *engine) ctxAt(night calendar.Date) matchCtx {
	return matchCtx{
		contract:         e.in.ContractType,
		category:         e.in.Resource.Category,
		groups:           e.in.Resource.GroupIDs,
		channel:          e.channel,
		customerCategory: e.in.CustomerCategory,
		period:           e.periods.of(night),
	}
}

// splitRule, totalRounding and deposit read the per-contract-type settings with the "*" fallback.
func (e *engine) splitRule() SplitRule {
	if v, ok := e.settings.Split[e.in.ContractType]; ok && v != "" {
		return v
	}
	if v, ok := e.settings.Split[AnyContractType]; ok && v != "" {
		return v
	}
	return SplitProRataDaily
}

func (e *engine) totalRounding() TotalRounding {
	if v, ok := e.settings.Rounding[e.in.ContractType]; ok && v != "" {
		return v
	}
	if v, ok := e.settings.Rounding[AnyContractType]; ok && v != "" {
		return v
	}
	return RoundNone
}

func (e *engine) depositPolicy() *DepositPolicy {
	if e.in.Deposit != nil {
		return e.in.Deposit
	}
	if v, ok := e.settings.Deposits[e.in.ContractType]; ok {
		return &v
	}
	if v, ok := e.settings.Deposits[AnyContractType]; ok {
		return &v
	}
	return nil
}

// selectBase picks the winning base rule of item for one of units at night (m2-plan §2.4 step 2) and computes
// its unit amount. q is the quantity for metered rules.
func (e *engine) selectBase(item *Item, units []TimeUnit, night calendar.Date, q qty.Milli) (unitPrice, error) {
	ctx := e.ctxAt(night)
	unitExists := false
	worst := failNone
	worstBand := ""
	candidates := 0
	for _, r := range e.rules {
		if !r.Active || r.Stage != StageBase || r.ItemID != item.ID || !slices.Contains(units, r.Unit) {
			continue
		}
		unitExists = true
		if r.Scope.matches(ctx) {
			candidates++
		}
	}
	dimsField := "vessel"
	for _, r := range e.rules {
		if !r.Active || r.Stage != StageBase || r.ItemID != item.ID || !slices.Contains(units, r.Unit) || !r.Scope.matches(ctx) {
			continue
		}
		up, fail, err := e.unitAmount(r, q)
		if err != nil {
			return up, err
		}
		if fail == failNone {
			up.candidates = candidates
			return up, nil
		}
		if fail > worst {
			worst = fail
			if fail == failDims && (r.Basis == BasisPerM2Berth || e.settings.DimensionSource == DimensionBerth) {
				dimsField = "resource"
			}
		}
		if fail == failNoAmount && up.band != "" && worstBand == "" {
			worstBand = up.band
		}
	}
	np := &NoPriceError{ItemID: item.ID, ContractType: e.in.ContractType, Night: night, Unit: units[0], Category: e.in.Resource.Category}
	switch {
	case worst == failDims:
		return unitPrice{}, &InputError{Field: dimsField, Code: InputDimensionsRequired}
	case worst == failNoBand:
		np.Reason = ReasonNoBand
	case worst == failNoAmount:
		np.Reason, np.Band = ReasonNoAmount, worstBand
	case !unitExists:
		np.Reason = ReasonUnitMissing
	default:
		np.Reason = ReasonNoRule
	}
	return unitPrice{}, np
}

// isNoPriceOrDims tells whether err only means "this unit cannot be priced here" (used by best_combination).
func isNoPriceOrDims(err error) bool {
	if errors.Is(err, ErrNoPrice) {
		return true
	}
	ie, ok := errors.AsType[*InputError](err)
	return ok && ie.Code == InputDimensionsRequired
}

// addCents sums with overflow detection.
func addCents(a, b money.Cents) (money.Cents, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, ErrOverflow
	}
	return s, nil
}
