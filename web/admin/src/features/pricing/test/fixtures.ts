import type {
  BerthCategoryList,
  Item,
  PriceListDocument,
  PricingSettings,
  RevenueClass,
} from '@porto/shared/api';
import type { MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute } from '../../../test/renderApp';

export const base = `/api/admin/v1/marinas/${MARINA_ID}`;

export const owner = makeMe({}, ['marina.read', 'map.view', 'pricing.read', 'pricing.configure']);
export const reader = makeMe({}, ['marina.read', 'pricing.read']);

export const settings: PricingSettings = {
  default_vat_rate_bp: 2200,
  default_price_includes_vat: false,
  version: 3,
};

export const revenueClasses: RevenueClass[] = [
  {
    id: 'rc1',
    code: 'ormeggi',
    name: { it: 'Ormeggi', en: 'Moorings' },
    parent_id: null,
    sort_order: 0,
    archived: false,
    item_count: 2,
    version: 1,
  },
  {
    id: 'rc2',
    code: 'transiti',
    name: { it: 'Transiti' },
    parent_id: 'rc1',
    sort_order: 1,
    archived: false,
    item_count: 1,
    version: 1,
  },
  {
    id: 'rc3',
    code: 'servizi',
    name: { it: 'Servizi' },
    parent_id: null,
    sort_order: 2,
    archived: false,
    item_count: 0,
    version: 1,
  },
];

export function makeItem(over: Partial<Item> = {}): Item {
  return {
    id: 'i1',
    code: 'TRN-NTT',
    name: { it: 'Transito notte' },
    description: {},
    item_type: 'berth_fee',
    unit: 'night',
    vat_rate_bp: 2200,
    vat_nature: null,
    price_includes_vat: true,
    revenue_class_id: 'rc2',
    external_code: '',
    active: true,
    online_visible: false,
    sort_order: 0,
    version: 4,
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

export const items: Item[] = [
  makeItem(),
  makeItem({
    id: 'i2',
    code: 'SRV-ALG',
    name: { it: 'Allaccio acqua' },
    item_type: 'service',
    unit: 'piece',
    price_includes_vat: false,
    revenue_class_id: 'rc3',
    version: 2,
  }),
];

export const categories: BerthCategoryList = {
  items: [
    {
      id: 'c1',
      code: 'L10',
      name: { it: 'Fino a 10 m' },
      applies_to: ['berth'],
      sort_order: 0,
      archived: false,
      usage: { berths: 12, dynamic_areas: 0, land_resources: 0, rules: 1 },
      version: 1,
    },
    {
      id: 'c2',
      code: 'L12',
      name: { it: 'Fino a 12 m' },
      applies_to: ['berth', 'dynamic_area'],
      sort_order: 1,
      archived: false,
      usage: { berths: 8, dynamic_areas: 1, land_resources: 0, rules: 0 },
      version: 2,
    },
  ],
  unknown_in_use: [{ code: 'L15', count: 3 }],
};

export function makeDocument(over: Partial<PriceListDocument> = {}): PriceListDocument {
  return {
    id: 'pl1',
    code: 'T-2027',
    name: { it: 'Transiti 2027' },
    status: 'draft',
    valid_from: '2027-01-01',
    valid_to: '2027-12-31',
    contract_types: ['transit'],
    settings: {
      dimension_source: 'vessel',
      length_rounding: { step_cm: 10, mode: 'up' },
      min_beam_ratio: { num: 1, den: 3 },
      split: { '*': 'pro_rata_daily' },
      rounding: { '*': 'none' },
      deposits: {},
      season_window: null,
      primary_items: { transit: 'i1' },
      publish_matrix: false,
    },
    periods: [
      {
        id: 'p1',
        code: 'BASSA',
        name: { it: 'Bassa stagione' },
        kind: 'season',
        color: 'chart-3',
        ranges: [
          { from: '2027-01-01', to: '2027-05-31' },
          { from: '2027-10-01', to: '2027-12-31' },
        ],
      },
      {
        id: 'p2',
        code: 'ALTA',
        name: { it: 'Alta stagione' },
        kind: 'season',
        color: 'chart-5',
        ranges: [{ from: '2027-06-01', to: '2027-09-30' }],
      },
    ],
    bands: [
      { id: 'b1', code: 'L10', max_cm: 1000, category: 'L10' },
      { id: 'b2', code: 'L12', max_cm: 1200, category: 'L12' },
    ],
    rules: [
      {
        id: 'r1',
        seq: 1,
        stage: 'base',
        name: 'Transito alta',
        active: true,
        priority: 0,
        stackable: false,
        item_id: 'i1',
        basis: 'per_meter',
        unit: 'night',
        unit_cents: 750,
        scope: { periods: ['ALTA'] },
      },
    ],
    issues: [],
    version: 5,
    updated_at: '2027-01-02T10:00:00Z',
    ...over,
  };
}

/** API routes every pricing page reads. */
export const customerCategoryCounts = {
  items: [
    { code: 'member', count: 12 },
    { code: 'resident', count: 3 },
    { code: 'affiliated', count: 0 },
    { code: 'charter', count: 1 },
    { code: 'yard', count: 0 },
  ],
  uncategorised: 40,
};

export function pricingRoutes(me = owner): MockRoute[] {
  return [
    meRoute(me),
    { path: `${base}/pricing-settings`, body: settings },
    { path: `${base}/revenue-classes`, body: { items: revenueClasses } },
    { path: `${base}/items`, body: { items, next_cursor: null } },
    { path: `${base}/berth-categories`, body: categories },
    { path: `${base}/berth-groups`, body: { items: [] } },
    { path: `${base}/customer-categories`, body: customerCategoryCounts },
  ];
}
