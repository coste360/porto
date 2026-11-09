package pricing

import (
	"slices"
	"sort"
	"strings"

	"github.com/coste360/porto/internal/money"
)

// Validate lists the issues of a catalog (m2-plan §4.8). Errors block activation; warnings are shown in the
// editor. The order follows the catalog: list and settings, periods, bands, rules (in input order).
// category_unknown needs the marina's berth categories and is reported by ValidateCategories.
func Validate(cat Catalog) []Issue {
	v := validator{cat: cat, items: map[string]*Item{}}
	for i := range cat.Items {
		v.items[cat.Items[i].ID] = &cat.Items[i]
	}
	v.list()
	v.periods()
	v.bands()
	for i := range cat.Rules {
		v.rule(&cat.Rules[i])
	}
	v.coverage()
	v.duplicates()
	if v.out == nil {
		return []Issue{}
	}
	return v.out
}

// ValidateCategories reports the category_unknown warnings: rule scopes, per_berth amounts and band links that
// name a category not in known (the marina's berth categories).
func ValidateCategories(cat Catalog, known []string) []Issue {
	out := []Issue{}
	seen := map[string]bool{}
	add := func(kind, id, field, c string) {
		k := kind + "\x00" + id + "\x00" + c
		if c == "" || slices.Contains(known, c) || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, Issue{Code: IssueCategoryUnknown, Severity: SeverityWarning, Ref: IssueRef{Kind: kind, ID: id, Field: field}, Params: map[string]string{"category": c}})
	}
	for _, b := range cat.PriceList.Bands {
		add(RefBand, b.Code, "category", b.Category)
	}
	for _, r := range cat.Rules {
		for _, c := range r.Scope.Categories {
			add(RefRule, r.ID, "scope.categories", c)
		}
		if r.Stage == StageBase && r.Basis == BasisPerBerth {
			for _, c := range sortedKeys(r.Amounts) {
				add(RefRule, r.ID, "amounts", c)
			}
		}
	}
	return out
}

type validator struct {
	cat   Catalog
	items map[string]*Item
	out   []Issue
}

func (v *validator) add(code, sev, kind, id, field string, params map[string]string) {
	v.out = append(v.out, Issue{Code: code, Severity: sev, Ref: IssueRef{Kind: kind, ID: id, Field: field}, Params: params})
}

func (v *validator) err(code, kind, id, field string, params map[string]string) {
	v.add(code, SeverityError, kind, id, field, params)
}

func (v *validator) warn(code, kind, id, field string, params map[string]string) {
	v.add(code, SeverityWarning, kind, id, field, params)
}

