// Contexts of the phone day map (m6b-plan §6.2 Mappa): presence, arrivals/departures, payments, contract type.
// Pure: styles and legend counts from the day view (and the payments lens for "payments").
import type { MapPaymentStatus } from '@porto/shared/api';
import {
  applyLens,
  applyPaymentsLens,
  DEFAULT_LENS_CONTEXT,
  PAYMENTS_LEGEND,
  type DaySubject,
  type DayView,
  type LensStyle,
} from '@porto/shared/map';

export const MAP_CONTEXTS = ['presence', 'movements', 'payments', 'contractType'] as const;
export type MapContext = (typeof MAP_CONTEXTS)[number];

export type ContextResult = {
  styles: Map<string, LensStyle>;
  /** Legend key → count, in display order. */
  legend: { key: string; count: number; token: LensStyle['colorToken'] }[];
};

/** Arrivals and departures of the night only: everything else is muted. */
export function movementStyle(s: DaySubject['subject']): LensStyle {
  if (!s.vessel) return { colorToken: 'free', legendKey: 'free' };
  if (s.departure_today)
    return { colorToken: 'departure', glyph: 'arrowOut', legendKey: 'departure' };
  if (s.arrival_today) return { colorToken: 'arrival', glyph: 'arrowIn', legendKey: 'arrival' };
  return { colorToken: 'muted', legendKey: 'noMovement' };
}

const MOVEMENT_KEYS = ['arrival', 'departure', 'free', 'noMovement'] as const;
const PAYMENT_TOKENS: Record<string, LensStyle['colorToken']> = {
  ok: 'ok',
  due_soon: 'warn',
  overdue: 'bad',
  overdue_60: 'bad',
  disputed: 'override',
  suspended: 'blocked',
  none: 'muted',
};

function legendOf(
  styles: Map<string, LensStyle>,
  order: readonly string[],
  counts?: Record<string, number>,
): ContextResult['legend'] {
  const tokens = new Map<string, LensStyle['colorToken']>();
  const tally = new Map<string, number>();
  for (const st of styles.values()) {
    if (!tokens.has(st.legendKey)) tokens.set(st.legendKey, st.colorToken);
    tally.set(st.legendKey, (tally.get(st.legendKey) ?? 0) + 1);
  }
  return order
    .map((key) => ({
      key,
      count: counts?.[key] ?? tally.get(key) ?? 0,
      token: tokens.get(key) ?? 'muted',
    }))
    .filter((l) => l.count > 0);
}

export function applyContext(
  ctx: MapContext,
  dv: DayView,
  subjects: readonly DaySubject[],
  payments: MapPaymentStatus | undefined,
): ContextResult {
  if (ctx === 'movements') {
    const styles = new Map(subjects.map((s) => [s.ref, movementStyle(s.subject)]));
    return { styles, legend: legendOf(styles, MOVEMENT_KEYS) };
  }
  if (ctx === 'payments') {
    const r = applyPaymentsLens(subjects, payments);
    const styles = new Map<string, LensStyle>(r.styles);
    const legend = PAYMENTS_LEGEND.map((key) => ({
      key,
      count: r.counts[key],
      token: PAYMENT_TOKENS[key] ?? 'muted',
    })).filter((l) => l.count > 0);
    return { styles, legend };
  }
  const r = applyLens(
    dv,
    ctx,
    { date: dv.date, ...DEFAULT_LENS_CONTEXT },
    {},
    new Map(),
    new Set(),
    subjects,
  );
  return { styles: r.styles, legend: legendOf(r.styles, Object.keys(r.counts), r.counts) };
}

/** Legend label key of a context's legend entry. */
export function legendLabelKey(ctx: MapContext, key: string): string {
  if (ctx === 'payments')
    return key === 'none' ? 'receivables:lens.none' : `billingEnums:lensState.${key}`;
  return `occupancyEnums:lens.legend.${key}`;
}
