// Client side of the day view (m3-plan §8.3): indexes by resource and by area, lens subjects, vessels to draw
// and the schema-mode tile layout. Pure; geometry here is render only (floats allowed, nothing is stored).
import type { Berth, Structure } from '../api/schema';
import { direction, pointAt, subPolyline, type Pt, type RectGeom } from './geometry';
import type { AreaPlacement, DayElement, DayView, LensSubject } from './lenses/types';
import { structureEdges, type SceneItem } from './scene';

export type DayIndex = {
  /** Day elements by resource id. */
  byResource: ReadonlyMap<string, DayElement>;
  /** Placements of each dynamic area, sorted by x then y. */
  placementsByArea: ReadonlyMap<string, readonly AreaPlacement[]>;
  /** Changes when the server recomputes the view (memo key). */
  version: string;
};

export function dayViewVersion(
  dv: Pick<DayView, 'map_id' | 'date' | 'generated_at' | 'map_version'>,
) {
  return `${dv.map_id}|${dv.date}|${dv.map_version}|${dv.generated_at}`;
}

export function indexDayView(dv: DayView): DayIndex {
  const byResource = new Map<string, DayElement>();
  for (const el of dv.elements) byResource.set(el.resource_id, el);
  const byArea = new Map<string, AreaPlacement[]>();
  for (const p of dv.area_placements) {
    const list = byArea.get(p.area_id) ?? [];
    list.push(p);
    byArea.set(p.area_id, list);
  }
  for (const list of byArea.values()) {
    list.sort((a, b) => a.x_cm - b.x_cm || (a.y_cm ?? 0) - (b.y_cm ?? 0));
  }
  return { byResource, placementsByArea: byArea, version: dayViewVersion(dv) };
}

/** Ref of a placement subject (a vessel in a dynamic area) in lens maps and on the map. */
export function placementRef(allocationId: string): string {
  return `alloc:${allocationId}`;
}

export function isPlacementRef(ref: string): boolean {
  return ref.startsWith('alloc:');
}

export function allocationOfRef(ref: string): string | null {
  return isPlacementRef(ref) ? ref.slice('alloc:'.length) : null;
}

export type DaySubject = {
  /** resource id (berth, land) or placementRef(allocation id) (area vessel). */
  ref: string;
  /** Resource the subject sits on (the area for a placement). */
  resourceId: string;
  resourceType: DayElement['resource_type'];
  code: string;
  subject: LensSubject;
  allocationId?: string;
  allocationVersion?: number;
};

/** Every element a lens colours: berths and land resources, plus every vessel placed in a dynamic area. */
export function daySubjects(dv: DayView): DaySubject[] {
  const out: DaySubject[] = [];
  const areaCodes = new Map<string, string>();
  for (const el of dv.elements) {
    if (el.resource_type === 'dynamic_area') {
      areaCodes.set(el.resource_id, el.code);
      continue;
    }
    out.push({
      ref: el.resource_id,
      resourceId: el.resource_id,
      resourceType: el.resource_type,
      code: el.code,
      subject: el,
      ...(el.allocation_id ? { allocationId: el.allocation_id } : {}),
      ...(el.allocation_version !== undefined ? { allocationVersion: el.allocation_version } : {}),
    });
  }
  for (const p of dv.area_placements) {
    out.push({
      ref: placementRef(p.allocation_id),
      resourceId: p.area_id,
      resourceType: 'dynamic_area',
      code: areaCodes.get(p.area_id) ?? '',
      allocationId: p.allocation_id,
      allocationVersion: p.allocation_version,
      subject: {
        status: p.status,
        kind: p.kind,
        contract: p.contract,
        vessel: p.vessel,
        customer: p.customer,
        presence: p.presence,
        arrival_today: false,
        departure_today: false,
        fit: p.fit,
        anomalies: p.anomalies,
      },
    });
  }
  return out;
}

// ---- vessels to draw ----

export type VesselItem = {
  /** Allocation id (memo key with version). */
  key: string;
  version: number;
  /** data-ref of the silhouette: the berth id on a berth, placementRef on an area. */
  ref: string;
  /** Interactive (area vessels are dragged by themselves; berth vessels belong to the berth). */
  interactive: boolean;
  loa: number;
  beam: number;
  hull: string;
  name: string;
  /** Stern position (local x = 0, y = 0) and heading of the bow in tenths of a degree. */
  origin: Pt;
  rotationDd: number;
};

/**
 * Silhouette pose on a berth: centred on the berth axis, bow toward the structure (the anchor is on the
 * structure edge and the berth extends along rotation_dd), so the bow touches the structure edge.
 */
export function berthVesselPose(b: RectGeom, loa: number): { origin: Pt; rotationDd: number } {
  const u = direction(b.rotation_dd);
  return {
    origin: { x: b.anchor.x + u.x * loa, y: b.anchor.y + u.y * loa },
    rotationDd: (b.rotation_dd + 1800) % 3600,
  };
}

