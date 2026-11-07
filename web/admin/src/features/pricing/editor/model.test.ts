import { describe, expect, it } from 'vitest';

import { makeDocument } from '../test/fixtures';
import {
  applyIdMap,
  bandRanges,
  duplicateRule,
  fromDocument,
  isDirty,
  issueTarget,
  newRule,
  periodOfNight,
  renameBand,
  renamePeriod,
  toSaveBody,
  upsertRule,
  validityYears,
  yearTimeline,
} from './model';

describe('bandRanges', () => {
  it('computes "da–a" from inclusive upper bounds, the next band starting 1 cm above', () => {
    expect(
      bandRanges([{ code: 'L8', max_cm: 800 }, { code: 'L10', max_cm: 1000 }, { code: 'XL' }]),
    ).toEqual([
      { code: 'L8', fromCm: 0, toCm: 800 },
      { code: 'L10', fromCm: 801, toCm: 1000 },
      { code: 'XL', fromCm: 1001, toCm: null },
    ]);
  });

  it('keeps the document order even when it is not increasing (validation reports it)', () => {
    expect(
      bandRanges([
        { code: 'A', max_cm: 1200 },
        { code: 'B', max_cm: 1000 },
      ]),
    ).toEqual([
      { code: 'A', fromCm: 0, toCm: 1200 },
      { code: 'B', fromCm: 1201, toCm: 1000 },
    ]);
  });
});

describe('save body', () => {
  it('keeps ids of saved rows, gives keys to new rows and drops read-only seq', () => {
    const doc = fromDocument(makeDocument());
    const rule = newRule('modifier', 'Multiscafo');
    const body = toSaveBody(upsertRule(doc, rule));
    expect(body.rules).toHaveLength(2);
    expect(body.rules[0]).toMatchObject({ id: 'r1' });
    expect(body.rules[0]).not.toHaveProperty('seq');
    expect(body.rules[0]).not.toHaveProperty('key');
    expect(body.rules[1]!.id).toBeUndefined();
    expect(body.rules[1]!.key).toBe(rule.key);
    expect(body.rules[1]!.key).toMatch(/^[A-Za-z0-9_:-]+$/);
    expect(body.periods.map((p) => p.id)).toEqual(['p1', 'p2']);
    expect(JSON.stringify(body)).not.toContain('undefined');
  });

  it('the document of the server is not dirty; an edit makes it dirty', () => {
    const saved = fromDocument(makeDocument());
    expect(isDirty(fromDocument(makeDocument()), saved)).toBe(false);
    expect(isDirty({ ...saved, code: 'T-2027B' }, saved)).toBe(true);
  });

  it('upsert replaces by identity, duplicate creates a new row', () => {
    const doc = fromDocument(makeDocument());
    const edited = upsertRule(doc, { ...doc.rules[0]!, unit_cents: 800 });
    expect(edited.rules).toHaveLength(1);
    expect(edited.rules[0]!.unit_cents).toBe(800);
    const copy = duplicateRule(doc.rules[0]!, '(copia)');
    expect(copy.id).toBeUndefined();
    expect(copy.key).toBeTruthy();
    expect(copy.name).toBe('Transito alta (copia)');
  });

  it('maps keys to the ids returned by the save', () => {
    const doc = fromDocument(makeDocument());
    const rule = newRule('base', 'Nuovo');
    const mapped = applyIdMap(upsertRule(doc, rule), { [rule.key!]: 'r9' });
    expect(mapped.rules[1]).toMatchObject({ id: 'r9' });
    expect(mapped.rules[1]!.key).toBeUndefined();
  });
});

describe('renames', () => {
  it('renaming a period updates the rule scopes', () => {
    const doc = fromDocument(makeDocument());
    const next = renamePeriod(doc, 1, 'ALTISSIMA');
    expect(next.periods[1]!.code).toBe('ALTISSIMA');
    expect(next.rules[0]!.scope?.periods).toEqual(['ALTISSIMA']);
  });

  it('renaming a band updates length_band amounts', () => {
    const doc = fromDocument(makeDocument());
    const withRule = upsertRule(doc, {
      key: 'r:x',
      stage: 'base',
      name: 'Annuale',
      basis: 'length_band',
      unit: 'year',
      item_id: 'i1',
      amounts: { L10: 300000, L12: 360000 },
    });
    const next = renameBand(withRule, 1, 'M12');
    expect(next.bands[1]!.code).toBe('M12');
    expect(next.rules[1]!.amounts).toEqual({ L10: 300000, M12: 360000 });
  });

  it('a code can be cleared and typed again', () => {
    const doc = fromDocument(makeDocument());
    const cleared = renamePeriod(doc, 0, '');
    expect(cleared.periods[0]!.code).toBe('');
    expect(renamePeriod(cleared, 0, 'B').periods[0]!.code).toBe('B');
  });
});

describe('period timeline', () => {
  const doc = makeDocument();

  it('colours each night with its period and events override seasons', () => {
    const periods = [
      ...doc.periods,
      {
        code: 'SALONE',
        name: { it: 'Salone nautico' },
        kind: 'event' as const,
        color: 'chart-1' as const,
        ranges: [{ from: '2027-09-15', to: '2027-09-20' }],
      },
    ];
    expect(periodOfNight(periods, '2027-09-16')?.code).toBe('SALONE');
    expect(periodOfNight(periods, '2027-09-21')?.code).toBe('ALTA');
    const months = yearTimeline(periods, 2027, { from: '2027-01-01', to: '2027-12-31' });
    expect(months).toHaveLength(12);
    expect(months[1]!.days).toHaveLength(28);
    expect(months[5]!.days[0]).toMatchObject({
      date: '2027-06-01',
      period: 'ALTA',
      color: 'chart-5',
    });
    expect(months[8]!.days[14]).toMatchObject({ period: 'SALONE', color: 'chart-1' });
    expect(months[0]!.days[0]).toMatchObject({ period: 'BASSA', inValidity: true });
  });

  it('marks nights without period and outside validity', () => {
    const months = yearTimeline([], 2028, { from: '2028-03-01', to: '2028-12-31' });
    expect(months[1]!.days).toHaveLength(29);
    expect(months[0]!.days[0]).toMatchObject({ period: null, color: null, inValidity: false });
    expect(months[2]!.days[0]).toMatchObject({ inValidity: true });
  });

  it('lists the years of the validity', () => {
    expect(validityYears('2027-07-01', '2028-06-30')).toEqual([2027, 2028]);
    expect(validityYears('2027-01-01', '2027-12-31')).toEqual([2027]);
  });
});

describe('issueTarget', () => {
  it('maps the issue reference to the tab and row', () => {
    expect(
      issueTarget({ code: 'period_overlap', severity: 'error', ref: { kind: 'period', id: 'p1' } }),
    ).toEqual({
      tab: 'periods',
      ref: 'p1',
    });
    expect(
      issueTarget({
        code: 'rule_basis_invalid',
        severity: 'error',
        ref: { kind: 'rule', id: 'r:1', field: 'unit_cents' },
      }),
    ).toEqual({ tab: 'rules', ref: 'r:1', field: 'unit_cents' });
    expect(
      issueTarget({ code: 'primary_item_missing', severity: 'error', ref: { kind: 'settings' } }),
    ).toEqual({
      tab: 'general',
    });
  });
});
