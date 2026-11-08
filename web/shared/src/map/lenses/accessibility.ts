// Accessibilità lens of the storage planner (spec 17.4, m9-plan §5, §12.1): from the accessibility entries of a
// date, every boat is blocked (its exit that day cannot be performed: red), blocking (it stands in the way of a
// blocked boat: outlined), leaving (accessible, with its extraction order) or free to stay. The state is never
// colour only: blocked boats carry the lock glyph, leaving ones the order badge. Pure: no API, no clock.
import type { components } from '../../api/schema';
import type { GlyphId } from './types';
import type { HarborToken } from './tokens';

type AccessEntry = components['schemas']['StorageAccessEntry'];

export const ACCESSIBILITY_LEGEND = [
  'blocked',
  'blocking',
  'leaving',
  'arrival_blocked',
  'ok',
] as const;
export type AccessibilityKey = (typeof ACCESSIBILITY_LEGEND)[number];

export type AccessibilityStyle = {
  key: AccessibilityKey;
  colorToken: HarborToken;
  /** Outline only (the blocking boats keep their fill and get a red outline). */
  outline?: HarborToken;
  glyph?: GlyphId;
  /** Extraction order of the day (1 = first out). */
  badge?: string;
  /** Allocation ids of the boats blocking this one. */
  blockedBy: string[];
};

const OK: AccessibilityStyle = { key: 'ok', colorToken: 'free', blockedBy: [] };

/**
 * Styles of the boats for a date: entries of other dates are ignored; an exit wins over an arrival of the same
 * boat. Boats without an entry are `ok`.
 */
export function accessibilityStyles(
  entries: readonly AccessEntry[],
  date: string,
): ReadonlyMap<string, AccessibilityStyle> {
  const out = new Map<string, AccessibilityStyle>();
  const blocking = new Set<string>();
  const day = entries.filter((e) => e.on === date);
  for (const e of day) {
    if (e.event !== 'exit') continue;
    if (e.accessible) {
      out.set(e.allocation_id, {
        key: 'leaving',
        colorToken: 'departure',
        glyph: 'arrowOut',
        ...(e.order ? { badge: String(e.order) } : {}),
        blockedBy: [],
      });
    } else {
      out.set(e.allocation_id, {
        key: 'blocked',
        colorToken: 'bad',
        glyph: 'lock',
        blockedBy: [...e.blocking],
      });
      for (const b of e.blocking) blocking.add(b);
    }
  }
  for (const e of day) {
    if (e.event !== 'arrival' || e.accessible || out.has(e.allocation_id)) continue;
    out.set(e.allocation_id, {
      key: 'arrival_blocked',
      colorToken: 'warn',
      glyph: 'arrowIn',
      blockedBy: [...e.blocking],
    });
    for (const b of e.blocking) blocking.add(b);
  }
  for (const id of blocking) {
    const cur = out.get(id);
    if (cur && cur.key !== 'ok') {
      out.set(id, { ...cur, outline: 'bad' });
    } else {
      out.set(id, { key: 'blocking', colorToken: 'free', outline: 'bad', blockedBy: [] });
    }
  }
  return out;
}

/** Style of one boat (ok when it has no entry). */
export function accessibilityOf(
  styles: ReadonlyMap<string, AccessibilityStyle>,
  allocationId: string,
): AccessibilityStyle {
  return styles.get(allocationId) ?? OK;
}

/** Blocked exits of the entries by date (YYYY-MM-DD → count), for the timeline markers and warnings. */
export function blockedExitsByDate(entries: readonly AccessEntry[]): ReadonlyMap<string, number> {
  const out = new Map<string, number>();
  for (const e of entries) {
    if (e.event === 'exit' && !e.accessible) out.set(e.on, (out.get(e.on) ?? 0) + 1);
  }
  return out;
}
