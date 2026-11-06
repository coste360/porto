// Scene model of the renderer: the elements of a map document with their kind, ref and bounds, plus the
// render geometry derived from them (pier outlines, structure edges, linear area bands). Floats, render only.
import type {
  Berth,
  Decoration,
  DynamicArea,
  LandResource,
  MapElements,
  Pedestal,
  Structure,
} from '../api/schema';
import {
  boundsOf,
  expandBox,
  offsetPolyline,
  pointAt,
  rectCorners,
  subPolyline,
  type Box,
  type Pt,
} from './geometry';

export type ElementKind = 'structure' | 'berth' | 'area' | 'land' | 'pedestal' | 'decoration';

export type ElementOf<K extends ElementKind> = K extends 'structure'
  ? Structure
  : K extends 'berth'
    ? Berth
    : K extends 'area'
      ? DynamicArea
      : K extends 'land'
        ? LandResource
        : K extends 'pedestal'
          ? Pedestal
          : Decoration;

export type AnyElement = Structure | Berth | DynamicArea | LandResource | Pedestal | Decoration;

export type SceneEntry =
  | { kind: 'structure'; el: Structure }
  | { kind: 'berth'; el: Berth }
  | { kind: 'area'; el: DynamicArea }
  | { kind: 'land'; el: LandResource }
  | { kind: 'pedestal'; el: Pedestal }
  | { kind: 'decoration'; el: Decoration };

export type SceneItem = SceneEntry & { ref: string; box: Box };

/** Map elements keyed by the API collection name, in the order they are drawn (bottom to top). */
export const KIND_COLLECTIONS = {
  structure: 'structures',
  area: 'dynamic_areas',
  land: 'land_resources',
  berth: 'berths',
  pedestal: 'pedestals',
  decoration: 'decorations',
} as const satisfies Record<ElementKind, keyof MapElements>;

export const DRAW_ORDER: readonly ElementKind[] = [
  'structure',
  'area',
  'land',
  'berth',
  'pedestal',
  'decoration',
];

/** The ref of an element: its id when saved, its client key when new. */
export function refOf(el: { id?: string; key?: string }): string {
  return el.id ?? el.key ?? '';
}

export const PEDESTAL_SIZE_CM = 120;
export const DEFAULT_DECORATION_SIZE_CM = 600;
export const DEFAULT_LINEAR_DEPTH_CM = 500;

export function isBuoy(b: Berth): boolean {
  return b.mooring_type === 'buoy';
}

/** Polyline points of a structure (or the finger rect corners). */
export function structurePoints(s: Structure): Pt[] {
  if (s.kind === 'finger' && s.rect) return rectCorners(s.rect);
  return s.points ?? [];
}

/** Whether the structure is drawn as a filled band of `width_cm` around its axis. */
export function isBand(s: Structure): boolean {
  return (
    !s.closed &&
    (s.kind === 'pier' || s.kind === 'walkway' || s.kind === 'breakwater') &&
    !!s.width_cm
  );
}

/** Outline polygon of a band structure (pier, walkway, breakwater line). */
export function bandOutline(points: readonly Pt[], width: number): Pt[] {
  if (points.length < 2) return [];
  const left = offsetPolyline(points, -width / 2);
  const right = offsetPolyline(points, width / 2);
  return [...left, ...right.reverse()];
}

export type StructureEdge = { side: 'left' | 'right'; points: Pt[] };

/**
 * Mooring edges of a structure: for a band structure the two side edges at ±width/2 from the axis, for a quay
 * (a line) the line itself on its mooring side.
 */
export function structureEdges(s: Structure): StructureEdge[] {
  const pts = s.points ?? [];
  if (s.closed || pts.length < 2) return [];
  if (isBand(s)) {
    const half = (s.width_cm ?? 0) / 2;
    return [
      { side: 'left', points: offsetPolyline(pts, -half) },
      { side: 'right', points: offsetPolyline(pts, half) },
    ];
  }
  if (s.kind === 'quay' || s.kind === 'walkway') {
    const sides = (s.sides ?? []).filter(
      (x): x is 'left' | 'right' => x === 'left' || x === 'right',
    );
    const side = sides[0] ?? 'left';
    return [{ side, points: pts.map((p) => ({ ...p })) }];
  }
  return [];
}

