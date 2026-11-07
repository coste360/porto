package pricelist

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Pricing tools (spec 12.5, 12.6): the matrix and the coverage check computed by the engine through probes, and
// the renewal (copy) of a list with its matrix diff. The service never computes a price itself.

// MatrixParams select a matrix; zero values take the defaults (first served contract type, primary item, band
// rows, desk channel, today in the marina time zone).
type MatrixParams struct {
	ContractType     pricing.ContractType
	ItemID           string
	Rows             string
	Channel          pricing.Channel
	CustomerCategory string
	QuotedOn         calendar.Date
}

// MatrixCell is a cell of a matrix with its probe in request form (PriceMatrixCell of the contract).
type MatrixCell struct {
	Row        int               `json:"row"`
	Col        int               `json:"col"`
	TotalCents *money.Cents      `json:"total_cents,omitempty"`
	NetCents   *money.Cents      `json:"net_cents,omitempty"`
	BaseCents  *money.Cents      `json:"base_cents,omitempty"`
	Unit       *pricing.TimeUnit `json:"unit,omitempty"`
	Error      string            `json:"error,omitempty"`
	Probe      QuoteRequest      `json:"probe"`
}

// Matrix is a price matrix (PriceMatrix of the contract).
type Matrix struct {
	PriceListID  string                 `json:"price_list_id"`
	ContractType pricing.ContractType   `json:"contract_type"`
	ItemID       string                 `json:"item_id"`
	QuotedOn     calendar.Date          `json:"quoted_on"`
	Rows         []pricing.MatrixRow    `json:"rows"`
	Columns      []pricing.MatrixColumn `json:"columns"`
	Cells        []MatrixCell           `json:"cells"`
	// Breakdowns are the engine results per cell (same order as Cells; nil for error cells). Not on the wire:
	// tests compare them with simulations of the probes.
	Breakdowns []*pricing.Breakdown `json:"-"`
}

func matrixView(listID string, quotedOn calendar.Date, m pricing.MatrixResult) Matrix {
	out := Matrix{
		PriceListID: listID, ContractType: m.ContractType, ItemID: m.ItemID, QuotedOn: quotedOn, Rows: m.Rows, Columns: m.Columns,
		Cells: make([]MatrixCell, 0, len(m.Cells)), Breakdowns: make([]*pricing.Breakdown, 0, len(m.Cells)),
	}
	if out.Rows == nil {
		out.Rows = []pricing.MatrixRow{}
	}
	if out.Columns == nil {
		out.Columns = []pricing.MatrixColumn{}
	}
	for _, c := range m.Cells {
		cell := MatrixCell{Row: c.Row, Col: c.Col, Error: c.Error, Probe: probeRequest(listID, c.Probe)}
		if b := c.Breakdown; b != nil {
			total, net := b.Total, b.Net
			cell.TotalCents, cell.NetCents = &total, &net
			if len(b.Lines) > 0 {
				base := b.Lines[0].Base
				cell.BaseCents = &base
				if len(b.Lines[0].Blocks) > 0 {
					u := b.Lines[0].Blocks[0].Unit
					cell.Unit = &u
				}
			}
		}
		out.Cells = append(out.Cells, cell)
		out.Breakdowns = append(out.Breakdowns, c.Breakdown)
	}
	return out
}

