package pricing

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// referenceUnit is the unit a contract type is priced in (m2-plan §12 D5).
func referenceUnit(ct ContractType) TimeUnit {
	switch ct {
	case ContractAnnual:
		return UnitYear
	case ContractSeasonal, ContractCharter:
		return UnitSeason
	case ContractMonthly, ContractStorage:
		return UnitMonth
	default:
		return UnitNight
	}
}

// pricedBlock is a block with the data the trace shows about it.
type pricedBlock struct {
	b         Block
	up        unitPrice
	discarded []Applied
	proRata   *[3]int64 // nights, unit nights, unit cents
}

// unitEnd returns the exclusive end of one unit starting at s (week = 7 nights, month and year with clamping,
// season = the window when s is its first night).
func (e *engine) unitEnd(u TimeUnit, s calendar.Date) calendar.Date {
	switch u {
	case UnitYear:
		return AddYears(s, 1)
	case UnitMonth:
		return AddMonths(s, 1)
	case UnitWeek:
		return s.AddDays(7)
	case UnitSeason:
		if w := e.settings.SeasonWindow; w != nil {
			return w.To.AddDays(1)
		}
		return s
	default:
		return s.AddDays(1)
	}
}

// span is one block [from, to) before pricing.
type span struct{ from, to calendar.Date }

// fullUnits returns the consecutive full reference units from the arrival that fit in the stay, and the night
// after the last one. Months and years are counted from the arrival (AddMonths(Arrival, k)), so Jan 31 + 2
// months ends on Mar 31 even though the first block ends on Feb 28.
func (e *engine) fullUnits(u TimeUnit) ([]span, calendar.Date) {
	a, d := e.in.Arrival, e.in.Departure
	var out []span
	switch u {
	case UnitSeason:
		w := e.settings.SeasonWindow
		if w != nil && a.Equal(w.From) && !w.To.Before(w.From) && !w.To.AddDays(1).After(d) {
			return []span{{a, w.To.AddDays(1)}}, w.To.AddDays(1)
		}
		return nil, a
	case UnitYear, UnitMonth:
		for k := 0; ; k++ {
			var s, end calendar.Date
			if u == UnitYear {
				s, end = AddYears(a, k), AddYears(a, k+1)
			} else {
				s, end = AddMonths(a, k), AddMonths(a, k+1)
			}
			if end.After(d) {
				return out, s
			}
			out = append(out, span{s, end})
		}
	default:
		for s := a; s.Before(d); s = s.AddDays(1) {
			out = append(out, span{s, s.AddDays(1)})
		}
		return out, d
	}
}

// monthSpans covers the stay with calendar months from the arrival; the last one may end after the departure.
func (e *engine) monthSpans() []span {
	var out []span
	for k := 0; ; k++ {
		s := AddMonths(e.in.Arrival, k)
		if !s.Before(e.in.Departure) {
			return out
		}
		out = append(out, span{s, AddMonths(e.in.Arrival, k+1)})
	}
}

// berthBlocks cuts the stay into blocks and prices them (m2-plan §2.4 step 5).
func (e *engine) berthBlocks(item *Item) ([]pricedBlock, error) {
	u := referenceUnit(e.in.ContractType)
	split := e.splitRule()
	starts, rest := e.fullUnits(u)
	exact := rest.Equal(e.in.Departure) && len(starts) > 0
	if u == UnitNight && split == SplitBestCombination {
		exact = false
	}
	if exact {
		blocks, err := e.unitBlocks(item, u, starts)
		if err != nil {
			return nil, err
		}
		e.addTrace(TraceSplitExact, 0, -1, "", map[string]string{"unit": string(u), "count": itoa(int64(len(blocks)))})
		return blocks, nil
	}
	switch split {
	case SplitWholeMonths:
		blocks, err := e.unitBlocks(item, UnitMonth, e.monthSpans())
		if err != nil {
			return nil, err
		}
		e.addTrace(TraceSplitWholeMonths, 0, -1, "", map[string]string{"months": itoa(int64(len(blocks)))})
		return blocks, nil
	case SplitBestCombination:
		blocks, err := e.bestCombination(item)
		if err != nil {
			return nil, err
		}
		e.addTrace(TraceSplitBest, 0, -1, "", map[string]string{"blocks": itoa(int64(len(blocks)))})
		return blocks, nil
	default:
		blocks, err := e.unitBlocks(item, u, starts)
		if err != nil {
			return nil, err
		}
		pb, err := e.proRataBlock(item, u, rest, len(starts))
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, pb)
		e.addTrace(TraceSplitProRata, 0, -1, "", map[string]string{
			"unit": string(u), "nights": itoa(pb.proRata[0]), "unit_nights": itoa(pb.proRata[1]), "unit_cents": itoa(pb.proRata[2]),
		})
		return blocks, nil
	}
}

// unitBlocks prices one block of unit u for each span.
func (e *engine) unitBlocks(item *Item, u TimeUnit, spans []span) ([]pricedBlock, error) {
	out := make([]pricedBlock, 0, len(spans))
	for _, sp := range spans {
		pb, err := e.priceBlock(item, u, sp.from, sp.to)
		if err != nil {
			return nil, err
		}
		out = append(out, pb)
	}
	return out, nil
}

