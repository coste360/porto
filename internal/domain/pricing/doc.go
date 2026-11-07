// Package pricing is Porto's pure pricing engine (spec 12.3-12.6, m2-plan §2): given a catalog (one price
// list with its periods, bands, rules and the items they reference) and an explicit input (contract type,
// nights, vessel, resource, channel, customer category, promo code, services, today's date), it computes a
// reproducible breakdown with blocks, modifiers, services, discounts, total rounding, VAT split and deposit,
// plus an explanation trace.
//
// The package is pure (INV-21): no database, no clock, no randomness, no I/O. Every input is a parameter, and
// the same input always gives the same output byte for byte (INV-20, Snapshot and Recompute). Money is
// money.Cents, lengths geo.Cm, quantities qty.Milli, percentages integer basis points; every division rounds
// half-even through the shared helpers. The order of rules, items and periods in the catalog never matters.
//
// # Semantics (normative; the hand-derived corpus in testdata/pricing follows them)
//
// Notation: N = nights of the stay = Arrival.DaysUntil(Departure); night d is the date the night starts;
// half_even(a/b) is money.DivRoundHalfEven.
//
// 0. Preconditions, in this order: Departure > Arrival (departure_before_arrival); N <= 3660
// (too_many_nights); the contract type is served (contract_type_not_served); the channel ("" = desk) is
// valid (invalid_channel); negative or absurd dimensions are refused (dimensions_required); the item is
// Input.ItemID or the primary item of the contract type (none: NoPrice no_primary_item; unknown or inactive:
// unknown_item); the arrival is within [ValidFrom, ValidTo] (NoPrice outside_validity). The list valid on the
// arrival night prices the whole stay; a last night after ValidTo adds the warning stay_exceeds_validity.
//
// 1. Period of a night: the event containing it, else the season containing it, else none (""). Periods are
// scanned in code order, so even overlapping periods (a validation error) give a stable answer.
//
// 2. Base rule of an item, a unit and a night: active, stage base, same item and unit, every non-empty scope
// list contains the input value (an empty input value never matches a non-empty list; groups intersect; the
// period is the one of the night). Candidates are taken in winner order (higher priority, higher specificity =
// number of non-empty scope lists, higher seq, lower id) and the first that can price the input wins:
// length_band needs a band for the length and an amount for it (or amount_cents), per_berth needs
// amounts[category] (or amount_cents), per_meter/per_m2 need the dimensions of the dimension source,
// per_m2_berth needs berth length and width, flat needs amount_cents, the others unit_cents. When no candidate
// can price, the most specific failure is reported: missing dimensions (InputError dimensions_required, field
// vessel or resource), then no_band, then no_amount; with no candidate at all the reason is unit_missing when
// the item has no active base rule for that unit anywhere in the list, else no_rule.
//
// 3. Dimensions: vessel = LOA/beam, berth = resource length/width, max = the larger of each (vessel alone or
// berth alone when the other is missing). Length rounding (up = ceil to the step, half_even = nearest multiple,
// ties to the even multiple) applies to per_meter, per_m2 and per_m2_berth, never to band selection, which
// uses the raw length with inclusive upper bounds. per_m2: B' = max(B, half_even(Lr × num / den)) on the
// rounded length Lr, then B' is rounded too.
//
// 4. Unit amounts: per_berth amounts[category] else amount_cents; length_band amounts[band] else amount_cents;
// per_meter half_even(unit × Lr / 100); per_m2 half_even(unit × Lr × B'r / 10 000); per_m2_berth the same
// with the rounded berth dimensions; flat amount_cents; metered quantity × unit_cents (services).
//
// 5. Blocks of the berth line. Reference unit: annual year, seasonal and charter season, monthly and storage
// month, transit night. An exact stay of k reference units is k blocks of that unit (years and months counted
// from the arrival with AddYears/AddMonths, so Jan 31 + 2 months ends on Mar 31 while the first block ends on
// Feb 28; a season is exactly the season window; nights always match exactly unless the split rule is
// best_combination). Otherwise the split rule of the contract type (else "*", else pro_rata_daily):
//   - pro_rata_daily: the full units from the arrival, then one remainder block of r nights priced
//     half_even(U × r / D), U = unit amount of the winning rule at the remainder's first night, D = nights of the
//     unit that would start there (counted from the arrival; a season is the window, and a stay that does not
//     start on the window's first night has no full season);
//   - whole_months: calendar months from the arrival covering the stay (the last may end after the departure),
//     each priced by the month rule at its first night;
//   - best_combination: night, week (7 nights) and calendar-month blocks (month = AddMonths(start, 1)), only
//     units with a rule that prices at that night, chosen by dynamic programming over the night index: lower
//     cost (block amounts after modifiers), then fewer blocks, then the path whose first differing block has the
//     longer unit. Blocks may overshoot the departure. No path: NoPrice no_rule (or dimensions_required).
//
// Each block records from, to (exclusive), nights covered within the stay, the period of its first night, the
// winning rule and the base.
//
// 6. Modifiers, per block, on the block base: active, stage modifier, targeting the line's item (target ids,
// else target types, both empty = berth_fee), scope matching at the block's first night, every condition true
// (hull types; overhang = LOA − berth length >= min, false without either; min/max nights on N; lead days =
// QuotedOn.DaysUntil(Arrival), false without QuotedOn). Delta = half_even(base × bp / 10 000) or the fixed
// amount. Every stackable modifier applies; among the non-stackable ones only the lowest delta (most favourable
// to the customer; ties in winner order). Deltas are additive on the base; the block amount is
// max(0, base + Σ deltas). Block.Modifiers lists the stackable ones in winner order, then the non-stackable one.
//
// 7. Services. Included: the inclusions of the winning rule of the first block become zero-amount lines
// (kind included). Added: each request is a line priced by the item's winning base rule of unit stay or night
// at the arrival night: unit amount × quantity (metered already includes it), × N for unit night, then
// modifiers as in 6 on one block covering the stay. Unknown or inactive items are unknown_item.
//
// 8. Discounts, in groups customer → promo → automatic. A rule matches when active, scope (period of the
// arrival night) and conditions hold; customer rules need a non-empty customer category list; promo rules need
// promo_code = upper(Input.PromoCode). Eligible amount E = Σ amounts at the start of the group of the lines the
// rule targets (default berth_fee). Reduction = half_even(E × bp / 10 000) or min(amount, E). All stackable
// rules apply plus the non-stackable one with the largest reduction (ties in winner order). The rules then apply
// in winner order, each capped at what is left on its lines (trace discount.capped) and spread over them with
// money.Allocate weighted by the amounts left, so no line goes negative. Line.Discounts carry negative deltas;
// Breakdown.DiscountTotal is the positive sum. A promo code with no promo rule applying to an eligible amount
// is ErrPromoNotApplicable.
//
// 9-11. VAT and rounding. Gross₀ = amount (VAT included) or amount + half_even(amount × rate / 10 000). The
// total rounding of the contract type (else "*", else none) rounds T₀ = Σ Gross₀ to the nearest 100 (euro) or
// 500 (five_euro) cents, ties to even; the difference is allocated to the lines with Gross₀ > 0 by largest
// remainder. Every line is split with money.VATSplit, so net + VAT = gross per line, per bucket and in total
// (INV-25). VATSummary buckets lines with a non-zero gross by (rate, nature), sorted by rate desc then nature.
//
// 12. Deposit: Input.Deposit, else the list's policy for the contract type (else "*"): half_even(total × bp /
// 10 000) or min(amount, total), never above the total.
//
// 13. Lines are ordered: berth line, included lines (inclusion order), added services (request order). The
// catalog hash is the sha256 of the canonical JSON (rules by stage, seq, id; periods by code; items by code).
//
// # Tools
//
// Matrix builds one probe Input per row (bands: LOA = max_cm, open band = previous max_cm + 1 or 100 cm, beam =
// half_even(LOA × min beam ratio or 1/3), monohull_motor, the band's category; categories: the dimensions
// passed in) and column (transit: one night at the first night inside validity of each period, seasons then
// events by code; monthly and storage: one month from there; annual: the first year of validity; seasonal and
// charter: the season window; a list without periods has one column "all" from valid_from) and calls Quote:
// each cell is exactly Quote(cell.Probe) (INV-23). Coverage runs the same probes for every served contract type
// and reports the NoPrice cells as gaps. Renew copies a list shifting dates by whole years and adjusting base
// rule amounts (ISTAT, then the item, category or "all" adjustment, then amount rounding); Diff compares the
// matrices before and after.
package pricing
