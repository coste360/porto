import { z } from 'zod';

import type { PricingSimulateRequest } from '@porto/shared/api';

import { nightsBetween } from '../lib/dates';
import { CHANNELS, CONTRACT_TYPES, CUSTOMER_CATEGORIES, HULL_TYPES } from '../lib/enums';

// Simulator form ↔ PricingSimulateRequest. Lengths, money, percentages and quantities are integers in the form
// state already (LengthInput, MoneyInput, PercentInput and QtyInput parse the typed text with the shared
// helpers), so the mapping only drops empty fields: no float ever reaches the request.

const MAX_CM = 20_000;
const MAX_NIGHTS = 3_660;

const length = z
  .number()
  .int('pricingTools:validation.length')
  .positive('pricingTools:validation.length')
  .max(MAX_CM, 'pricingTools:validation.length')
  .nullable();

export const simulatorSchema = z
  .object({
    price_list_id: z.string(),
    contract_type: z.enum(CONTRACT_TYPES),
    arrival: z.string().nullable(),
    departure: z.string().nullable(),
    item_id: z.string(),
    vessel_mode: z.enum(['manual', 'registry']),
    vessel_id: z.string().nullable(),
    loa_cm: length,
    beam_cm: length,
    hull_type: z.enum(HULL_TYPES),
    resource_mode: z.enum(['category', 'berth']),
    berth_id: z.string().nullable(),
    category: z.string().trim().max(32),
    berth_length_cm: length,
    berth_width_cm: length,
    group_ids: z.array(z.string()).max(50),
    customer_mode: z.enum(['category', 'registry']),
    customer_id: z.string().nullable(),
    customer_category: z.union([z.enum(CUSTOMER_CATEGORIES), z.literal('')]),
    channel: z.enum(CHANNELS),
    promo_code: z.string().trim().max(32),
    quoted_on: z.string().nullable(),
    services: z
      .array(
        z.object({
          item_id: z.string(),
          qty_milli: z
            .number()
            .int('pricingTools:validation.qty')
            .positive('pricingTools:validation.qty')
            .nullable(),
        }),
      )
      .max(50),
    deposit_mode: z.enum(['default', 'percent', 'amount']),
    deposit_percent_bp: z
      .number()
      .int('pricingTools:validation.percent')
      .min(0, 'pricingTools:validation.percent')
      .max(10_000, 'pricingTools:validation.percent')
      .nullable(),
    deposit_amount_cents: z
      .number()
      .int('pricingTools:validation.amount')
      .min(0, 'pricingTools:validation.amount')
      .nullable(),
  })
  .superRefine((v, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: 'custom', path: [path], message });
    if (!v.arrival) issue('arrival', 'common:validation.required');
    if (!v.departure) issue('departure', 'common:validation.required');
    const nights = nightsBetween(v.arrival, v.departure);
    if (nights !== null && (nights < 1 || nights > MAX_NIGHTS))
      issue('departure', 'pricingTools:validation.departure');
    if (v.vessel_mode === 'registry' && !v.vessel_id)
      issue('vessel_id', 'common:validation.required');
    if (v.vessel_mode === 'manual' && (v.loa_cm === null) !== (v.beam_cm === null)) {
      issue(v.loa_cm === null ? 'loa_cm' : 'beam_cm', 'pricingTools:validation.length');
    }
    if (v.resource_mode === 'berth' && !v.berth_id) issue('berth_id', 'common:validation.required');
    if (v.customer_mode === 'registry' && !v.customer_id)
      issue('customer_id', 'common:validation.required');
    v.services.forEach((s, i) => {
      if (!s.item_id)
        ctx.addIssue({
          code: 'custom',
          path: ['services', i, 'item_id'],
          message: 'common:validation.required',
        });
    });
    if (v.deposit_mode === 'percent' && v.deposit_percent_bp === null)
      issue('deposit_percent_bp', 'pricingTools:validation.percent');
    if (v.deposit_mode === 'amount' && v.deposit_amount_cents === null)
      issue('deposit_amount_cents', 'pricingTools:validation.amount');
  });

export type SimulatorValues = z.infer<typeof simulatorSchema>;

export function emptySimulatorValues(today: string | null = null): SimulatorValues {
  return {
    price_list_id: '',
    contract_type: 'transit',
    arrival: null,
    departure: null,
    item_id: '',
    vessel_mode: 'manual',
    vessel_id: null,
    loa_cm: null,
    beam_cm: null,
    hull_type: 'monohull_motor',
    resource_mode: 'category',
    berth_id: null,
    category: '',
    berth_length_cm: null,
    berth_width_cm: null,
    group_ids: [],
    customer_mode: 'category',
    customer_id: null,
    customer_category: '',
    channel: 'desk',
    promo_code: '',
    quoted_on: today,
    services: [],
    deposit_mode: 'default',
    deposit_percent_bp: null,
    deposit_amount_cents: null,
  };
}

