import type { PlacementInput, Schemas } from '@porto/shared/api';

import { addDays, addMonths, daysBetween, isIsoDate } from './dates';

type Term = Schemas['ContractTerm'];

/**
 * Default `ends_on` of a mooring contract (m3-plan D27): annual = start + 1 year (29 Feb → 28 Feb), monthly =
 * + 1 calendar month (end-of-month clamp). Seasonal is left to the server (season window of the active list) and
 * period must be typed, so both return null.
 */
export function defaultEndsOn(term: Term, startsOn: string): string | null {
  switch (term) {
    case 'annual':
      return addMonths(startsOn, 12);
    case 'monthly':
      return addMonths(startsOn, 1);
    default:
      return null;
  }
}

/** Whether the wizard must ask for `ends_on` (period) or may leave it to the server default. */
export function endsOnRequired(term: Term): boolean {
  return term === 'period';
}

export type ContractDraft = {
  term: Term;
  customerId: string | null;
  vesselId: string | null;
  startsOn: string | null;
  /** Null = server default for annual, seasonal and monthly; required for period. */
  endsOn: string | null;
  target: PlacementInput | null;
  /** Land resources added as extra lines (parking, box). */
  extraLandIds: string[];
  notes: string;
  activate: boolean;
  fitOverrideReason: string | null;
};

export type ContractDraftError =
  | 'desk:contract.errors.customer'
  | 'desk:contract.errors.vessel'
  | 'desk:contract.errors.dates'
  | 'desk:contract.errors.placement';

/**
 * createContract body, or the first problem. The placement covers [starts_on, ends_on) when the end is known;
 * with a server-defaulted end (seasonal) the wizard needs it typed, so the defaulted terms send the computed end.
 */
export function contractBody(
  d: ContractDraft,
): { ok: true; body: Schemas['ContractCreate'] } | { ok: false; error: ContractDraftError } {
  if (!d.customerId) return { ok: false, error: 'desk:contract.errors.customer' };
  if (!d.vesselId) return { ok: false, error: 'desk:contract.errors.vessel' };
  if (!isIsoDate(d.startsOn)) return { ok: false, error: 'desk:contract.errors.dates' };
  const endsOn = d.endsOn ?? defaultEndsOn(d.term, d.startsOn);
  if (!isIsoDate(endsOn) || daysBetween(d.startsOn, endsOn) < 1)
    return { ok: false, error: 'desk:contract.errors.dates' };
  if (!d.target) return { ok: false, error: 'desk:contract.errors.placement' };
  const body: Schemas['ContractCreate'] = {
    term: d.term,
    customer_id: d.customerId,
    vessel_id: d.vesselId,
    starts_on: d.startsOn,
    ends_on: endsOn,
    placements: [{ ...d.target, first_night: d.startsOn, last_night: addDays(endsOn, -1) }],
    activate: d.activate,
  };
  const extras = [...new Set(d.extraLandIds)].filter((id) => id !== d.target?.resource_id);
  if (extras.length)
    body.extra_resources = extras.map((land_resource_id) => ({ land_resource_id }));
  if (d.notes.trim()) body.notes = d.notes.trim();
  if (d.fitOverrideReason) body.fit_override = { reason: d.fitOverrideReason };
  return { ok: true, body };
}
