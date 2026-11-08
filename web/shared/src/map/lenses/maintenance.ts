// Manutenzione lens of the map (spec 20.2, m9-plan §8.3, §12.2): from the maintenance map entries of a date, every
// element is out of service (a scheduled or in-progress task covers the date), overdue (a live task due before the
// date), open (live tasks) or without work. The state is never colour only: out of service carries the lock glyph,
// overdue the alert glyph, open the wrench; the badge counts the live tasks. Pure: no API, no clock.
import type { components } from '../../api/schema';
import type { GlyphId } from './types';
import type { HarborToken } from './tokens';

type MapEntry = components['schemas']['MaintenanceMapEntry'];

export const MAINTENANCE_LEGEND = ['out_of_service', 'overdue', 'open', 'none'] as const;
export type MaintenanceKey = (typeof MAINTENANCE_LEGEND)[number];

export type MaintenanceStyle = {
  key: MaintenanceKey;
  colorToken: HarborToken;
  glyph?: GlyphId;
  /** Number of live tasks (absent when none). */
  badge?: string;
};

const NONE: MaintenanceStyle = { key: 'none', colorToken: 'free' };

/** Style of one entry: out of service wins over overdue, overdue over open. */
export function maintenanceStyle(entry: MapEntry | undefined): MaintenanceStyle {
  if (!entry || entry.open <= 0) return NONE;
  const badge = String(entry.open);
  if (entry.out_of_service)
    return { key: 'out_of_service', colorToken: 'maintenance', glyph: 'lock', badge };
  if (entry.overdue > 0) return { key: 'overdue', colorToken: 'bad', glyph: 'alert', badge };
  return { key: 'open', colorToken: 'warn', glyph: 'wrench', badge };
}

/** Styles keyed by `${element_type}:${element_id}`; elements without an entry are `none`. */
export function maintenanceStyles(
  entries: readonly MapEntry[],
): ReadonlyMap<string, MaintenanceStyle> {
  const out = new Map<string, MaintenanceStyle>();
  for (const e of entries) out.set(`${e.element_type}:${e.element_id}`, maintenanceStyle(e));
  return out;
}

/** Counts per legend key (the legend shows only the keys present). */
export function maintenanceLegend(entries: readonly MapEntry[]): Record<MaintenanceKey, number> {
  const out: Record<MaintenanceKey, number> = { out_of_service: 0, overdue: 0, open: 0, none: 0 };
  for (const e of entries) out[maintenanceStyle(e).key]++;
  return out;
}
