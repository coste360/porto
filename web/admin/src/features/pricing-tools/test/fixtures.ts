// Fixtures of the pricing tool pages: synthetic ids and codes, hand-computed integer amounts (no PII).
import type {
  Item,
  Me,
  PriceBreakdown,
  PriceCoverage,
  PriceListDocument,
  PriceMatrix,
  PricingSimulateRequest,
} from '@porto/shared/api';

import { MARINA_ID, makeMe } from '../../../test/renderApp';

export const API = `/api/admin/v1/marinas/${MARINA_ID}`;
export const LIST_ID = '0190a000-0000-7000-8000-0000000b0001';
export const ITEM_ID = '0190a000-0000-7000-8000-0000000b1001';
export const SERVICE_ID = '0190a000-0000-7000-8000-0000000b1002';
export const RULE_ID = '0190a000-0000-7000-8000-0000000b2001';
export const MOD_ID = '0190a000-0000-7000-8000-0000000b2002';

export const meWithPricing = (
  permissions: Me['marinas'][number]['permissions'] = ['marina.read', 'pricing.read'],
) => makeMe({}, permissions);

export const item: Item = {
  id: ITEM_ID,
  code: 'TRN-NTT',
  name: { it: 'Transito notte', en: 'Transit night' },
  description: {},
  item_type: 'berth_fee',
  unit: 'night',
  vat_rate_bp: 2200,
  vat_nature: null,
  price_includes_vat: true,
  revenue_class_id: '0190a000-0000-7000-8000-0000000b3001',
  external_code: '',
  active: true,
  online_visible: true,
  sort_order: 0,
  version: 1,
  updated_at: '2026-03-01T10:00:00Z',
};

export const service: Item = {
  ...item,
  id: SERVICE_ID,
  code: 'SRV-ELE',
  name: { it: 'Corrente', en: 'Power' },
  item_type: 'utility',
  unit: 'kwh',
};

export const priceList: PriceListDocument = {
  id: LIST_ID,
  code: 'T-2027',
  name: { it: 'Transiti 2027', en: 'Transits 2027' },
  status: 'active',
  valid_from: '2027-01-01',
  valid_to: '2027-12-31',
  contract_types: ['transit', 'annual'],
  settings: {
    dimension_source: 'vessel',
    length_rounding: { step_cm: 10, mode: 'up' },
    primary_items: { transit: ITEM_ID },
  },
  periods: [
    {
      id: 'p1',
      code: 'BASSA',
      name: { it: 'Bassa' },
      kind: 'season',
      ranges: [{ from: '2027-01-01', to: '2027-06-30' }],
    },
    {
      id: 'p2',
      code: 'ALTA',
      name: { it: 'Alta' },
      kind: 'season',
      ranges: [{ from: '2027-07-01', to: '2027-12-31' }],
    },
  ],
  bands: [
    { id: 'b1', code: 'L10', max_cm: 1000, category: 'L10' },
    { id: 'b2', code: 'L12', max_cm: 1200, category: 'L12' },
  ],
  rules: [
    {
      id: RULE_ID,
      stage: 'base',
      name: 'Transito alta al metro',
      item_id: ITEM_ID,
      basis: 'per_meter',
      unit: 'night',
      unit_cents: 750,
    },
    {
      id: MOD_ID,
      stage: 'modifier',
      name: 'Multiscafo +50%',
      effect: { percent_bp: 5000 },
      conditions: { hull_types: ['catamaran', 'trimaran'] },
    },
  ],
  version: 3,
  issues: [],
  updated_at: '2026-03-01T10:00:00Z',
};

/** 3 nights in ALTA, 11,34 m rounded to 11,40 m × 7,50 € = 256,50 € VAT included (22 %): 210,25 + 46,25. */
export const breakdown: PriceBreakdown = {
  engine_version: 1,
  price_list_id: LIST_ID,
  catalog_hash: 'abc123',
  contract_type: 'transit',
  nights: 3,
  lines: [
    {
      seq: 1,
      kind: 'berth',
      item_id: ITEM_ID,
      item_code: 'TRN-NTT',
      item_type: 'berth_fee',
      qty_milli: 3000,
      blocks: [
        {
          from: '2027-07-10',
          to: '2027-07-13',
          unit: 'night',
          nights: 3,
          period: 'ALTA',
          rule_id: RULE_ID,
          base_cents: 25650,
          modifiers: [],
          amount_cents: 25650,
        },
      ],
      base_cents: 25650,
      modifier_total_cents: 0,
      discounts: [],
      amount_cents: 25650,
      rounding_share_cents: 0,
      gross_cents: 25650,
      net_cents: 21025,
      vat_cents: 4625,
      vat_rate_bp: 2200,
      price_includes_vat: true,
      service_from: '2027-07-10',
      service_to: '2027-07-12',
    },
  ],
  subtotal_cents: 25650,
  discount_total_cents: 0,
  rounding_total_cents: 0,
  total_cents: 25650,
  net_cents: 21025,
  vat_cents: 4625,
  vat_summary: [{ vat_rate_bp: 2200, net_cents: 21025, vat_cents: 4625, gross_cents: 25650 }],
  deposit_cents: 0,
  trace: [
    { code: 'list.selected', line: 0, block: 0, params: { price_list_id: LIST_ID } },
    {
      code: 'dimensions.rounded',
      line: 1,
      block: 0,
      params: { from_cm: '1134', to_cm: '1140', step_cm: '10', mode: 'up' },
    },
    { code: 'period.selected', line: 1, block: 0, params: { night: '2027-07-10', period: 'ALTA' } },
    { code: 'rule.selected', line: 1, block: 0, rule_id: RULE_ID, params: { unit: 'night' } },
    { code: 'block.priced', line: 1, block: 0, params: { unit: 'night', base_cents: '25650' } },
    {
      code: 'vat.line',
      line: 1,
      block: 0,
      params: { vat_rate_bp: '2200', gross_cents: '25650' },
    },
  ],
  warnings: [],
  resolved: {
    price_list_id: LIST_ID,
    price_list_code: 'T-2027',
    item_id: ITEM_ID,
    loa_cm: 1134,
    beam_cm: 380,
    hull_type: 'monohull_motor',
    category: 'L12',
    channel: 'desk',
    quoted_on: '2027-03-01',
    dimension_origin: 'input',
  },
};

