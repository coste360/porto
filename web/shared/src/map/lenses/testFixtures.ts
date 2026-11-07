// Day-view fixtures of the lens tests (imported by tests only).
import type { AreaPlacement, DayElement, DayView } from './types';

export const DATE = '2026-11-07';

export const el = (id: string, over: Partial<DayElement> = {}): DayElement => ({
  resource_type: 'berth',
  resource_id: id,
  code: id.toUpperCase(),
  status: 'free',
  arrival_today: false,
  departure_today: false,
  anomalies: [],
  ...over,
});

export const vessel = (name: string, over: Partial<NonNullable<DayElement['vessel']>> = {}) => ({
  id: `v-${name}`,
  name,
  loa_cm: 1200,
  beam_cm: 400,
  hull_type: 'monohull_sail' as const,
  ...over,
});

export const placement = (id: string, over: Partial<AreaPlacement> = {}): AreaPlacement => ({
  area_id: 'area-1',
  allocation_id: id,
  allocation_version: 1,
  x_cm: 0,
  cells: { first: 0, last: 4 },
  status: 'transit',
  kind: 'transit',
  vessel: vessel(id),
  anomalies: [],
  ...over,
});

/** A day of a small harbour: every occupancy status once, a transit in the linear area. */
export function makeDayView(over: Partial<DayView> = {}): DayView {
  return {
    map_id: 'map-1',
    date: DATE,
    map_version: 3,
    generated_at: '2026-11-07T08:00:00Z',
    elements: [
      el('a1'),
      el('a2', {
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
        vessel: vessel('Aurora'),
        customer: { id: 'cu1', display_name: 'Rossi Mario' },
        presence: { state: 'in_port', since: '2026-11-01T10:00:00Z', away_days: 0 },
        fit: { fill_bp: 8800, override: false, blocking_codes: [] },
        deadlines: { insurance: 'ok', documents: 'ok', contract_ends_on: '2027-10-31' },
        utilities: { power_a: 32, water: true },
      }),
      el('a3', {
        status: 'transit',
        allocation_id: 'al-3',
        allocation_version: 2,
        kind: 'transit',
        contract: { id: 'c3', kind: 'transit', status: 'confirmed', code: 'TRN-2026-0001' },
        vessel: vessel('Brezza', { hull_type: 'catamaran', loa_cm: 1100, beam_cm: 600 }),
        arrival_today: true,
        fit: { fill_bp: 5000, override: false, blocking_codes: [] },
        deadlines: { insurance: 'missing', documents: 'ok' },
        anomalies: ['not_arrived'],
      }),
      el('a4', {
        status: 'hold',
        allocation_id: 'al-4',
        allocation_version: 1,
        kind: 'hold',
        hold_expires_at: '2026-11-07T12:00:00Z',
      }),
      el('a5', {
        status: 'released',
        allocation_id: 'al-5',
        kind: 'contract',
        contract: {
          id: 'c5',
          kind: 'mooring',
          term: 'seasonal',
          status: 'active',
          code: 'ORM-2026-0002',
        },
        vessel: vessel('Cometa', { hull_type: 'monohull_motor' }),
        presence: { state: 'out', since: '2026-10-01T10:00:00Z', away_days: 37 },
        release: { id: 'r5', from: DATE, to: '2026-11-10', resold: false },
        fit: { fill_bp: 7000, override: true, blocking_codes: ['loa_exceeds'] },
        deadlines: { insurance: 'expiring', documents: 'ok', contract_ends_on: '2026-11-20' },
        anomalies: ['vessel_exceeds_limits', 'insurance_expired_active'],
      }),
      el('a6', { status: 'block', allocation_id: 'al-6', kind: 'block' }),
      el('a7', { status: 'maintenance', allocation_id: 'al-7', kind: 'maintenance' }),
      el('a8', { status: 'closed' }),
      el('area-1', { resource_type: 'dynamic_area', code: 'L1' }),
    ],
    area_placements: [
      placement('al-9', {
        x_cm: 300,
        contract: { id: 'c9', kind: 'transit', status: 'arrived', code: 'TRN-2026-0002' },
        vessel: vessel('Delfino', { hull_type: 'rib', loa_cm: 600, beam_cm: 250 }),
        presence: { state: 'in_port', since: '2026-11-06T15:00:00Z', away_days: 0 },
      }),
      placement('al-8', { x_cm: 0, vessel: vessel('Eolo') }),
    ],
    unplaced: [],
    counts: { anomalies: 3 },
    ...over,
  };
}
