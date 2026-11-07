import type { Schemas } from '@porto/shared/api';

import { addDays, daysBetween, isIsoDate } from './dates';
import { singlePlacement, solutionPlacements } from './search';

// The transit wizard (m3-plan §8.6) as data: steps, readiness of each step and the createTransit body.

type S = Schemas;

export const TRANSIT_STEPS = ['vessel', 'stay', 'confirm'] as const;
export type TransitStep = (typeof TRANSIT_STEPS)[number];

export type TransitDraft = {
  vesselId: string | null;
  newVessel: S['NewVesselInput'] | null;
  customerId: string | null;
  arrival: string | null;
  departure: string | null;
  solution: S['OccupancySolution'] | null;
  target: S['PlacementInput'] | null;
  expectedArrivalTime: string;
  notes: string;
  /** "Conferma e registra pagamento manuale" (manual driver) instead of "Tieni in opzione". */
  confirm: { reference: string; payAtDeparture: boolean } | null;
  fitOverrideReason: string | null;
};

export function stayValid(d: Pick<TransitDraft, 'arrival' | 'departure'>): boolean {
  return isIsoDate(d.arrival) && isIsoDate(d.departure) && daysBetween(d.arrival, d.departure) >= 1;
}

/** Placements of the draft: the chosen solution's segments, or the target over the whole stay. */
export function draftPlacements(d: TransitDraft): S['SegmentInput'][] | null {
  if (!stayValid(d)) return null;
  if (d.solution) return solutionPlacements(d.solution);
  if (d.target) return singlePlacement(d.target, d.arrival!, d.departure!);
  return null;
}

export function stepReady(step: TransitStep, d: TransitDraft): boolean {
  switch (step) {
    case 'vessel':
      return d.vesselId !== null || d.newVessel !== null;
    case 'stay':
      return draftPlacements(d) !== null;
    case 'confirm':
      // Confirmation needs a customer (422 customer_required otherwise); a hold does not.
      return (
        stepReady('vessel', d) &&
        stepReady('stay', d) &&
        (d.confirm === null || d.customerId !== null)
      );
  }
}

export function transitBody(d: TransitDraft): S['TransitCreate'] | null {
  const placements = draftPlacements(d);
  if (!placements || !stepReady('confirm', d)) return null;
  const body: S['TransitCreate'] = {
    arrival: d.arrival!,
    departure: d.departure!,
    placements,
    channel: 'desk',
  };
  if (d.vesselId) body.vessel_id = d.vesselId;
  else if (d.newVessel) body.new_vessel = d.newVessel;
  if (d.customerId) body.customer_id = d.customerId;
  if (/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(d.expectedArrivalTime))
    body.expected_arrival_time = d.expectedArrivalTime;
  if (d.notes.trim()) body.notes = d.notes.trim();
  if (d.confirm) {
    body.confirm = { pay_at_departure: d.confirm.payAtDeparture };
    if (d.confirm.reference.trim()) body.confirm.reference = d.confirm.reference.trim();
  }
  if (d.fitOverrideReason) body.fit_override = { reason: d.fitOverrideReason };
  return body;
}

export type TransitTab = 'today' | 'upcoming' | 'inPort' | 'all';

/** Filters of a transits tab: arrivals today, arrivals after today, arrived and still in port, everything. */
export function transitTabFilters(
  tab: TransitTab,
  today: string,
): { status?: S['ContractStatus']; arrival_from?: string; arrival_to?: string } {
  switch (tab) {
    case 'today':
      return { arrival_from: today, arrival_to: today };
    case 'upcoming':
      return { arrival_from: addDays(today, 1) };
    case 'inPort':
      return { status: 'arrived' };
    default:
      return {};
  }
}
