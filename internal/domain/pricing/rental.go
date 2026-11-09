package pricing

import (
	"errors"
	"slices"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Rental pricing (M11, m11-plan §5): the pure RentalQuote prices rental items (revenue class rental) from the same
// price lists as moorings, with the time units hour, half_day, day and week on flat base rules, rate periods per
// priced date and the scope time_bands for half days. EngineVersion does not change: Quote ignores these rules.
// Owned by FLEET after the contract phase.

// Rental time units (alongside UnitWeek). They are valid only on flat base rules of rental items (validate.go).
const (
	UnitHour    TimeUnit = "hour"
	UnitHalfDay TimeUnit = "half_day"
	UnitDay     TimeUnit = "day"
)

// RentalTimeUnits returns the time units of rental rules.
func RentalTimeUnits() []TimeUnit { return []TimeUnit{UnitHour, UnitHalfDay, UnitDay, UnitWeek} }

// RentalEngineVersion is stored in rental price snapshots (INV-20 extended).
const RentalEngineVersion = 1

// RentalKind is a rental formula kind.
type RentalKind string

// Rental formula kinds (rental_formulas.kind).
const (
	RentalHourly   RentalKind = "hourly"
	RentalHalfDay  RentalKind = "half_day"
	RentalDay      RentalKind = "day"
	RentalMultiDay RentalKind = "multi_day"
	RentalWeek     RentalKind = "week"
)

// Rental line kinds.
const (
	RentalLineUnit    = "unit"
	RentalLineSkipper = "skipper"
	RentalLineExtra   = "extra"
)

// RentalExtra is an extra of a rental quote; PerDay multiplies by the rental days (hourly and half day = 1).
type RentalExtra struct {
	ItemID string
	Qty    qty.Milli
	PerDay bool
}

// RentalInput is the input of RentalQuote. Start is the first date; Days the calendar dates touched (1 for hourly,
// half day and day); Slots the 30-minute slots of an hourly rental; Band the band code of a half day.
type RentalInput struct {
	ItemID           string
	Kind             RentalKind
	Start            calendar.Date
	Days             int
	Slots            int
	Band             string
	SkipperItemID    string // "" = no skipper
	Extras           []RentalExtra
	Channel          Channel
	CustomerCategory string
	QuotedOn         calendar.Date
}

// RentalLine is a line of a rental quote.
type RentalLine struct {
	Seq              int         `json:"seq"`
	Kind             string      `json:"kind"` // unit, skipper, extra
	ItemID           string      `json:"item_id"`
	ItemCode         string      `json:"item_code"`
	Qty              qty.Milli   `json:"qty_milli"`
	UnitCents        money.Cents `json:"unit_cents"`
	Amount           money.Cents `json:"amount_cents"`
	RuleIDs          []string    `json:"rule_ids,omitempty"`
	PriceIncludesVAT bool        `json:"price_includes_vat"`
	RevenueClass     string      `json:"revenue_class"`
}

// RentalBreakdown is the result of RentalQuote.
type RentalBreakdown struct {
	Lines         []RentalLine `json:"lines"`
	Total         money.Cents  `json:"total_cents"`
	EngineVersion int          `json:"engine_version"`
	Trace         []string     `json:"trace,omitempty"`
}

// rentalUnitOf is the time unit a formula kind prices (multi_day is priced per date with day rules).
func rentalUnitOf(k RentalKind) (TimeUnit, bool) {
	switch k {
	case RentalHourly:
		return UnitHour, true
	case RentalHalfDay:
		return UnitHalfDay, true
	case RentalDay, RentalMultiDay:
		return UnitDay, true
	case RentalWeek:
		return UnitWeek, true
	}
	return "", false
}

// RentalDays returns the days per-day extras are multiplied by: the dates touched for multi_day and week, 1 for
// hourly, half day and day.
func RentalDays(k RentalKind, dates int) int {
	if (k == RentalMultiDay || k == RentalWeek) && dates > 1 {
		return dates
	}
	return 1
}

// rentalScope keeps the scope lists RentalQuote honours: channels, customer categories, rate periods and time
// bands. Contract types, berth categories and groups do not apply to rentals.
func rentalScope(s Scope) Scope {
	return Scope{Channels: s.Channels, CustomerCategories: s.CustomerCategories, Periods: s.Periods, TimeBands: s.TimeBands}
}

func bandMatches(s Scope, band string) bool {
	return len(s.TimeBands) == 0 || (band != "" && slices.Contains(s.TimeBands, band))
}

// rentalPick is the winning rule of one priced date.
type rentalPick struct {
	rule *Rule
	cat  *Catalog
}

type rentalPricer struct {
	cats    []Catalog
	in      RentalInput
	channel Channel
	trace   []string
}

// pick returns the winning flat base rule of item for unit on the date (list validity and period of each catalog,
// channel, customer category and time band scopes; winner order ruleCmp).
func (p *rentalPricer) pick(itemID string, unit TimeUnit, on calendar.Date, band string) (rentalPick, error) {
	var best rentalPick
	var bestRule Rule
	for ci := range p.cats {
		cat := &p.cats[ci]
		pl := cat.PriceList
		if on.Before(pl.ValidFrom) || on.After(pl.ValidTo) {
			continue
		}
		ctx := matchCtx{channel: p.channel, customerCategory: p.in.CustomerCategory, period: newPeriodIndex(pl.Periods).of(on)}
		for ri := range cat.Rules {
			r := &cat.Rules[ri]
			if !r.Active || r.Stage != StageBase || r.ItemID != itemID || r.Unit != unit || r.AmountCents == nil {
				continue
			}
			sc := rentalScope(r.Scope)
			if !sc.matches(ctx) || !bandMatches(sc, band) {
				continue
			}
			cand := *r
			cand.Scope = sc
			if best.rule == nil || ruleCmp(&cand, &bestRule) < 0 {
				bestRule = cand
				best = rentalPick{rule: r, cat: cat}
			}
		}
	}
	if best.rule == nil {
		return best, &NoPriceError{ItemID: itemID, Night: on, Unit: unit, Band: band, Reason: ReasonNoRule}
	}
	p.trace = append(p.trace, "rule "+best.rule.ID+" "+string(unit)+" "+on.String())
	return best, nil
}

// amountOn is the flat amount of the winning rule of item for unit on the date.
func (p *rentalPricer) amountOn(itemID string, unit TimeUnit, on calendar.Date, band string, used *[]string) (money.Cents, *Catalog, error) {
	pk, err := p.pick(itemID, unit, on, band)
	if err != nil {
		return 0, nil, err
	}
	if !slices.Contains(*used, pk.rule.ID) {
		*used = append(*used, pk.rule.ID)
	}
	return *pk.rule.AmountCents, pk.cat, nil
}

// findItem returns an item by id: from the preferred catalog first, then any catalog.
func findItem(cats []Catalog, pref *Catalog, id string) *Item {
	if pref != nil {
		for i := range pref.Items {
			if pref.Items[i].ID == id {
				return &pref.Items[i]
			}
		}
	}
	for ci := range cats {
		for i := range cats[ci].Items {
			if cats[ci].Items[i].ID == id {
				return &cats[ci].Items[i]
			}
		}
	}
	return nil
}

// timeLine prices the unit or skipper line of itemID (m11-plan §5.2).
func (p *rentalPricer) timeLine(kind, itemID string) (RentalLine, error) {
	in := p.in
	unit, _ := rentalUnitOf(in.Kind)
	var (
		amount    money.Cents
		q         qty.Milli
		cat       *Catalog
		used      []string
		err       error
		hourCents money.Cents
	)
	add := func(c money.Cents) error {
		amount, err = addCents(amount, c)
		return err
	}
	switch in.Kind {
	case RentalHourly:
		var hour money.Cents
		if hour, cat, err = p.amountOn(itemID, UnitHour, in.Start, "", &used); err != nil {
			return RentalLine{}, err
		}
		if amount, err = hour.MulRatChecked(int64(in.Slots), 2); err != nil {
			return RentalLine{}, ErrOverflow
		}
		q, hourCents = qty.Milli(int64(in.Slots)*500), hour
	case RentalHalfDay, RentalDay:
		band := ""
		if in.Kind == RentalHalfDay {
			band = in.Band
		}
		if amount, cat, err = p.amountOn(itemID, unit, in.Start, band, &used); err != nil {
			return RentalLine{}, err
		}
		q = qty.One
	case RentalMultiDay:
		for i := 0; i < in.Days; i++ {
			var c money.Cents
			var dc *Catalog
			if c, dc, err = p.amountOn(itemID, UnitDay, in.Start.AddDays(i), "", &used); err != nil {
				return RentalLine{}, err
			}
			if cat == nil {
				cat = dc
			}
			if add(c) != nil {
				return RentalLine{}, ErrOverflow
			}
		}
		q = qty.Milli(int64(in.Days) * 1000)
	case RentalWeek:
		weeks, rest := in.Days/7, in.Days%7
		for k := 0; k < weeks; k++ {
			var c money.Cents
			var wc *Catalog
			if c, wc, err = p.amountOn(itemID, UnitWeek, in.Start.AddDays(7*k), "", &used); err != nil {
				return RentalLine{}, err
			}
			if cat == nil {
				cat = wc
			}
			if add(c) != nil {
				return RentalLine{}, ErrOverflow
			}
		}
		if rest > 0 {
			first := in.Start.AddDays(7 * weeks)
			capC, wc, werr := p.amountOn(itemID, UnitWeek, first, "", &used)
			if werr != nil {
				return RentalLine{}, werr
			}
			if cat == nil {
				cat = wc
			}
			var days money.Cents
			daysOK := true
			var dayUsed []string
			for i := 0; i < rest; i++ {
				c, _, derr := p.amountOn(itemID, UnitDay, first.AddDays(i), "", &dayUsed)
				if derr != nil {
					daysOK = false
					break
				}
				if days, err = addCents(days, c); err != nil {
					return RentalLine{}, ErrOverflow
				}
			}
			rem := capC
			if daysOK && days < capC {
				rem = days
				for _, id := range dayUsed {
					if !slices.Contains(used, id) {
						used = append(used, id)
					}
				}
			}
			if add(rem) != nil {
				return RentalLine{}, ErrOverflow
			}
		}
		q = qty.Milli(int64(in.Days) * 1000)
	}
	line := RentalLine{Kind: kind, ItemID: itemID, Qty: q, Amount: amount, RuleIDs: used}
	switch {
	case in.Kind == RentalHourly:
		line.UnitCents = hourCents
	case q == qty.One:
		line.UnitCents = amount
	default:
		line.UnitCents = money.Cents(money.DivRoundHalfEven(int64(amount), int64(q)/1000))
	}
	p.fillItem(&line, cat)
	return line, nil
}

func (p *rentalPricer) fillItem(line *RentalLine, cat *Catalog) {
	if it := findItem(p.cats, cat, line.ItemID); it != nil {
		line.ItemCode, line.PriceIncludesVAT, line.RevenueClass = it.Code, it.PriceIncludesVAT, it.RevenueClass
	}
}

func rentalInputErr(field, code string) error { return &InputError{Field: field, Code: code} }

// Input codes of a rental quote.
const (
	InputRentalKind    = "invalid_rental_kind"
	InputRentalSlots   = "invalid_slots"
	InputRentalDays    = "invalid_days"
	InputRentalQty     = "invalid_qty"
	InputRentalMissing = "required"
)

func (in RentalInput) validate() error {
	if in.ItemID == "" {
		return rentalInputErr("item_id", InputRentalMissing)
	}
	if _, ok := rentalUnitOf(in.Kind); !ok {
		return rentalInputErr("kind", InputRentalKind)
	}
	if in.Start.IsZero() {
		return rentalInputErr("start", InputRentalMissing)
	}
	if in.Channel != "" && !slices.Contains(Channels(), in.Channel) {
		return rentalInputErr("channel", InputInvalidChannel)
	}
	switch in.Kind {
	case RentalHourly:
		if in.Slots < 1 || in.Slots > 48 {
			return rentalInputErr("slots", InputRentalSlots)
		}
	case RentalHalfDay:
		if in.Band == "" {
			return rentalInputErr("band", InputRentalMissing)
		}
	case RentalMultiDay, RentalWeek:
		if in.Days < 1 || in.Days > MaxNights {
			return rentalInputErr("days", InputRentalDays)
		}
	}
	for _, x := range in.Extras {
		if x.ItemID == "" {
			return rentalInputErr("extras.item_id", InputRentalMissing)
		}
		if x.Qty < 0 || x.Qty > MaxQtyMilli {
			return rentalInputErr("extras.qty_milli", InputRentalQty)
		}
	}
	return nil
}

// RentalQuote prices a rental (m11-plan §5.2): the unit line, the skipper line (same algorithm on SkipperItemID)
// and the extras (UnitPrice on Start × qty, × days when per day). No rule gives *NoPriceError{Reason:
// ReasonNoRule}. No modifiers, discounts or total rounding (D4). Pure: no store, no clock, no float.
func RentalQuote(cats []Catalog, in RentalInput) (RentalBreakdown, error) {
	if err := in.validate(); err != nil {
		return RentalBreakdown{}, err
	}
	if in.Kind == RentalHourly || in.Kind == RentalHalfDay || in.Kind == RentalDay {
		in.Days = 1
	}
	p := &rentalPricer{cats: cats, in: in, channel: in.Channel}
	if p.channel == "" {
		p.channel = ChannelDesk
	}
	out := RentalBreakdown{Lines: []RentalLine{}, EngineVersion: RentalEngineVersion}
	push := func(l RentalLine) error {
		l.Seq = len(out.Lines) + 1
		out.Lines = append(out.Lines, l)
		t, err := addCents(out.Total, l.Amount)
		if err != nil {
			return ErrOverflow
		}
		out.Total = t
		return nil
	}
	unit, err := p.timeLine(RentalLineUnit, in.ItemID)
	if err != nil {
		return RentalBreakdown{}, err
	}
	if err := push(unit); err != nil {
		return RentalBreakdown{}, err
	}
	if in.SkipperItemID != "" {
		sk, err := p.timeLine(RentalLineSkipper, in.SkipperItemID)
		if err != nil {
			return RentalBreakdown{}, err
		}
		if err := push(sk); err != nil {
			return RentalBreakdown{}, err
		}
	}
	days := int64(RentalDays(in.Kind, in.Days))
	for _, x := range in.Extras {
		up, err := UnitPrice(cats, x.ItemID, in.Start, in.CustomerCategory)
		if err != nil {
			return RentalBreakdown{}, err
		}
		q := x.Qty
		if q == 0 {
			q = qty.One
		}
		if x.PerDay {
			if q, err = q.MulInt(days); err != nil {
				return RentalBreakdown{}, ErrOverflow
			}
		}
		amount, err := q.MulCents(up.UnitCents)
		if err != nil {
			return RentalBreakdown{}, ErrOverflow
		}
		line := RentalLine{Kind: RentalLineExtra, ItemID: x.ItemID, Qty: q, UnitCents: up.UnitCents, Amount: amount, RuleIDs: []string{up.RuleID}}
		var cat *Catalog
		for ci := range cats {
			if cats[ci].PriceList.ID == up.PriceListID {
				cat = &cats[ci]
				break
			}
		}
		p.fillItem(&line, cat)
		p.trace = append(p.trace, "extra "+x.ItemID+" qty "+strconv.FormatInt(int64(q), 10))
		if err := push(line); err != nil {
			return RentalBreakdown{}, err
		}
	}
	if out.Total > MaxAmountCents*100 {
		return RentalBreakdown{}, ErrOverflow
	}
	out.Trace = p.trace
	return out, nil
}

// ErrRentalNotImplemented was returned by the CONTRACT skeleton; kept because errmap.go maps it (owned elsewhere).
var ErrRentalNotImplemented = errors.New("pricing: rental quote not implemented")
