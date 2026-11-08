import type { StoragePlan, StorageScenarioDetail } from '@porto/shared/api';

// Fixtures of the storage pages (m9-plan §14.5): a 20 × 10 shed with 1 m cells, a door on the left side, two
// boats on contracts and a block.

export const AREA_ID = '01900000-0000-7000-8000-0000000000a1';
export const SCENARIO_ID = '01900000-0000-7000-8000-0000000000c1';

const area = {
  id: AREA_ID,
  map_id: 'm1',
  map_kind: 'shed' as const,
  code: 'CAPANNONE',
  name: 'Capannone',
  cell_cm: 100,
  cols: 20,
  rows: 10,
  usable_cells: 200,
  usable_cm2: 2_000_000,
  occupied_cells_today: 50,
  boats_today: 2,
  exits_blocked_next_30: 1,
  draft_scenarios: 1,
  gates_valid: true,
};

const vessel = (id: string, name: string) => ({
  id,
  name,
  loa_cm: 800,
  beam_cm: 300,
  hull_type: 'monohull_sail' as const,
});

export const plan: StoragePlan = {
  area,
  from: '2026-11-08',
  to: '2026-12-08',
  gates: [{ index: 0, cells: [80, 100], valid: true }],
  obstacles: [
    {
      allocation_id: 'blk',
      kind: 'block',
      cells: [199],
      first_night: '2026-11-01',
      last_night: '2026-12-31',
    },
  ],
  items: [
    {
      allocation_id: 'al-a',
      kind: 'contract',
      vessel: vessel('v-a', 'Aria'),
      contract: {
        id: 'c-a',
        kind: 'storage',
        status: 'active',
        code: 'CTR-2026-0001',
        term: 'seasonal',
      },
      x_cm: 0,
      y_cm: 0,
      rotation_dd: 0,
      cells: [0, 1],
      first_night: '2026-10-01',
      last_night: '2027-04-30',
      arrival_on: '2026-10-01',
      exit_on: '2026-11-20',
      version: 3,
    },
    {
      allocation_id: 'al-b',
      kind: 'contract',
      vessel: vessel('v-b', 'Bora'),
      x_cm: 1000,
      y_cm: 500,
      rotation_dd: 0,
      cells: [110],
      first_night: '2026-10-01',
      last_night: '2027-04-30',
      arrival_on: '2026-10-01',
      exit_on: '2027-05-01',
      version: 1,
    },
  ],
  access: [
    {
      allocation_id: 'al-a',
      event: 'exit',
      on: '2026-11-20',
      accessible: false,
      blocking: ['al-b'],
    },
  ],
};

const indicators = {
  usable_cm2: 2_000_000,
  used_hull_cm2: 480_000,
  used_footprint_cm2: 600_000,
  moves: 1,
  unplaced: 0,
  overlaps: 0,
  exits_blocked: 0,
  arrivals_blocked: 0,
  largest_free_rect_cm2: 900_000,
  blocked_by_date: [],
};

export const scenario: StorageScenarioDetail = {
  scenario: {
    id: SCENARIO_ID,
    area_id: AREA_ID,
    name: 'Inverno',
    author_user_id: 'u1',
    from_on: '2026-11-08',
    to_on: '2026-12-08',
    status: 'draft',
    seed: 42,
    stale: false,
    version: 4,
    indicators,
    created_at: '2026-11-01T10:00:00Z',
    updated_at: '2026-11-01T10:00:00Z',
  },
  items: plan.items.map((i) => ({
    allocation_id: i.allocation_id,
    kind: i.kind,
    origin: 'area' as const,
    ...(i.vessel ? { vessel: i.vessel } : {}),
    x_cm: i.x_cm,
    y_cm: i.y_cm,
    rotation_dd: i.rotation_dd,
    pinned: false,
    first_night: i.first_night,
    last_night: i.last_night,
    arrival_on: i.arrival_on,
    exit_on: i.exit_on,
    base_version: i.version,
  })),
  moves: [
    {
      seq: 1,
      allocation_id: 'al-b',
      kind: 'move',
      to: { resource_type: 'dynamic_area', resource_id: AREA_ID, x_cm: 1500, y_cm: 0 },
      move_on: '2026-11-10',
    },
  ],
  issues: [],
  access: [],
};

export { indicators };
