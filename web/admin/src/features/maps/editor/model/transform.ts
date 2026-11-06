// Geometric edits of a selection (m1-plan §5.3): move, rotate, align, distribute, duplicate, mirror. Each
// returns one Command; every coordinate written is integer cm (roundHalfEven), every angle 0..3599.
import {
  angleDdOf,
  normalizeDd,
  reflectPoint,
  refOf,
  rotatePoint,
  roundHalfEven,
  roundPoint,
  type Box,
  type Pt,
  type SceneEntry,
  type SceneItem,
} from '@porto/shared/map';

import { batch, updateCommand, addCommand, type Command } from './commands';
import { familyCodes, occupiableCodes, uniqueCode, type EditorDocument } from './document';
import { newKey } from './keys';

/** Arrow-key nudge (cm): plain and with Shift. */
export const NUDGE_CM = 10;
export const NUDGE_BIG_CM = 100;
/** Rotation step (tenths of a degree): plain and with Shift. */
export const ROTATE_STEP_DD = 10;
export const ROTATE_BIG_DD = 150;

function mapPoints(points: readonly Pt[] | undefined, fn: (p: Pt) => Pt): Pt[] | undefined {
  return points ? points.map((p) => roundPoint(fn(p))) : undefined;
}

/** Applies a point transform to every coordinate of an entry; `angle` maps rotations. */
export function transformEntry(
  entry: SceneEntry,
  fn: (p: Pt) => Pt,
  angle: (dd: number) => number,
  swapSides = false,
): SceneEntry {
  const side = <S extends string | undefined>(s: S): S =>
    (swapSides ? (s === 'left' ? 'right' : s === 'right' ? 'left' : s) : s) as S;
  switch (entry.kind) {
    case 'structure': {
      const el = { ...entry.el };
      if (el.points) el.points = mapPoints(el.points, fn);
      if (el.rect)
        el.rect = {
          ...el.rect,
          anchor: roundPoint(fn(el.rect.anchor)),
          rotation_dd: angle(el.rect.rotation_dd),
        };
      if (swapSides && el.sides) el.sides = el.sides.map((s) => side(s));
      return { kind: 'structure', el };
    }
    case 'berth':
      return {
        kind: 'berth',
        el: {
          ...entry.el,
          anchor: roundPoint(fn(entry.el.anchor)),
          rotation_dd: angle(entry.el.rotation_dd),
          side: side(entry.el.side),
        },
      };
    case 'land':
      return {
        kind: 'land',
        el: {
          ...entry.el,
          anchor: roundPoint(fn(entry.el.anchor)),
          rotation_dd: angle(entry.el.rotation_dd),
        },
      };
    case 'area': {
      const el = { ...entry.el };
      if (el.kind === 'surface' && el.outline) el.outline = mapPoints(el.outline, fn);
      if (el.side) el.side = side(el.side);
      return { kind: 'area', el };
    }
    case 'pedestal':
      return { kind: 'pedestal', el: { ...entry.el, position: roundPoint(fn(entry.el.position)) } };
    case 'decoration': {
      const el = { ...entry.el };
      if (el.position) el.position = roundPoint(fn(el.position));
      if (el.points) el.points = mapPoints(el.points, fn);
      if (el.rotation_dd !== undefined) el.rotation_dd = angle(el.rotation_dd);
      return { kind: 'decoration', el };
    }
  }
}

const identityAngle = (dd: number) => dd;

export function moveCommand(
  doc: EditorDocument,
  refs: Iterable<string>,
  dx: number,
  dy: number,
): Command {
  const ix = roundHalfEven(dx);
  const iy = roundHalfEven(dy);
  if (ix === 0 && iy === 0) return { label: 'editor:commands.move', changes: [] };
  return updateCommand(
    doc,
    refs,
    (e) => transformEntry(e, (p) => ({ x: p.x + ix, y: p.y + iy }), identityAngle),
    'editor:commands.move',
  );
}

/** Rotates around `pivot` by `dd` tenths of a degree (clockwise on screen). */
export function rotateCommand(
  doc: EditorDocument,
  refs: Iterable<string>,
  dd: number,
  pivot: Pt,
): Command {
  return updateCommand(
    doc,
    refs,
    (e) =>
      transformEntry(
        e,
        (p) => rotatePoint(p, pivot, dd),
        (r) => normalizeDd(r + dd),
      ),
    'editor:commands.rotate',
  );
}