func knownContractType(ct ContractType) bool { return slices.Contains(ContractTypes(), ct) }

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedCTKeys[V any](m map[ContractType]V) []ContractType {
	ks := make([]ContractType, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func amountOK(c money.Cents) bool { return c >= 0 && c <= MaxAmountCents }

func (v *validator) list() {
	pl := v.cat.PriceList
	s := pl.Settings
	if pl.ValidTo.Before(pl.ValidFrom) {
		v.err(IssueRangeInvalid, RefList, pl.ID, "valid_to", nil)
	}
	for _, ct := range pl.ContractTypes {
		if !knownContractType(ct) {
			v.err(IssueContractTypeUnknown, RefList, pl.ID, "contract_types", map[string]string{"contract_type": string(ct)})
		}
	}
	checkKeys := func(field string, keys []ContractType) {
		for _, ct := range keys {
			if ct != AnyContractType && !knownContractType(ct) {
				v.err(IssueContractTypeUnknown, RefSettings, "", field, map[string]string{"contract_type": string(ct)})
			}
		}
	}
	checkKeys("split", sortedCTKeys(s.Split))
	checkKeys("rounding", sortedCTKeys(s.Rounding))
	checkKeys("deposits", sortedCTKeys(s.Deposits))
	checkKeys("primary_items", sortedCTKeys(s.PrimaryItems))
	if s.DimensionSource != "" && !slices.Contains([]DimensionSource{DimensionVessel, DimensionBerth, DimensionMax}, s.DimensionSource) {
		v.err(IssueAmountOutOfRange, RefSettings, "", "dimension_source", nil)
	}
	lr := s.LengthRounding
	unset := lr.StepCm == 0 && lr.Mode == ""
	badStep := !slices.Contains([]int64{1, 10, 50, 100}, int64(lr.StepCm))
	badMode := lr.Mode != LengthRoundUp && lr.Mode != LengthRoundHalfEven
	if !unset && (badStep || badMode) {
		v.err(IssueAmountOutOfRange, RefSettings, "", "length_rounding", nil)
	}
	if r := s.MinBeamRatio; r != nil && (r.Den <= 0 || r.Num < 0 || r.Num > r.Den*10) {
		v.err(IssueAmountOutOfRange, RefSettings, "", "min_beam_ratio", nil)
	}
	for _, ct := range sortedCTKeys(s.Split) {
		if !slices.Contains([]SplitRule{SplitProRataDaily, SplitWholeMonths, SplitBestCombination}, s.Split[ct]) {
			v.err(IssueAmountOutOfRange, RefSettings, "", "split", map[string]string{"contract_type": string(ct)})
		}
	}
	for _, ct := range sortedCTKeys(s.Rounding) {
		if roundingStep(s.Rounding[ct]) == 0 && s.Rounding[ct] != RoundNone {
			v.err(IssueAmountOutOfRange, RefSettings, "", "rounding", map[string]string{"contract_type": string(ct)})
		}
	}
	for _, ct := range sortedCTKeys(s.Deposits) {
		d := s.Deposits[ct]
		if (d.PercentBP != nil && d.AmountCents != nil) || (d.PercentBP != nil && (*d.PercentBP < 0 || *d.PercentBP > 10_000)) || (d.AmountCents != nil && !amountOK(*d.AmountCents)) {
			v.err(IssueAmountOutOfRange, RefSettings, "", "deposits", map[string]string{"contract_type": string(ct)})
		}
	}
	if w := s.SeasonWindow; w != nil && w.To.Before(w.From) {
		v.err(IssueRangeInvalid, RefSettings, "", "season_window", nil)
	}
	if slices.Contains(pl.ContractTypes, ContractSeasonal) && s.SeasonWindow == nil {
		v.err(IssueSeasonalWithoutWindow, RefSettings, "", "season_window", nil)
	}
	for _, ct := range pl.ContractTypes {
		id := s.PrimaryItems[ct]
		if it, ok := v.items[id]; id == "" || !ok {
			v.err(IssuePrimaryItemMissing, RefSettings, "", "primary_items", map[string]string{"contract_type": string(ct)})
		} else if !it.Active {
			v.err(IssueRuleItemInactive, RefSettings, "", "primary_items", map[string]string{"contract_type": string(ct), "item_id": id})
		}
	}
	if len(v.cat.Rules) > MaxRules {
		v.err(IssueAmountOutOfRange, RefList, pl.ID, "rules", map[string]string{"max": itoa(MaxRules)})
	}
	if len(pl.Periods) > MaxPeriods {
		v.err(IssueAmountOutOfRange, RefList, pl.ID, "periods", map[string]string{"max": itoa(MaxPeriods)})
	}
	if len(pl.Bands) > MaxBands {
		v.err(IssueAmountOutOfRange, RefList, pl.ID, "bands", map[string]string{"max": itoa(MaxBands)})
	}
}

func (v *validator) periods() {
	pl := v.cat.PriceList
	type span struct {
		code string
		r    DateRange
	}
	var seasons, events []span
	for _, p := range pl.Periods {
		if p.Kind != PeriodSeason && p.Kind != PeriodEvent {
			v.err(IssueRangeInvalid, RefPeriod, p.Code, "kind", nil)
		}
		for _, r := range p.Ranges {
			if r.To.Before(r.From) {
				v.err(IssueRangeInvalid, RefPeriod, p.Code, "ranges", map[string]string{"from": r.From.String(), "to": r.To.String()})
				continue
			}
			if r.From.Before(pl.ValidFrom) || r.To.After(pl.ValidTo) {
				v.warn(IssuePeriodOutsideValidity, RefPeriod, p.Code, "ranges", map[string]string{"from": r.From.String(), "to": r.To.String()})
			}
			if p.Kind == PeriodEvent {
				events = append(events, span{p.Code, r})
			} else {
				seasons = append(seasons, span{p.Code, r})
			}
		}
	}
	for _, group := range [][]span{seasons, events} {
		reported := map[string]bool{}
		for i := range group {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if !a.r.To.Before(b.r.From) && !b.r.To.Before(a.r.From) {
					key := a.code + "|" + b.code
					if reported[key] {
						continue
					}
					reported[key] = true
					v.err(IssuePeriodOverlap, RefPeriod, b.code, "ranges", map[string]string{"other": a.code})
				}
			}
		}
	}
	if len(pl.Periods) > 0 && !pl.ValidTo.Before(pl.ValidFrom) {
		ix := newPeriodIndex(pl.Periods)
		for d, n := pl.ValidFrom, 0; !d.After(pl.ValidTo) && n <= MaxNights; d, n = d.AddDays(1), n+1 {
			if ix.of(d) == "" {
				v.warn(IssueValidityNotCovered, RefList, pl.ID, "periods", map[string]string{"night": d.String()})
				break
			}
		}
	}
}

func (v *validator) bands() {
	var prev *Band
	bands := v.cat.PriceList.Bands
	for i := range bands {
		b := &bands[i]
		if b.MaxCm == nil {
			if i != len(bands)-1 {
				v.err(IssueBandOpenNotLast, RefBand, b.Code, "max_cm", nil)
			}
			continue
		}
		if *b.MaxCm <= 0 || *b.MaxCm > MaxLengthCm {
			v.err(IssueAmountOutOfRange, RefBand, b.Code, "max_cm", nil)
		}
		if prev != nil && *b.MaxCm <= *prev.MaxCm {
			v.err(IssueBandOrder, RefBand, b.Code, "max_cm", nil)
		}
		prev = b
	}
}

func (v *validator) periodKnown(code string) bool {
	for _, p := range v.cat.PriceList.Periods {
		if p.Code == code {
			return true
		}
	}
	return false
}

func (v *validator) rule(r *Rule) {
	id := r.ID
	if r.Priority < -MaxPriority || r.Priority > MaxPriority {
		v.err(IssueAmountOutOfRange, RefRule, id, "priority", nil)
	}
	for _, ct := range r.Scope.ContractTypes {
		if !knownContractType(ct) {
			v.err(IssueContractTypeUnknown, RefRule, id, "scope.contract_types", map[string]string{"contract_type": string(ct)})
		}
	}
	for _, p := range r.Scope.Periods {
		if !v.periodKnown(p) {
			v.err(IssuePeriodCodeUnknown, RefRule, id, "scope.periods", map[string]string{"period": p})
		}
	}
	switch r.Stage {
	case StageBase:
		v.baseRule(r)
	case StageModifier, StageDiscount:
		v.effectRule(r)
	default:
		v.err(IssueRuleBasisInvalid, RefRule, id, "stage", nil)
	}
}

func (v *validator) baseRule(r *Rule) {
	id := r.ID
	item, ok := v.items[r.ItemID]
	if !ok {
		v.err(IssueRuleItemUnknown, RefRule, id, "item_id", map[string]string{"item_id": r.ItemID})
	} else if r.Active && !item.Active {
		v.err(IssueRuleItemInactive, RefRule, id, "item_id", map[string]string{"item_id": r.ItemID})
	}
	switch {
	case slices.Contains(RentalTimeUnits(), r.Unit) && r.Unit != UnitWeek:
		v.rentalRule(r, item, ok)
	case !slices.Contains(TimeUnits(), r.Unit):
		v.err(IssueRuleUnitInvalid, RefRule, id, "unit", nil)
	default:
		if ok && r.Unit == UnitStay && item.Type == ItemBerthFee {
			v.err(IssueRuleUnitInvalid, RefRule, id, "unit", map[string]string{"unit": string(r.Unit)})
		}
		if r.Unit == UnitSeason && v.cat.PriceList.Settings.SeasonWindow == nil {
			v.err(IssueRuleUnitInvalid, RefRule, id, "unit", map[string]string{"unit": string(r.Unit)})
		}
	}
	bad := func(field string) {
		v.err(IssueRuleBasisInvalid, RefRule, id, field, map[string]string{"basis": string(r.Basis)})
	}
	if r.Unit == UnitWeek && ok && isRentalItem(item) && r.Basis != BasisFlat {
		bad("basis")
	}
	if len(r.Scope.TimeBands) > 0 && r.Unit != UnitHalfDay {
		v.err(IssueRuleUnitInvalid, RefRule, id, "scope.time_bands", map[string]string{"unit": string(r.Unit)})
	}
	switch r.Basis {
	case BasisFlat:
		if r.AmountCents == nil {
			bad("amount_cents")
		}
	case BasisPerBerth, BasisLengthBand:
		if r.AmountCents == nil && len(r.Amounts) == 0 {
			bad("amounts")
		}
	case BasisPerMeter, BasisPerM2, BasisPerM2Berth:
		if r.UnitCents == nil {
			bad("unit_cents")
		}
	case BasisMetered:
		if r.UnitCents == nil {
			bad("unit_cents")
		}
		if ok && item.Type == ItemBerthFee {
			bad("basis")
		}
	default:
		bad("basis")
	}
	if r.Basis == BasisLengthBand {
		for _, k := range sortedKeys(r.Amounts) {
			if !slices.ContainsFunc(v.cat.PriceList.Bands, func(b Band) bool { return b.Code == k }) {
				v.err(IssueBandCodeUnknown, RefRule, id, "amounts", map[string]string{"band": k})
			}
		}
	}
	out := r.AmountCents != nil && !amountOK(*r.AmountCents)
	if r.UnitCents != nil && !amountOK(*r.UnitCents) {
		out = true
	}
	for _, a := range r.Amounts {
		if !amountOK(a) {
			out = true
		}
	}
	if out {
		v.err(IssueAmountOutOfRange, RefRule, id, "amounts", nil)
	}
	for _, inc := range r.Includes {
		if _, ok := v.items[inc.ItemID]; !ok {
			v.err(IssueRuleItemUnknown, RefRule, id, "includes", map[string]string{"item_id": inc.ItemID})
		}
		if inc.Qty < 0 || inc.Qty > MaxQtyMilli {
			v.err(IssueAmountOutOfRange, RefRule, id, "includes", nil)
		}
	}
}

// RevenueClassRental is the revenue class of rental items (D3): only they take the rental time units.
const RevenueClassRental = "rental"

func isRentalItem(it *Item) bool { return it != nil && it.RevenueClass == RevenueClassRental }

// rentalRule checks a base rule with a rental time unit (hour, half_day, day; m11-plan §5.1): only on rental items
// and only with basis flat. Quote never uses these rules (no contract type prices them).
func (v *validator) rentalRule(r *Rule, item *Item, known bool) {
	if known && !isRentalItem(item) {
		v.err(IssueRuleUnitInvalid, RefRule, r.ID, "unit", map[string]string{"unit": string(r.Unit)})
	}
	if r.Basis != BasisFlat {
		v.err(IssueRuleBasisInvalid, RefRule, r.ID, "basis", map[string]string{"basis": string(r.Basis), "unit": string(r.Unit)})
	}
}

func (v *validator) effectRule(r *Rule) {
	id := r.ID
	if len(r.Scope.TimeBands) > 0 {
		v.err(IssueRuleUnitInvalid, RefRule, id, "scope.time_bands", nil)
	}
	for _, t := range r.TargetItemIDs {
		if _, ok := v.items[t]; !ok {
			v.err(IssueRuleItemUnknown, RefRule, id, "target_item_ids", map[string]string{"item_id": t})
		}
	}
	for _, t := range r.TargetItemTypes {
		if !slices.Contains(ItemTypes(), t) {
			v.err(IssueRuleEffectInvalid, RefRule, id, "target_item_types", nil)
		}
	}
	eff := r.Effect
	if (eff.PercentBP == nil) == (eff.AmountCents == nil) {
		v.err(IssueRuleEffectInvalid, RefRule, id, "effect", nil)
		return
	}
	if r.Stage == StageModifier {
		if eff.PercentBP != nil && (*eff.PercentBP < -MaxModifierBP || *eff.PercentBP > MaxModifierBP) {
			v.err(IssueAmountOutOfRange, RefRule, id, "effect.percent_bp", nil)
		}
		if eff.AmountCents != nil && (*eff.AmountCents < -MaxAmountCents || *eff.AmountCents > MaxAmountCents) {
			v.err(IssueAmountOutOfRange, RefRule, id, "effect.amount_cents", nil)
		}
		return
	}
	if eff.PercentBP != nil && (*eff.PercentBP < 1 || *eff.PercentBP > MaxDiscountBP) {
		v.err(IssueRuleEffectInvalid, RefRule, id, "effect.percent_bp", nil)
	}
	if eff.AmountCents != nil && (*eff.AmountCents <= 0 || *eff.AmountCents > MaxAmountCents) {
		v.err(IssueRuleEffectInvalid, RefRule, id, "effect.amount_cents", nil)
	}
	switch r.DiscountKind {
	case DiscountPromo:
		if strings.TrimSpace(r.PromoCode) == "" {
			v.err(IssuePromoWithoutCode, RefRule, id, "promo_code", nil)
		}
	case DiscountCustomer:
		if len(r.Scope.CustomerCategories) == 0 {
			v.err(IssueCustomerNoCategory, RefRule, id, "scope.customer_categories", nil)
		}
	case DiscountAutomatic:
	default:
		v.err(IssueRuleEffectInvalid, RefRule, id, "discount_kind", nil)
	}
}

// hasBase tells whether an active base rule of item with one of units can serve ct.
func (v *validator) hasBase(item string, ct ContractType, units ...TimeUnit) bool {
	for _, r := range v.cat.Rules {
		if r.Active && r.Stage == StageBase && r.ItemID == item && slices.Contains(units, r.Unit) &&
			(len(r.Scope.ContractTypes) == 0 || slices.Contains(r.Scope.ContractTypes, ct)) {
			return true
		}
	}
	return false
}

// coverage reports the warnings about served contract types without base rules or split units.
func (v *validator) coverage() {
	pl := v.cat.PriceList
	s := normalizeSettings(pl.Settings)
	for _, ct := range pl.ContractTypes {
		item := s.PrimaryItems[ct]
		if item == "" || !knownContractType(ct) {
			continue
		}
		e := engine{settings: s, in: Input{ContractType: ct}}
		ref := referenceUnit(ct)
		split := e.splitRule()
		bestHasUnits := split == SplitBestCombination && v.hasBase(item, ct, UnitNight, UnitWeek, UnitMonth)
		if !v.hasBase(item, ct, ref) && !bestHasUnits {
			v.warn(IssueNoBaseRuleForContract, RefSettings, "", "primary_items", map[string]string{"contract_type": string(ct), "unit": string(ref)})
			continue
		}
		switch {
		case split == SplitWholeMonths && ref != UnitNight && !v.hasBase(item, ct, UnitMonth):
			v.warn(IssueSplitUnitMissing, RefSettings, "", "split", map[string]string{"contract_type": string(ct), "unit": string(UnitMonth)})
		case split == SplitBestCombination && !v.hasBase(item, ct, UnitNight):
			v.warn(IssueSplitUnitMissing, RefSettings, "", "split", map[string]string{"contract_type": string(ct), "unit": string(UnitNight)})
		}
	}
}

// duplicates reports active base rules with the same item, unit, scope and priority (only recency separates
// them, which is rarely intended).
func (v *validator) duplicates() {
	seen := map[string]string{}
	for _, r := range v.cat.Rules {
		if !r.Active || r.Stage != StageBase {
			continue
		}
		key := strings.Join([]string{r.ItemID, string(r.Unit), itoa(r.Priority), scopeKey(r.Scope)}, "\x00")
		if first, ok := seen[key]; ok {
			v.warn(IssueDuplicateRule, RefRule, r.ID, "scope", map[string]string{"other": first})
			continue
		}
		seen[key] = r.ID
	}
}

func scopeKey(s Scope) string {
	norm := func(xs []string) string {
		c := slices.Clone(xs)
		slices.Sort(c)
		return strings.Join(slices.Compact(c), ",")
	}
	str := func(xs ...[]string) string {
		parts := make([]string, len(xs))
		for i, x := range xs {
			parts[i] = norm(x)
		}
		return strings.Join(parts, "|")
	}
	cts := make([]string, len(s.ContractTypes))
	for i, c := range s.ContractTypes {
		cts[i] = string(c)
	}
	chs := make([]string, len(s.Channels))
	for i, c := range s.Channels {
		chs[i] = string(c)
	}
	return str(cts, s.Categories, s.GroupIDs, chs, s.CustomerCategories, s.Periods, s.TimeBands)
}

// HasErrors reports whether issues contains an error (activation is blocked).
func HasErrors(issues []Issue) bool {
	return slices.ContainsFunc(issues, func(i Issue) bool { return i.Severity == SeverityError })
}
