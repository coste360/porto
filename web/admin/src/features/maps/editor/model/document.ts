// Editor document (m1-plan §5.3): the map meta plus every element normalized by ref (saved id or client key),
// immutable. Each change returns a new document that shares the untouched element objects, so the renderer
// re-draws only the elements that changed.
import type { MapDocument, MapMeta, MapSaveRequest } from '@porto/shared/api';
import {
  KIND_COLLECTIONS,
  emptyElements,
  entryBounds,
  refOf,
  type Box,
  type ElementKind,
  type SceneEntry,
  type SceneItem,
} from '@porto/shared/map';

export type EditorMeta = MapMeta;

export type EditorDocument = {
  readonly mapId: string;
  /** Server version the document is based on (If-Match of the next save). */
  readonly version: number;
  readonly meta: EditorMeta;
  readonly elements: ReadonlyMap<string, SceneEntry>;
};

const META_KEYS = [
  'kind',
  'name',
  'icon',
  'width_cm',
  'height_cm',
  'grid_cm',
  'sort_order',
  'background',
  'georef',
  'concession_outline',
  'water_polygons',
  'land_polygons',
] as const satisfies readonly (keyof MapMeta)[];

export function metaOf(doc: MapMeta): EditorMeta {
  const out = {} as Record<string, unknown>;
  for (const k of META_KEYS) out[k] = doc[k];
  return out as EditorMeta;
}

export function fromApi(doc: MapDocument): EditorDocument {
  const elements = new Map<string, SceneEntry>();
  const add = (entry: SceneEntry) => elements.set(refOf(entry.el), entry);
  for (const el of doc.structures) add({ kind: 'structure', el });
  for (const el of doc.dynamic_areas) add({ kind: 'area', el });
  for (const el of doc.land_resources) add({ kind: 'land', el });
  for (const el of doc.berths) add({ kind: 'berth', el });
  for (const el of doc.pedestals) add({ kind: 'pedestal', el });
  for (const el of doc.decorations) add({ kind: 'decoration', el });
  return { mapId: doc.id, version: doc.version, meta: metaOf(doc), elements };
}

/** The PUT /maps/{id} body: meta plus elements grouped by collection (read-only fields stripped). */
export function toSaveRequest(doc: EditorDocument): MapSaveRequest {
  const elements = emptyElements();
  for (const entry of doc.elements.values()) {
    switch (entry.kind) {
      case 'structure':
        elements.structures.push(entry.el);
        break;
      case 'berth':
        elements.berths.push(entry.el);
        break;
      case 'area': {
        const { cell_count: _readOnly, ...area } = entry.el;
        elements.dynamic_areas.push(area);
        break;
      }
      case 'land':
        elements.land_resources.push(entry.el);
        break;
      case 'pedestal':
        elements.pedestals.push(entry.el);
        break;
      case 'decoration':
        elements.decorations.push(entry.el);
        break;
    }
  }
  return { ...doc.meta, ...elements };
}

export function getEntry(doc: EditorDocument, ref: string): SceneEntry | undefined {
  return doc.elements.get(ref);
}

/** Replaces, adds (after) or removes (undefined) entries. */
export function withEntries(
  doc: EditorDocument,
  changes: Iterable<readonly [string, SceneEntry | undefined]>,
): EditorDocument {
  const next = new Map(doc.elements);
  for (const [ref, entry] of changes) {
    if (entry) next.set(ref, entry);
    else next.delete(ref);
  }
  return { ...doc, elements: next };
}

export function withMeta(doc: EditorDocument, meta: EditorMeta): EditorDocument {
  return { ...doc, meta };
}

export function entriesOfKind<K extends ElementKind>(
  doc: EditorDocument,
  kind: K,
): Extract<SceneEntry, { kind: K }>[] {
  const out: Extract<SceneEntry, { kind: K }>[] = [];
  for (const e of doc.elements.values())
    if (e.kind === kind) out.push(e as Extract<SceneEntry, { kind: K }>);
  return out;
}

export type DocumentCounts = Record<(typeof KIND_COLLECTIONS)[ElementKind], number>;

export function countsOf(doc: EditorDocument): DocumentCounts {
  const counts: DocumentCounts = {
    structures: 0,
    berths: 0,
    dynamic_areas: 0,
    land_resources: 0,
    pedestals: 0,
    decorations: 0,
  };
  for (const e of doc.elements.values()) counts[KIND_COLLECTIONS[e.kind]]++;
  return counts;
}

/**
 * Scene items of a document. Items are cached per entry object so an unchanged element keeps the same item
 * (memoised rendering); linear areas are recomputed because their geometry follows their structure.
 */
export function createSceneBuilder() {
  const cache = new WeakMap<object, SceneItem>();
  return (doc: EditorDocument): SceneItem[] => {
    const structures = new Map<string, Extract<SceneEntry, { kind: 'structure' }>['el']>();
    for (const [ref, e] of doc.elements) if (e.kind === 'structure') structures.set(ref, e.el);
    const out: SceneItem[] = [];
    for (const [ref, entry] of doc.elements) {
      const cached = cache.get(entry.el);
      if (cached && cached.ref === ref && entry.kind !== 'area') {
        out.push(cached);
        continue;
      }
      const item = { ...entry, ref, box: entryBounds(entry, structures) } as SceneItem;
      cache.set(entry.el, item);
      out.push(item);
    }
    return out;
  };
}

export function boundsOfRefs(items: readonly SceneItem[], refs: ReadonlySet<string>): Box | null {
  let box: Box | null = null;
  for (const item of items) {
    if (!refs.has(item.ref)) continue;
    box = box
      ? {
          minX: Math.min(box.minX, item.box.minX),
          minY: Math.min(box.minY, item.box.minY),
          maxX: Math.max(box.maxX, item.box.maxX),
          maxY: Math.max(box.maxY, item.box.maxY),
        }
      : { ...item.box };
  }
  return box;
}

/** Codes in the shared berth / area / land resource code space (spec 8.4 duplicate_code). */
export function occupiableCodes(
  doc: EditorDocument,
  exclude: ReadonlySet<string> = new Set(),
): Set<string> {
  const out = new Set<string>();
  for (const [ref, e] of doc.elements) {
    if (exclude.has(ref)) continue;
    if (e.kind === 'berth' || e.kind === 'area' || e.kind === 'land') out.add(e.el.code);
  }
  return out;
}

/** Codes of one non-occupiable family (structures or pedestals). */
export function familyCodes(doc: EditorDocument, kind: 'structure' | 'pedestal'): Set<string> {
  const out = new Set<string>();
  for (const e of doc.elements.values()) if (e.kind === kind) out.add(e.el.code);
  return out;
}

/** First free code after `code`: trailing number incremented keeping its padding, else "-2", "-3"... */
export function uniqueCode(code: string, taken: ReadonlySet<string>): string {
  if (!taken.has(code)) return code;
  const m = /^(.*?)(\d+)$/.exec(code);
  if (m) {
    const [, head = '', digits = ''] = m;
    let n = Number(digits);
    for (;;) {
      n++;
      const next = `${head}${String(n).padStart(digits.length, '0')}`.slice(0, 32);
      if (!taken.has(next)) return next;
    }
  }
  for (let n = 2; ; n++) {
    const next = `${code.slice(0, 28)}-${n}`;
    if (!taken.has(next)) return next;
  }
}