/** Occupiable band of a linear dynamic area on its structure side (render only). */
export function linearAreaPolygon(a: DynamicArea, structure: Structure | undefined): Pt[] {
  if (!structure || a.from_cm === undefined || a.to_cm === undefined) return [];
  const depth =
    (a.max_beam_cm ?? DEFAULT_LINEAR_DEPTH_CM) +
    (a.mooring_mode === 'alongside' ? (a.margin_cm ?? 100) : 0);
  const axis = structure.points ?? [];
  if (axis.length < 2) return [];
  if (a.side === 'head') {
    const end = pointAt(axis, Number.MAX_SAFE_INTEGER);
    const half = (structure.width_cm ?? 0) / 2;
    const u = {
      x: Math.cos((end.dd / 10) * (Math.PI / 180)),
      y: Math.sin((end.dd / 10) * (Math.PI / 180)),
    };
    const left = { x: u.y, y: -u.x };
    const a0 = { x: end.point.x + left.x * half, y: end.point.y + left.y * half };
    const a1 = { x: end.point.x - left.x * half, y: end.point.y - left.y * half };
    return [
      a0,
      a1,
      { x: a1.x + u.x * depth, y: a1.y + u.y * depth },
      { x: a0.x + u.x * depth, y: a0.y + u.y * depth },
    ];
  }
  const edges = structureEdges(structure);
  const edge = edges.find((e) => e.side === a.side) ?? edges[0];
  if (!edge) return [];
  const sub = subPolyline(edge.points, a.from_cm, a.to_cm);
  if (sub.length < 2) return [];
  const outward = edge.side === 'left' ? -depth : depth;
  return [...sub, ...offsetPolyline(sub, outward).reverse()];
}

function circleBox(c: Pt, r: number): Box {
  return { minX: c.x - r, minY: c.y - r, maxX: c.x + r, maxY: c.y + r };
}

/** Bounds of one element. Linear areas need their structure. */
export function entryBounds(entry: SceneEntry, structures: ReadonlyMap<string, Structure>): Box {
  switch (entry.kind) {
    case 'structure': {
      const s = entry.el;
      const pts = structurePoints(s);
      const box = boundsOf(pts);
      return isBand(s) ? expandBox(box, (s.width_cm ?? 0) / 2) : box;
    }
    case 'berth': {
      const b = entry.el;
      if (isBuoy(b)) return circleBox(b.anchor, b.swing_radius_cm ?? b.length_cm / 2);
      return boundsOf(
        rectCorners({
          anchor: b.anchor,
          width_cm: b.width_cm,
          length_cm: b.length_cm,
          rotation_dd: b.rotation_dd,
        }),
      );
    }
    case 'area': {
      const a = entry.el;
      if (a.kind === 'surface') return boundsOf(a.outline ?? []);
      const s = a.structure_id ? structures.get(a.structure_id) : undefined;
      return boundsOf(linearAreaPolygon(a, s));
    }
    case 'land': {
      const l = entry.el;
      return boundsOf(
        rectCorners({
          anchor: l.anchor,
          width_cm: l.width_cm,
          length_cm: l.length_cm,
          rotation_dd: l.rotation_dd,
        }),
      );
    }
    case 'pedestal':
      return circleBox(entry.el.position, PEDESTAL_SIZE_CM / 2);
    case 'decoration': {
      const d = entry.el;
      if (d.points && d.points.length > 0) return boundsOf(d.points);
      return circleBox(d.position ?? { x: 0, y: 0 }, (d.size_cm ?? DEFAULT_DECORATION_SIZE_CM) / 2);
    }
  }
}

/** Every element of a document as scene entries (structures first, as drawn). */
export function entriesOf(elements: MapElements): SceneEntry[] {
  const out: SceneEntry[] = [];
  for (const el of elements.structures) out.push({ kind: 'structure', el });
  for (const el of elements.dynamic_areas) out.push({ kind: 'area', el });
  for (const el of elements.land_resources) out.push({ kind: 'land', el });
  for (const el of elements.berths) out.push({ kind: 'berth', el });
  for (const el of elements.pedestals) out.push({ kind: 'pedestal', el });
  for (const el of elements.decorations) out.push({ kind: 'decoration', el });
  return out;
}

export function structuresByRef(entries: Iterable<SceneEntry>): Map<string, Structure> {
  const out = new Map<string, Structure>();
  for (const e of entries) if (e.kind === 'structure') out.set(refOf(e.el), e.el);
  return out;
}

/** Scene items with bounds, from scene entries. */
export function buildScene(entries: readonly SceneEntry[]): SceneItem[] {
  const structures = structuresByRef(entries);
  return entries.map(
    (e) => ({ ...e, ref: refOf(e.el), box: entryBounds(e, structures) }) as SceneItem,
  );
}

export function emptyElements(): MapElements {
  return {
    structures: [],
    berths: [],
    dynamic_areas: [],
    land_resources: [],
    pedestals: [],
    decorations: [],
  };
}

export function countElements(elements: MapElements): number {
  return (
    elements.structures.length +
    elements.berths.length +
    elements.dynamic_areas.length +
    elements.land_resources.length +
    elements.pedestals.length +
    elements.decorations.length
  );
}
