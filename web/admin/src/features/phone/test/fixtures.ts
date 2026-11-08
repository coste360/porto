// Fixtures of the phone screens: synthetic data only.
import { vi } from 'vitest';

import type { Me } from '@porto/shared/api';
import type { NewOp, QueuedOp } from '@porto/shared/offline';

import { MARINA_ID, makeMe } from '../../../test/renderApp';
import { ALLOC_ID, API, B1, MAP_ID, TRANSIT_ID, vesselBrief } from '../../desk/test/fixtures';

export { API, ALLOC_ID, B1, MAP_ID, MARINA_ID, TRANSIT_ID, vesselBrief };

export const DOCK_PERMISSIONS: Me['marinas'][number]['permissions'] = [
  'marina.read',
  'map.view',
  'customers.read',
  'contracts.read',
  'transits.desk',
  'occupancy.move',
  'presence.write',
  'payments.collect_cash',
];

export function meDock(
  permissions = DOCK_PERMISSIONS,
  role: Me['marinas'][number]['roles'][number] = 'dock',
): Me {
  const me = makeMe({}, permissions);
  me.marinas[0]!.roles = [role];
  return me;
}

export const CHARGE_ID = '0190a000-0000-7000-8000-0000000cc001';
export const REGISTER_ID = '0190a000-0000-7000-8000-0000000dd001';

export const movements = {
  date: '2026-07-10',
  arrivals_expected: [
    {
      vessel: vesselBrief,
      contract: { id: TRANSIT_ID, kind: 'transit', status: 'confirmed', code: 'TRN-2026-0001' },
      resource: { type: 'berth', id: B1, code: 'A01' },
      expected_time: '14:00',
      allocation_id: ALLOC_ID,
    },
  ],
  departures_expected: [],
  vessels_out: [],
  arrived_without_booking: [],
  arrived: [],
  departed: [],
};

export const dayView = {
  map_id: MAP_ID,
  date: '2026-07-10',
  map_version: 3,
  generated_at: '2026-07-10T08:00:00Z',
  elements: [
    {
      resource_type: 'berth',
      resource_id: B1,
      code: 'A01',
      status: 'transit',
      arrival_today: true,
      departure_today: false,
      anomalies: [],
      allocation_id: ALLOC_ID,
      allocation_version: 1,
      contract: { id: TRANSIT_ID, kind: 'transit', status: 'confirmed', code: 'TRN-2026-0001' },
      vessel: vesselBrief,
    },
  ],
  area_placements: [],
  unplaced: [],
  counts: { anomalies: 0 },
};

export const collectible = (o: Record<string, unknown> = {}) => ({
  charge_id: CHARGE_ID,
  number: 'ADD-2026-0007',
  contract_id: TRANSIT_ID,
  contract_code: 'TRN-2026-0001',
  vessel: { id: vesselBrief.id, name: vesselBrief.name },
  customer_id: '0190a000-0000-7000-8000-00000000c001',
  description: 'Sosta',
  due_on: '2026-07-10',
  residual_cents: 4500,
  fiscal_mode: 'invoice',
  ...o,
});

export const cashSession = {
  id: '0190a000-0000-7000-8000-0000000dd101',
  register_id: REGISTER_ID,
  register_code: 'CASSA1',
  status: 'open',
  opened_at: '2026-07-10T07:00:00Z',
  business_date: '2026-07-10',
  float_cents: 10000,
  summary: {},
  version: 1,
};

/** A fake offline queue: enqueue records the ops; the view lists them. */
export function fakeQueue() {
  const state = {
    online: true,
    ops: [] as QueuedOp[],
    enqueued: [] as NewOp[],
  };
  let n = 0;
  const enqueue = vi.fn(async (op: NewOp): Promise<QueuedOp> => {
    state.enqueued.push(op);
    n += 1;
    const { blob: _blob, ...rest } = op;
    void _blob;
    const queued: QueuedOp = {
      ...rest,
      id: `op-${n}`,
      key: `key-${n}`,
      createdAt: `2026-07-10T08:00:0${n}Z`,
      state: 'pending',
      attempts: 0,
    };
    return queued;
  });
  return { state, enqueue };
}
