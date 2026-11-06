import type { Berth, I18nText, MapExport, MapKind, Structure } from '@porto/shared/api';
import { isBuoy, type SceneItem } from '@porto/shared/map';

export const MAP_KINDS: readonly MapKind[] = ['water', 'yard', 'shed', 'parking', 'boxes', 'other'];

/** Name of a map in the UI language (Italian when English is missing). */
export function localName(name: I18nText | undefined, lang: string): string {
  if (!name) return '';
  return (lang.startsWith('en') ? name.en : undefined) || name.it;
}

/** Downloads a JSON value as a file. */
export function downloadJson(value: unknown, filename: string, type = 'application/json'): void {
  const blob = new Blob([JSON.stringify(value, null, 2)], { type });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

/** A safe file name from a map name. */
export function fileSlug(name: string): string {
  return (
    name
      .normalize('NFKD')
      .replace(/[^\w-]+/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '')
      .toLowerCase() || 'map'
  );
}

/** Validates the shape of an exported map file before sending it. */
export function parseMapExport(text: string): MapExport | null {
  try {
    const value = JSON.parse(text) as Partial<MapExport>;
    if (value?.format !== 'porto-map/1' || !value.map || !Array.isArray(value.groups)) return null;
    return value as MapExport;
  } catch {
    return null;
  }
}

/** Moves `id` to index `to`. */
export function moveId(ids: readonly string[], id: string, to: number): string[] {
  const next = ids.filter((x) => x !== id);
  next.splice(Math.max(0, Math.min(next.length, to)), 0, id);
  return next;
}

export type BerthGroupRow = {
  /** Structure ref; null for the buoy field and loose berth groups. */
  ref: string | null;
  /** Structure kind, `buoys` (buoys not on a structure) or `loose` (berths not on a structure). */
  kind: Structure['kind'] | 'buoys' | 'loose';
  code: string;
  /** Structure name as entered in the editor (may be empty). */
  name: string;
  berths: { ref: string; el: Berth }[];
};

/** Title and optional subtitle of a group: the structure name, or its kind and code when it has none. */
export function groupTitle(
  g: BerthGroupRow,
  t: (key: string, opts?: Record<string, unknown>) => string,
): { title: string; subtitle?: string } {
  if (g.kind === 'buoys') return { title: t('maps:list.buoys') };
  if (g.kind === 'loose') return { title: t('maps:list.unattached') };
  const kindCode = t(`maps:element.structure.${g.kind}`, { code: g.code });
  if (!g.name || g.name === kindCode) return { title: kindCode };
  return { title: g.name, subtitle: kindCode };
}

const byCode = (a: BerthGroupRow, b: BerthGroupRow) =>
  a.code.localeCompare(b.code, undefined, { numeric: true });

/**
 * Berths grouped by structure (the accessible alternative to the canvas): structures with berths by code, then
 * buoys and berths that hang on no structure, then the structures without berths (shown muted).
 */
export function groupBerthsByStructure(items: readonly SceneItem[]): BerthGroupRow[] {
  const groups = new Map<string, BerthGroupRow>();
  for (const item of items) {
    if (item.kind === 'structure' && (item.el.kind === 'pier' || item.el.kind === 'quay')) {
      groups.set(item.ref, {
        ref: item.ref,
        kind: item.el.kind,
        code: item.el.code,
        name: item.el.name?.trim() ?? '',
        berths: [],
      });
    }
  }
  const buoys: BerthGroupRow = { ref: null, kind: 'buoys', code: '', name: '', berths: [] };
  const loose: BerthGroupRow = { ref: null, kind: 'loose', code: '', name: '', berths: [] };
  for (const item of items) {
    if (item.kind !== 'berth') continue;
    const g =
      (item.el.structure_id && groups.get(item.el.structure_id)) ||
      (isBuoy(item.el) ? buoys : loose);
    g.berths.push({ ref: item.ref, el: item.el });
  }
  const all = [...groups.values()];
  const out = [
    ...all.filter((g) => g.berths.length > 0).sort(byCode),
    ...[buoys, loose].filter((g) => g.berths.length > 0),
    ...all.filter((g) => g.berths.length === 0).sort(byCode),
  ];
  for (const g of out)
    g.berths.sort(
      (a, b) =>
        a.el.position - b.el.position ||
        a.el.code.localeCompare(b.el.code, undefined, { numeric: true }),
    );
  return out;
}

/**
 * Aspect-ratio classes the map canvas can take on narrow screens (Tailwind needs the literal class names).
 * Ratio = width / height.
 */
const MAP_ASPECTS: readonly (readonly [number, string])[] = [
  [3, 'aspect-3/1'],
  [2, 'aspect-2/1'],
  [16 / 9, 'aspect-video'],
  [3 / 2, 'aspect-3/2'],
  [4 / 3, 'aspect-4/3'],
  [1, 'aspect-square'],
  [3 / 4, 'aspect-3/4'],
  [2 / 3, 'aspect-2/3'],
  [1 / 2, 'aspect-1/2'],
];

/**
 * The aspect class nearest to the map's own proportions, so that on a phone the canvas is as wide as the
 * screen and as tall as the map needs (no empty bands above and below it at "fit to view").
 */
export function mapAspectClass(widthCm: number, heightCm: number): string {
  const ratio = widthCm > 0 && heightCm > 0 ? widthCm / heightCm : 4 / 3;
  let best = MAP_ASPECTS[0]!;
  for (const a of MAP_ASPECTS) {
    if (Math.abs(Math.log(a[0] / ratio)) < Math.abs(Math.log(best[0] / ratio))) best = a;
  }
  return best[1];
}
