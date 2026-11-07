import type { Schemas } from '@porto/shared/api';

import { addDays, daysBetween, isIsoDate, lastNightOf } from './dates';

// Incoming-vessel search form (spec 9.6, m3-plan §8.6) and its mapping to the API request. Lengths stay integer
// centimetres from the inputs to the wire.

type S = Schemas;
export type ResourceType = S['ResourceType'];
export type HullType = S['HullType'];
export type Solution = S['OccupancySolution'];

export const RESOURCE_TYPES = [
  'berth',
  'dynamic_area',
  'land_resource',
] as const satisfies readonly ResourceType[];

export type SearchForm = {
  purpose: 'transit' | 'contract';
  term: S['ContractTerm'];
  /** Existing vessel, or null to search by dimensions. */
  vesselId: string | null;
  loaCm: number | null;
  beamCm: number | null;
  draftCm: number | null;
  hullType: HullType;
  arrival: string | null;
  departure: string | null;
  resourceTypes: ResourceType[];
  considerOut: boolean;
  maxSegments: number;
  powerA: number | null;
  water: boolean;
};

export function emptySearchForm(today: string): SearchForm {
  return {
    purpose: 'transit',
    term: 'annual',
    vesselId: null,
    loaCm: null,
    beamCm: null,
    draftCm: null,
    hullType: 'monohull_motor',
    arrival: today,
    departure: addDays(today, 1),
    resourceTypes: ['berth', 'dynamic_area'],
    considerOut: false,
    maxSegments: 0,
    powerA: null,
    water: false,
  };
}

export type SearchFormError =
  'desk:search.errors.vessel' | 'desk:search.errors.dates' | 'desk:search.errors.resourceTypes';

/** Builds the search request, or the i18n key of the first problem of the form. */
export function toSearchRequest(
  f: SearchForm,
): { ok: true; request: S['OccupancySearchRequest'] } | { ok: false; error: SearchFormError } {
  if (!f.vesselId && (f.loaCm === null || f.beamCm === null || f.loaCm <= 0 || f.beamCm <= 0)) {
    return { ok: false, error: 'desk:search.errors.vessel' };
  }
  if (!isIsoDate(f.arrival) || !isIsoDate(f.departure) || daysBetween(f.arrival, f.departure) < 1) {
    return { ok: false, error: 'desk:search.errors.dates' };
  }
  if (f.resourceTypes.length === 0) return { ok: false, error: 'desk:search.errors.resourceTypes' };
  const request: S['OccupancySearchRequest'] = {
    purpose: f.purpose,
    arrival: f.arrival,
    departure: f.departure,
    resource_types: [...f.resourceTypes],
    channel: 'desk',
  };
  if (f.vesselId) request.vessel_id = f.vesselId;
  else {
    request.vessel = { loa_cm: f.loaCm!, beam_cm: f.beamCm!, hull_type: f.hullType };
    if (f.draftCm !== null && f.draftCm > 0) request.vessel.draft_cm = f.draftCm;
  }
  if (f.purpose === 'contract') request.term = f.term;
  if (f.considerOut) request.consider_out = true;
  if (f.maxSegments > 0) request.max_segments = f.maxSegments;
  const prefs: NonNullable<S['OccupancySearchRequest']['prefs']> = {};
  if (f.powerA !== null) prefs.power_a = f.powerA;
  if (f.water) prefs.water = true;
  if (Object.keys(prefs).length > 0) request.prefs = prefs;
  return { ok: true, request };
}

/** Segments of a solution as placements for createTransit / quote options / contracts. */
export function solutionPlacements(s: Solution): S['SegmentInput'][] {
  return s.segments.map((seg) => {
    const p: S['SegmentInput'] = {
      resource_type: seg.resource_type,
      resource_id: seg.resource_id,
      first_night: seg.first_night,
      last_night: seg.last_night,
    };
    if (seg.x_cm !== undefined) p.x_cm = seg.x_cm;
    if (seg.y_cm !== undefined) p.y_cm = seg.y_cm;
    if (seg.rotation_dd !== undefined) p.rotation_dd = seg.rotation_dd;
    return p;
  });
}

/** One placement covering the whole stay [arrival, departure). */
export function singlePlacement(
  target: S['PlacementInput'],
  arrival: string,
  departure: string,
): S['SegmentInput'][] {
  return [{ ...target, first_night: arrival, last_night: lastNightOf(departure) }];
}

/** Codes of the solution's resources, "A12 → B03" for a segmented stay. */
export function solutionCodes(s: Solution): string {
  return s.segments.map((seg) => seg.code).join(' → ');
}

/** Resource ids of a solution, for the map highlight. */
export function solutionResourceIds(s: Solution): string[] {
  return s.segments.map((seg) => seg.resource_id);
}
