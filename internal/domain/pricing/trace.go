package pricing

// Trace step codes (TraceStep.Code). The web shows them with the i18n keys pricingEnums:trace.<code>, so a code
// never changes once released and every new code gets its it/en labels in the same change.
const (
	TraceListSelected      = "list.selected"       // params: price_list_id
	TraceItemResolved      = "item.resolved"       // params: item_id, source (input|primary)
	TraceDimensions        = "dimensions.resolved" // params: source (vessel|berth|max), length_cm, width_cm
	TraceLengthRounded     = "dimensions.rounded"  // params: from_cm, to_cm, step_cm, mode
	TraceMinBeam           = "dimensions.min_beam" // params: beam_cm, min_cm
	TracePeriod            = "period.selected"     // params: night, period
	TraceBand              = "band.selected"       // params: band, length_cm
	TraceRuleSelected      = "rule.selected"       // params: unit, candidates
	TraceSplitExact        = "split.exact"         // params: unit, count
	TraceSplitProRata      = "split.pro_rata"      // params: unit, nights, unit_nights, unit_cents
	TraceSplitWholeMonths  = "split.whole_months"  // params: months
	TraceSplitBest         = "split.best"          // params: blocks
	TraceBlockPriced       = "block.priced"        // params: unit, base_cents
	TraceModifierApplied   = "modifier.applied"    // params: delta_cents, stackable
	TraceModifierDiscarded = "modifier.discarded"  // params: delta_cents (non-stackable not chosen)
	TraceServiceIncluded   = "service.included"    // params: item_id, qty_milli
	TraceServiceAdded      = "service.added"       // params: item_id, qty_milli, amount_cents
	TraceDiscountApplied   = "discount.applied"    // params: kind, reduction_cents
	TraceDiscountCapped    = "discount.capped"     // params: kind, cap_cents
	TraceDiscountDiscarded = "discount.discarded"  // params: kind, reduction_cents
	TraceVATLine           = "vat.line"            // params: vat_rate_bp, gross_cents
	TraceRoundingTotal     = "rounding.total"      // params: mode, from_cents, to_cents
	TraceDeposit           = "deposit.computed"    // params: deposit_cents
	TraceStayExceeds       = "warning.stay_exceeds_validity"
)

// TraceCodes returns every trace code (the i18n catalogue test checks each has a label).
func TraceCodes() []string {
	return []string{
		TraceListSelected, TraceItemResolved, TraceDimensions, TraceLengthRounded, TraceMinBeam, TracePeriod,
		TraceBand, TraceRuleSelected, TraceSplitExact, TraceSplitProRata, TraceSplitWholeMonths, TraceSplitBest,
		TraceBlockPriced, TraceModifierApplied, TraceModifierDiscarded, TraceServiceIncluded, TraceServiceAdded,
		TraceDiscountApplied, TraceDiscountCapped, TraceDiscountDiscarded, TraceVATLine, TraceRoundingTotal,
		TraceDeposit, TraceStayExceeds,
	}
}

// Warning codes (Breakdown.Warnings).
const (
	WarningStayExceedsValidity = "stay_exceeds_validity"
)
