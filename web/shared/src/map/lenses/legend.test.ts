import { describe, expect, it } from 'vitest';

import { daySubjects } from '../dayView';
import {
  DEFAULT_LENS_CONTEXT,
  applyLens,
  legendCounts,
  matchesFilters,
  type ResourceMeta,
} from './index';
import { DATE, makeDayView } from './testFixtures';

const ctx = { date: DATE, ...DEFAULT_LENS_CONTEXT };
const dv = makeDayView();
const meta = new Map<string, ResourceMeta>([
  ['a1', { structureId: 'pier-a', groupIds: ['g1'], category: 'C1' }],
  ['a2', { structureId: 'pier-a', groupIds: ['g1'], category: 'C2' }],
  ['a3', { structureId: 'pier-b', groupIds: [], category: 'C2' }],
  ['area-1', { structureId: 'pier-b', groupIds: [] }],
]);

describe('legend counts', () => {
  it('counts every subject once per legend key, area vessels included', () => {
    const counts = legendCounts(dv, 'occupancy', ctx);
    expect(counts).toMatchObject({
      free: 1,
      annual: 1,
      transit: 3,
      hold: 1,
      released: 1,
      blocked: 1,
      closed: 1,
      maintenance: 1,
      seasonal: 0,
    });
    const total = Object.values(counts).reduce((a, b) => a + b, 0);
    expect(total).toBe(daySubjects(dv).length);
  });

  it('counts only the filtered elements and greys the others', () => {
    const r = applyLens(dv, 'occupancy', ctx, { structureId: 'pier-a' }, meta);
    expect(Object.values(r.counts).reduce((a, b) => a + b, 0)).toBe(2);
    expect(r.counts).toMatchObject({ free: 1, annual: 1, transit: 0 });
    expect(r.dimmed.has('a3')).toBe(true);
    expect(r.dimmed.has('a2')).toBe(false);
    // Styles exist for greyed elements too: they are dimmed, not hidden.
    expect(r.styles.get('a3')?.legendKey).toBe('transit');
  });

  it('a legend key unchecked dims its elements but keeps the counts', () => {
    const r = applyLens(dv, 'occupancy', ctx, {}, meta, new Set(['transit']));
    expect(r.counts.transit).toBe(3);
    expect([...r.dimmed].sort()).toEqual(['a3', 'alloc:al-8', 'alloc:al-9']);
  });

  it('filters by group, category, LOA band, contract type and free text', () => {
    const subjects = daySubjects(dv);
    const pick = (f: Parameters<typeof matchesFilters>[1]) =>
      subjects.filter((s) => matchesFilters(s, f, meta)).map((s) => s.ref);
    expect(pick({ groupId: 'g1' })).toEqual(['a1', 'a2']);
    expect(pick({ category: 'C2' })).toEqual(['a2', 'a3']);
    expect(pick({ loaMin: 1000, loaMax: 1150 })).toEqual(['a3']);
    expect(pick({ contractType: 'seasonal' })).toEqual(['a5']);
    expect(pick({ text: 'rossi' })).toEqual(['a2']);
    expect(pick({ text: 'delf' })).toEqual(['alloc:al-9']);
    expect(pick({ text: 'A8' })).toEqual(['a8']);
  });
});
