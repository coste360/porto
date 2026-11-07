import { z } from 'zod';

import type { PriceRule, Schemas } from '@porto/shared/api';

import {
  BASES,
  CHANNELS,
  CONTRACT_TYPES,
  CUSTOMER_CATEGORIES,
  DISCOUNT_KINDS,
  HULL_TYPES,
  ITEM_TYPES,
  STAGES,
  TIME_UNITS,
} from '../lib/enums';

// Rule Sheet form (m2-plan §2.2, §7.2): one form for the three stages; the fields shown depend on the stage
// and, for base rules, on the basis. `ruleFromForm` keeps only the fields of the stage/basis.

const MAX_CENTS = 100_000_000;
const cents = z.number().int().nullable();
const intOrNull = z.number().int().nullable();

/** Bases priced by a unit amount (¢/m, ¢/m², ¢/unit). */
export const UNIT_BASES: readonly Schemas['PricingBasis'][] = [
  'per_meter',
  'per_m2',
  'per_m2_berth',
  'metered',
];
/** Bases priced by an amount per band or per category, with an optional default amount. */
export const GRID_BASES: readonly Schemas['PricingBasis'][] = ['length_band', 'per_berth'];

export const ruleSchema = z
  .object({
    stage: z.enum(STAGES),
    name: z.string().trim().min(1, 'common:validation.required').max(120),
    active: z.boolean(),
    priority: z
      .number('pricing:rules.validation.priority')
      .int('pricing:rules.validation.priority')
      .min(-1000, 'pricing:rules.validation.priority')
      .max(1000, 'pricing:rules.validation.priority')
      .nullable(),
    stackable: z.boolean(),
    item_id: z.string(),
    basis: z.enum(BASES),
    unit: z.enum(TIME_UNITS),
    amount_cents: cents,
    unit_cents: cents,
    amounts: z.array(z.object({ key: z.string(), cents })),
    includes: z.array(z.object({ item_id: z.string(), qty_milli: intOrNull })),
    target_item_ids: z.array(z.string()),
    target_item_types: z.array(z.enum(ITEM_TYPES)),
    hull_types: z.array(z.enum(HULL_TYPES)),
    overhang_min_cm: intOrNull,
    min_nights: intOrNull,
    max_nights: intOrNull,
    lead_min_days: intOrNull,
    lead_max_days: intOrNull,
    effect_kind: z.enum(['percent', 'amount']),
    effect_percent_bp: intOrNull,
    effect_amount_cents: cents,
    discount_kind: z.enum(DISCOUNT_KINDS),
    promo_code: z.string().trim().max(32),
    scope: z.object({
      contract_types: z.array(z.enum(CONTRACT_TYPES)),
      categories: z.array(z.string()),
      group_ids: z.array(z.string()),
      channels: z.array(z.enum(CHANNELS)),
      customer_categories: z.array(z.enum(CUSTOMER_CATEGORIES)),
      periods: z.array(z.string()),
    }),
  })
  .superRefine((v, ctx) => {
    const issue = (path: (string | number)[], message: string) =>
      ctx.addIssue({ code: 'custom', path, message });
    const amountOk = (c: number | null) => c !== null && c >= 0 && c <= MAX_CENTS;
    if (v.stage === 'base') {
      if (!v.item_id) issue(['item_id'], 'common:validation.required');
      if (v.basis === 'flat' && !amountOk(v.amount_cents))
        issue(['amount_cents'], 'pricing:rules.validation.amount');
      if (UNIT_BASES.includes(v.basis) && !amountOk(v.unit_cents))
        issue(['unit_cents'], 'pricing:rules.validation.amount');
      if (GRID_BASES.includes(v.basis)) {
        const any = v.amounts.some((a) => a.cents !== null) || v.amount_cents !== null;
        if (!any) issue(['amount_cents'], 'pricing:rules.validation.gridEmpty');
        v.amounts.forEach((a, i) => {
          if (a.cents !== null && !amountOk(a.cents))
            issue(['amounts', i, 'cents'], 'pricing:rules.validation.amount');
        });
        if (v.amount_cents !== null && !amountOk(v.amount_cents))
          issue(['amount_cents'], 'pricing:rules.validation.amount');
      }
      v.includes.forEach((inc, i) => {
        if (!inc.item_id) issue(['includes', i, 'item_id'], 'common:validation.required');
        if (inc.qty_milli === null || inc.qty_milli < 1)
          issue(['includes', i, 'qty_milli'], 'pricing:rules.validation.qty');
      });
      return;
    }
    if (v.effect_kind === 'percent') {
      const bp = v.effect_percent_bp;
      const [min, max] = v.stage === 'discount' ? [1, 10_000] : [-100_000, 100_000];
      if (bp === null || bp < min || bp > max || (v.stage === 'modifier' && bp === 0))
        issue(['effect_percent_bp'], 'pricing:rules.validation.percent');
    } else {
      const c = v.effect_amount_cents;
      const min = v.stage === 'discount' ? 1 : -MAX_CENTS;
      if (c === null || c < min || c > MAX_CENTS || c === 0)
        issue(['effect_amount_cents'], 'pricing:rules.validation.amount');
    }
    if (v.stage === 'discount') {
      if (v.discount_kind === 'promo' && v.promo_code === '')
        issue(['promo_code'], 'pricing:rules.validation.promoCode');
      if (v.discount_kind === 'customer' && v.scope.customer_categories.length === 0)
        issue(['scope', 'customer_categories'], 'pricing:rules.validation.customerCategory');
    }
    if (v.min_nights !== null && v.max_nights !== null && v.min_nights > v.max_nights)
      issue(['max_nights'], 'pricing:rules.validation.range');
    if (v.lead_min_days !== null && v.lead_max_days !== null && v.lead_min_days > v.lead_max_days)
      issue(['lead_max_days'], 'pricing:rules.validation.range');
  });

