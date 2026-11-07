// Rights helpers (spec 14.1).
import type { RightKind, RightStatus } from '@porto/shared/api';

export const RIGHT_KINDS: readonly RightKind[] = ['ownership_share', 'long_term_use', 'membership'];
export const RIGHT_STATUSES: readonly RightStatus[] = [
  'active',
  'transferred',
  'ended',
  'cancelled',
];

export function rightVariant(s: RightStatus) {
  return s === 'active' ? 'default' : s === 'cancelled' ? 'destructive' : 'secondary';
}

/** The ISO date `days` after `iso` (calendar arithmetic in UTC, no time zone involved). */
export function addDaysIso(iso: string, days: number): string {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** Default effective date of a transfer: today, or the day after the right starts when that is later (the
 * transfer must fall strictly inside the right's period). */
export function defaultTransferDate(today: string, rightFrom: string | undefined): string {
  if (!rightFrom) return today;
  const first = addDaysIso(rightFrom, 1);
  return first > today ? first : today;
}
