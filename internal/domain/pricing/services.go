package pricing

import (
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// newLine starts a line for item with the item's fiscal and classification data.
func (e *engine) newLine(kind string, item *Item, q qty.Milli) Line {
	return Line{
		Kind: kind, ItemID: item.ID, ItemCode: item.Code, ItemType: item.Type, RevenueClass: item.RevenueClass,
		ExternalCode: item.ExternalCode, Qty: q, VATRateBP: item.VATRateBP, VATNature: item.VATNature,
		PriceIncludesVAT: item.PriceIncludesVAT, ServiceFrom: e.in.Arrival, ServiceTo: e.stayLastNight(),
		Blocks: []Block{}, Discounts: []Applied{},
	}
}

// sumBlocks fills Base, ModifierTotal and Amount of a line from its blocks.
func sumBlocks(l *Line) error {
	var base, amount money.Cents
	var err error
	for _, b := range l.Blocks {
		if base, err = addCents(base, b.Base); err != nil {
			return err
		}
		if amount, err = addCents(amount, b.Amount); err != nil {
			return err
		}
	}
	l.Base, l.Amount, l.ModifierTotal = base, amount, amount-base
	return nil
}

// includedLines turns the inclusions of the winning base rule of the first block into zero-amount lines.
func (e *engine) includedLines(rule *Rule) ([]Line, []*Item, error) {
	var lines []Line
	var items []*Item
	for _, inc := range rule.Includes {
		it, ok := e.items[inc.ItemID]
		if !ok {
			return nil, nil, &InputError{Field: "includes", Code: InputUnknownItem}
		}
		q := inc.Qty
		if q <= 0 {
			q = qty.One
		}
		l := e.newLine(LineIncluded, it, q)
		l.Blocks = []Block{{
			From: e.in.Arrival, To: e.in.Departure, Unit: UnitStay, Nights: e.nights,
			Period: e.periods.of(e.in.Arrival), RuleID: rule.ID, Modifiers: []Applied{},
		}}
		lines = append(lines, l)
		items = append(items, it)
	}
	return lines, items, nil
}

// serviceLine prices an added service with the item's winning base rule at the arrival night (unit stay once,
// unit night × N; metered rules take the quantity, the others are multiplied by it).
func (e *engine) serviceLine(req ServiceRequest) (Line, *Item, pricedBlock, error) {
	it, ok := e.items[req.ItemID]
	if !ok || !it.Active {
		return Line{}, nil, pricedBlock{}, &InputError{Field: "services", Code: InputUnknownItem}
	}
	q := req.Qty
	if q <= 0 {
		q = qty.One
	}
	if q > MaxQtyMilli {
		return Line{}, nil, pricedBlock{}, ErrOverflow
	}
	up, err := e.selectBase(it, []TimeUnit{UnitStay, UnitNight}, e.in.Arrival, q)
	if err != nil {
		return Line{}, nil, pricedBlock{}, err
	}
	amount := up.amount
	if up.rule.Basis != BasisMetered {
		if amount, err = q.MulCents(amount); err != nil {
			return Line{}, nil, pricedBlock{}, ErrOverflow
		}
	}
	if up.rule.Unit == UnitNight {
		a, err := amount.MulRatChecked(int64(e.nights), 1)
		if err != nil {
			return Line{}, nil, pricedBlock{}, ErrOverflow
		}
		amount = a
	}
	pb := pricedBlock{up: up, b: Block{
		From: e.in.Arrival, To: e.in.Departure, Unit: up.rule.Unit, Nights: e.nights,
		Period: e.periods.of(e.in.Arrival), RuleID: up.rule.ID, Base: amount,
	}}
	if err := e.applyModifiers(&pb, it); err != nil {
		return Line{}, nil, pricedBlock{}, err
	}
	l := e.newLine(LineService, it, q)
	l.Blocks = []Block{pb.b}
	if err := sumBlocks(&l); err != nil {
		return Line{}, nil, pricedBlock{}, err
	}
	return l, it, pb, nil
}
