// Elements created by the drawing tools from the clicked (snapped) points. Integer cm everywhere.
import type { Berth, DynamicArea, LandResource, Pedestal, Structure } from '@porto/shared/api';
import { roundHalfEven, type KitAssetId, type Pt, type SceneEntry } from '@porto/shared/map';

import { addCommand, metaCommand, type Command } from '../model/commands';
import { familyCodes, occupiableCodes, uniqueCode, type EditorDocument } from '../model/document';
import { newKey } from '../model/keys';
import type { SnapResult } from '../model/snap';
import type { PolygonTarget, ToolId } from './index';

export const DEFAULTS = {
  berthWidthCm: 400,
  berthLengthCm: 1200,
  pierWidthCm: 300,
  walkwayWidthCm: 150,
  fingerWidthCm: 60,
  fingerLengthCm: 900,
  buoyRadiusCm: 1200,
  landWidthCm: 250,
  landLengthCm: 500,
  decorationSizeCm: 600,
  depthCm: 300,
  linearDepthCm: 500,
};

export type ToolOptions = {
  polygonTarget: PolygonTarget;
  landKind: LandResource['kind'];
  asset: KitAssetId;
  /** Default text of a new free text label (translated). */
  textLabel: string;
};

const LETTERS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';

function nextLetterCode(taken: ReadonlySet<string>): string {
  for (const l of LETTERS) if (!taken.has(l)) return l;
  return uniqueCode('A1', taken);
}

function structureCodeOf(doc: EditorDocument, ref: string | undefined): string | undefined {
  const e = ref ? doc.elements.get(ref) : undefined;
  return e?.kind === 'structure' ? e.el.code : undefined;
}

