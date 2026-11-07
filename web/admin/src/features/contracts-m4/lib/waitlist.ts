// Waiting list helpers (spec 13.9, D26): the quote proposed from an entry. Pure.
import type { PlacementInput, Schemas, WaitlistEntry } from '@porto/shared/api';

export function addYear(iso: string): string {
  return `${Number(iso.slice(0, 4)) + 1}${iso.slice(4)}`;
}

export function addDaysIso(iso: string, days: number): string {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** Quote input from a waiting-list entry and the berth chosen by the office (one option, held). */
export function waitlistQuoteInput(
  e: Pick<WaitlistEntry, 'term' | 'customer' | 'vessel' | 'requested_from'>,
  target: PlacementInput,
  today: string,
): Schemas['QuoteInput'] | null {
  if (!e.vessel) return null;
  const startsOn = e.requested_from > today ? e.requested_from : today;
  const endsOn = addYear(startsOn);
  return {
    kind: 'mooring',
    term: e.term,
    customer_id: e.customer.id,
    vessel_id: e.vessel.id,
    starts_on: startsOn,
    ends_on: endsOn,
    options: [
      {
        hold: true,
        placements: [{ ...target, first_night: startsOn, last_night: addDaysIso(endsOn, -1) }],
      },
    ],
  };
}
