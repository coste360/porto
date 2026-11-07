// Joins the map document and the day view for the operational map: static resource attributes (filters),
// vessels in scale, schema tiles, resource lookups. Pure, memoised by the page per (map version, day version).
import type { DayViewData, DynamicArea, MapDocument, Structure } from '@porto/shared/api';
import {
  berthVesselPose,
  buildScene,
  entriesOf,
  linearVesselPose,
  placementRef,
  surfaceAreaOf,
  surfaceVesselPose,
  type ResourceMeta,
  type SceneEntry,
  type SceneItem,
  type VesselItem,
  schemaTiles,
} from '@porto/shared/map';

export function resourceMeta(doc: MapDocument): Map<string, ResourceMeta> {
  const out = new Map<string, ResourceMeta>();
  for (const b of doc.berths) {
    if (!b.id) continue;
    out.set(b.id, {
      ...(b.structure_id ? { structureId: b.structure_id } : {}),
      groupIds: b.group_ids,
      ...(b.price_class ? { category: b.price_class } : {}),
    });
  }
  for (const a of doc.dynamic_areas) {
    if (!a.id) continue;
    out.set(a.id, {
      ...(a.structure_id ? { structureId: a.structure_id } : {}),
      groupIds: a.group_ids,
      ...(a.price_class ? { category: a.price_class } : {}),
    });
  }
  for (const l of doc.land_resources) {
    if (!l.id) continue;
    out.set(l.id, {
      groupIds: l.group_ids ?? [],
      ...(l.price_class ? { category: l.price_class } : {}),
    });
  }
  return out;
}

export function structuresById(doc: MapDocument): Map<string, Structure> {
  return new Map(doc.structures.filter((s) => s.id).map((s) => [s.id!, s]));
}

export function areasById(doc: MapDocument): Map<string, DynamicArea> {
  return new Map(doc.dynamic_areas.filter((a) => a.id).map((a) => [a.id!, a]));
}

/** Scene in scale, or with berths as schema tiles (areas and land stay in scale, spec 9.1). */
export function opsScene(
  doc: MapDocument,
  mode: 'scale' | 'schema',
  minTileCm: number,
): SceneItem[] {
  const entries = entriesOf(doc);
  if (mode === 'scale') return buildScene(entries);
  const tiles = schemaTiles(doc.berths, minTileCm);
  const swapped: SceneEntry[] = entries.map((e) => {
    if (e.kind !== 'berth') return e;
    const t = tiles.get(e.el.id ?? e.el.key ?? '');
    return t ? { kind: 'berth', el: { ...e.el, anchor: t.anchor, width_cm: t.width_cm } } : e;
  });
  return buildScene(swapped);
}

/** Vessels to draw: on berths (from the day elements) and in dynamic areas (from the placements). */
export function opsVessels(doc: MapDocument, dv: DayViewData): VesselItem[] {
  const berths = new Map(doc.berths.filter((b) => b.id).map((b) => [b.id!, b]));
  const lands = new Map(doc.land_resources.filter((l) => l.id).map((l) => [l.id!, l]));
  const areas = areasById(doc);
  const structures = structuresById(doc);
  const out: VesselItem[] = [];
  for (const el of dv.elements) {
    if (!el.vessel || !el.allocation_id || el.resource_type === 'dynamic_area') continue;
    if (el.presence && el.presence.state !== 'in_port' && el.status !== 'transit') continue;
    const geom =
      el.resource_type === 'berth' ? berths.get(el.resource_id) : lands.get(el.resource_id);
    if (!geom || ('mooring_type' in geom && geom.mooring_type === 'buoy')) continue;
    const pose = berthVesselPose(geom, el.vessel.loa_cm);
    out.push({
      key: el.allocation_id,
      version: el.allocation_version ?? 0,
      ref: el.resource_id,
      interactive: false,
      loa: el.vessel.loa_cm,
      beam: el.vessel.beam_cm,
      hull: el.vessel.hull_type,
      name: el.vessel.name,
      ...pose,
    });
  }
  for (const p of dv.area_placements) {
    const area = areas.get(p.area_id);
    if (!area) continue;
    const v = { loa: p.vessel.loa_cm, beam: p.vessel.beam_cm };
    const pose = areaPose(area, structures, v, {
      x: p.x_cm,
      y: p.y_cm ?? 0,
      rotationDd: p.rotation_dd ?? 0,
    });
    if (!pose) continue;
    out.push({
      key: p.allocation_id,
      version: p.allocation_version,
      ref: placementRef(p.allocation_id),
      interactive: true,
      loa: v.loa,
      beam: v.beam,
      hull: p.vessel.hull_type,
      name: p.vessel.name,
      ...pose,
    });
  }
  return out;
}

/** Pose of a vessel at a placement in an area (linear or surface). */
export function areaPose(
  area: DynamicArea,
  structures: ReadonlyMap<string, Structure>,
  v: { loa: number; beam: number },
  p: { x: number; y: number; rotationDd: number },
) {
  if (area.kind === 'linear') {
    return linearVesselPose(
      area,
      area.structure_id ? structures.get(area.structure_id) : undefined,
      v,
      p.x,
    );
  }
  const grid = surfaceAreaOf({
    outline: area.outline ?? [],
    grid_rotation_dd: area.grid_rotation_dd,
    cell_cm: area.cell_cm ?? 100,
    margin_cm: area.margin_cm ?? 0,
    blocked_cells: area.blocked_cells,
  });
  return surfaceVesselPose(area, { x: grid.originX, y: grid.originY }, v, p);
}
