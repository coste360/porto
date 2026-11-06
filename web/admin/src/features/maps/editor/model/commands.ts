// Undoable commands (m1-plan §5.3). A command records, for every element it touches, the entry before and
// after (undefined = absent), plus the meta before and after when it changes the meta. apply() writes the
// "after" side, invert() the "before" side, so every user action is exactly one undo step.
import { refOf, type SceneEntry } from '@porto/shared/map';

import { withEntries, withMeta, type EditorDocument, type EditorMeta } from './document';

export type Change = { ref: string; before?: SceneEntry; after?: SceneEntry };

export type Command = {
  /** i18n key of the action (editor:commands.*), shown in the undo tooltip. */
  label: string;
  changes: Change[];
  meta?: { before: EditorMeta; after: EditorMeta };
};

export function apply(doc: EditorDocument, cmd: Command): EditorDocument {
  let next = withEntries(
    doc,
    cmd.changes.map((c) => [c.ref, c.after] as const),
  );
  if (cmd.meta) next = withMeta(next, cmd.meta.after);
  return next;
}

export function invert(doc: EditorDocument, cmd: Command): EditorDocument {
  let next = withEntries(
    doc,
    [...cmd.changes].reverse().map((c) => [c.ref, c.before] as const),
  );
  if (cmd.meta) next = withMeta(next, cmd.meta.before);
  return next;
}

export function isEmpty(cmd: Command): boolean {
  return cmd.changes.length === 0 && !cmd.meta;
}

/** Adds new elements (keys already set). */
export function addCommand(label: string, entries: readonly SceneEntry[]): Command {
  return { label, changes: entries.map((e) => ({ ref: refOf(e.el), after: e })) };
}

/** Updates elements through `fn`; unchanged results (same object) are skipped. */
export function updateCommand(
  doc: EditorDocument,
  refs: Iterable<string>,
  fn: (entry: SceneEntry) => SceneEntry,
  label: string,
): Command {
  const changes: Change[] = [];
  for (const ref of refs) {
    const before = doc.elements.get(ref);
    if (!before) continue;
    const after = fn(before);
    if (after !== before) changes.push({ ref, before, after });
  }
  return { label, changes };
}

/**
 * Removes elements and clears the references other elements hold to them (berth structure and pedestal,
 * finger parent, area structure), in the same command.
 */
export function removeCommand(
  doc: EditorDocument,
  refs: ReadonlySet<string>,
  label = 'editor:commands.delete',
): Command {
  const changes: Change[] = [];
  for (const ref of refs) {
    const before = doc.elements.get(ref);
    if (before) changes.push({ ref, before });
  }
  for (const [ref, entry] of doc.elements) {
    if (refs.has(ref)) continue;
    const after = clearReferences(entry, refs);
    if (after !== entry) changes.push({ ref, before: entry, after });
  }
  return { label, changes };
}

function clearReferences(entry: SceneEntry, removed: ReadonlySet<string>): SceneEntry {
  switch (entry.kind) {
    case 'berth': {
      const el = entry.el;
      const dropStructure = !!el.structure_id && removed.has(el.structure_id);
      const dropPedestal = !!el.pedestal_id && removed.has(el.pedestal_id);
      if (!dropStructure && !dropPedestal) return entry;
      const next = { ...el };
      if (dropStructure) delete next.structure_id;
      if (dropPedestal) {
        delete next.pedestal_id;
        delete next.pedestal_socket;
      }
      return { kind: 'berth', el: next };
    }
    case 'structure': {
      if (!entry.el.parent_id || !removed.has(entry.el.parent_id)) return entry;
      const { parent_id: _gone, ...el } = entry.el;
      return { kind: 'structure', el };
    }
    case 'area': {
      if (!entry.el.structure_id || !removed.has(entry.el.structure_id)) return entry;
      const { structure_id: _gone, ...el } = entry.el;
      return { kind: 'area', el };
    }
    default:
      return entry;
  }
}

export function metaCommand(
  doc: EditorDocument,
  patch: Partial<EditorMeta>,
  label: string,
): Command {
  return { label, changes: [], meta: { before: doc.meta, after: { ...doc.meta, ...patch } } };
}

/** Composes commands applied in order into one undo step. */
export function batch(label: string, commands: readonly Command[]): Command {
  const byRef = new Map<string, Change>();
  const order: string[] = [];
  let meta: Command['meta'];
  for (const cmd of commands) {
    for (const c of cmd.changes) {
      const prev = byRef.get(c.ref);
      if (prev) prev.after = c.after;
      else {
        byRef.set(c.ref, { ...c });
        order.push(c.ref);
      }
    }
    if (cmd.meta) meta = { before: meta?.before ?? cmd.meta.before, after: cmd.meta.after };
  }
  const changes = order.map((r) => byRef.get(r)!).filter((c) => c.before !== c.after);
  return meta ? { label, changes, meta } : { label, changes };
}