/** Mirrors across the line a-b: points reflect, rotations become 2θ − r, left and right swap. */
export function mirrorCommand(doc: EditorDocument, refs: Iterable<string>, a: Pt, b: Pt): Command {
  const theta = angleDdOf(b.x - a.x, b.y - a.y);
  return updateCommand(
    doc,
    refs,
    (e) =>
      transformEntry(
        e,
        (p) => reflectPoint(p, a, b),
        (r) => normalizeDd(2 * theta - r),
        true,
      ),
    'editor:commands.mirror',
  );
}

export type AlignMode = 'left' | 'right' | 'top' | 'bottom' | 'centre_x' | 'centre_y';

function itemsOf(items: readonly SceneItem[], refs: ReadonlySet<string>): SceneItem[] {
  return items.filter((i) => refs.has(i.ref));
}

/** Aligns the bounds of the selected elements to the selection's extreme or centre. */
export function alignCommand(
  doc: EditorDocument,
  items: readonly SceneItem[],
  refs: ReadonlySet<string>,
  mode: AlignMode,
): Command {
  const sel = itemsOf(items, refs);
  if (sel.length < 2) return { label: 'editor:commands.align', changes: [] };
  const minX = Math.min(...sel.map((i) => i.box.minX));
  const maxX = Math.max(...sel.map((i) => i.box.maxX));
  const minY = Math.min(...sel.map((i) => i.box.minY));
  const maxY = Math.max(...sel.map((i) => i.box.maxY));
  const cmds = sel.map((i) => {
    const b = i.box;
    let dx = 0;
    let dy = 0;
    if (mode === 'left') dx = minX - b.minX;
    if (mode === 'right') dx = maxX - b.maxX;
    if (mode === 'top') dy = minY - b.minY;
    if (mode === 'bottom') dy = maxY - b.maxY;
    if (mode === 'centre_x') dx = (minX + maxX) / 2 - (b.minX + b.maxX) / 2;
    if (mode === 'centre_y') dy = (minY + maxY) / 2 - (b.minY + b.maxY) / 2;
    return moveCommand(doc, [i.ref], dx, dy);
  });
  return batch('editor:commands.align', cmds);
}

/** Distributes the selected elements with equal gaps between their bounds along x or y. */
export function distributeCommand(
  doc: EditorDocument,
  items: readonly SceneItem[],
  refs: ReadonlySet<string>,
  axis: 'x' | 'y',
): Command {
  const sel = itemsOf(items, refs);
  if (sel.length < 3) return { label: 'editor:commands.distribute', changes: [] };
  const lo = (b: Box) => (axis === 'x' ? b.minX : b.minY);
  const hi = (b: Box) => (axis === 'x' ? b.maxX : b.maxY);
  const sorted = [...sel].sort((a, b) => lo(a.box) - lo(b.box));
  const start = lo(sorted[0]!.box);
  const end = Math.max(...sorted.map((i) => hi(i.box)));
  const total = sorted.reduce((sum, i) => sum + (hi(i.box) - lo(i.box)), 0);
  const gap = (end - start - total) / (sorted.length - 1);
  let cursor = start;
  const cmds: Command[] = [];
  for (const i of sorted) {
    const d = cursor - lo(i.box);
    cmds.push(axis === 'x' ? moveCommand(doc, [i.ref], d, 0) : moveCommand(doc, [i.ref], 0, d));
    cursor += hi(i.box) - lo(i.box) + gap;
  }
  return batch('editor:commands.distribute', cmds);
}

function remapRef(ref: string | undefined, map: ReadonlyMap<string, string>): string | undefined {
  return ref !== undefined ? (map.get(ref) ?? ref) : undefined;
}

/**
 * Duplicates the selection offset by one width (a single berth or land resource: its own width sideways; a
 * group: the selection width along x). New keys, unique codes, references inside the copy remapped.
 */
