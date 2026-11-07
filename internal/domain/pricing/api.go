package pricing

import (
	"encoding/json"
	"slices"
)

// Entry points of the engine (m2-plan §2.2). Every function is pure: the same arguments give the same result.

// Quote prices in with the catalog cat (m2-plan §2.4).
func Quote(in Input, cat Catalog) (Breakdown, error) {
	return quote(in, cat, "")
}

// quote is Quote with a precomputed catalog hash ("" = compute it), so the tools hash a catalog once.
func quote(in Input, cat Catalog, hash string) (Breakdown, error) {
	e := newEngine(in, cat)
	item, err := e.preconditions()
	if err != nil {
		return Breakdown{}, err
	}
	if hash == "" {
		hash = catalogHash(cat)
	}
	bd := Breakdown{
		EngineVersion: EngineVersion, PriceListID: cat.PriceList.ID, CatalogHash: hash, ContractType: in.ContractType,
		Nights: e.nights, Warnings: []string{},
	}
	if e.stayLastNight().After(cat.PriceList.ValidTo) {
		bd.Warnings = append(bd.Warnings, WarningStayExceedsValidity)
		e.addTrace(TraceStayExceeds, -1, -1, "", map[string]string{"valid_to": cat.PriceList.ValidTo.String()})
	}
	if e.dims.ok {
		e.addTrace(TraceDimensions, -1, -1, "", map[string]string{
			"source": string(e.dims.source), "length_cm": itoa(int64(e.dims.l)), "width_cm": itoa(int64(e.dims.b)),
		})
	}

	// Berth line.
	blocks, err := e.berthBlocks(item)
	if err != nil {
		return Breakdown{}, err
	}
	berth := e.newLine(LineBerth, item, 1000)
	for bi, pb := range blocks {
		berth.Blocks = append(berth.Blocks, pb.b)
		e.traceBlock(0, bi, pb)
	}
	if err := sumBlocks(&berth); err != nil {
		return Breakdown{}, err
	}
	lines := []Line{berth}
	items := []*Item{item}

	// Included services of the winning rule of the first block, then added services in request order.
	inc, incItems, err := e.includedLines(blocks[0].up.rule)
	if err != nil {
		return Breakdown{}, err
	}
	for k := range inc {
		e.addTrace(TraceServiceIncluded, len(lines), -1, blocks[0].up.rule.ID, map[string]string{
			"item_id": inc[k].ItemID, "qty_milli": itoa(int64(inc[k].Qty)),
		})
		lines = append(lines, inc[k])
		items = append(items, incItems[k])
	}
	for _, req := range in.Services {
		l, it, pb, err := e.serviceLine(req)
		if err != nil {
			return Breakdown{}, err
		}
		li := len(lines)
		e.traceBlock(li, 0, pb)
		e.addTrace(TraceServiceAdded, li, -1, pb.b.RuleID, map[string]string{
			"item_id": it.ID, "qty_milli": itoa(int64(l.Qty)), "amount_cents": itoa(int64(l.Amount)),
		})
		lines = append(lines, l)
		items = append(items, it)
	}
	for i := range lines {
		lines[i].Seq = i + 1
		v, err := addCents(bd.Subtotal, lines[i].Amount)
		if err != nil {
			return Breakdown{}, err
		}
		bd.Subtotal = v
	}

	if bd.DiscountTotal, err = e.applyDiscounts(lines, items); err != nil {
		return Breakdown{}, err
	}
	bd.Lines = lines
	if err := e.finalize(&bd); err != nil {
		return Breakdown{}, err
	}
	bd.Trace = e.trace
	return bd, nil
}

