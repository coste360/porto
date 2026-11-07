import { describe, expect, it } from 'vitest';

import { DEFAULT_FIT_RULES, checkFit, fillBp, type FitTarget, type FitVessel } from './fit';

const vessel = (over: Partial<FitVessel> = {}): FitVessel => ({
  loa: 1200,
  beam: 400,
  hull: 'monohull_sail',
  insuranceUntil: '2027-12-31',
  ...over,
});

const berth = (over: Partial<FitTarget> = {}): FitTarget => ({
  kind: 'berth',
  maxLoa: 1200,
  maxBeam: 420,
  length: 1300,
  width: 450,
  multihullOk: false,
  overhangAllowed: false,
  ...over,
});

const codes = (r: { code: string }[]) => r.map((i) => i.code);

describe('checkFit', () => {
  it('accepts a fitting vessel with its fill ratio in basis points', () => {
    const r = checkFit(vessel(), berth(), '2026-12-31');
    expect(r.ok).toBe(true);
    expect(r.blocking).toEqual([]);
    expect(r.fillBp).toBe(fillBp(1200, 400, 1300, 450));
    expect(Number.isInteger(r.fillBp)).toBe(true);
  });

  it('applies the LOA tolerance and reports the overhang inside it', () => {
    const rules = { ...DEFAULT_FIT_RULES, loaTolerance: 50 };
    expect(codes(checkFit(vessel({ loa: 1250 }), berth(), '2026-12-31', rules).warnings)).toContain(
      'overhang',
    );
    const over = checkFit(vessel({ loa: 1251 }), berth(), '2026-12-31', rules);
    expect(codes(over.blocking)).toEqual(['loa_exceeds']);
    expect(over.blocking[0]).toMatchObject({ limit: 1250, actual: 1251 });
    expect(over.overridable).toBe(true);
    expect(
      codes(
        checkFit(vessel({ loa: 1250 }), berth({ overhangAllowed: true }), '2026-12-31', rules)
          .warnings,
      ),
    ).not.toContain('overhang');
  });

  it('has no tolerance on the beam (boundary ±1 cm)', () => {
    expect(checkFit(vessel({ beam: 420 }), berth(), '2026-12-31').ok).toBe(true);
    expect(codes(checkFit(vessel({ beam: 421 }), berth(), '2026-12-31').blocking)).toEqual([
      'beam_exceeds',
    ]);
  });

  it('checks the draft with the safety clearance and warns near the limit', () => {
    const t = berth({ depth: 230 });
    expect(codes(checkFit(vessel({ draft: 201 }), t, '2026-12-31').blocking)).toEqual([
      'draft_exceeds',
    ]);
    expect(codes(checkFit(vessel({ draft: 200 }), t, '2026-12-31').warnings)).toContain(
      'depth_near_limit',
    );
    expect(codes(checkFit(vessel({ draft: 190 }), t, '2026-12-31').warnings)).not.toContain(
      'depth_near_limit',
    );
    // Unknown draft or depth: skipped.
    expect(checkFit(vessel({ draft: null }), t, '2026-12-31').ok).toBe(true);
    expect(checkFit(vessel({ draft: 500 }), berth(), '2026-12-31').ok).toBe(true);
  });

  it('refuses multihulls without the berth tag, always accepts them in areas', () => {
    expect(codes(checkFit(vessel({ hull: 'catamaran' }), berth(), '2026-12-31').blocking)).toEqual([
      'multihull_not_allowed',
    ]);
    expect(
      checkFit(vessel({ hull: 'trimaran' }), berth({ multihullOk: true }), '2026-12-31').ok,
    ).toBe(true);
    expect(
      checkFit(
        vessel({ hull: 'catamaran' }),
        {
          kind: 'linear_area',
          maxLoa: 0,
          maxBeam: 0,
          length: 0,
          width: 0,
          multihullOk: true,
          overhangAllowed: false,
        },
        '2026-12-31',
      ),
    ).toMatchObject({ ok: true, fillBp: 10_000 });
  });

  it('reports vessel_invalid as not overridable', () => {
    const r = checkFit(vessel({ loa: 0 }), berth(), '2026-12-31');
    expect(r).toMatchObject({ ok: false, overridable: false });
    expect(codes(r.blocking)).toEqual(['vessel_invalid']);
  });

  it('warns about insurance, power and waste, sorted by code', () => {
    const r = checkFit(
      vessel({ insuranceUntil: '2026-11-30', powerRequiredA: 32, loa: 600, beam: 250 }),
      berth({ powerA: 16 }),
      '2026-12-31',
    );
    expect(codes(r.warnings)).toEqual([
      'fill_below_threshold',
      'insurance_expires_in_period',
      'power_insufficient',
    ]);
    expect(
      codes(checkFit(vessel({ insuranceUntil: null }), berth(), '2026-12-31').warnings),
    ).toEqual(['insurance_missing']);
  });

  it('never blocks on land kinds that do not hold a boat', () => {
    const land = (landKind: string): FitTarget => ({
      kind: 'land',
      maxLoa: 500,
      maxBeam: 250,
      length: 500,
      width: 250,
      multihullOk: false,
      overhangAllowed: false,
      landKind,
    });
    for (const k of ['parking', 'box', 'storeroom', 'locker', 'custom']) {
      expect(checkFit(vessel(), land(k), '2026-12-31')).toMatchObject({ ok: true, fillBp: 0 });
    }
    for (const k of ['cradle', 'trailer_spot']) {
      expect(codes(checkFit(vessel(), land(k), '2026-12-31').blocking)).toEqual([
        'beam_exceeds',
        'loa_exceeds',
      ]);
    }
  });
});
