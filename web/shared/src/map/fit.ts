// TypeScript mirror of internal/domain/fit (m3-plan §2.1): whether a vessel is compatible with a berth, a
// dynamic area or a land resource. Pure and integer only (lengths in cm, ratios in basis points). The drag
// preview uses it to colour the ghost; the server check is the only guarantee. fit.parity.test.ts runs the
// shared vectors of testdata/fit/cases.json against this file.
import { divHalfEven } from './geometry';

export type FitTargetKind = 'berth' | 'linear_area' | 'surface_area' | 'land';

export type FitVessel = {
  loa: number;
  beam: number;
  /** Unknown values skip the related check. */
  draft?: number | null;
  airDraft?: number | null;
  hull: string;
  powerRequiredA?: number | null;
  /** Latest expiry (YYYY-MM-DD) of a valid insurance policy; null when none is on file. */
  insuranceUntil?: string | null;
};

export type FitTarget = {
  kind: FitTargetKind;
  /** 0 = unlimited. Land: length/width of the resource. */
  maxLoa: number;
  maxBeam: number;
  /** Berth length/width (fill ratio); land length/width. */
  length: number;
  width: number;
  depth?: number | null;
  maxAirDraft?: number | null;
  multihullOk: boolean;
  overhangAllowed: boolean;
  powerA?: number | null;
  /** Land only. */
  landKind?: string;
};

export type FitRules = {
  loaTolerance: number;
  safetyClearance: number;
  depthWarnMargin: number;
  wasteThresholdBp: number;
};

export const DEFAULT_FIT_RULES: FitRules = {
  loaTolerance: 0,
  safetyClearance: 30,
  depthWarnMargin: 10,
  wasteThresholdBp: 6000,
};

export type FitCode =
  | 'loa_exceeds'
  | 'beam_exceeds'
  | 'draft_exceeds'
  | 'air_draft_exceeds'
  | 'multihull_not_allowed'
  | 'vessel_invalid'
  | 'fill_below_threshold'
  | 'depth_near_limit'
  | 'power_insufficient'
  | 'insurance_missing'
  | 'insurance_expires_in_period'
  | 'overhang';

export type FitIssue = { code: FitCode; limit?: number; actual?: number };

export type FitOutcome = {
  ok: boolean;
  blocking: FitIssue[];
  warnings: FitIssue[];
  fillBp: number;
  overridable: boolean;
};

const MULTIHULLS = new Set(['catamaran', 'trimaran']);
/** Land kinds that hold a boat (cradle, trailer): the others never block. */
const BOAT_LAND_KINDS = new Set(['cradle', 'trailer_spot']);

function known(n: number | null | undefined): n is number {
  return typeof n === 'number' && Number.isSafeInteger(n);
}

function byCode(a: FitIssue, b: FitIssue): number {
  return a.code < b.code ? -1 : a.code > b.code ? 1 : 0;
}

/** Vessel LOA×Beam over Length×Width in basis points, half-even, capped at 100 % (0 without a size). */
export function fillBp(loa: number, beam: number, length: number, width: number): number {
  if (length <= 0 || width <= 0) return 0;
  return Math.min(divHalfEven(loa * beam * 10_000, length * width), 10_000);
}

/**
 * Compatibility of v with t for a stay whose last night is lastNight (insurance warnings). Blocking codes:
 * loa_exceeds, beam_exceeds, draft_exceeds, air_draft_exceeds, multihull_not_allowed, vessel_invalid (not
 * overridable). Overlap is never a fit issue: it is the occupancy rows' job.
 */
export function checkFit(
  v: FitVessel,
  t: FitTarget,
  lastNight: string,
  r: FitRules = DEFAULT_FIT_RULES,
): FitOutcome {
  if (!known(v.loa) || !known(v.beam) || v.loa <= 0 || v.beam <= 0) {
    return {
      ok: false,
      blocking: [{ code: 'vessel_invalid' }],
      warnings: [],
      fillBp: 0,
      overridable: false,
    };
  }
  const blocking: FitIssue[] = [];
  const warnings: FitIssue[] = [];
  const isLand = t.kind === 'land';
  const boatLand = isLand && BOAT_LAND_KINDS.has(t.landKind ?? '');

  if (isLand && !boatLand) {
    return { ok: true, blocking, warnings, fillBp: 0, overridable: true };
  }

  if (t.maxLoa > 0) {
    if (v.loa > t.maxLoa + r.loaTolerance) {
      blocking.push({ code: 'loa_exceeds', limit: t.maxLoa + r.loaTolerance, actual: v.loa });
    } else if (v.loa > t.maxLoa && !t.overhangAllowed) {
      warnings.push({ code: 'overhang', limit: t.maxLoa, actual: v.loa });
    }
  }
  if (t.maxBeam > 0 && v.beam > t.maxBeam) {
    blocking.push({ code: 'beam_exceeds', limit: t.maxBeam, actual: v.beam });
  }
  if (known(v.draft) && known(t.depth)) {
    const need = v.draft + r.safetyClearance;
    if (need > t.depth) {
      blocking.push({ code: 'draft_exceeds', limit: t.depth, actual: need });
    } else if (need + r.depthWarnMargin > t.depth) {
      warnings.push({ code: 'depth_near_limit', limit: t.depth, actual: need });
    }
  }
  if (known(v.airDraft) && known(t.maxAirDraft) && v.airDraft > t.maxAirDraft) {
    blocking.push({ code: 'air_draft_exceeds', limit: t.maxAirDraft, actual: v.airDraft });
  }
  if (MULTIHULLS.has(v.hull) && !t.multihullOk) blocking.push({ code: 'multihull_not_allowed' });

  const fill =
    t.kind === 'linear_area' || t.kind === 'surface_area'
      ? 10_000
      : fillBp(v.loa, v.beam, t.length, t.width);
  if (t.kind === 'berth' && fill < r.wasteThresholdBp)
    warnings.push({ code: 'fill_below_threshold' });
  if (
    !isLand &&
    known(v.powerRequiredA) &&
    v.powerRequiredA > 0 &&
    (!known(t.powerA) || t.powerA < v.powerRequiredA)
  ) {
    warnings.push({ code: 'power_insufficient' });
  }
  if (!v.insuranceUntil) warnings.push({ code: 'insurance_missing' });
  else if (v.insuranceUntil < lastNight) warnings.push({ code: 'insurance_expires_in_period' });

  blocking.sort(byCode);
  warnings.sort(byCode);
  return {
    ok: blocking.length === 0,
    blocking,
    warnings,
    fillBp: fill,
    overridable: true,
  };
}
