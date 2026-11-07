// Drop targets of the drag (m3-plan §8.5): hit testing in map coordinates, fit targets of berths and areas,
// area placements through the TS footprint mirror, and the ghost geometry. Render-side floats are rounded to
// integer cm before any footprint or fit call.
import type { Berth, DynamicArea, LandResource, Structure } from '@porto/shared/api';
import {
  FootprintError,
  footprint,
  DEFAULT_LINEAR_DEPTH_CM,
  linearAreaPolygon,
  pointInPolygon,
  projectOntoPolyline,
  rectCorners,
  roundHalfEven,
  structureEdges,
  subPolyline,
  surfaceAreaOf,
  type FitTarget,
  type FootprintArea,
  type Pt,
  type SceneItem,
} from '@porto/shared/map';

import { axisWidth, snapAxis, type AxisSpan } from './axis';
import type { BusyEntry } from './busy';

export function hitTest(items: readonly SceneItem[], p: Pt): SceneItem | null {
  // Berths and land on top of areas (draw order).
  let area: SceneItem | null = null;
  for (const it of items) {
    if (it.kind === 'berth' || it.kind === 'land') {
      const el = it.el;
      if (pointInPolygon(p, rectCorners(el))) return it;
    } else if (it.kind === 'area' && !area) {
      area = it;
    }
  }
  return area;
}

/**
 * Whether p is inside an area's occupiable band or outline. `reachCm` widens the band of a linear area to the
 * depth a vessel takes from the quay (its LOA when moored stern-to), so a vessel grabbed by its middle can be
 * dropped where it is drawn.
 */
export function inArea(
  a: DynamicArea,
  structure: Structure | undefined,
  p: Pt,
  reachCm = 0,
): boolean {
  const band =
    a.kind !== 'surface' && reachCm > (a.max_beam_cm ?? DEFAULT_LINEAR_DEPTH_CM)
      ? { ...a, max_beam_cm: reachCm }
      : a;
  const poly = a.kind === 'surface' ? (a.outline ?? []) : linearAreaPolygon(band, structure);
  return poly.length >= 3 && pointInPolygon(p, poly);
}

const tagged = (b: Berth, tag: string) => b.attributes.includes(tag);

export function berthFitTarget(b: Berth): FitTarget {
  return {
    kind: 'berth',
    maxLoa: b.max_loa_cm ?? b.length_cm,
    maxBeam: b.max_beam_cm ?? b.width_cm,
    length: b.length_cm,
    width: b.width_cm,
    depth: b.depth_cm ?? null,
    maxAirDraft: b.max_air_draft_cm ?? null,
    multihullOk: tagged(b, 'multihull_ok'),
    overhangAllowed: b.overhang_allowed,
    powerA: b.power_a ?? null,
  };
}

export function landFitTarget(l: LandResource): FitTarget {
  return {
    kind: 'land',
    maxLoa: l.length_cm,
    maxBeam: l.width_cm,
    length: l.length_cm,
    width: l.width_cm,
    multihullOk: true,
    overhangAllowed: false,
    landKind: l.kind,
  };
}

export function areaFitTarget(a: DynamicArea): FitTarget {
  return {
    kind: a.kind === 'surface' ? 'surface_area' : 'linear_area',
    maxLoa: a.max_loa_cm ?? 0,
    maxBeam: a.max_beam_cm ?? 0,
    length: 0,
    width: 0,
    maxAirDraft: a.kind === 'surface' ? (a.max_height_cm ?? null) : null,
    multihullOk: true,
    overhangAllowed: false,
  };
}

/** Footprint view of an area (linear: length from from_cm/to_cm). */
export function footprintArea(
  a: DynamicArea,
): FootprintArea & { originX: number; originY: number } {
  if (a.kind === 'surface') {
    return surfaceAreaOf({
      outline: a.outline ?? [],
      grid_rotation_dd: a.grid_rotation_dd,
      cell_cm: a.cell_cm ?? 100,
      margin_cm: a.margin_cm ?? 0,
      blocked_cells: a.blocked_cells,
    });
  }
  return {
    kind: 'linear',
    length: (a.to_cm ?? 0) - (a.from_cm ?? 0),
    mode: a.mooring_mode ?? 'stern_to',
    cell: a.cell_cm ?? 100,
    margin: a.margin_cm ?? 0,
    cols: 0,
    rows: 0,
    usable: [],
    originX: 0,
    originY: 0,
  };
}

