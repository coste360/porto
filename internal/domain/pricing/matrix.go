package pricing

import (
	"errors"
	"slices"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
)

// Matrix column keys besides period codes.
const (
	ColumnYear   = "year"
	ColumnSeason = "season"
	ColumnAll    = "all" // a list without periods: one column from the first night of validity
)

// Matrix computes a price matrix through probes priced by Quote (INV-23): it never computes a price itself,
// every cell is Quote(cell.Probe, cat).
func Matrix(cat Catalog, spec MatrixSpec) (MatrixResult, error) {
	if !slices.Contains(cat.PriceList.ContractTypes, spec.ContractType) {
		return MatrixResult{}, &InputError{Field: "contract_type", Code: InputContractTypeNotServed}
	}
	if spec.Channel != "" && !slices.Contains(Channels(), spec.Channel) {
		return MatrixResult{}, &InputError{Field: "channel", Code: InputInvalidChannel}
	}
	rows, probes, err := matrixRows(cat, spec)
	if err != nil {
		return MatrixResult{}, err
	}
	cols := matrixColumns(cat, spec.ContractType)
	res := MatrixResult{ContractType: spec.ContractType, ItemID: spec.ItemID, Rows: rows, Columns: cols, Cells: []MatrixCell{}}
	if res.ItemID == "" {
		res.ItemID = cat.PriceList.Settings.PrimaryItems[spec.ContractType]
	}
	hash := catalogHash(cat)
	for ri := range rows {
		for ci, col := range cols {
			probe := probes[ri]
			probe.ContractType = spec.ContractType
			probe.ItemID = spec.ItemID
			probe.Arrival, probe.Departure = col.Arrival, col.Departure
			probe.Channel = spec.Channel
			if probe.Channel == "" {
				probe.Channel = ChannelDesk
			}
			probe.CustomerCategory = spec.CustomerCategory
			probe.QuotedOn = spec.QuotedOn
			cell := MatrixCell{Row: ri, Col: ci, Probe: probe}
			bd, err := quote(probe, cat, hash)
			if err != nil {
				cell.Error = ErrorCode(err)
			} else {
				cell.Breakdown = &bd
			}
			res.Cells = append(res.Cells, cell)
		}
	}
	return res, nil
}

// ErrorCode is the stable code of an engine error as the corpus and the matrix cells write it:
// no_price:<reason>, invalid_input:<code>, promo_not_applicable, overflow.
func ErrorCode(err error) string {
	if np, ok := errors.AsType[*NoPriceError](err); ok {
		return "no_price:" + np.Reason
	}
	if ie, ok := errors.AsType[*InputError](err); ok {
		return "invalid_input:" + ie.Code
	}
	switch {
	case errors.Is(err, ErrPromoNotApplicable):
		return "promo_not_applicable"
	case errors.Is(err, ErrOverflow):
		return "overflow"
	case err == nil:
		return ""
	}
	return "error"
}

// probeBeam is the beam of a band probe: half_even(LOA × ratio), ratio = the list's minimum beam ratio or 1/3.
func probeBeam(cat Catalog, loa geo.Cm) geo.Cm {
	num, den := int64(1), int64(3)
	if r := cat.PriceList.Settings.MinBeamRatio; r != nil && r.Den > 0 && r.Num > 0 {
		num, den = r.Num, r.Den
	}
	return geo.Cm(money.DivRoundHalfEven(int64(loa)*num, den))
}

func matrixRows(cat Catalog, spec MatrixSpec) ([]MatrixRow, []Input, error) {
	rows := []MatrixRow{}
	var probes []Input
	switch spec.Rows {
	case "", RowsBands:
		var prev geo.Cm
		bands := cat.PriceList.Bands
		for _, b := range bands {
			loa := geo.Cm(100)
			if b.MaxCm != nil {
				loa = *b.MaxCm
			} else if prev > 0 {
				loa = prev + 1
			}
			if b.MaxCm != nil {
				prev = *b.MaxCm
			}
			beam := probeBeam(cat, loa)
			rows = append(rows, MatrixRow{Key: b.Code, Kind: RowKindBand, ProbeLOA: loa, ProbeBeam: beam})
			probes = append(probes, Input{
				Vessel:   &Vessel{LOA: loa, Beam: beam, HullType: DefaultHullType},
				Resource: Resource{Category: b.Category},
			})
		}
	case RowsCategories:
		for _, c := range spec.Categories {
			rows = append(rows, MatrixRow{Key: c.Code, Kind: RowKindCategory, ProbeLOA: c.LOA, ProbeBeam: c.Beam})
			probes = append(probes, categoryProbe(c))
		}
	default:
		return nil, nil, &InputError{Field: "rows", Code: "invalid_rows"}
	}
	return rows, probes, nil
}

