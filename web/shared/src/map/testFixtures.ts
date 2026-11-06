// Test fixtures of the map renderer (imported by tests only).
import type { Berth } from '../api/schema';

export const berth = (over: Partial<Berth> = {}): Berth => ({
  key: 'b',
  code: 'A-1',
  side: 'left',
  position: 10,
  anchor: { x: 150, y: 500 },
  rotation_dd: 0,
  width_cm: 400,
  length_cm: 1200,
  mooring_type: 'mooring_line',
  overhang_allowed: false,
  water: false,
  wifi: false,
  attributes: [],
  online_sellable: false,
  active: true,
  group_ids: [],
  ...over,
});
