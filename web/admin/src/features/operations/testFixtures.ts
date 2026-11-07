// Fixtures of the operational view tests (imported by tests only): the M1 map document with berths b1..b3
// and a day view on it.
import type {
  DayElementData,
  DayViewData,
  MapDocument,
  MapSummary,
  Permission,
} from '@porto/shared/api';

import { MARINA_ID } from '../../test/renderApp';

export const base = `/api/admin/v1/marinas/${MARINA_ID}`;
export const DATE = '2026-11-07';

export const summary = (doc: MapDocument): MapSummary => ({
  id: doc.id!,
  kind: doc.kind,
  name: doc.name,
  icon: '',
  width_cm: doc.width_cm,
  height_cm: doc.height_cm,
  sort_order: 0,
  version: doc.version!,
  counts: {
    structures: 1,
    berths: doc.berths.length,
    dynamic_areas: 0,
    land_resources: 0,
    pedestals: 0,
    decorations: 0,
  },
  georeferenced: false,
  has_background: false,
  updated_at: '2026-11-06T10:00:00Z',
});

const el = (id: string, code: string, over: Partial<DayElementData> = {}): DayElementData => ({
  resource_type: 'berth',
  resource_id: id,
  code,
  status: 'free',
  arrival_today: false,
  departure_today: false,
  anomalies: [],
  ...over,
});

export function makeDayView(over: Partial<DayViewData> = {}): DayViewData {
  return {
    map_id: 'map-1',
    date: DATE,
    map_version: 3,
    generated_at: '2026-11-07T08:00:00Z',
    elements: [
      el('b1', 'A-1'),
      el('b2', 'A-2', {
        status: 'contract',
        allocation_id: 'al-2',
        allocation_version: 1,
        kind: 'contract',
        contract: {
          id: 'c2',
          kind: 'mooring',
          term: 'annual',
          status: 'active',
          code: 'ORM-2026-0001',
        },
        vessel: {
          id: 'v2',
          name: 'Aurora',
          loa_cm: 1100,
          beam_cm: 380,
          hull_type: 'monohull_sail',
        },
        presence: { state: 'in_port', since: '2026-11-01T10:00:00Z', away_days: 0 },
      }),
      el('b3', 'A-3', {
        status: 'transit',
        allocation_id: 'al-3',
        allocation_version: 2,
        kind: 'transit',
        contract: { id: 'c3', kind: 'transit', status: 'confirmed', code: 'TRN-2026-0001' },
        vessel: {
          id: 'v3',
          name: 'Brezza',
          loa_cm: 1000,
          beam_cm: 350,
          hull_type: 'monohull_motor',
        },
        arrival_today: true,
        anomalies: ['not_arrived'],
      }),
    ],
    area_placements: [],
    unplaced: [],
    counts: { anomalies: 1 },
    ...over,
  };
}

export const ALL_OPS: Permission[] = [
  'marina.read',
  'map.view',
  'occupancy.move',
  'transits.desk',
  'contracts.write',
  'contracts.read',
  'presence.write',
  'customers.read',
];
