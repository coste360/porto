package pricelist

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
)

// ErrNoPublicMatrix means no active price list on the day publishes its transit matrix (settings.publish_matrix
// with contract type transit): the public prices section is hidden (M12, m12-plan §12.5).
var ErrNoPublicMatrix = errors.New("pricelist: no public transit matrix")

// PublicCell is one cell of the public matrix.
type PublicCell struct {
	Row, Col   int
	TotalCents *int64
	Unit       string
}

// PublicRow is a row (band or category) with its label in the requested language.
type PublicRow struct {
	Key, Label          string
	MaxLoaCm, MaxBeamCm *int64
}

// PublicColumn is a column (rate period, or year / season).
type PublicColumn struct {
	Key, Label         string
	Arrival, Departure calendar.Date
}

// PublicMatrix is the transit price matrix published on the profile page: the engine matrix of the active list
// flagged publish_matrix (INV-23), labels resolved in a language, no ids.
type PublicMatrix struct {
	PriceListID      string // for the ETag only, never published
	PriceListVersion int
	QuotedOn         calendar.Date
	Currency         string
	PriceIncludesVAT bool
	Rows             []PublicRow
	Columns          []PublicColumn
	Cells            []PublicCell
}

// columnLabels are the labels of the synthetic columns of pricing.Matrix.
var columnLabels = map[string][2]string{
	pricing.ColumnYear:   {"Anno", "Year"},
	pricing.ColumnSeason: {"Stagione", "Season"},
	pricing.ColumnAll:    {"Tariffa", "Rate"},
}

func pick(raw store.JSON, lang string) string {
	var t struct{ It, En string }
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
		return ""
	}
	if lang == "en" && t.En != "" {
		return t.En
	}
	return t.It
}

// PublicTransitList returns the active list of the marina on a day that publishes its transit matrix: valid on
// the day, serving transit, settings.publish_matrix set; the latest valid_from wins. ErrNoPublicMatrix when none.
func PublicTransitList(ctx context.Context, db bun.IDB, scope store.MarinaScope, on calendar.Date) (*store.PriceList, error) {
	lists, err := store.ActivePriceLists(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var best *store.PriceList
	for i := range lists {
		l := &lists[i]
		if l.ValidFrom.After(on) || (!l.ValidTo.IsZero() && l.ValidTo.Before(on)) {
			continue
		}
		if !slices.Contains([]string(l.ContractTypes), string(pricing.ContractTransit)) {
			continue
		}
		var set pricing.ListSettings
		if len(l.Settings) == 0 || json.Unmarshal(l.Settings, &set) != nil || !set.PublishMatrix {
			continue
		}
		if best == nil || !l.ValidFrom.Before(best.ValidFrom) {
			best = l
		}
	}
	if best == nil {
		return nil, ErrNoPublicMatrix
	}
	return best, nil
}

// PublicTransitMatrix returns the public transit matrix of the marina on a day, as system (no principal: the
// caller resolved the marina from a published profile). ErrNoPublicMatrix when no list publishes it. The cells
// are the engine matrix of getPriceMatrix with contract type transit, band rows (categories when the list has no
// bands) and the online channel; totals are gross (VAT included).
func (s *Service) PublicTransitMatrix(ctx context.Context, scope store.MarinaScope, lang string, on calendar.Date) (*PublicMatrix, error) {
	var out *PublicMatrix
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		l, err := PublicTransitList(ctx, tx, scope, on)
		if err != nil {
			return err
		}
		cat, err := LoadCatalog(ctx, tx, scope, l.ID)
		if err != nil {
			return err
		}
		mp := MatrixParams{ContractType: pricing.ContractTransit, Rows: pricing.RowsBands, Channel: pricing.ChannelOnline, QuotedOn: on}
		if len(cat.PriceList.Bands) == 0 {
			mp.Rows = pricing.RowsCategories
		}
		spec, err := s.matrixSpec(ctx, tx, scope, &cat, mp)
		if err != nil {
			return err
		}
		res, err := pricing.Matrix(cat, spec)
		if err != nil {
			return err
		}
		m := matrixView(l.ID, spec.QuotedOn, res)
		children, err := store.LoadPriceListChildren(ctx, tx, scope, l.ID)
		if err != nil {
			return err
		}
		cats, err := store.ListBerthCategories(ctx, tx, scope, true)
		if err != nil {
			return err
		}
		marina, err := store.GetMarina(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = publicMatrixOf(l, m, cat, children, cats, marina.Currency, lang)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func publicMatrixOf(l *store.PriceList, m Matrix, cat pricing.Catalog, children *store.PriceListChildren, cats []store.BerthCategory, currency, lang string) *PublicMatrix {
	catName := map[string]string{}
	for _, c := range cats {
		catName[c.Code] = pick(c.Name, lang)
	}
	bandOf := map[string]pricing.Band{}
	for _, b := range cat.PriceList.Bands {
		bandOf[b.Code] = b
	}
	periodName := map[string]string{}
	for _, p := range children.Periods {
		periodName[p.Code] = pick(p.Name, lang)
	}
	out := &PublicMatrix{
		PriceListID: l.ID, PriceListVersion: l.Version, QuotedOn: m.QuotedOn, Currency: currency, PriceIncludesVAT: true,
		Rows: []PublicRow{}, Columns: []PublicColumn{}, Cells: []PublicCell{},
	}
	for _, r := range m.Rows {
		row := PublicRow{Key: r.Key, Label: r.Key}
		if r.Kind == pricing.RowKindBand {
			b := bandOf[r.Key]
			if b.MaxCm != nil {
				v := int64(*b.MaxCm)
				row.MaxLoaCm = &v
			}
			if n := catName[b.Category]; b.Category != "" && n != "" {
				row.Label = n
			}
		} else {
			if n := catName[r.Key]; n != "" {
				row.Label = n
			}
			loa, beam := int64(r.ProbeLOA), int64(r.ProbeBeam)
			row.MaxLoaCm, row.MaxBeamCm = &loa, &beam
		}
		out.Rows = append(out.Rows, row)
	}
	for _, c := range m.Columns {
		col := PublicColumn{Key: c.Key, Label: periodName[c.Key], Arrival: c.Arrival, Departure: c.Departure}
		if l, ok := columnLabels[c.Key]; ok && col.Label == "" {
			col.Label = l[0]
			if lang == "en" {
				col.Label = l[1]
			}
		}
		if col.Label == "" {
			col.Label = c.Key
		}
		out.Columns = append(out.Columns, col)
	}
	for _, c := range m.Cells {
		cell := PublicCell{Row: c.Row, Col: c.Col}
		if c.TotalCents != nil {
			v := int64(*c.TotalCents)
			cell.TotalCents = &v
		}
		if c.Unit != nil {
			cell.Unit = string(*c.Unit)
		}
		out.Cells = append(out.Cells, cell)
	}
	return out
}