// coverageCategories returns the probe dimensions of the registered berth categories in use by active elements
// (largest elements of each category); categories without elements are skipped. withBandLinked false also skips
// the categories linked to a band of the list, whose band probe already covers them.
func coverageCategories(ctx context.Context, tx bun.IDB, scope store.MarinaScope, cat *pricing.Catalog, withBandLinked bool) ([]pricing.CoverageCategory, error) {
	codes, err := catalog.CategoryCodes(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	dims, err := store.PriceClassMaxDims(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	linked := map[string]bool{}
	for _, b := range cat.PriceList.Bands {
		if b.Category != "" {
			linked[b.Category] = true
		}
	}
	out := []pricing.CoverageCategory{}
	for _, d := range dims {
		if !slices.Contains(codes, d.Code) || (!withBandLinked && linked[d.Code]) {
			continue
		}
		out = append(out, pricing.CoverageCategory{
			Code: d.Code, LOA: geo.Cm(d.MaxLOACm), Beam: geo.Cm(d.MaxBeamCm), BerthLength: geo.Cm(d.MaxLengthCm), BerthWidth: geo.Cm(d.MaxWidthCm),
		})
	}
	return out, nil
}

// Matrix computes the price matrix of a list (pricing.read). Every cell is the engine result of its probe, and
// simulating the probe gives the same breakdown (INV-23).
func (s *Service) Matrix(ctx context.Context, p *auth.Principal, marinaID, id string, mp MatrixParams) (*Matrix, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if mp.ContractType != "" && !slices.Contains(pricing.ContractTypes(), mp.ContractType) {
		v.Add("contract_type", "invalid")
	}
	if mp.Rows != "" && mp.Rows != pricing.RowsBands && mp.Rows != pricing.RowsCategories {
		v.Add("rows", "invalid")
	}
	if mp.Channel != "" && !slices.Contains(pricing.Channels(), mp.Channel) {
		v.Add("channel", "invalid")
	}
	if mp.CustomerCategory != "" && !slices.Contains(customerCategories, mp.CustomerCategory) {
		v.Add("customer_category", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out Matrix
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cat, err := LoadCatalog(ctx, tx, scope, id, mp.ItemID)
		if err != nil {
			return err
		}
		if mp.ItemID != "" && !slices.ContainsFunc(cat.Items, func(it pricing.Item) bool { return it.ID == mp.ItemID }) {
			v := &auth.ValidationError{}
			v.Add("item_id", "unknown")
			return v
		}
		spec, err := s.matrixSpec(ctx, tx, scope, &cat, mp)
		if err != nil {
			return err
		}
		m, err := pricing.Matrix(cat, spec)
		if err != nil {
			return err
		}
		out = matrixView(id, spec.QuotedOn, m)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) matrixSpec(ctx context.Context, tx bun.IDB, scope store.MarinaScope, cat *pricing.Catalog, mp MatrixParams) (pricing.MatrixSpec, error) {
	spec := pricing.MatrixSpec{
		ContractType: mp.ContractType, ItemID: mp.ItemID, Rows: mp.Rows, Channel: mp.Channel,
		CustomerCategory: mp.CustomerCategory, QuotedOn: mp.QuotedOn,
	}
	if spec.ContractType == "" && len(cat.PriceList.ContractTypes) > 0 {
		spec.ContractType = cat.PriceList.ContractTypes[0]
	}
	if spec.Rows == "" {
		spec.Rows = pricing.RowsBands
	}
	if spec.Channel == "" {
		spec.Channel = pricing.ChannelDesk
	}
	if spec.QuotedOn.IsZero() {
		today, err := s.today(ctx, tx, scope)
		if err != nil {
			return spec, err
		}
		spec.QuotedOn = today
	}
	if spec.Rows == pricing.RowsCategories {
		cats, err := coverageCategories(ctx, tx, scope, cat, true)
		if err != nil {
			return spec, err
		}
		spec.Categories = cats
	}
	return spec, nil
}

// ElementCounts counts elements per resource type.
type ElementCounts struct {
	Berths        int `json:"berths"`
	DynamicAreas  int `json:"dynamic_areas"`
	LandResources int `json:"land_resources"`
}

// Coverage is the result of a coverage check (PriceCoverage of the contract).
type Coverage struct {
	Gaps                    []pricing.Gap       `json:"gaps"`
	ElementsWithoutCategory ElementCounts       `json:"elements_without_category"`
	UnknownCategories       []catalog.CodeCount `json:"unknown_categories"`
	Checked                 int                 `json:"checked"`
}

// Coverage lists the combinations of category/band × period × contract type without a price, the elements
// without a price_class and the codes in use that are not categories (pricing.read).
func (s *Service) Coverage(ctx context.Context, p *auth.Principal, marinaID, id string, quotedOn calendar.Date) (*Coverage, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	out := &Coverage{Gaps: []pricing.Gap{}, UnknownCategories: []catalog.CodeCount{}}
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cat, err := LoadCatalog(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if quotedOn.IsZero() {
			if quotedOn, err = s.today(ctx, tx, scope); err != nil {
				return err
			}
		}
		cats, err := coverageCategories(ctx, tx, scope, &cat, false)
		if err != nil {
			return err
		}
		rep := pricing.Coverage(cat, pricing.CoverageSpec{Categories: cats, QuotedOn: quotedOn})
		if rep.Gaps != nil {
			out.Gaps = rep.Gaps
		}
		out.Checked = rep.Checked
		without, err := store.ElementsWithoutPriceClass(ctx, tx, scope)
		if err != nil {
			return err
		}
		out.ElementsWithoutCategory = ElementCounts{Berths: without["berth"], DynamicAreas: without["dynamic_area"], LandResources: without["land_resource"]}
		counts, err := store.PriceClassCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		all, err := store.ListBerthCategories(ctx, tx, scope, false)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, c := range all {
			known[c.Code] = true
		}
		sum := map[string]int{}
		for _, m := range counts {
			for code, n := range m {
				if !known[code] {
					sum[code] += n
				}
			}
		}
		for code, n := range sum {
			out.UnknownCategories = append(out.UnknownCategories, catalog.CodeCount{Code: code, Count: n})
		}
		slices.SortFunc(out.UnknownCategories, func(a, b catalog.CodeCount) int { return strings.Compare(a.Code, b.Code) })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- renewal ----

// RenewalMatrix selects the matrices of a renewal preview.
type RenewalMatrix struct {
	ContractTypes    []pricing.ContractType `json:"contract_types,omitempty"`
	Channel          pricing.Channel        `json:"channel,omitempty"`
	CustomerCategory string                 `json:"customer_category,omitempty"`
}

// RenewalRequest asks for a new draft from a list (spec 12.6).
type RenewalRequest struct {
	DryRun         bool                   `json:"dry_run"`
	Code           string                 `json:"code"`
	Name           catalog.I18nText       `json:"name"`
	ShiftYears     int                    `json:"shift_years"`
	ISTATBP        int64                  `json:"istat_bp,omitempty"`
	Adjustments    []pricing.Adjustment   `json:"adjustments,omitempty"`
	AmountRounding pricing.AmountRounding `json:"amount_rounding"`
	ValidFrom      *calendar.Date         `json:"valid_from,omitempty"`
	ValidTo        *calendar.Date         `json:"valid_to,omitempty"`
	Matrix         *RenewalMatrix         `json:"matrix,omitempty"`
}

// RenewalDiff compares the matrix of one contract type before and after.
type RenewalDiff struct {
	ContractType pricing.ContractType `json:"contract_type"`
	Before       Matrix               `json:"before"`
	After        Matrix               `json:"after"`
	Cells        []pricing.DiffCell   `json:"cells"`
}

// RenewalSummary counts the rules and the base amounts that changed.
type RenewalSummary struct {
	Rules          int `json:"rules"`
	AmountsChanged int `json:"amounts_changed"`
}

// RenewalPreview is the dry run of a renewal.
type RenewalPreview struct {
	Document SaveRequest    `json:"document"`
	Diffs    []RenewalDiff  `json:"diffs"`
	Summary  RenewalSummary `json:"summary"`
}

func checkRenewal(req *RenewalRequest) error {
	c := checker{&auth.ValidationError{}}
	req.Code = strings.TrimSpace(req.Code)
	c.code("code", req.Code)
	c.i18n("name", req.Name)
	if req.ShiftYears < 0 || req.ShiftYears > 5 {
		c.add("shift_years", "out_of_range")
	}
	if req.ISTATBP < 0 || req.ISTATBP > 10000 {
		c.add("istat_bp", "out_of_range")
	}
	switch req.AmountRounding {
	case pricing.AmountRoundNone, pricing.AmountRoundTenCents, pricing.AmountRoundEuro:
	default:
		c.add("amount_rounding", "invalid")
	}
	if len(req.Adjustments) > 200 {
		c.add("adjustments", "too_many")
	}
	for i, a := range req.Adjustments {
		f := fmt.Sprintf("adjustments[%d]", i)
		switch a.Scope {
		case pricing.AdjustAll:
		case pricing.AdjustItem:
			if a.ItemID == "" {
				c.add(f+".item_id", "required")
			}
		case pricing.AdjustCategory:
			if a.Category == "" {
				c.add(f+".category", "required")
			}
		default:
			c.add(f+".scope", "invalid")
		}
		if (a.PercentBP == nil) == (a.AmountCents == nil) {
			c.add(f, "one_of_percent_or_amount")
		}
	}
	if req.Matrix != nil {
		c.contractTypes("matrix.contract_types", req.Matrix.ContractTypes, false)
	}
	return c.v.Err()
}

// renewedDocument is the unsaved document of a renewed catalog: names and colours of periods come from the
// source by code; new rows carry keys.
func renewedDocument(req *RenewalRequest, src *Document, cat pricing.Catalog) SaveRequest {
	doc := SaveRequest{
		Code: req.Code, Name: req.Name, ValidFrom: cat.PriceList.ValidFrom, ValidTo: cat.PriceList.ValidTo,
		ContractTypes: slices.Clone(cat.PriceList.ContractTypes), Settings: cat.PriceList.Settings,
		Periods: []Period{}, Bands: []Band{}, Rules: []Rule{},
	}
	byCode := map[string]Period{}
	for _, p := range src.Periods {
		byCode[p.Code] = p
	}
	for i, p := range cat.PriceList.Periods {
		sp := byCode[p.Code]
		doc.Periods = append(doc.Periods, Period{Key: fmt.Sprintf("p%d", i+1), Code: p.Code, Name: sp.Name, Kind: p.Kind, Color: sp.Color, Ranges: p.Ranges})
	}
	for i, b := range cat.PriceList.Bands {
		doc.Bands = append(doc.Bands, Band{Key: fmt.Sprintf("b%d", i+1), Code: b.Code, MaxCm: b.MaxCm, Category: b.Category})
	}
	for i, r := range cat.Rules {
		r.ID = ""
		a := r.Active
		doc.Rules = append(doc.Rules, Rule{Rule: r, Key: fmt.Sprintf("r%d", i+1), Active: &a})
	}
	return doc
}

// amountsChanged counts the base-rule amounts (amount, unit price, each band/category amount) that differ
// between rules of the same seq.
func amountsChanged(before, after []pricing.Rule) int {
	old := map[int64]pricing.Rule{}
	for _, r := range before {
		old[r.Seq] = r
	}
	n := 0
	diff := func(a, b *money.Cents) {
		if (a == nil) != (b == nil) || (a != nil && *a != *b) {
			n++
		}
	}
	for _, r := range after {
		if r.Stage != pricing.StageBase {
			continue
		}
		o := old[r.Seq]
		diff(o.AmountCents, r.AmountCents)
		diff(o.UnitCents, r.UnitCents)
		for k, v := range r.Amounts {
			if ov, ok := o.Amounts[k]; !ok || ov != v {
				n++
			}
		}
	}
	return n
}

// Renew creates a new draft from a list (pricing.configure): dates shifted, ISTAT and adjustments applied to
// base amounts, amounts rounded. With DryRun nothing is written and the preview with the matrix diff of every
// served contract type (or those asked) is returned instead.
func (s *Service) Renew(ctx context.Context, p *auth.Principal, marinaID, id string, req RenewalRequest) (*RenewalPreview, *Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, nil, err
	}
	if err := checkRenewal(&req); err != nil {
		return nil, nil, err
	}
	spec := pricing.RenewalSpec{
		ShiftYears: req.ShiftYears, ISTATBP: req.ISTATBP, Adjustments: req.Adjustments, AmountRounding: req.AmountRounding,
		Code: req.Code, ValidFrom: req.ValidFrom, ValidTo: req.ValidTo,
	}
	if req.DryRun {
		var out RenewalPreview
		err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			src, before, after, err := s.renewed(ctx, tx, scope, id, spec)
			if err != nil {
				return err
			}
			out = RenewalPreview{
				Document: renewedDocument(&req, src, after), Diffs: []RenewalDiff{},
				Summary: RenewalSummary{Rules: len(after.Rules), AmountsChanged: amountsChanged(before.Rules, after.Rules)},
			}
			today, err := s.today(ctx, tx, scope)
			if err != nil {
				return err
			}
			cts := after.PriceList.ContractTypes
			ms := pricing.MatrixSpec{Channel: pricing.ChannelDesk, Rows: pricing.RowsBands, QuotedOn: today}
			if req.Matrix != nil {
				if len(req.Matrix.ContractTypes) > 0 {
					cts = req.Matrix.ContractTypes
				}
				if req.Matrix.Channel != "" {
					ms.Channel = req.Matrix.Channel
				}
				ms.CustomerCategory = req.Matrix.CustomerCategory
			}
			for _, ct := range cts {
				if !slices.Contains(before.PriceList.ContractTypes, ct) || !slices.Contains(after.PriceList.ContractTypes, ct) {
					continue
				}
				ms.ContractType = ct
				d, err := pricing.Diff(before, after, ms)
				if err != nil {
					return err
				}
				cells := d.Cells
				if cells == nil {
					cells = []pricing.DiffCell{}
				}
				out.Diffs = append(out.Diffs, RenewalDiff{
					ContractType: ct, Before: matrixView(id, today, d.Before), After: matrixView("", today, d.After), Cells: cells,
				})
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		return &out, nil, nil
	}
	var doc *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		src, _, after, err := s.renewed(ctx, tx, scope, id, spec)
		if err != nil {
			return err
		}
		save := renewedDocument(&req, src, after)
		now := s.now()
		var next int64 = 1
		for _, r := range after.Rules {
			next = max(next, r.Seq+1)
		}
		l := &store.PriceList{
			ID: store.NewID(), Code: req.Code, Name: store.MustJSON(req.Name), Status: store.PriceListDraft,
			ValidFrom: save.ValidFrom, ValidTo: save.ValidTo, ContractTypes: stringsOf(save.ContractTypes),
			Settings: store.MustJSON(save.Settings), SourcePriceListID: id, NextRuleSeq: next, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertPriceList(ctx, tx, scope, l); err != nil {
			return err
		}
		rows, _ := buildChildren(&save, &store.PriceListChildren{}, &next, now)
		// buildChildren assigns new seqs to new rules; a renewal keeps the source recency.
		for i := range rows.Rules {
			rows.Rules[i].Seq = after.Rules[i].Seq
			e := after.Rules[i]
			e.ID = rows.Rules[i].ID
			rows.Rules[i].Body = ruleBody(e)
		}
		if err := store.InsertPriceListChildren(ctx, tx, scope, l.ID, rows); err != nil {
			return mapListErr(err)
		}
		if doc, err = loadDocument(ctx, tx, scope, l.ID); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, "price_list.renew", l.ID, map[string]any{
			"source_id": id, "shift_years": req.ShiftYears, "istat_bp": req.ISTATBP, "adjustments": len(req.Adjustments),
			"rules": len(rows.Rules),
		}, event.PricingChangedData{PriceListID: l.ID, Version: l.Version, Change: event.ChangeRenewed})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, doc, nil
}

// renewed loads the source list and runs the engine renewal on it.
func (s *Service) renewed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, spec pricing.RenewalSpec) (*Document, pricing.Catalog, pricing.Catalog, error) {
	src, err := loadDocument(ctx, tx, scope, id)
	if err != nil {
		return nil, pricing.Catalog{}, pricing.Catalog{}, err
	}
	before, err := LoadCatalog(ctx, tx, scope, id)
	if err != nil {
		return nil, pricing.Catalog{}, pricing.Catalog{}, err
	}
	after, err := pricing.Renew(before, spec)
	if err != nil {
		if errors.Is(err, pricing.ErrInvalidCatalog) {
			return nil, pricing.Catalog{}, pricing.Catalog{}, &InvalidError{Issues: errorsOnly(src.Issues)}
		}
		return nil, pricing.Catalog{}, pricing.Catalog{}, err
	}
	return src, before, after, nil
}