/** A catamaran stay: the multihull modifier adds 50 % (128,25 €) to the block. */
export const multihullBreakdown: PriceBreakdown = {
  ...breakdown,
  lines: breakdown.lines.map((l) => ({
    ...l,
    blocks: l.blocks.map((b) => ({
      ...b,
      modifiers: [{ rule_id: MOD_ID, delta_cents: 12825 }],
      amount_cents: 38475,
    })),
    modifier_total_cents: 12825,
    amount_cents: 38475,
    gross_cents: 38475,
    net_cents: 31537,
    vat_cents: 6938,
  })),
  subtotal_cents: 38475,
  total_cents: 38475,
  net_cents: 31537,
  vat_cents: 6938,
  vat_summary: [{ vat_rate_bp: 2200, net_cents: 31537, vat_cents: 6938, gross_cents: 38475 }],
  trace: [
    ...breakdown.trace,
    {
      code: 'modifier.applied',
      line: 1,
      block: 0,
      rule_id: MOD_ID,
      params: { delta_cents: '12825', stackable: 'true' },
    },
  ],
  warnings: ['stay_exceeds_validity'],
};

export const probe = (col: number, row: number): PricingSimulateRequest => ({
  price_list_id: LIST_ID,
  contract_type: 'transit',
  arrival: col === 0 ? '2027-01-01' : '2027-07-01',
  departure: col === 0 ? '2027-01-02' : '2027-07-02',
  vessel: {
    loa_cm: row === 0 ? 1000 : 1200,
    beam_cm: row === 0 ? 333 : 400,
    hull_type: 'monohull_motor',
  },
  category: row === 0 ? 'L10' : 'L12',
  channel: 'desk',
  quoted_on: '2027-03-01',
});

/** Transit matrix: bands L10/L12 × BASSA/ALTA; L10 × BASSA has no price. */
export const matrix: PriceMatrix = {
  price_list_id: LIST_ID,
  contract_type: 'transit',
  item_id: ITEM_ID,
  quoted_on: '2027-03-01',
  rows: [
    { key: 'L10', kind: 'band', probe_loa_cm: 1000, probe_beam_cm: 333 },
    { key: 'L12', kind: 'band', probe_loa_cm: 1200, probe_beam_cm: 400 },
  ],
  columns: [
    { key: 'BASSA', arrival: '2027-01-01', departure: '2027-01-02' },
    { key: 'ALTA', arrival: '2027-07-01', departure: '2027-07-02' },
  ],
  cells: [
    { row: 0, col: 0, error: 'no_price:no_rule', probe: probe(0, 0) },
    {
      row: 0,
      col: 1,
      total_cents: 7500,
      net_cents: 6148,
      base_cents: 7500,
      unit: 'night',
      probe: probe(1, 0),
    },
    {
      row: 1,
      col: 0,
      total_cents: 4800,
      net_cents: 3934,
      base_cents: 4800,
      unit: 'night',
      probe: probe(0, 1),
    },
    {
      row: 1,
      col: 1,
      total_cents: 9000,
      net_cents: 7377,
      base_cents: 9000,
      unit: 'night',
      probe: probe(1, 1),
    },
  ],
};

/** The same matrix after +5 %. */
export const matrixAfter: PriceMatrix = {
  ...matrix,
  cells: matrix.cells.map((c) =>
    c.total_cents === undefined ? c : { ...c, total_cents: (c.total_cents / 100) * 105 },
  ),
};

export const coverage: PriceCoverage = {
  checked: 12,
  gaps: [
    { contract_type: 'transit', band: 'L10', period: 'BASSA', item_id: ITEM_ID, reason: 'no_rule' },
    {
      contract_type: 'transit',
      category: 'L14',
      period: 'ALTA',
      item_id: ITEM_ID,
      reason: 'no_band',
    },
    { contract_type: 'annual', category: 'L12', item_id: ITEM_ID, reason: 'no_amount' },
  ],
  elements_without_category: { berths: 3, dynamic_areas: 1, land_resources: 0 },
  unknown_categories: [{ code: 'L30', count: 2 }],
};
