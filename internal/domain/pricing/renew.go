package pricing

import (
	"encoding/json"
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// InputError codes of the tools.
const (
	InputOutOfRange  = "out_of_range"
	InputInvalidRows = "invalid_rows"
)

// Renew returns the content of a new draft list derived from cat (spec 12.6, m2-plan §2.5): periods, bands,
// settings and rules are copied, every date is shifted by ShiftYears (Feb 29 → Feb 28), base-rule amounts are
// adjusted (ISTAT first, then the most specific adjustment, then the amount rounding), modifier and discount
// effects are copied unchanged. Ids of the list and of the rules are cleared; Seq values are kept.
func Renew(cat Catalog, spec RenewalSpec) (Catalog, error) {
	if err := checkRenewal(spec); err != nil {
		return Catalog{}, err
	}
	out, err := deepCopy(cat)
	if err != nil {
		return Catalog{}, err
	}
	pl := &out.PriceList
	shift := func(d calendar.Date) calendar.Date {
		if d.IsZero() || spec.ShiftYears == 0 {
			return d
		}
		return AddYears(d, spec.ShiftYears)
	}
	pl.ID = ""
	pl.Code = spec.Code
	pl.ValidFrom, pl.ValidTo = shift(pl.ValidFrom), shift(pl.ValidTo)
	if spec.ValidFrom != nil {
		pl.ValidFrom = *spec.ValidFrom
	}
	if spec.ValidTo != nil {
		pl.ValidTo = *spec.ValidTo
	}
	for i := range pl.Periods {
		for j := range pl.Periods[i].Ranges {
			r := &pl.Periods[i].Ranges[j]
			r.From, r.To = shift(r.From), shift(r.To)
		}
	}
	if w := pl.Settings.SeasonWindow; w != nil {
		w.From, w.To = shift(w.From), shift(w.To)
	}
	bandCategory := map[string]string{}
	for _, b := range pl.Bands {
		bandCategory[b.Code] = b.Category
	}
	for i := range out.Rules {
		r := &out.Rules[i]
		r.ID = ""
		if r.Stage != StageBase {
			continue
		}
		adj := func(a money.Cents, category string) (money.Cents, error) {
			return adjustAmount(a, r.ItemID, category, spec)
		}
		if r.AmountCents != nil {
			v, err := adj(*r.AmountCents, "")
			if err != nil {
				return Catalog{}, err
			}
			r.AmountCents = &v
		}
		if r.UnitCents != nil {
			v, err := adj(*r.UnitCents, "")
			if err != nil {
				return Catalog{}, err
			}
			r.UnitCents = &v
		}
		for _, k := range sortedKeys(r.Amounts) {
			category := ""
			switch r.Basis {
			case BasisPerBerth:
				category = k
			case BasisLengthBand:
				category = bandCategory[k]
			}
			v, err := adj(r.Amounts[k], category)
			if err != nil {
				return Catalog{}, err
			}
			r.Amounts[k] = v
		}
	}
	return out, nil
}

func checkRenewal(spec RenewalSpec) error {
	if spec.ShiftYears < 0 || spec.ShiftYears > 5 {
		return &InputError{Field: "shift_years", Code: InputOutOfRange}
	}
	if spec.ISTATBP < 0 || spec.ISTATBP > 10_000 {
		return &InputError{Field: "istat_bp", Code: InputOutOfRange}
	}
	switch spec.AmountRounding {
	case "", AmountRoundNone, AmountRoundTenCents, AmountRoundEuro:
	default:
		return &InputError{Field: "amount_rounding", Code: InputOutOfRange}
	}
	for _, a := range spec.Adjustments {
		if (a.PercentBP == nil) == (a.AmountCents == nil) {
			return &InputError{Field: "adjustments", Code: InputOutOfRange}
		}
		if a.PercentBP != nil && (*a.PercentBP < -10_000 || *a.PercentBP > MaxModifierBP) {
			return &InputError{Field: "adjustments", Code: InputOutOfRange}
		}
		if a.AmountCents != nil && (*a.AmountCents < -MaxAmountCents || *a.AmountCents > MaxAmountCents) {
			return &InputError{Field: "adjustments", Code: InputOutOfRange}
		}
		switch a.Scope {
		case AdjustAll:
		case AdjustItem:
			if a.ItemID == "" {
				return &InputError{Field: "adjustments", Code: InputOutOfRange}
			}
		case AdjustCategory:
			if a.Category == "" {
				return &InputError{Field: "adjustments", Code: InputOutOfRange}
			}
		default:
			return &InputError{Field: "adjustments", Code: InputOutOfRange}
		}
	}
	return nil
}

// adjustAmount applies ISTAT, the most specific adjustment (item, else category, else all; the first one of
// that scope in the list) and the amount rounding.
func adjustAmount(a money.Cents, itemID, category string, spec RenewalSpec) (money.Cents, error) {
	var err error
	if spec.ISTATBP != 0 {
		if a, err = a.MulRatChecked(10_000+spec.ISTATBP, 10_000); err != nil {
			return 0, ErrOverflow
		}
	}
	pick := func(scope string, ok func(Adjustment) bool) *Adjustment {
		i := slices.IndexFunc(spec.Adjustments, func(x Adjustment) bool { return x.Scope == scope && ok(x) })
		if i < 0 {
			return nil
		}
		return &spec.Adjustments[i]
	}
	adj := pick(AdjustItem, func(x Adjustment) bool { return x.ItemID == itemID })
	if adj == nil && category != "" {
		adj = pick(AdjustCategory, func(x Adjustment) bool { return x.Category == category })
	}
	if adj == nil {
		adj = pick(AdjustAll, func(Adjustment) bool { return true })
	}
	if adj != nil {
		switch {
		case adj.PercentBP != nil:
			if a, err = a.MulRatChecked(10_000+*adj.PercentBP, 10_000); err != nil {
				return 0, ErrOverflow
			}
		case adj.AmountCents != nil:
			if a, err = addCents(a, *adj.AmountCents); err != nil {
				return 0, err
			}
		}
		a = max(a, 0)
	}
	switch spec.AmountRounding {
	case AmountRoundTenCents:
		a = roundToStep(a, 10)
	case AmountRoundEuro:
		a = roundToStep(a, 100)
	}
	return a, nil
}

// deepCopy copies a catalog so the result shares no slice or map with the input.
func deepCopy(cat Catalog) (Catalog, error) {
	raw, err := json.Marshal(cat)
	if err != nil {
		return Catalog{}, err
	}
	var out Catalog
	if err := json.Unmarshal(raw, &out); err != nil {
		return Catalog{}, err
	}
	return out, nil
}

// Diff compares the matrices of before and after for the contract type of spec (m2-plan §2.5): the probes of
// after use its own (shifted) dates. Cells are matched by row and column index.
func Diff(before, after Catalog, spec MatrixSpec) (MatrixDiff, error) {
	mb, err := Matrix(before, spec)
	if err != nil {
		return MatrixDiff{}, err
	}
	ma, err := Matrix(after, spec)
	if err != nil {
		return MatrixDiff{}, err
	}
	d := MatrixDiff{ContractType: spec.ContractType, Before: mb, After: ma, Cells: []DiffCell{}}
	index := map[[2]int]*MatrixCell{}
	for i := range mb.Cells {
		index[[2]int{mb.Cells[i].Row, mb.Cells[i].Col}] = &mb.Cells[i]
	}
	for i := range ma.Cells {
		ac := &ma.Cells[i]
		dc := DiffCell{Row: ac.Row, Col: ac.Col}
		if ac.Breakdown != nil {
			v := ac.Breakdown.Total
			dc.NewCents = &v
		}
		if bc := index[[2]int{ac.Row, ac.Col}]; bc != nil && bc.Breakdown != nil {
			v := bc.Breakdown.Total
			dc.OldCents = &v
		}
		if dc.OldCents != nil && dc.NewCents != nil {
			delta := *dc.NewCents - *dc.OldCents
			dc.DeltaCents = &delta
			if *dc.OldCents != 0 {
				bp, err := delta.MulRatChecked(10_000, int64(*dc.OldCents))
				if err != nil {
					return MatrixDiff{}, ErrOverflow
				}
				v := int64(bp)
				dc.DeltaBP = &v
			}
		}
		d.Cells = append(d.Cells, dc)
	}
	return d, nil
}
