package account

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// ErrInvalidMode is returned for a billing mode outside actual, fixed, progress.
var ErrInvalidMode = errors.New("workorder/account: invalid billing mode")

// Key returns the line key of an item and work type (m10-plan §9.1): item:<item_id>|wt:<work_type_id>.
func Key(itemID, workTypeID string) string { return "item:" + itemID + "|wt:" + workTypeID }

// ExternalKey returns the line key of an external cost.
func ExternalKey(id string) string { return "ext:" + id }

// IsLabour reports whether an item type is billed by approved hours.
func IsLabour(itemType string) bool { return itemType == "labour" }

// IsMaterial reports whether an item type is billed by issued material movements.
func IsMaterial(itemType string) bool { return itemType == "material" || itemType == "product" }

// lineAcc accumulates one actual line.
type lineAcc struct {
	line      Line
	actualNet money.Cents // revenue of the actual quantities (mode actual)
	budgetVAT money.Cents
	order     int
}

// vatOf computes the VAT of a revenue amount in the item's price terms (M5 lines): net-priced items charge
// net.PercentBP(rate); gross-priced items split the amount (money.VATSplit) and the revenue is the net part.
func vatOf(amount money.Cents, v VAT) (net, vat money.Cents) {
	if v.PriceIncludesVAT {
		return money.VATSplit(amount, v.RateBP)
	}
	return amount, amount.PercentBP(v.RateBP)
}

// MarginBP is margin × 10000 / revenue half-even, nil when revenue is 0.
func MarginBP(margin, revenue money.Cents) *int64 {
	if revenue == 0 {
		return nil
	}
	v := money.DivRoundHalfEven(int64(margin)*10000, int64(revenue))
	return &v
}

// Build computes the account (pure). Lines are one per key: budget keys always appear (with zero actuals when
// nothing was recorded) so that Σ_keys (actual − budget) = revenue − quote net (§9.3 eq. 7).
func Build(in Input) (Account, error) {
	switch in.Mode {
	case ModeActual, ModeFixed, ModeProgress:
	default:
		return Account{}, ErrInvalidMode
	}
	accs := map[string]*lineAcc{}
	n := 0
	get := func(key, source, itemID, wt string) *lineAcc {
		a := accs[key]
		if a == nil {
			n++
			a = &lineAcc{line: Line{Key: key, Source: source, ItemID: itemID, WorkTypeID: wt, VAT: in.ItemVAT[itemID]}, order: n}
			accs[key] = a
		}
		return a
	}
	wtOr := func(wt string) string {
		if wt == "" {
			return in.Rates.OrderWorkType
		}
		return wt
	}

	// Budget per key (quote and variation lines).
	var quoteNet money.Cents
	for _, b := range in.Budget {
		src := SourceService
		switch {
		case IsLabour(b.ItemType):
			src = SourceLabour
		case IsMaterial(b.ItemType):
			src = SourceMaterial
		}
		a := get(Key(b.ItemID, wtOr(b.WorkTypeID)), src, b.ItemID, wtOr(b.WorkTypeID))
		if a.line.QualificationID == "" {
			a.line.QualificationID = b.QualificationID
		}
		var err error
		if a.line.BudgetQty, err = a.line.BudgetQty.Add(b.Qty); err != nil {
			return Account{}, err
		}
		a.line.BudgetNet += b.Net
		a.budgetVAT += b.VAT
		quoteNet += b.Net
		if src == SourceService {
			// Services and other lines: actual = consumed quantity (default the budget quantity) at the budget unit
			// price, minus the budget discount; cost at the frozen unit cost.
			q := b.Qty
			if b.ConsumedQty != nil {
				q = *b.ConsumedQty
			}
			rev, err := q.MulCents(b.Unit)
			if err != nil {
				return Account{}, err
			}
			rev -= b.Discount
			unitCost := b.UnitCost
			if unitCost == 0 {
				unitCost = in.ItemCost[b.ItemID]
			}
			cost, err := q.MulCents(unitCost)
			if err != nil {
				return Account{}, err
			}
			if a.line.Qty, err = a.line.Qty.Add(q); err != nil {
				return Account{}, err
			}
			// Gross-priced services: the budget amounts are gross; the revenue is the net part (vatOf below).
			a.actualNet += rev
			a.line.Cost += cost
		}
	}

	// Labour: approved hours per labour item of the qualification × work type.
	var hours qty.Milli
	for _, e := range in.Entries {
		var err error
		if hours, err = hours.Add(e.Hours); err != nil {
			return Account{}, err
		}
		item := in.Rates.LabourItems[e.QualificationID]
		key := Key(item, wtOr(e.WorkTypeID))
		if item == "" {
			key = "labour:" + e.QualificationID + "|wt:" + wtOr(e.WorkTypeID)
		}
		a := get(key, SourceLabour, item, wtOr(e.WorkTypeID))
		if a.line.QualificationID == "" {
			a.line.QualificationID = e.QualificationID
		}
		if a.line.Qty, err = a.line.Qty.Add(e.Hours); err != nil {
			return Account{}, err
		}
		a.line.Cost += e.CostCents
	}
	// Labour revenue on the aggregate hours (rounding once per line, §9.1).
	for _, a := range accs {
		if a.line.Source != SourceLabour || a.line.ItemID == "" {
			continue
		}
		rev, err := a.line.Qty.MulCents(in.Rates.Units[a.line.ItemID])
		if err != nil {
			return Account{}, err
		}
		a.actualNet += rev
	}

	// Materials: signed movements per item × work type.
	for _, m := range in.Movements {
		a := get(Key(m.ItemID, wtOr(m.WorkTypeID)), SourceMaterial, m.ItemID, wtOr(m.WorkTypeID))
		q := m.Qty
		if m.Return {
			q = -q
		}
		rev, err := q.MulCents(m.UnitPrice)
		if err != nil {
			return Account{}, err
		}
		cost, err := q.MulCents(m.UnitCost)
		if err != nil {
			return Account{}, err
		}
		if a.line.Qty, err = a.line.Qty.Add(q); err != nil {
			return Account{}, err
		}
		a.actualNet += rev
		a.line.Cost += cost
	}

	// External costs: one line each.
	for _, x := range in.External {
		a := get(ExternalKey(x.ID), SourceExternal, x.ItemID, wtOr(x.WorkTypeID))
		a.line.Qty = 1000
		a.line.Cost = x.Cost
		if x.Billable {
			a.actualNet = x.Price
		}
	}

	lines := make([]*lineAcc, 0, len(accs))
	for _, a := range accs {
		lines = append(lines, a)
	}
	slices.SortFunc(lines, func(x, y *lineAcc) int { return x.order - y.order })

	out := Account{QuoteNet: quoteNet, Lines: make([]Line, 0, len(lines))}
	t := &out.Totals
	t.HoursLabour = hours
	byWT := map[string]*WorkTypeTotals{}
	for _, a := range lines {
		l := a.line
		var net, vat money.Cents
		switch {
		case in.Mode == ModeActual || l.Source == SourceExternal:
			net, vat = vatOf(a.actualNet, l.VAT)
			if l.Source == SourceExternal && in.Mode != ModeActual && in.FixedIncludesExternal {
				net, vat = 0, 0
			}
		default:
			// fixed / progress: the budget per key, with the quote VAT exactly.
			net, vat = l.BudgetNet, a.budgetVAT
		}
		l.Revenue, l.VATCents, l.Gross = net, vat, net+vat
		l.Margin = l.Revenue - l.Cost
		l.MarginBP = MarginBP(l.Margin, l.Revenue)
		l.DeltaNet = l.Revenue - l.BudgetNet
		switch l.Source {
		case SourceLabour:
			t.RevenueLabour += l.Revenue
			t.CostLabour += l.Cost
		case SourceMaterial:
			t.RevenueMaterial += l.Revenue
			t.CostMaterial += l.Cost
		case SourceService:
			t.RevenueService += l.Revenue
			t.CostService += l.Cost
		case SourceExternal:
			t.RevenueExt += l.Revenue
			t.CostExt += l.Cost
		}
		t.Revenue += l.Revenue
		t.Cost += l.Cost
		t.VATCents += l.VATCents
		w := byWT[l.WorkTypeID]
		if w == nil {
			w = &WorkTypeTotals{WorkTypeID: l.WorkTypeID}
			byWT[l.WorkTypeID] = w
		}
		w.Revenue += l.Revenue
		w.Cost += l.Cost
		out.Lines = append(out.Lines, l)
		if l.Gross != 0 || l.Revenue != 0 {
			out.Target = append(out.Target, TargetLine{
				PlanKey: l.Key, ItemID: l.ItemID, Qty: l.Qty, Net: l.Revenue, VAT: l.VAT, VATCents: l.VATCents, Gross: l.Gross,
			})
		}
	}
	t.Margin = t.Revenue - t.Cost
	t.MarginBP = MarginBP(t.Margin, t.Revenue)
	t.Gross = t.Revenue + t.VATCents
	out.DeltaNet = t.Revenue - quoteNet
	for _, w := range byWT {
		w.Margin = w.Revenue - w.Cost
		w.MarginBP = MarginBP(w.Margin, w.Revenue)
		out.ByWorkType = append(out.ByWorkType, *w)
	}
	slices.SortFunc(out.ByWorkType, func(a, b WorkTypeTotals) int { return strings.Compare(a.WorkTypeID, b.WorkTypeID) })
	return out, nil
}

