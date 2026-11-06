// Test fixtures of the map feature (imported by tests only).
import type { Berth, MapDocument, Structure } from '@porto/shared/api';

const pier: Structure = {
  id: 'pier-a',
  kind: 'pier',
  code: 'A',
  points: [
    { x: 1000, y: 0 },
    { x: 1000, y: 10_000 },
  ],
  width_cm: 300,
  sides: ['left', 'right'],
};

export function berth(id: string, over: Partial<Berth> = {}): Berth {
  return {
    id,
    code: id.toUpperCase(),
    structure_id: 'pier-a',
    side: 'left',
    position: 10,
    anchor: { x: 1150, y: 1000 },
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
  };
}

export function makeMapDocument(over: Partial<MapDocument> = {}): MapDocument {
  return {
    id: 'map-1',
    version: 3,
    updated_at: '2026-11-06T10:00:00Z',
    kind: 'water',
    name: { it: 'Porto' },
    icon: '',
    width_cm: 50_000,
    height_cm: 30_000,
    grid_cm: 50,
    sort_order: 0,
    background: null,
    georef: null,
    concession_outline: null,
    water_polygons: [],
    land_polygons: [],
    structures: [pier],
    berths: [
      berth('b1', { position: 10, code: 'A-1' }),
      berth('b2', { position: 20, code: 'A-2', anchor: { x: 1150, y: 1400 } }),
      berth('b3', {
        position: 10,
        code: 'A-3',
        side: 'right',
        anchor: { x: 850, y: 1000 },
        rotation_dd: 1800,
        group_ids: ['g1'],
      }),
    ],
    dynamic_areas: [],
    land_resources: [],
    pedestals: [],
    decorations: [],
    ...over,
  };
}