/** Pose of a vessel in a linear area at x along the axis (stern_to: across the band; alongside: along). */
export function linearVesselPose(
  area: {
    from_cm?: number;
    to_cm?: number;
    side?: string;
    mooring_mode?: string;
    margin_cm?: number;
  },
  structure: Structure | undefined,
  v: { loa: number; beam: number },
  xCm: number,
): { origin: Pt; rotationDd: number } | null {
  if (!structure || area.from_cm === undefined || area.to_cm === undefined) return null;
  const edges = structureEdges(structure);
  const edge = edges.find((e) => e.side === area.side) ?? edges[0];
  if (!edge) return null;
  const sub = subPolyline(edge.points, area.from_cm, area.to_cm);
  if (sub.length < 2) return null;
  const margin = area.margin_cm ?? 0;
  const outward = edge.side === 'left' ? -1 : 1;
  if (area.mooring_mode === 'alongside') {
    // Along the axis: stern at x + margin/2, bow toward increasing x, half margin off the edge.
    const at = pointAt(sub, xCm + margin / 2);
    const n = direction(at.dd + 900 * outward);
    const off = margin / 2 + v.beam / 2;
    return { origin: { x: at.point.x + n.x * off, y: at.point.y + n.y * off }, rotationDd: at.dd };
  }
  // Stern-to: centred in the footprint [x, x + beam + margin), bow toward the structure.
  const at = pointAt(sub, xCm + (v.beam + margin) / 2);
  const dd = (at.dd + 900 * outward + 3600) % 3600;
  const n = direction(dd);
  return {
    origin: { x: at.point.x + n.x * v.loa, y: at.point.y + n.y * v.loa },
    rotationDd: (dd + 1800) % 3600,
  };
}

/** Pose of a vessel in a surface area: (x, y) is the top-left of its footprint in grid space. */
export function surfaceVesselPose(
  area: { outline?: Pt[]; grid_rotation_dd: number; margin_cm?: number },
  origin: { x: number; y: number },
  v: { loa: number; beam: number },
  p: { x: number; y: number; rotationDd: number },
): { origin: Pt; rotationDd: number } {
  const half = (area.margin_cm ?? 0) / 2;
  const horizontal = p.rotationDd === 0 || p.rotationDd === 1800;
  const wx = horizontal ? v.loa : v.beam;
  const wy = horizontal ? v.beam : v.loa;
  const cx = origin.x + p.x + half + wx / 2;
  const cy = origin.y + p.y + half + wy / 2;
  // Stern position in grid space, then rotated back by the grid rotation around the outline pivot.
  const u = direction(p.rotationDd);
  const stern = { x: cx - (u.x * v.loa) / 2, y: cy - (u.y * v.loa) / 2 };
  const pivot = area.outline?.[0] ?? { x: 0, y: 0 };
  const g = direction(area.grid_rotation_dd);
  const dx = stern.x - pivot.x;
  const dy = stern.y - pivot.y;
  return {
    origin: { x: pivot.x + dx * g.x - dy * g.y, y: pivot.y + dx * g.y + dy * g.x },
    rotationDd: (p.rotationDd + area.grid_rotation_dd) % 3600,
  };
}

// ---- schema mode ----

/** Minimum tile width (cm) so that the code of a tile is at least minPx at pxPerCm (font = 0.42 × width). */
export function schemaMinTileCm(pxPerCm: number, minPx = 11): number {
  return pxPerCm > 0 ? Math.ceil(minPx / (0.42 * pxPerCm)) : 0;
}

/**
 * Schema-mode layout (spec 9.1): berths of each structure side keep their order but every tile is at least
 * minTileCm wide, so a pier gets longer than in scale. Returns the tile rect of each berth id; buoys and berths
 * without structure keep their geometry. Pure.
 */
export function schemaTiles(berths: readonly Berth[], minTileCm: number): Map<string, RectGeom> {
  const out = new Map<string, RectGeom>();
  const groups = new Map<string, Berth[]>();
  for (const b of berths) {
    const ref = b.id ?? b.key ?? '';
    if (!b.structure_id || b.mooring_type === 'buoy') {
      out.set(ref, {
        anchor: b.anchor,
        width_cm: b.width_cm,
        length_cm: b.length_cm,
        rotation_dd: b.rotation_dd,
      });
      continue;
    }
    const k = `${b.structure_id}|${b.side}`;
    const list = groups.get(k) ?? [];
    list.push(b);
    groups.set(k, list);
  }
  for (const list of groups.values()) {
    list.sort((a, b) => a.position - b.position);
    const first = list[0]!;
    const last = list[list.length - 1]!;
    // Axis along the structure: perpendicular to the berth direction, oriented from the first to the last berth.
    const n = direction(first.rotation_dd + 900);
    const dot = (last.anchor.x - first.anchor.x) * n.x + (last.anchor.y - first.anchor.y) * n.y;
    const sign = dot < 0 ? -1 : 1;
    let offset = -first.width_cm / 2;
    for (const b of list) {
      const w = Math.max(b.width_cm, minTileCm);
      const centre = offset + w / 2;
      out.set(b.id ?? b.key ?? '', {
        anchor: {
          x: first.anchor.x + n.x * sign * centre,
          y: first.anchor.y + n.y * sign * centre,
        },
        width_cm: w,
        length_cm: b.length_cm,
        rotation_dd: b.rotation_dd,
      });
      offset += w;
    }
  }
  return out;
}

/** The scene with berths replaced by their schema tiles (bounds recomputed by the caller's buildScene). */
export function schemaBerths(
  items: readonly SceneItem[],
  minTileCm: number,
): Map<string, RectGeom> {
  const berths: Berth[] = [];
  for (const it of items) if (it.kind === 'berth') berths.push(it.el);
  return schemaTiles(berths, minTileCm);
}
