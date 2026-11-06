/** Toggleable map layers (spec 8.1). */
export const LAYERS = [
  'background',
  'water_land',
  'grid',
  'structures',
  'berths',
  'areas',
  'land_resources',
  'pedestals',
  'decorations',
  'labels',
  'concession',
] as const;

export type LayerId = (typeof LAYERS)[number];
export type LayerVisibility = Readonly<Record<LayerId, boolean>>;

export const DEFAULT_LAYERS: LayerVisibility = {
  background: true,
  water_land: true,
  grid: false,
  structures: true,
  berths: true,
  areas: true,
  land_resources: true,
  pedestals: true,
  decorations: true,
  labels: true,
  concession: true,
};

export function toggleLayer(layers: LayerVisibility, id: LayerId, on: boolean): LayerVisibility {
  return { ...layers, [id]: on };
}