export function duplicateCommand(
  doc: EditorDocument,
  items: readonly SceneItem[],
  refs: ReadonlySet<string>,
): { command: Command; newRefs: string[] } {
  const sel = itemsOf(items, refs);
  if (sel.length === 0)
    return { command: { label: 'editor:commands.duplicate', changes: [] }, newRefs: [] };
  let offset: Pt;
  const only = sel.length === 1 ? sel[0]! : null;
  if (only && (only.kind === 'berth' || only.kind === 'land')) {
    const rad = (only.el.rotation_dd / 10) * (Math.PI / 180);
    // Sideways = the rect's right direction (−left), one width away.
    offset = { x: -Math.sin(rad) * only.el.width_cm, y: Math.cos(rad) * only.el.width_cm };
  } else {
    const minX = Math.min(...sel.map((i) => i.box.minX));
    const maxX = Math.max(...sel.map((i) => i.box.maxX));
    offset = { x: Math.max(100, maxX - minX), y: 0 };
  }
  const keyMap = new Map<string, string>();
  for (const i of sel) keyMap.set(i.ref, newKey(doc));
  const occupiable = occupiableCodes(doc);
  const structures = familyCodes(doc, 'structure');
  const pedestals = familyCodes(doc, 'pedestal');
  const entries: SceneEntry[] = [];
  for (const i of sel) {
    const moved = transformEntry(
      i as SceneEntry,
      (p) => ({ x: p.x + offset.x, y: p.y + offset.y }),
      identityAngle,
    );
    const key = keyMap.get(i.ref)!;
    switch (moved.kind) {
      case 'structure': {
        const { id: _id, ...el } = moved.el;
        const code = uniqueCode(el.code, structures);
        structures.add(code);
        entries.push({
          kind: 'structure',
          el: { ...el, key, code, parent_id: remapRef(el.parent_id, keyMap) },
        });
        break;
      }
      case 'berth': {
        const { id: _id, ...el } = moved.el;
        const code = uniqueCode(el.code, occupiable);
        occupiable.add(code);
        entries.push({
          kind: 'berth',
          el: {
            ...el,
            key,
            code,
            structure_id: remapRef(el.structure_id, keyMap),
            pedestal_id: remapRef(el.pedestal_id, keyMap),
          },
        });
        break;
      }
      case 'area': {
        const { id: _id, cell_count: _c, ...el } = moved.el;
        const code = uniqueCode(el.code, occupiable);
        occupiable.add(code);
        entries.push({
          kind: 'area',
          el: { ...el, key, code, structure_id: remapRef(el.structure_id, keyMap) },
        });
        break;
      }
      case 'land': {
        const { id: _id, ...el } = moved.el;
        const code = uniqueCode(el.code, occupiable);
        occupiable.add(code);
        entries.push({ kind: 'land', el: { ...el, key, code } });
        break;
      }
      case 'pedestal': {
        const { id: _id, ...el } = moved.el;
        const code = uniqueCode(el.code, pedestals);
        pedestals.add(code);
        entries.push({ kind: 'pedestal', el: { ...el, key, code } });
        break;
      }
      case 'decoration': {
        const { id: _id, ...el } = moved.el;
        entries.push({ kind: 'decoration', el: { ...el, key } });
        break;
      }
    }
  }
  for (const e of entries) {
    // Drop references that stayed undefined after the remap (exactOptionalPropertyTypes friendliness).
    for (const k of ['structure_id', 'pedestal_id', 'parent_id'] as const) {
      if (k in e.el && (e.el as Record<string, unknown>)[k] === undefined)
        delete (e.el as Record<string, unknown>)[k];
    }
  }
  return {
    command: addCommand('editor:commands.duplicate', entries),
    newRefs: entries.map((e) => refOf(e.el)),
  };
}

/** Centre of the bounds of the selected items. */
export function selectionCentre(items: readonly SceneItem[], refs: ReadonlySet<string>): Pt | null {
  const sel = itemsOf(items, refs);
  if (sel.length === 0) return null;
  const minX = Math.min(...sel.map((i) => i.box.minX));
  const maxX = Math.max(...sel.map((i) => i.box.maxX));
  const minY = Math.min(...sel.map((i) => i.box.minY));
  const maxY = Math.max(...sel.map((i) => i.box.maxY));
  return { x: roundHalfEven((minX + maxX) / 2), y: roundHalfEven((minY + maxY) / 2) };
}

/** Axis of the pier of the first selected berth (mirror "across the pier axis"). */
export function pierAxisOf(doc: EditorDocument, refs: ReadonlySet<string>): [Pt, Pt] | null {
  for (const ref of refs) {
    const e = doc.elements.get(ref);
    const sref =
      e?.kind === 'berth' ? e.el.structure_id : e?.kind === 'structure' ? ref : undefined;
    const s = sref ? doc.elements.get(sref) : undefined;
    const pts = s?.kind === 'structure' ? s.el.points : undefined;
    if (pts && pts.length >= 2) return [pts[0]!, pts[pts.length - 1]!];
  }
  return null;
}
