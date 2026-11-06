import type { SceneItem } from '@porto/shared/map';

import type { EditorDocument } from './document';

export type Selection = ReadonlySet<string>;

export const emptySelection: Selection = new Set();

export function single(ref: string): Selection {
  return new Set([ref]);
}

/** Shift-click: adds or removes one ref. */
export function toggle(sel: Selection, ref: string): Selection {
  const next = new Set(sel);
  if (next.has(ref)) next.delete(ref);
  else next.add(ref);
  return next;
}

/** Elements whose bounds lie inside the marquee box; `add` keeps the current selection. */
export function marquee(
  items: readonly SceneItem[],
  box: { minX: number; minY: number; maxX: number; maxY: number },
  sel: Selection = emptySelection,
  add = false,
): Selection {
  const next = new Set(add ? sel : []);
  for (const item of items) {
    const b = item.box;
    if (b.minX >= box.minX && b.maxX <= box.maxX && b.minY >= box.minY && b.maxY <= box.maxY)
      next.add(item.ref);
  }
  return next;
}

/** A structure and everything attached to it: berths and areas on it, fingers whose parent it is. */
export function byStructure(doc: EditorDocument, structureRef: string): Selection {
  const next = new Set<string>([structureRef]);
  for (const [ref, e] of doc.elements) {
    if (e.kind === 'berth' && e.el.structure_id === structureRef) next.add(ref);
    if (e.kind === 'area' && e.el.structure_id === structureRef) next.add(ref);
    if (e.kind === 'structure' && e.el.parent_id === structureRef && e.el.kind === 'finger')
      next.add(ref);
  }
  return next;
}

/** Members of a berth group (berths, areas, land resources). */
export function byGroup(doc: EditorDocument, groupId: string): Selection {
  const next = new Set<string>();
  for (const [ref, e] of doc.elements) {
    if (
      (e.kind === 'berth' || e.kind === 'area' || e.kind === 'land') &&
      e.el.group_ids.includes(groupId)
    ) {
      next.add(ref);
    }
  }
  return next;
}

export function allOfKind(doc: EditorDocument, kind: SceneItem['kind']): Selection {
  const next = new Set<string>();
  for (const [ref, e] of doc.elements) if (e.kind === kind) next.add(ref);
  return next;
}

/** Drops refs that no longer exist (after undo, remove or reload); `idMap` renames saved keys to ids. */
export function reconcile(
  sel: Selection,
  doc: EditorDocument,
  idMap: Record<string, string> = {},
): Selection {
  const next = new Set<string>();
  for (const ref of sel) {
    const r = idMap[ref] ?? ref;
    if (doc.elements.has(r)) next.add(r);
  }
  return next.size === sel.size && [...next].every((r) => sel.has(r)) ? sel : next;
}
