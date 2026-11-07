import { z } from 'zod';

import type { RenewalRequest, Schemas } from '@porto/shared/api';

import { ADJUSTMENT_SCOPES, AMOUNT_ROUNDINGS } from '../lib/enums';

// Renewal wizard state (spec 12.6, m2-plan §2.5, §12 D13): target, adjustments, preview, creation.

export type AdjustmentRow = {
  scope: (typeof ADJUSTMENT_SCOPES)[number];
  item_id: string;
  category: string;
  kind: 'percent' | 'amount';
  percent_bp: number | null;
  amount_cents: number | null;
};

export type RenewalState = {
  code: string;
  name_it: string;
  name_en: string;
  shift_years: number | null;
  valid_from: string | null;
  valid_to: string | null;
  istat_bp: number | null;
  adjustments: AdjustmentRow[];
  amount_rounding: Schemas['AmountRounding'];
};

/** Same day N years later; 29 February becomes 28 February in a common year (§12 D6). */
export function shiftIsoDate(iso: string, years: number): string {
  const y = Number(iso.slice(0, 4)) + years;
  const md = iso.slice(5);
  const leap = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
  return `${String(y).padStart(4, '0')}-${md === '02-29' && !leap ? '02-28' : md}`;
}

/** Suggested code of the renewed list: the year in the code moves forward, else "-R" is appended. */
export function suggestCode(code: string, years: number): string {
  const m = /^(.*?)(\d{4})(\D*)$/.exec(code);
  if (m && years > 0) return `${m[1]}${Number(m[2]) + years}${m[3]}`.slice(0, 32);
  return `${code}-R`.slice(0, 32);
}

export function initialState(source: {
  code: string;
  name: { it: string; en?: string };
}): RenewalState {
  return {
    code: suggestCode(source.code, 1),
    name_it: source.name.it,
    name_en: source.name.en ?? '',
    shift_years: 1,
    valid_from: null,
    valid_to: null,
    istat_bp: null,
    adjustments: [],
    amount_rounding: 'none',
  };
}

const adjustmentSchema = z
  .object({
    scope: z.enum(ADJUSTMENT_SCOPES),
    item_id: z.string(),
    category: z.string(),
    kind: z.enum(['percent', 'amount']),
    percent_bp: z.number().int().nullable(),
    amount_cents: z.number().int().nullable(),
  })
  .superRefine((a, ctx) => {
    if (a.scope === 'item' && !a.item_id)
      ctx.addIssue({ code: 'custom', path: ['item_id'], message: 'common:validation.required' });
    if (a.scope === 'category' && !a.category)
      ctx.addIssue({ code: 'custom', path: ['category'], message: 'common:validation.required' });
    if (a.kind === 'percent') {
      if (a.percent_bp === null || a.percent_bp < -10_000 || a.percent_bp > 100_000)
        ctx.addIssue({
          code: 'custom',
          path: ['percent_bp'],
          message: 'pricing:renew.validation.percent',
        });
    } else if (
      a.amount_cents === null ||
      a.amount_cents < -100_000_000 ||
      a.amount_cents > 100_000_000
    ) {
      ctx.addIssue({
        code: 'custom',
        path: ['amount_cents'],
        message: 'pricing:renew.validation.amount',
      });
    }
  });

export const targetSchema = z
  .object({
    code: z.string().trim().min(1, 'common:validation.required').max(32),
    name_it: z.string().trim().min(1, 'common:validation.required').max(200),
    name_en: z.string().trim().max(200),
    shift_years: z
      .number('pricing:renew.validation.shift')
      .int('pricing:renew.validation.shift')
      .min(0, 'pricing:renew.validation.shift')
      .max(5, 'pricing:renew.validation.shift'),
    valid_from: z.string().nullable(),
    valid_to: z.string().nullable(),
  })
  .refine((v) => !v.valid_from || !v.valid_to || v.valid_to >= v.valid_from, {
    path: ['valid_to'],
    message: 'pricing:lists.validation.validTo',
  });

export const adjustmentsSchema = z.object({
  istat_bp: z
    .number()
    .int()
    .min(0, 'pricing:renew.validation.istat')
    .max(10_000, 'pricing:renew.validation.istat')
    .nullable(),
  adjustments: z.array(adjustmentSchema).max(200),
  amount_rounding: z.enum(AMOUNT_ROUNDINGS),
});

/** i18n error keys by field path ("code", "adjustments.0.percent_bp") for one step, empty when valid. */
export function stepErrors(state: RenewalState, step: 1 | 2): Record<string, string> {
  const res = step === 1 ? targetSchema.safeParse(state) : adjustmentsSchema.safeParse(state);
  if (res.success) return {};
  const out: Record<string, string> = {};
  for (const issue of res.error.issues) {
    const path = issue.path.join('.');
    if (!(path in out)) out[path] = issue.message;
  }
  return out;
}

/** Request body without `dry_run` (the hooks add it). */
export function renewalBody(state: RenewalState): Omit<RenewalRequest, 'dry_run'> {
  return {
    code: state.code.trim(),
    name: state.name_en.trim()
      ? { it: state.name_it.trim(), en: state.name_en.trim() }
      : { it: state.name_it.trim() },
    shift_years: state.shift_years ?? 0,
    ...(state.istat_bp ? { istat_bp: state.istat_bp } : {}),
    adjustments: state.adjustments.map((a) => ({
      scope: a.scope,
      ...(a.scope === 'item' ? { item_id: a.item_id } : {}),
      ...(a.scope === 'category' ? { category: a.category } : {}),
      ...(a.kind === 'percent'
        ? { percent_bp: a.percent_bp ?? 0 }
        : { amount_cents: a.amount_cents ?? 0 }),
    })),
    amount_rounding: state.amount_rounding,
    ...(state.valid_from ? { valid_from: state.valid_from } : {}),
    ...(state.valid_to ? { valid_to: state.valid_to } : {}),
  };
}