// BudgetTarget is the billing target of the budget (advance at acceptance, progress billings): the budget lines
// aggregated by key with their quote amounts.
func BudgetTarget(budget []BudgetLine, itemVAT map[string]VAT, orderWorkType string) []TargetLine {
	idx := map[string]int{}
	var out []TargetLine
	for _, b := range budget {
		wt := b.WorkTypeID
		if wt == "" {
			wt = orderWorkType
		}
		k := Key(b.ItemID, wt)
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, TargetLine{PlanKey: k, ItemID: b.ItemID, VAT: itemVAT[b.ItemID]})
		}
		out[i].Qty += b.Qty
		out[i].Net += b.Net
		out[i].VATCents += b.VAT
		out[i].Gross += b.Gross
	}
	kept := out[:0]
	for _, l := range out {
		if l.Gross != 0 {
			kept = append(kept, l)
		}
	}
	return kept
}

// Completion returns the order completion: Σ(progress_bp × est_hours) / Σ est_hours half-even, the plain mean when
// Σ est_hours = 0; cancelled tasks are excluded by the caller (m10-plan §5.3). No task is 0.
func Completion(progressBP []int64, estHours []qty.Milli) (int64, error) {
	if len(progressBP) != len(estHours) {
		return 0, fmt.Errorf("workorder/account: %d progress values for %d estimates", len(progressBP), len(estHours))
	}
	if len(progressBP) == 0 {
		return 0, nil
	}
	var num, den int64
	for i, p := range progressBP {
		if p < 0 || p > 10000 || estHours[i] < 0 {
			return 0, fmt.Errorf("workorder/account: invalid task %d", i)
		}
		num += p * int64(estHours[i])
		den += int64(estHours[i])
	}
	if den == 0 {
		var s int64
		for _, p := range progressBP {
			s += p
		}
		return money.DivRoundHalfEven(s, int64(len(progressBP))), nil
	}
	return money.DivRoundHalfEven(num, den), nil
}
