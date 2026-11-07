// Pagamenti lens (spec 9.2, m5-plan §4.5, §9.1): composes the day view with `getMapPaymentStatus` for the same
// map and night. Every allocated resource is in order, has an instalment or charge due within 15 days, is
// overdue, overdue for more than 60 days, or disputed; a suspended contract (arrears) wins over the state. The
// state is never colour-only: overdue states carry the euro badge and the alert glyph, as the spec asks. Amounts
// are not part of the style (they only reach staff with charges.manage). Pure: no API, no clock.
import type { components } from '../../api/schema';
import type { DaySubject } from '../dayView';
import type { LensStyle } from './types';

type MapPaymentStatus = components['schemas']['MapPaymentStatus'];
type MapPaymentResource = MapPaymentStatus['resources'][number];

/** Legend keys of the lens: `billingEnums:lensState.<key>` (`none` = `opsMap` "no data", shown muted). */
export const PAYMENTS_LEGEND = [
  'ok',
  'due_soon',
  'overdue',
  'overdue_60',
  'disputed',
  'suspended',
  'none',
] as const;
export type PaymentsLegendKey = (typeof PAYMENTS_LEGEND)[number];

export type PaymentsStyle = LensStyle & {
  paymentsKey: PaymentsLegendKey;
  contractId?: string;
  residualCents?: number;
};

const key = (type: string, id: string) => `${type}:${id}`;

/** Index of the endpoint resources by resource type and id. */
export function indexMapPayments(
  s: MapPaymentStatus | undefined,
): ReadonlyMap<string, MapPaymentResource> {
  return new Map((s?.resources ?? []).map((r) => [key(r.resource_type, r.resource_id), r]));
}

/** Style of one subject; resources without an entry (free, blocked, no contract) are muted. */
export function paymentsStyle(
  s: Pick<DaySubject, 'resourceId' | 'resourceType' | 'ref'>,
  index: ReadonlyMap<string, MapPaymentResource>,
): PaymentsStyle {
  const r = index.get(key(s.resourceType, s.resourceId));
  if (!r) return { colorToken: 'muted', legendKey: 'none', paymentsKey: 'none' };
  const extra = {
    ...(r.contract_id ? { contractId: r.contract_id } : {}),
    ...(r.residual_cents !== undefined ? { residualCents: r.residual_cents } : {}),
  };
  if (r.suspended) {
    return {
      colorToken: 'blocked',
      glyph: 'lock',
      badge: '€',
      legendKey: 'blocked',
      paymentsKey: 'suspended',
      ...extra,
    };
  }
  switch (r.state) {
    case 'ok':
      return { colorToken: 'ok', legendKey: 'ok', paymentsKey: 'ok', ...extra };
    case 'due_soon':
      return {
        colorToken: 'warn',
        glyph: 'clock',
        legendKey: 'warn',
        paymentsKey: 'due_soon',
        ...extra,
      };
    case 'overdue':
      return {
        colorToken: 'bad',
        glyph: 'alert',
        badge: '€',
        legendKey: 'bad',
        paymentsKey: 'overdue',
        ...extra,
      };
    case 'overdue_60':
      return {
        colorToken: 'bad',
        glyph: 'alert',
        badge: '€€',
        legendKey: 'bad',
        paymentsKey: 'overdue_60',
        ...extra,
      };
    case 'disputed':
      return {
        colorToken: 'override',
        glyph: 'alert',
        badge: '!',
        legendKey: 'override',
        paymentsKey: 'disputed',
        ...extra,
      };
  }
}

export type PaymentsLensResult = {
  styles: Map<string, PaymentsStyle>;
  counts: Record<PaymentsLegendKey, number>;
  dimmed: Set<string>;
};

/** Styles, legend counts and dimmed refs (hidden legend keys) of every subject of the day view. */
export function applyPaymentsLens(
  subjects: readonly DaySubject[],
  status: MapPaymentStatus | undefined,
  hidden: ReadonlySet<string> = new Set(),
): PaymentsLensResult {
  const index = indexMapPayments(status);
  const styles = new Map<string, PaymentsStyle>();
  const counts = Object.fromEntries(PAYMENTS_LEGEND.map((k) => [k, 0])) as Record<
    PaymentsLegendKey,
    number
  >;
  const dimmed = new Set<string>();
  for (const s of subjects) {
    const st = paymentsStyle(s, index);
    styles.set(s.ref, st);
    counts[st.paymentsKey] += 1;
    if (hidden.has(st.paymentsKey)) dimmed.add(s.ref);
  }
  return { styles, counts, dimmed };
}

/** The dock's departure warning (never a block): the resource has an overdue or disputed balance. */
export function hasPaymentWarning(r: Pick<MapPaymentResource, 'state' | 'suspended'>): boolean {
  return r.suspended || r.state === 'overdue' || r.state === 'overdue_60' || r.state === 'disputed';
}
