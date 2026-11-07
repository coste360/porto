import type { TFunction } from 'i18next';

import type { Item, PriceRule, RevenueClass, Schemas } from '@porto/shared/api';
import { formatBp, formatCents, formatCm } from '@porto/shared/lib/format';

import type { BandRange } from '../editor/model';
import { localText } from './enums';

/** "da 10,01 a 12,00 m" or "oltre 12,01 m" for an open band. */
export function rangeLabel(t: TFunction, r: BandRange, lang: string): string {
  return r.toCm === null
    ? t('pricing:bands.rangeOpen', { from: formatCm(r.fromCm, lang) })
    : t('pricing:bands.range', {
        // "da 10,01 a 12,00 m": the unit once, after the upper bound.
        from: formatCm(r.fromCm, lang).replace(/\s*m$/, ''),
        to: formatCm(r.toCm, lang),
      });
}

/** Revenue classes as "Category / Subcategory", sorted by label. */
export function revenueClassOptions(classes: RevenueClass[], lang: string) {
  const byId = new Map(classes.map((c) => [c.id, c]));
  return classes
    .map((c) => {
      const parent = c.parent_id ? byId.get(c.parent_id) : undefined;
      const label = parent
        ? `${localText(parent.name, lang)} / ${localText(c.name, lang)}`
        : localText(c.name, lang);
      return { id: c.id, label, archived: c.archived };
    })
    .sort((a, b) => a.label.localeCompare(b.label));
}

/** One-line description of what a rule charges or changes. */
export function ruleDetail(t: TFunction, r: PriceRule, items: Item[], lang: string): string {
  const money = (c: number) => formatCents(c, 'EUR', lang);
  if (r.stage === 'base') {
    const item = items.find((i) => i.id === r.item_id)?.code ?? '';
    const basis = r.basis ? t(`pricingEnums:bases.${r.basis}`) : '';
    const unit = r.unit ? t(`pricingEnums:timeUnits.${r.unit}`) : '';
    let amount = '';
    if (r.unit_cents !== undefined) amount = money(r.unit_cents);
    else if (r.amounts && Object.keys(r.amounts).length)
      amount = t('pricing:rules.amountsCount', { count: Object.keys(r.amounts).length });
    else if (r.amount_cents !== undefined) amount = money(r.amount_cents);
    return [item, basis, unit, amount].filter(Boolean).join(' · ');
  }
  const e = r.effect ?? {};
  const effect =
    e.percent_bp !== undefined
      ? formatBp(e.percent_bp, lang)
      : e.amount_cents !== undefined
        ? money(e.amount_cents)
        : '';
  const kind =
    r.stage === 'discount' && r.discount_kind
      ? t(`pricingEnums:discountKinds.${r.discount_kind}`)
      : '';
  return [kind, r.promo_code ?? '', effect].filter(Boolean).join(' · ');
}

/** Short scope description: the non-empty scope lists, "any" otherwise. */
export function scopeSummary(t: TFunction, s: Schemas['PricingScope'] | undefined): string {
  if (!s) return t('pricing:rules.any');
  const parts: string[] = [];
  if (s.contract_types?.length)
    parts.push(s.contract_types.map((c) => t(`pricingEnums:contractTypes.${c}`)).join(', '));
  if (s.periods?.length) parts.push(s.periods.join(', '));
  if (s.categories?.length) parts.push(s.categories.join(', '));
  if (s.group_ids?.length)
    parts.push(t('pricing:rules.groupsCount', { count: s.group_ids.length }));
  if (s.channels?.length)
    parts.push(s.channels.map((c) => t(`pricingEnums:channels.${c}`)).join(', '));
  if (s.customer_categories?.length)
    parts.push(
      s.customer_categories.map((c) => t(`pricingEnums:customerCategories.${c}`)).join(', '),
    );
  return parts.length ? parts.join(' · ') : t('pricing:rules.any');
}