func categoryProbe(c CoverageCategory) Input {
	in := Input{Resource: Resource{Category: c.Code, Length: c.BerthLength, Width: c.BerthWidth}}
	if c.LOA > 0 {
		in.Vessel = &Vessel{LOA: c.LOA, Beam: c.Beam, HullType: DefaultHullType}
	}
	return in
}

// matrixColumns builds the columns of a contract type (m2-plan §2.5).
func matrixColumns(cat Catalog, ct ContractType) []MatrixColumn {
	pl := cat.PriceList
	cols := []MatrixColumn{}
	switch referenceUnit(ct) {
	case UnitYear:
		return append(cols, MatrixColumn{Key: ColumnYear, Arrival: pl.ValidFrom, Departure: AddYears(pl.ValidFrom, 1)})
	case UnitSeason:
		if w := pl.Settings.SeasonWindow; w != nil {
			return append(cols, MatrixColumn{Key: ColumnSeason, Arrival: w.From, Departure: w.To.AddDays(1)})
		}
		return append(cols, MatrixColumn{Key: ColumnSeason, Arrival: pl.ValidFrom, Departure: pl.ValidTo.AddDays(1)})
	}
	month := referenceUnit(ct) == UnitMonth
	end := func(d calendar.Date) calendar.Date {
		if month {
			return AddMonths(d, 1)
		}
		return d.AddDays(1)
	}
	periods := slices.Clone(pl.Periods)
	slices.SortStableFunc(periods, func(a, b RatePeriod) int {
		if a.Kind != b.Kind {
			if a.Kind == PeriodSeason {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Code, b.Code)
	})
	for _, p := range periods {
		ranges := slices.Clone(p.Ranges)
		slices.SortStableFunc(ranges, func(a, b DateRange) int { return a.From.Compare(b.From) })
		for _, r := range ranges {
			if r.To.Before(r.From) || r.To.Before(pl.ValidFrom) || r.From.After(pl.ValidTo) {
				continue
			}
			first := dateMax(r.From, pl.ValidFrom)
			cols = append(cols, MatrixColumn{Key: p.Code, Arrival: first, Departure: end(first)})
			break
		}
	}
	if len(pl.Periods) == 0 {
		cols = append(cols, MatrixColumn{Key: ColumnAll, Arrival: pl.ValidFrom, Departure: end(pl.ValidFrom)})
	}
	return cols
}

// Coverage runs the matrix probes of every served contract type (category rows from spec, then band rows) and
// reports every combination that has no price (m2-plan §2.5).
func Coverage(cat Catalog, spec CoverageSpec) CoverageReport {
	rep := CoverageReport{Gaps: []Gap{}}
	for _, ct := range ContractTypes() {
		if !slices.Contains(cat.PriceList.ContractTypes, ct) {
			continue
		}
		item := cat.PriceList.Settings.PrimaryItems[ct]
		for _, rows := range []MatrixSpec{
			{ContractType: ct, Rows: RowsCategories, Categories: spec.Categories, QuotedOn: spec.QuotedOn},
			{ContractType: ct, Rows: RowsBands, QuotedOn: spec.QuotedOn},
		} {
			m, err := Matrix(cat, rows)
			if err != nil {
				continue
			}
			for _, c := range m.Cells {
				rep.Checked++
				if c.Breakdown != nil {
					continue
				}
				gap := Gap{ContractType: ct, ItemID: item, Reason: strings.TrimPrefix(c.Error, "no_price:")}
				if !strings.HasPrefix(c.Error, "no_price:") {
					continue
				}
				row, col := m.Rows[c.Row], m.Columns[c.Col]
				if row.Kind == RowKindBand {
					gap.Band = row.Key
					gap.Category = c.Probe.Resource.Category
				} else {
					gap.Category = row.Key
				}
				if col.Key != ColumnYear && col.Key != ColumnSeason && col.Key != ColumnAll {
					gap.Period = col.Key
				}
				rep.Gaps = append(rep.Gaps, gap)
			}
		}
	}
	return rep
}