export type RuleFormIn = z.input<typeof ruleSchema>;
export type RuleFormOut = z.output<typeof ruleSchema>;

/**
 * Amount rows of the grid: for `length_band` the bands, for `per_berth` the categories, plus any key already
 * present in the rule (a renamed or removed band stays visible so that it can be cleared).
 */
export function gridKeys(
  basis: Schemas['PricingBasis'],
  rule: PriceRule,
  bands: string[],
  categories: string[],
): string[] {
  const base = basis === 'length_band' ? bands : basis === 'per_berth' ? categories : [];
  const extra = Object.keys(rule.basis === basis ? (rule.amounts ?? {}) : {}).filter(
    (k) => !base.includes(k),
  );
  return [...base, ...extra];
}

export function ruleDefaults(rule: PriceRule, bands: string[], categories: string[]): RuleFormIn {
  const basis = rule.basis ?? 'per_meter';
  const c = rule.conditions ?? {};
  const e = rule.effect ?? {};
  const s = rule.scope ?? {};
  return {
    stage: rule.stage,
    name: rule.name,
    active: rule.active ?? true,
    priority: rule.priority ?? 0,
    stackable: rule.stackable ?? false,
    item_id: rule.item_id ?? '',
    basis,
    unit: rule.unit ?? 'night',
    amount_cents: rule.amount_cents ?? null,
    unit_cents: rule.unit_cents ?? null,
    amounts: gridKeys(basis, rule, bands, categories).map((key) => ({
      key,
      cents: rule.amounts?.[key] ?? null,
    })),
    includes: (rule.includes ?? []).map((i) => ({ item_id: i.item_id, qty_milli: i.qty_milli })),
    target_item_ids: rule.target_item_ids ?? [],
    target_item_types: rule.target_item_types ?? [],
    hull_types: c.hull_types ?? [],
    overhang_min_cm: c.overhang_min_cm ?? null,
    min_nights: c.min_nights ?? null,
    max_nights: c.max_nights ?? null,
    lead_min_days: c.lead_min_days ?? null,
    lead_max_days: c.lead_max_days ?? null,
    effect_kind: e.amount_cents !== undefined ? 'amount' : 'percent',
    effect_percent_bp: e.percent_bp ?? null,
    effect_amount_cents: e.amount_cents ?? null,
    discount_kind: rule.discount_kind ?? 'automatic',
    promo_code: rule.promo_code ?? '',
    scope: {
      contract_types: s.contract_types ?? [],
      categories: s.categories ?? [],
      group_ids: s.group_ids ?? [],
      channels: s.channels ?? [],
      customer_categories: s.customer_categories ?? [],
      periods: s.periods ?? [],
    },
  };
}

