// Test fixtures of the desk pages: synthetic data only.
import type { Me, Schemas } from '@porto/shared/api';

import { MARINA_ID, makeMe } from '../../../test/renderApp';

export const API = `/api/admin/v1/marinas/${MARINA_ID}`;
export const MAP_ID = '0190a000-0000-7000-8000-0000000aa001';
export const B1 = '0190a000-0000-7000-8000-00000000b001';
export const B2 = '0190a000-0000-7000-8000-00000000b002';
export const AREA = '0190a000-0000-7000-8000-00000000ae01';
export const VESSEL_ID = '0190a000-0000-7000-8000-00000000a001';
export const CUSTOMER_ID = '0190a000-0000-7000-8000-00000000c001';
export const TRANSIT_ID = '0190a000-0000-7000-8000-00000000f001';
export const CONTRACT_ID = '0190a000-0000-7000-8000-00000000f002';
export const QUOTE_ID = '0190a000-0000-7000-8000-00000000e001';
export const ALLOC_ID = '0190a000-0000-7000-8000-000000000a01';

export const DESK_PERMISSIONS: Me['marinas'][number]['permissions'] = [
  'marina.read',
  'map.view',
  'customers.read',
  'contracts.read',
  'contracts.write',
  'transits.desk',
  'occupancy.move',
  'presence.write',
  'harbor.configure',
];

export const meDesk = (permissions = DESK_PERMISSIONS) => makeMe({}, permissions);

export const maps = {
  items: [
    {
      id: MAP_ID,
      kind: 'water',
      name: { it: 'Porto' },
      icon: 'anchor',
      width_cm: 60000,
      height_cm: 40000,
      sort_order: 1,
      version: 1,
      counts: {},
      georeferenced: false,
      has_background: false,
      updated_at: '2026-07-01T10:00:00Z',
    },
  ],
};

const berth = (id: string, code: string, position: number) => ({
  id,
  code,
  side: 'left',
  position,
  anchor: { x: 0, y: 0 },
  rotation_dd: 0,
  width_cm: 450,
  length_cm: 1400,
  mooring_type: 'mooring_line',
  overhang_allowed: false,
  water: true,
  wifi: false,
  attributes: [],
  online_sellable: false,
  active: true,
  group_ids: [],
  structure_id: 's1',
});

export const mapDoc = {
  id: MAP_ID,
  version: 3,
  updated_at: '2026-07-01T10:00:00Z',
  kind: 'water',
  name: { it: 'Porto' },
  structures: [{ id: 's1', kind: 'pier', code: 'A', name: 'Pontile A' }],
  berths: [berth(B1, 'A01', 1), berth(B2, 'A02', 2)],
  dynamic_areas: [
    {
      id: AREA,
      kind: 'linear',
      code: 'L1',
      blocked_cells: [],
      exits: [],
      grid_rotation_dd: 0,
      active: true,
      group_ids: [],
    },
  ],
  land_resources: [],
  pedestals: [],
  decorations: [],
};

export const vesselBrief: Schemas['VesselBrief'] = {
  id: VESSEL_ID,
  name: 'Libeccio',
  loa_cm: 1134,
  beam_cm: 380,
  hull_type: 'monohull_sail',
};

export const vesselsPage = {
  items: [
    {
      id: VESSEL_ID,
      name: 'Libeccio',
      registration: 'GE1234',
      hull_type: 'monohull_sail',
      loa_cm: 1134,
      beam_cm: 380,
      effective_loa_cm: 1134,
      effective_beam_cm: 380,
      dimensions_source: 'declared',
      unit_class: 'imbarcazione',
      owners: [],
      archived_at: null,
    },
  ],
  next_cursor: null,
};

export const customersPage = {
  items: [{ id: CUSTOMER_ID, display_name: 'Mario Rossi', anonymized_at: null, kind: 'person' }],
  next_cursor: null,
};

export const solution = (key: string, code: string, resourceId: string, total = 12000) =>
  ({
    key,
    score: 100,
    fit: { ok: true, overridable: false, fill_bp: 8100, blocking: [], warnings: [] },
    segments: [
      {
        resource_type: 'berth',
        resource_id: resourceId,
        code,
        map_id: MAP_ID,
        first_night: '2026-07-10',
        last_night: '2026-07-12',
      },
    ],
    pricing: { price_list_id: 'pl', total_cents: total, deposit_cents: 0, engine_version: 1 },
  }) satisfies Schemas['OccupancySolution'];

export const transit = (o: Partial<Schemas['Contract']> = {}): Schemas['Contract'] => ({
  id: TRANSIT_ID,
  code: 'TRN-2026-0001',
  kind: 'transit',
  number: 1,
  year: 2026,
  status: 'held',
  vessel: vesselBrief,
  starts_on: '2026-07-10',
  ends_on: '2026-07-13',
  channel: 'desk',
  total_cents: 12000,
  deposit_cents: 0,
  engine_version: 1,
  hold_lost: false,
  signed_on_paper: false,
  pay_at_departure: false,
  hold_expires_at: '2026-07-11T10:00:00Z',
  lines: [],
  allocations: [
    {
      id: ALLOC_ID,
      resource_type: 'berth',
      resource_id: B1,
      resource_code: 'A01',
      map_id: MAP_ID,
      first_night: '2026-07-10',
      last_night: '2026-07-12',
      kind: 'hold',
      status: 'active',
      version: 1,
      released: [],
    },
  ],
  releases: [],
  presences: [],
  created_at: '2026-07-10T08:00:00Z',
  version: 4,
  ...o,
});

export const marinaDetail = {
  id: MARINA_ID,
  timezone: 'Europe/Rome',
  name: 'Marina Prova',
  slug: 'prova',
};

/** Routes every desk page needs: me, marina (timezone), maps, map document, vessels and customers. */
export const baseRoutes = (me = meDesk()) => [
  { path: '/api/admin/v1/auth/me', body: me },
  { path: API, body: marinaDetail },
  { path: `${API}/maps`, body: maps },
  { path: `${API}/maps/${MAP_ID}`, body: mapDoc },
  { path: `${API}/vessels`, body: vesselsPage },
  { path: `${API}/vessels/${VESSEL_ID}`, body: { ...vesselsPage.items[0] } },
  { path: `${API}/customers`, body: customersPage },
  { path: `${API}/berth-groups`, body: { items: [] } },
];