// priceBlock prices the block [from, to) of unit u with the winning rule at its first night and its modifiers.
func (e *engine) priceBlock(item *Item, u TimeUnit, from, to calendar.Date) (pricedBlock, error) {
	up, err := e.selectBase(item, []TimeUnit{u}, from, qty.One)
	if err != nil {
		return pricedBlock{}, err
	}
	end := to
	if end.After(e.in.Departure) {
		end = e.in.Departure
	}
	pb := pricedBlock{up: up, b: Block{
		From: from, To: to, Unit: u, Nights: from.DaysUntil(end), Period: e.periods.of(from), RuleID: up.rule.ID, Base: up.amount,
	}}
	if err := e.applyModifiers(&pb, item); err != nil {
		return pricedBlock{}, err
	}
	return pb, nil
}

// proRataBlock prices the remainder [from, Departure) as half_even(U × r / D) of the unit starting at from.
// D is the length of the unit that would start there, counted from the arrival like the full units
// (k = number of full units before it).
func (e *engine) proRataBlock(item *Item, u TimeUnit, from calendar.Date, k int) (pricedBlock, error) {
	up, err := e.selectBase(item, []TimeUnit{u}, from, qty.One)
	if err != nil {
		return pricedBlock{}, err
	}
	r := int64(from.DaysUntil(e.in.Departure))
	var unitNights int64
	if u == UnitSeason {
		w := e.settings.SeasonWindow
		if w == nil || w.To.Before(w.From) {
			return pricedBlock{}, &NoPriceError{ItemID: item.ID, ContractType: e.in.ContractType, Night: from, Unit: u, Category: e.in.Resource.Category, Reason: ReasonUnitMissing}
		}
		unitNights = int64(w.From.DaysUntil(w.To)) + 1
	} else {
		end := e.unitEnd(u, from)
		switch u {
		case UnitYear:
			end = AddYears(e.in.Arrival, k+1)
		case UnitMonth:
			end = AddMonths(e.in.Arrival, k+1)
		}
		unitNights = int64(from.DaysUntil(end))
	}
	base, err := up.amount.MulRatChecked(r, unitNights)
	if err != nil {
		return pricedBlock{}, ErrOverflow
	}
	pb := pricedBlock{up: up, proRata: &[3]int64{r, unitNights, int64(up.amount)}, b: Block{
		From: from, To: e.in.Departure, Unit: u, Nights: int(r), Period: e.periods.of(from), RuleID: up.rule.ID, Base: base,
	}}
	if err := e.applyModifiers(&pb, item); err != nil {
		return pricedBlock{}, err
	}
	return pb, nil
}

// bestCombination chooses night, week and month blocks by dynamic programming over the night index, backwards:
// best[j] is the cheapest cover of nights j..N. Ties: lower cost, then fewer blocks, then the longer first unit,
// which (going backwards) gives the path whose first differing block has the longer unit.
func (e *engine) bestCombination(item *Item) ([]pricedBlock, error) {
	n := e.nights
	type step struct {
		ok     bool
		cost   money.Cents
		count  int
		unit   int // index in units: higher = longer
		next   int
		priced pricedBlock
	}
	units := []TimeUnit{UnitNight, UnitWeek, UnitMonth}
	best := make([]step, n+1)
	best[n] = step{ok: true}
	var dimsErr *InputError // reported when no path exists and some unit needed missing dimensions
	for j := n - 1; j >= 0; j-- {
		s := e.in.Arrival.AddDays(j)
		for ui, u := range units {
			pb, err := e.priceBlock(item, u, s, e.unitEnd(u, s))
			if err != nil {
				if !isNoPriceOrDims(err) {
					return nil, err
				}
				if ie, ok := errors.AsType[*InputError](err); ok && dimsErr == nil {
					dimsErr = ie
				}
				continue
			}
			next := min(n, j+s.DaysUntil(pb.b.To))
			if !best[next].ok {
				continue
			}
			cost, err := addCents(pb.b.Amount, best[next].cost)
			if err != nil {
				return nil, err
			}
			cand := step{ok: true, cost: cost, count: best[next].count + 1, unit: ui, next: next, priced: pb}
			cur := best[j]
			if !cur.ok || cand.cost < cur.cost || (cand.cost == cur.cost && (cand.count < cur.count || (cand.count == cur.count && cand.unit > cur.unit))) {
				best[j] = cand
			}
		}
	}
	if !best[0].ok {
		if dimsErr != nil {
			return nil, dimsErr
		}
		return nil, &NoPriceError{ItemID: item.ID, ContractType: e.in.ContractType, Night: e.in.Arrival, Unit: UnitNight, Category: e.in.Resource.Category, Reason: ReasonNoRule}
	}
	var out []pricedBlock
	for j := 0; j < n; j = best[j].next {
		out = append(out, best[j].priced)
	}
	return out, nil
}