function nonEmpty<T>(list: T[]): T[] | undefined {
  return list.length > 0 ? list : undefined;
}

function scopeOf(v: RuleFormOut): Schemas['PricingScope'] {
  const s: Schemas['PricingScope'] = {};
  if (v.scope.contract_types.length) s.contract_types = v.scope.contract_types;
  if (v.scope.categories.length) s.categories = v.scope.categories;
  if (v.scope.group_ids.length) s.group_ids = v.scope.group_ids;
  if (v.scope.channels.length) s.channels = v.scope.channels;
  if (v.scope.customer_categories.length) s.customer_categories = v.scope.customer_categories;
  if (v.scope.periods.length) s.periods = v.scope.periods;
  return s;
}

/** The rule of a submitted form, with the fields of its stage (and basis) only; identity kept. */
export function ruleFromForm(v: RuleFormOut, original: PriceRule): PriceRule {
  const common: PriceRule = {
    ...(original.id ? { id: original.id } : { key: original.key }),
    stage: v.stage,
    name: v.name,
    active: v.active,
    priority: v.priority ?? 0,
    stackable: v.stackable,
    scope: scopeOf(v),
  };
  if (v.stage === 'base') {
    const rule: PriceRule = { ...common, item_id: v.item_id, basis: v.basis, unit: v.unit };
    if (v.basis === 'flat') rule.amount_cents = v.amount_cents ?? 0;
    if (UNIT_BASES.includes(v.basis)) rule.unit_cents = v.unit_cents ?? 0;
    if (GRID_BASES.includes(v.basis)) {
      const amounts: Record<string, number> = {};
      for (const a of v.amounts) if (a.cents !== null) amounts[a.key] = a.cents;
      if (Object.keys(amounts).length) rule.amounts = amounts;
      if (v.amount_cents !== null) rule.amount_cents = v.amount_cents;
    }
    const includes = v.includes.map((i) => ({
      item_id: i.item_id,
      qty_milli: i.qty_milli ?? 1000,
    }));
    if (includes.length) rule.includes = includes;
    return rule;
  }
  const conditions: Schemas['PricingConditions'] = {};
  if (v.hull_types.length) conditions.hull_types = v.hull_types;
  if (v.overhang_min_cm !== null) conditions.overhang_min_cm = v.overhang_min_cm;
  if (v.min_nights !== null) conditions.min_nights = v.min_nights;
  if (v.max_nights !== null) conditions.max_nights = v.max_nights;
  if (v.lead_min_days !== null) conditions.lead_min_days = v.lead_min_days;
  if (v.lead_max_days !== null) conditions.lead_max_days = v.lead_max_days;
  const rule: PriceRule = {
    ...common,
    effect:
      v.effect_kind === 'percent'
        ? { percent_bp: v.effect_percent_bp ?? 0 }
        : { amount_cents: v.effect_amount_cents ?? 0 },
  };
  const targetIds = nonEmpty(v.target_item_ids);
  const targetTypes = nonEmpty(v.target_item_types);
  if (targetIds) rule.target_item_ids = targetIds;
  if (targetTypes) rule.target_item_types = targetTypes;
  if (Object.keys(conditions).length) rule.conditions = conditions;
  if (v.stage === 'discount') {
    rule.discount_kind = v.discount_kind;
    if (v.discount_kind === 'promo') rule.promo_code = v.promo_code.toUpperCase();
  }
  return rule;
}