/** Footprints of the busy neighbours of a linear area along the axis, for the same nights. */
export function axisNeighbours(entries: readonly BusyEntry[], a: DynamicArea): AxisSpan[] {
  const margin = a.margin_cm ?? 0;
  return entries
    .filter((e) => e.xCm !== undefined && e.loa !== undefined && e.beam !== undefined)
    .map((e) => ({
      x: e.xCm!,
      w: axisWidth({ loa: e.loa!, beam: e.beam! }, a.mooring_mode, margin),
    }));
}

export type AreaCandidate = {
  x: number;
  y?: number;
  rotation?: number;
  cells: number[];
  error?: string;
  free?: { left: number; right: number };
};

/**
 * Placement of a vessel dragged at map point p in an area: linear areas slide along the axis with snapping,
 * surfaces take the footprint's top-left in grid space (10 cm snap) with the rotation.
 */
export function areaCandidate(
  a: DynamicArea,
  structure: Structure | undefined,
  v: { loa: number; beam: number },
  p: Pt,
  neighbours: readonly AxisSpan[],
  rotation = 0,
): AreaCandidate {
  const fa = footprintArea(a);
  const vessel = { loa: v.loa, beam: v.beam };
  if (a.kind !== 'surface') {
    const edges = structure ? structureEdges(structure) : [];
    const edge = edges.find((e) => e.side === a.side) ?? edges[0];
    const sub = edge ? subPolyline(edge.points, a.from_cm ?? 0, a.to_cm ?? 0) : [];
    const proj = sub.length >= 2 ? projectOntoPolyline(p, sub) : null;
    const w = axisWidth(vessel, a.mooring_mode, fa.margin);
    const s = snapAxis({
      rawX: roundHalfEven((proj?.s ?? 0) - w / 2),
      w,
      length: fa.length,
      margin: fa.margin,
      neighbours,
    });
    try {
      const cells = footprint(vessel, { x: s.x, y: 0, rotation: 0 }, fa);
      return { x: s.x, cells: cells.indexes, free: { left: s.freeLeft, right: s.freeRight } };
    } catch (e) {
      return { x: s.x, cells: [], error: e instanceof FootprintError ? e.code : 'outside_area' };
    }
  }
  // Surface: point to grid space (rotate back around the outline pivot), footprint centred on the pointer.
  const pivot = a.outline?.[0] ?? { x: 0, y: 0 };
  const rad = (-a.grid_rotation_dd / 10) * (Math.PI / 180);
  const dx = p.x - pivot.x;
  const dy = p.y - pivot.y;
  const gx = pivot.x + dx * Math.cos(rad) - dy * Math.sin(rad) - fa.originX;
  const gy = pivot.y + dx * Math.sin(rad) + dy * Math.cos(rad) - fa.originY;
  const horizontal = rotation === 0 || rotation === 1800;
  const wx = (horizontal ? v.loa : v.beam) + fa.margin;
  const wy = (horizontal ? v.beam : v.loa) + fa.margin;
  const x = Math.max(0, roundHalfEven((gx - wx / 2) / 10) * 10);
  const y = Math.max(0, roundHalfEven((gy - wy / 2) / 10) * 10);
  try {
    const cells = footprint(vessel, { x, y, rotation }, fa);
    return { x, y, rotation, cells: cells.indexes };
  } catch (e) {
    return {
      x,
      y,
      rotation,
      cells: [],
      error: e instanceof FootprintError ? e.code : 'outside_area',
    };
  }
}

/** Depth from the quay a vessel takes in a linear area: its LOA stern-to, nothing extra alongside. */
export function reachOf(a: DynamicArea, loaCm: number): number {
  return a.kind !== 'surface' && a.mooring_mode !== 'alongside' ? loaCm : 0;
}
