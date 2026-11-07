import { z } from 'zod';

import type { Item, ItemCreate, ItemPatch, PricingSettings, Schemas } from '@porto/shared/api';

import { ITEM_TYPES, ITEM_UNITS, VAT_NATURES, VAT_PRESETS_BP } from '../lib/enums';

// Item form (m2-plan §7.2): VAT rate from the presets or a custom percentage (integer bp), a FatturaPA nature
// exactly when the rate is 0 (DB CHECK, §12 D9), codes upper case.

export const ITEM_CODE_RE = /^[A-Z0-9][A-Z0-9._-]{0,31}$/;
export const CUSTOM_VAT = 'custom';

const presetValues: [string, ...string[]] = [CUSTOM_VAT, ...VAT_PRESETS_BP.map(String)];

export const itemSchema = z
  .object({
    code: z
      .string()
      .trim()
      .transform((s) => s.toUpperCase())
      .pipe(z.string().regex(ITEM_CODE_RE, 'pricing:items.validation.code')),
    name_it: z.string().trim().min(1, 'common:validation.required').max(200),
    name_en: z.string().trim().max(200),
    description_it: z.string().trim().max(2000),
    description_en: z.string().trim().max(2000),
    item_type: z.enum(ITEM_TYPES),
    unit: z.enum(ITEM_UNITS),
    vat_preset: z.enum(presetValues),
    vat_custom_bp: z.number().int().nullable(),
    vat_nature: z.union([z.enum(VAT_NATURES), z.literal('')]),
    price_includes_vat: z.boolean(),
    revenue_class_id: z.string().min(1, 'common:validation.required'),
    external_code: z.string().trim().max(64),
    active: z.boolean(),
    online_visible: z.boolean(),
    sort_order: z
      .number('pricing:items.validation.sortOrder')
      .int('pricing:items.validation.sortOrder')
      .nullable()
      .transform((n) => n ?? 0),
  })
  .superRefine((v, ctx) => {
    const rate = vatRateOf(v);
    if (rate === null) {
      ctx.addIssue({
        code: 'custom',
        path: ['vat_custom_bp'],
        message: 'pricing:items.validation.vatRate',
      });
      return;
    }
    if (rate === 0 && v.vat_nature === '') {
      ctx.addIssue({
        code: 'custom',
        path: ['vat_nature'],
        message: 'pricing:items.validation.natureRequired',
      });
    }
  });

export type ItemFormIn = z.input<typeof itemSchema>;
export type ItemFormOut = z.output<typeof itemSchema>;

/** The VAT rate in basis points chosen in the form, or null when the custom percentage is invalid. */
export function vatRateOf(v: Pick<ItemFormIn, 'vat_preset' | 'vat_custom_bp'>): number | null {
  if (v.vat_preset !== CUSTOM_VAT) return Number(v.vat_preset);
  const bp = v.vat_custom_bp;
  return bp !== null && Number.isSafeInteger(bp) && bp >= 0 && bp <= 10_000 ? bp : null;
}

function presetOf(bp: number): string {
  return (VAT_PRESETS_BP as readonly number[]).includes(bp) ? String(bp) : CUSTOM_VAT;
}

export function itemDefaults(
  item: Item | null,
  settings: PricingSettings | undefined,
  defaultRevenueClassId = '',
): ItemFormIn {
  if (!item) {
    const bp = settings?.default_vat_rate_bp ?? 2200;
    return {
      code: '',
      name_it: '',
      name_en: '',
      description_it: '',
      description_en: '',
      item_type: 'service',
      unit: 'piece',
      vat_preset: presetOf(bp),
      vat_custom_bp: presetOf(bp) === CUSTOM_VAT ? bp : null,
      vat_nature: '',
      price_includes_vat: settings?.default_price_includes_vat ?? false,
      revenue_class_id: defaultRevenueClassId,
      external_code: '',
      active: true,
      online_visible: false,
      sort_order: 0,
    };
  }
  const preset = presetOf(item.vat_rate_bp);
  return {
    code: item.code,
    name_it: item.name.it,
    name_en: item.name.en ?? '',
    description_it: item.description.it ?? '',
    description_en: item.description.en ?? '',
    item_type: item.item_type,
    unit: item.unit,
    vat_preset: preset,
    vat_custom_bp: preset === CUSTOM_VAT ? item.vat_rate_bp : null,
    vat_nature: item.vat_nature ?? '',
    price_includes_vat: item.price_includes_vat,
    revenue_class_id: item.revenue_class_id,
    external_code: item.external_code,
    active: item.active,
    online_visible: item.online_visible,
    sort_order: item.sort_order,
  };
}

function texts(it: string, en: string) {
  return en ? { it, en } : { it };
}

function optionalTexts(it: string, en: string) {
  return { ...(it ? { it } : {}), ...(en ? { en } : {}) };
}

/** POST body: the nature is sent only with a 0 % rate. */
export function itemCreateBody(v: ItemFormOut): ItemCreate {
  const rate = vatRateOf(v) ?? 0;
  return {
    code: v.code,
    name: texts(v.name_it, v.name_en),
    description: optionalTexts(v.description_it, v.description_en),
    item_type: v.item_type,
    unit: v.unit,
    vat_rate_bp: rate,
    ...(rate === 0 && v.vat_nature ? { vat_nature: v.vat_nature as Schemas['VatNature'] } : {}),
    price_includes_vat: v.price_includes_vat,
    revenue_class_id: v.revenue_class_id,
    external_code: v.external_code,
    active: v.active,
    online_visible: v.online_visible,
    sort_order: v.sort_order,
  };
}

/** PATCH body with the changed fields only; a non-zero rate clears a stored nature (null). */
export function itemPatchBody(v: ItemFormOut, item: Item): ItemPatch {
  const full = itemCreateBody(v);
  const patch: ItemPatch = {};
  if (full.code !== item.code) patch.code = full.code;
  if (JSON.stringify(full.name) !== JSON.stringify(texts(item.name.it, item.name.en ?? '')))
    patch.name = full.name;
  if (
    JSON.stringify(full.description) !==
    JSON.stringify(optionalTexts(item.description.it ?? '', item.description.en ?? ''))
  )
    patch.description = full.description;
  if (full.item_type !== item.item_type) patch.item_type = full.item_type;
  if (full.unit !== item.unit) patch.unit = full.unit;
  if (full.vat_rate_bp !== item.vat_rate_bp) patch.vat_rate_bp = full.vat_rate_bp;
  const nature = full.vat_nature ?? null;
  if (nature !== (item.vat_nature ?? null)) patch.vat_nature = nature;
  if (full.price_includes_vat !== item.price_includes_vat)
    patch.price_includes_vat = full.price_includes_vat;
  if (full.revenue_class_id !== item.revenue_class_id)
    patch.revenue_class_id = full.revenue_class_id;
  if (full.external_code !== item.external_code) patch.external_code = full.external_code;
  if (full.active !== item.active) patch.active = full.active;
  if (full.online_visible !== item.online_visible) patch.online_visible = full.online_visible;
  if (full.sort_order !== item.sort_order) patch.sort_order = full.sort_order;
  return patch;
}