export function newBerth(doc: EditorDocument, snap: SnapResult, over: Partial<Berth> = {}): Berth {
  const prefix = structureCodeOf(doc, snap.structureRef);
  const code = uniqueCode(prefix ? `${prefix}-1` : 'P-1', occupiableCodes(doc));
  const berth: Berth = {
    key: newKey(doc),
    code,
    side: snap.side ?? 'none',
    position: snap.s ?? 0,
    anchor: snap.point,
    rotation_dd: snap.rotation_dd ?? 0,
    width_cm: DEFAULTS.berthWidthCm,
    length_cm: DEFAULTS.berthLengthCm,
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
  if (snap.structureRef) berth.structure_id = snap.structureRef;
  return berth;
}

/**
 * The command a creation tool produces from its clicks, or null when the clicks are not enough (or the tool
 * creates nothing: measure, calibrate).
 */
export function createFromClicks(
  tool: ToolId,
  clicks: readonly SnapResult[],
  doc: EditorDocument,
  options: ToolOptions,
): Command | null {
  const pts = clicks.map((c) => c.point);
  const first = clicks[0];
  const add = (label: string, entry: SceneEntry) => addCommand(label, [entry]);
  switch (tool) {
    case 'quay': {
      if (pts.length < 2) return null;
      const el: Structure = {
        key: newKey(doc),
        kind: 'quay',
        code: uniqueCode('BAN-1', familyCodes(doc, 'structure')),
        points: pts,
        sides: ['left'],
      };
      return add('editor:commands.addQuay', { kind: 'structure', el });
    }
    case 'pier':
    case 'walkway': {
      if (pts.length < 2) return null;
      const codes = familyCodes(doc, 'structure');
      const el: Structure = {
        key: newKey(doc),
        kind: tool,
        code: tool === 'pier' ? nextLetterCode(codes) : uniqueCode('PAS-1', codes),
        points: pts,
        width_cm: tool === 'pier' ? DEFAULTS.pierWidthCm : DEFAULTS.walkwayWidthCm,
        sides: tool === 'pier' ? ['left', 'right'] : [],
      };
      if (tool === 'pier' && first?.structureRef) el.parent_id = first.structureRef;
      return add(tool === 'pier' ? 'editor:commands.addPier' : 'editor:commands.addWalkway', {
        kind: 'structure',
        el,
      });
    }
    case 'finger': {
      if (!first?.structureRef || first.kind !== 'edge') return null;
      const pier = structureCodeOf(doc, first.structureRef) ?? 'F';
      const el: Structure = {
        key: newKey(doc),
        kind: 'finger',
        code: uniqueCode(`${pier}-F1`, familyCodes(doc, 'structure')),
        rect: {
          anchor: first.point,
          width_cm: DEFAULTS.fingerWidthCm,
          length_cm: DEFAULTS.fingerLengthCm,
          rotation_dd: first.rotation_dd ?? 0,
        },
        parent_id: first.structureRef,
      };
      return add('editor:commands.addFinger', { kind: 'structure', el });
    }
    case 'polygon': {
      if (pts.length < 3) return null;
      const t = options.polygonTarget;
      if (t === 'water')
        return metaCommand(
          doc,
          { water_polygons: [...doc.meta.water_polygons, pts] },
          'editor:commands.addWater',
        );
      if (t === 'land')
        return metaCommand(
          doc,
          { land_polygons: [...doc.meta.land_polygons, pts] },
          'editor:commands.addLand',
        );
      if (t === 'concession')
        return metaCommand(doc, { concession_outline: pts }, 'editor:commands.setConcession');
      const el: Structure = {
        key: newKey(doc),
        kind: t,
        code: uniqueCode(t === 'breakwater' ? 'DIG-1' : 'CAN-1', familyCodes(doc, 'structure')),
        points: pts,
        closed: true,
      };
      return add('editor:commands.addPolygon', { kind: 'structure', el });
    }
    case 'berth':
      return first
        ? add('editor:commands.addBerth', { kind: 'berth', el: newBerth(doc, first) })
        : null;
    case 'buoy': {
      if (!first) return null;
      const el = newBerth(
        doc,
        { ...first, rotation_dd: 0 },
        {
          side: 'none',
          code: uniqueCode('BOA-1', occupiableCodes(doc)),
          mooring_type: 'buoy',
          swing_radius_cm: DEFAULTS.buoyRadiusCm,
          width_cm: 300,
          length_cm: 1000,
        },
      );
      delete el.structure_id;
      return add('editor:commands.addBuoy', { kind: 'berth', el });
    }
    case 'linear_area': {
      const [a, b] = clicks;
      if (!a || !b || a.kind !== 'edge' || b.kind !== 'edge' || a.structureRef !== b.structureRef)
        return null;
      const from = Math.min(a.s ?? 0, b.s ?? 0);
      const to = Math.max(a.s ?? 0, b.s ?? 0);
      if (to - from < 100) return null;
      const el: DynamicArea = {
        key: newKey(doc),
        kind: 'linear',
        code: uniqueCode('AD-1', occupiableCodes(doc)),
        structure_id: a.structureRef!,
        side: a.side ?? 'left',
        from_cm: roundHalfEven(from / 10) * 10,
        to_cm: roundHalfEven(to / 10) * 10,
        mooring_mode: 'stern_to',
        cell_cm: 50,
        margin_cm: 50,
        max_beam_cm: DEFAULTS.linearDepthCm,
        blocked_cells: [],
        exits: [],
        grid_rotation_dd: 0,
        active: true,
        group_ids: [],
      };
      return add('editor:commands.addArea', { kind: 'area', el });
    }
    case 'surface_area': {
      if (pts.length < 3) return null;
      const el: DynamicArea = {
        key: newKey(doc),
        kind: 'surface',
        code: uniqueCode('PZ-1', occupiableCodes(doc)),
        outline: pts,
        cell_cm: 100,
        margin_cm: 60,
        blocked_cells: [],
        exits: [],
        grid_rotation_dd: 0,
        active: true,
        group_ids: [],
      };
      return add('editor:commands.addArea', { kind: 'area', el });
    }
    case 'land_resource': {
      if (!first) return null;
      const el: LandResource = {
        key: newKey(doc),
        kind: options.landKind,
        code: uniqueCode('R-1', occupiableCodes(doc)),
        anchor: first.point,
        rotation_dd: first.rotation_dd ?? 0,
        width_cm: DEFAULTS.landWidthCm,
        length_cm: DEFAULTS.landLengthCm,
        attributes: [],
        active: true,
        group_ids: [],
      };
      return add('editor:commands.addLandResource', { kind: 'land', el });
    }
    case 'pedestal': {
      if (!first) return null;
      const el: Pedestal = {
        key: newKey(doc),
        code: uniqueCode('C-1', familyCodes(doc, 'pedestal')),
        position: first.point,
        sockets: [
          { code: '1', kind: 'power', amps: 16, phase: 'single' },
          { code: '2', kind: 'power', amps: 16, phase: 'single' },
          { code: 'W', kind: 'water' },
        ],
        meters: [],
      };
      return add('editor:commands.addPedestal', { kind: 'pedestal', el });
    }
    case 'decoration':
      return first
        ? add('editor:commands.addDecoration', {
            kind: 'decoration',
            el: {
              key: newKey(doc),
              kind: 'symbol',
              asset: options.asset,
              position: first.point,
              size_cm: DEFAULTS.decorationSizeCm,
              rotation_dd: 0,
            },
          })
        : null;
    case 'text':
      return first
        ? add('editor:commands.addText', {
            kind: 'decoration',
            el: {
              key: newKey(doc),
              kind: 'text',
              position: first.point,
              size_cm: DEFAULTS.decorationSizeCm,
              label: { it: options.textLabel },
            },
          })
        : null;
    case 'depth_label':
      return first
        ? add('editor:commands.addDepthLabel', {
            kind: 'decoration',
            el: {
              key: newKey(doc),
              kind: 'depth_label',
              position: first.point,
              size_cm: DEFAULTS.decorationSizeCm,
              depth_cm: DEFAULTS.depthCm,
            },
          })
        : null;
    default:
      return null;
  }
}

/** Distance of a measure (cm, integer). */
export function measureCm(a: Pt, b: Pt): number {
  return roundHalfEven(Math.hypot(b.x - a.x, b.y - a.y));
}