// preconditions checks step 0 of m2-plan §2.4 and resolves the item and the dimensions.
func (e *engine) preconditions() (*Item, error) {
	in, pl := e.in, e.cat.PriceList
	if in.Arrival.IsZero() {
		return nil, &InputError{Field: "arrival", Code: InputDepartureBeforeArrival}
	}
	if in.Departure.IsZero() || !in.Departure.After(in.Arrival) {
		return nil, &InputError{Field: "departure", Code: InputDepartureBeforeArrival}
	}
	e.nights = in.Arrival.DaysUntil(in.Departure)
	if e.nights > MaxNights {
		return nil, &InputError{Field: "departure", Code: InputTooManyNights}
	}
	if in.ContractType == "" || !slices.Contains(pl.ContractTypes, in.ContractType) {
		return nil, &InputError{Field: "contract_type", Code: InputContractTypeNotServed}
	}
	if !slices.Contains(Channels(), e.channel) {
		return nil, &InputError{Field: "channel", Code: InputInvalidChannel}
	}
	if v := in.Vessel; v != nil && (v.LOA < 0 || v.Beam < 0 || v.LOA > MaxLengthCm*10 || v.Beam > MaxLengthCm*10) {
		return nil, &InputError{Field: "vessel", Code: InputDimensionsRequired}
	}
	if r := in.Resource; r.Length < 0 || r.Width < 0 || r.Length > MaxLengthCm*10 || r.Width > MaxLengthCm*10 {
		return nil, &InputError{Field: "resource", Code: InputDimensionsRequired}
	}
	source := "input"
	id := in.ItemID
	if id == "" {
		source = "primary"
		id = e.settings.PrimaryItems[in.ContractType]
		if id == "" {
			return nil, &NoPriceError{ContractType: in.ContractType, Night: in.Arrival, Category: in.Resource.Category, Reason: ReasonNoPrimaryItem}
		}
	}
	item, ok := e.items[id]
	if !ok || !item.Active {
		return nil, &InputError{Field: "item_id", Code: InputUnknownItem}
	}
	if in.Arrival.Before(pl.ValidFrom) || in.Arrival.After(pl.ValidTo) {
		return nil, &NoPriceError{ItemID: id, ContractType: in.ContractType, Night: in.Arrival, Category: in.Resource.Category, Reason: ReasonOutsideValidity}
	}
	e.dims = e.resolveDims()
	e.addTrace(TraceListSelected, -1, -1, "", map[string]string{"price_list_id": pl.ID})
	e.addTrace(TraceItemResolved, 0, -1, "", map[string]string{"item_id": id, "source": source})
	return item, nil
}

// traceBlock records how a block was priced.
func (e *engine) traceBlock(line, block int, pb pricedBlock) {
	b := pb.b
	e.addTrace(TracePeriod, line, block, "", map[string]string{"night": b.From.String(), "period": b.Period})
	if pb.up.band != "" {
		e.addTrace(TraceBand, line, block, "", map[string]string{"band": pb.up.band, "length_cm": itoa(int64(pb.up.bandLength))})
	}
	for _, r := range pb.up.rounded {
		e.addTrace(TraceLengthRounded, line, block, "", map[string]string{
			"from_cm": itoa(r[0]), "to_cm": itoa(r[1]), "step_cm": itoa(r[2]), "mode": e.settings.LengthRounding.Mode,
		})
	}
	if mb := pb.up.minBeam; mb != nil {
		e.addTrace(TraceMinBeam, line, block, "", map[string]string{"beam_cm": itoa(int64(mb[0])), "min_cm": itoa(int64(mb[1]))})
	}
	e.addTrace(TraceRuleSelected, line, block, b.RuleID, map[string]string{"unit": string(b.Unit), "candidates": itoa(int64(pb.up.candidates))})
	e.addTrace(TraceBlockPriced, line, block, b.RuleID, map[string]string{"unit": string(b.Unit), "base_cents": itoa(int64(b.Base))})
	for _, m := range b.Modifiers {
		e.addTrace(TraceModifierApplied, line, block, m.RuleID, map[string]string{"delta_cents": itoa(int64(m.Delta))})
	}
	for _, m := range pb.discarded {
		e.addTrace(TraceModifierDiscarded, line, block, m.RuleID, map[string]string{"delta_cents": itoa(int64(m.Delta))})
	}
}

// NewSnapshot quotes in and returns the reproducible snapshot (deep copies of input, catalog and breakdown).
func NewSnapshot(in Input, cat Catalog) (Snapshot, error) {
	b, err := Quote(in, cat)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{EngineVersion: EngineVersion, Input: in, Catalog: cat, Breakdown: b}
	// A JSON round trip gives copies that share nothing with the caller's slices and maps.
	raw, err := json.Marshal(s)
	if err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

// Recompute quotes a snapshot again: the result equals s.Breakdown (INV-20).
func Recompute(s Snapshot) (Breakdown, error) { return Quote(s.Input, s.Catalog) }

// CatalogHash is the sha256 (hex) of the canonical JSON of cat.
func CatalogHash(cat Catalog) string { return catalogHash(cat) }