/** Validated form values → simulate request. Empty fields are left out so that the server resolves them. */
export function toSimulateRequest(v: SimulatorValues): PricingSimulateRequest {
  const req: PricingSimulateRequest = {
    contract_type: v.contract_type,
    arrival: v.arrival ?? '',
    departure: v.departure ?? '',
    channel: v.channel,
  };
  if (v.price_list_id) req.price_list_id = v.price_list_id;
  if (v.item_id) req.item_id = v.item_id;
  if (v.vessel_mode === 'registry' && v.vessel_id) req.vessel_id = v.vessel_id;
  if (v.vessel_mode === 'manual' && v.loa_cm !== null && v.beam_cm !== null) {
    req.vessel = { loa_cm: v.loa_cm, beam_cm: v.beam_cm, hull_type: v.hull_type };
  }
  if (v.resource_mode === 'berth' && v.berth_id) req.resource = { type: 'berth', id: v.berth_id };
  if (v.category) req.category = v.category;
  if (v.berth_length_cm !== null) req.berth_length_cm = v.berth_length_cm;
  if (v.berth_width_cm !== null) req.berth_width_cm = v.berth_width_cm;
  if (v.group_ids.length > 0) req.group_ids = [...v.group_ids];
  if (v.customer_mode === 'registry' && v.customer_id) req.customer_id = v.customer_id;
  if (v.customer_mode === 'category' && v.customer_category)
    req.customer_category = v.customer_category;
  const promo = v.promo_code.trim().toUpperCase();
  if (promo) req.promo_code = promo;
  if (v.quoted_on) req.quoted_on = v.quoted_on;
  if (v.services.length > 0) {
    req.services = v.services.map((s) =>
      s.qty_milli === null
        ? { item_id: s.item_id }
        : { item_id: s.item_id, qty_milli: s.qty_milli },
    );
  }
  if (v.deposit_mode === 'percent' && v.deposit_percent_bp !== null)
    req.deposit = { percent_bp: v.deposit_percent_bp };
  if (v.deposit_mode === 'amount' && v.deposit_amount_cents !== null)
    req.deposit = { amount_cents: v.deposit_amount_cents };
  return req;
}

/** A simulate request (a matrix probe) → form values, so that submitting it sends the same request back. */
export function fromSimulateRequest(
  req: PricingSimulateRequest,
  today: string | null = null,
): SimulatorValues {
  const v = emptySimulatorValues(today);
  v.contract_type = req.contract_type;
  v.arrival = req.arrival;
  v.departure = req.departure;
  v.price_list_id = req.price_list_id ?? '';
  v.item_id = req.item_id ?? '';
  if (req.vessel_id) {
    v.vessel_mode = 'registry';
    v.vessel_id = req.vessel_id;
  } else if (req.vessel) {
    v.loa_cm = req.vessel.loa_cm;
    v.beam_cm = req.vessel.beam_cm;
    if (req.vessel.hull_type) v.hull_type = req.vessel.hull_type;
  }
  if (req.resource?.type === 'berth') {
    v.resource_mode = 'berth';
    v.berth_id = req.resource.id;
  }
  v.category = req.category ?? '';
  v.berth_length_cm = req.berth_length_cm ?? null;
  v.berth_width_cm = req.berth_width_cm ?? null;
  v.group_ids = req.group_ids ? [...req.group_ids] : [];
  if (req.customer_id) {
    v.customer_mode = 'registry';
    v.customer_id = req.customer_id;
  }
  v.customer_category = req.customer_category ?? '';
  v.channel = req.channel ?? 'desk';
  v.promo_code = req.promo_code ?? '';
  v.quoted_on = req.quoted_on ?? today;
  v.services = (req.services ?? []).map((s) => ({
    item_id: s.item_id,
    qty_milli: s.qty_milli ?? null,
  }));
  if (req.deposit?.percent_bp !== undefined) {
    v.deposit_mode = 'percent';
    v.deposit_percent_bp = req.deposit.percent_bp;
  } else if (req.deposit?.amount_cents !== undefined) {
    v.deposit_mode = 'amount';
    v.deposit_amount_cents = req.deposit.amount_cents;
  }
  return v;
}
