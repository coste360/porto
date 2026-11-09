// Legend of a lens and the operational filters (spec 9.2): "the legend is the filter". Counts are computed
// client side from the day view over the elements that pass the filters (D23).
import { daySubjects, type DaySubject } from '../dayView';
import { anomaliesLens } from './anomalies';
import { contractTypeLens } from './contractType';
import { deadlinesLens } from './deadlines';
import { fitnessLens } from './fitness';
import { occupancyLens } from './occupancy';
import { presenceLens } from './presence';
import type { DayView, Lens, LensContext, LensId, LensStyle, LegendKey } from './types';
import { utilitiesLens } from './utilities';
import { vesselTypeLens } from './vesselType';

const BY_ID: Record<LensId, Lens> = {
  occupancy: occupancyLens,
  contractType: contractTypeLens,
  vesselType: vesselTypeLens,
  presence: presenceLens,
  deadlines: deadlinesLens,
  fitness: fitnessLens,
  utilities: utilitiesLens,
  anomalies: anomaliesLens,
};

/** Legend keys of each lens, in display order. */
export const LENS_LEGEND: Readonly<Record<LensId, readonly LegendKey[]>> = {
  occupancy: [
    'free',
    'annual',
    'seasonal',
    'monthly',
    'period',
    'contract',
    'transit',
    'hold',
    'released',
    'blocked',
    'closed',
    'maintenance',
    'owner',
  ],
  contractType: [
    'annual',
    'seasonal',
    'monthly',
    'period',
    'transit',
    'owner',
    'hold',
    'other',
    'none',
  ],
  vesselType: [
    'monohull_motor',
    'monohull_sail',
    'catamaran',
    'trimaran',
    'rib',
    'gozzo',
    'other',
    'none',
  ],
  presence: ['in', 'out', 'awayLong', 'arrival', 'departure', 'free', 'unrecorded'],
  deadlines: ['ok', 'warn', 'bad', 'none'],
  fitness: ['ok', 'warn', 'bad', 'override', 'none'],
  utilities: ['power', 'water', 'none'],
  anomalies: ['anomaly', 'none'],
};

/** Static attributes of a resource from the map document (filters). */
export type ResourceMeta = {
  structureId?: string;
  groupIds: readonly string[];
  category?: string;
};

export type ContractFilter = 'annual' | 'seasonal' | 'monthly' | 'period' | 'transit' | 'none';

export type OpsFilters = {
  groupId?: string;
  structureId?: string;
  category?: string;
  /** LOA band of the vessel in cm, inclusive. */
  loaMin?: number;
  loaMax?: number;
  contractType?: ContractFilter;
  /** Free text: code, vessel name, customer name. */
  text?: string;
};

export function hasFilters(f: OpsFilters): boolean {
  return Object.values(f).some((v) => v !== undefined && v !== '');
}

function contractFilterOf(s: DaySubject): ContractFilter {
  const c = s.subject.contract;
  if (!c) return 'none';
  if (c.kind === 'transit') return 'transit';
  return (c.term ?? 'period') as ContractFilter;
}

/** Whether a subject passes the filters (excluded subjects are greyed, never hidden). */
export function matchesFilters(
  s: DaySubject,
  f: OpsFilters,
  meta: ReadonlyMap<string, ResourceMeta>,
): boolean {
  const m = meta.get(s.resourceId);
  if (f.structureId && m?.structureId !== f.structureId) return false;
  if (f.groupId && !m?.groupIds.includes(f.groupId)) return false;
  if (f.category && m?.category !== f.category) return false;
  const loa = s.subject.vessel?.loa_cm;
  if (f.loaMin !== undefined && (loa === undefined || loa < f.loaMin)) return false;
  if (f.loaMax !== undefined && (loa === undefined || loa > f.loaMax)) return false;
  if (f.contractType && contractFilterOf(s) !== f.contractType) return false;
  const text = f.text?.trim().toLocaleLowerCase();
  if (text) {
    const hay = [s.code, s.subject.vessel?.name, s.subject.customer?.display_name]
      .filter(Boolean)
      .join(' ')
      .toLocaleLowerCase();
    if (!hay.includes(text)) return false;
  }
  return true;
}

export type LensResult = {
  /** Style of every subject by ref. */
  styles: Map<string, LensStyle>;
  /** Count of each legend key over the subjects passing the filters. */
  counts: Record<string, number>;
  /** Refs greyed: outside the filters or with their legend key unchecked. */
  dimmed: Set<string>;
};

/**
 * Applies a lens to a day view: styles, legend counts (filtered elements only) and the dimmed set. `hidden`
 * holds the legend keys whose checkbox is off.
 */
export function applyLens(
  dv: DayView,
  lens: LensId,
  ctx: LensContext,
  filters: OpsFilters = {},
  meta: ReadonlyMap<string, ResourceMeta> = new Map(),
  hidden: ReadonlySet<string> = new Set(),
  subjects: readonly DaySubject[] = daySubjects(dv),
): LensResult {
  const fn = BY_ID[lens];
  const styles = new Map<string, LensStyle>();
  const counts: Record<string, number> = Object.fromEntries(LENS_LEGEND[lens].map((k) => [k, 0]));
  const dimmed = new Set<string>();
  const filtering = hasFilters(filters);
  for (const s of subjects) {
    const style = fn(s.subject, ctx);
    styles.set(s.ref, style);
    const pass = !filtering || matchesFilters(s, filters, meta);
    if (pass) counts[style.legendKey] = (counts[style.legendKey] ?? 0) + 1;
    if (!pass || hidden.has(style.legendKey)) dimmed.add(s.ref);
  }
  return { styles, counts, dimmed };
}

/** Legend counts only (tests and the list alternative). */
export function legendCounts(
  dv: DayView,
  lens: LensId,
  ctx: LensContext,
  filters: OpsFilters = {},
  meta: ReadonlyMap<string, ResourceMeta> = new Map(),
): Record<string, number> {
  return applyLens(dv, lens, ctx, filters, meta).counts;
}
