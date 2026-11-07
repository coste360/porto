// Go ↔ TypeScript parity of fit.Check (m3-plan §2.1): this test and the Go test TestFit_ParityVectors read the
// same file testdata/fit/cases.json at the repo root.
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { checkFit, type FitTargetKind } from './fit';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', '..');
const CASES = join(ROOT, 'testdata', 'fit', 'cases.json');

type Opt = number | null | undefined;

type Vector = {
  name: string;
  vessel: {
    loa_cm: number;
    beam_cm: number;
    draft_cm?: Opt;
    air_draft_cm?: Opt;
    hull?: string;
    hull_type?: string;
    power_required_a?: Opt;
    insurance_until?: string | null;
  };
  target: {
    kind: FitTargetKind;
    max_loa_cm?: number;
    max_beam_cm?: number;
    length_cm?: number;
    width_cm?: number;
    depth_cm?: Opt;
    max_air_draft_cm?: Opt;
    multihull_ok?: boolean;
    overhang_allowed?: boolean;
    power_a?: Opt;
    land_kind?: string;
  };
  last_night: string;
  rules: {
    loa_tolerance_cm?: number;
    safety_clearance_cm?: number;
    depth_warn_margin_cm?: number;
    waste_threshold_bp?: number;
  };
  expect: { ok: boolean; blocking: string[]; warnings: string[]; fill_bp: number };
};

function run(v: Vector) {
  const r = checkFit(
    {
      loa: v.vessel.loa_cm,
      beam: v.vessel.beam_cm,
      draft: v.vessel.draft_cm,
      airDraft: v.vessel.air_draft_cm,
      hull: v.vessel.hull ?? v.vessel.hull_type ?? 'other',
      powerRequiredA: v.vessel.power_required_a,
      insuranceUntil: v.vessel.insurance_until ?? null,
    },
    {
      kind: v.target.kind,
      maxLoa: v.target.max_loa_cm ?? 0,
      maxBeam: v.target.max_beam_cm ?? 0,
      length: v.target.length_cm ?? 0,
      width: v.target.width_cm ?? 0,
      depth: v.target.depth_cm,
      maxAirDraft: v.target.max_air_draft_cm,
      multihullOk: v.target.multihull_ok ?? false,
      overhangAllowed: v.target.overhang_allowed ?? false,
      powerA: v.target.power_a,
      landKind: v.target.land_kind,
    },
    v.last_night,
    {
      loaTolerance: v.rules.loa_tolerance_cm ?? 0,
      safetyClearance: v.rules.safety_clearance_cm ?? 0,
      depthWarnMargin: v.rules.depth_warn_margin_cm ?? 0,
      wasteThresholdBp: v.rules.waste_threshold_bp ?? 0,
    },
  );
  return {
    ok: r.ok,
    blocking: r.blocking.map((i) => i.code),
    warnings: r.warnings.map((i) => i.code),
    fill_bp: r.fillBp,
  };
}

const present = existsSync(CASES);

describe('fit parity vectors (testdata/fit/cases.json)', () => {
  // The vectors are written by OCCUPANCY-ENGINE (m3-plan §2.1); until they land the suite is skipped.
  it.skipIf(!present)('matches every vector', () => {
    const file = JSON.parse(readFileSync(CASES, 'utf8')) as { version: number; cases: Vector[] };
    expect(file.version).toBe(1);
    expect(file.cases.length).toBeGreaterThanOrEqual(60);
    for (const v of file.cases) {
      expect({ name: v.name, ...run(v) }).toEqual({
        name: v.name,
        ok: v.expect.ok,
        blocking: v.expect.blocking,
        warnings: v.expect.warnings,
        fill_bp: v.expect.fill_bp,
      });
    }
  });
});
