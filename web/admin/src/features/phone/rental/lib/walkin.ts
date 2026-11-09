import type { SlotPoint, SlotRange } from '@porto/shared/planner';

const DEFAULT_TIMEZONE = 'Europe/Rome';

/** The half-hour slot in progress at `now` in an IANA timezone, with its marina-local date (ADR 0002). */
export function slotNowIn(timeZone: string, now: Date = new Date()): SlotPoint {
  try {
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(now);
    const get = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
    const slot = Number(get('hour')) * 2 + (Number(get('minute')) >= 30 ? 1 : 0);
    return { date: `${get('year')}-${get('month')}-${get('day')}`, slot };
  } catch {
    return slotNowIn(DEFAULT_TIMEZONE, now);
  }
}

/** The whole hours a walk-in starting at `start` can last within the open window (never past openTo). */
export function walkInHours(start: SlotPoint, openFrom: number, openTo: number): number[] {
  if (start.slot < openFrom || start.slot >= openTo) return [];
  const max = Math.floor((openTo - start.slot) / 2);
  return Array.from({ length: max }, (_, i) => i + 1);
}

/** The range of a walk-in of `hours` from `start` (same date). */
export function walkInRange(start: SlotPoint, hours: number): SlotRange {
  return { start, end: { date: start.date, slot: start.slot + hours * 2 } };
}
