import type { Dashboard, Tile } from '@porto/shared/api';

// Fixtures of the M7 dashboard tests (shape of GET .../dashboard).

export const productionTile: Tile = {
  key: 'production',
  unit: 'cents',
  available: true,
  dims_applied: false,
  value: 1234500,
  previous: { value: 1000000 },
  delta: 234500,
  delta_bp: 2345,
  trend: 'up',
  series: [
    { bucket: '2026-01-01', value: 400000, previous: 300000 },
    { bucket: '2026-02-01', value: 834500, previous: 700000 },
  ],
  drill: { report: 'production', params: {} },
};

export const occupancyTile: Tile = {
  key: 'occupancy',
  unit: 'bp',
  available: true,
  dims_applied: true,
  value: 6250,
  numerator: 5,
  denominator: 8,
  previous: { value: 6250, numerator: 5, denominator: 8 },
  delta: 0,
  delta_bp: 0,
  trend: 'flat',
  series: [],
  drill: {
    report: 'occupancy',
    params: { variant: 'physical', family: 'berth', map_kind: 'water' },
  },
};

export const newCustomersTile: Tile = {
  key: 'new_customers',
  unit: 'count',
  available: true,
  dims_applied: false,
  value: 3,
  trend: 'none',
  series: [],
  breakdown: [
    { key: 'IT', label: 'Italia', value: 2 },
    { key: 'FR', value: 1 },
  ],
  drill: { report: 'customers', params: { new: 'true' } },
};

export const yardTile: Tile = {
  key: 'yard_hours',
  unit: 'count',
  available: false,
  reason: 'milestone_m10',
  dims_applied: false,
  value: null,
  trend: 'none',
  series: [],
};

export function makeDashboard(tiles: Tile[], over: Partial<Dashboard> = {}): Dashboard {
  return {
    period: { from: '2026-01-01', to: '2026-11-08' },
    previous_period: { from: '2025-01-01', to: '2025-11-08' },
    today: '2026-11-08',
    compare: 'previous_year',
    align: 'date',
    granularity: 'month',
    filters: {},
    tiles,
    ...over,
  };
}

// M11 (m11-plan §11): the rental tiles, available since FLEET.
export const rentalUtilisationTile: Tile = {
  key: 'rental_utilisation',
  unit: 'bp',
  available: true,
  dims_applied: false,
  value: 2500,
  numerator: 10,
  denominator: 40,
  trend: 'none',
  series: [],
  drill: { report: 'rental', params: { view: 'utilisation' } },
};

export const rentalRevenueTile: Tile = {
  key: 'rental_revenue',
  unit: 'cents',
  available: true,
  dims_applied: false,
  value: 2108600,
  trend: 'none',
  series: [],
  drill: { report: 'rental', params: { view: 'revenue' } },
};
