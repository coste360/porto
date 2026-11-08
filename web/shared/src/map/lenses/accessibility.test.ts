import { describe, expect, it } from 'vitest';

import { accessibilityOf, accessibilityStyles, blockedExitsByDate } from './accessibility';

const D = '2026-04-10';

describe('accessibility lens', () => {
  const entries = [
    { allocation_id: 'a', event: 'exit' as const, on: D, accessible: true, order: 1, blocking: [] },
    { allocation_id: 'b', event: 'exit' as const, on: D, accessible: false, blocking: ['c', 'a'] },
    { allocation_id: 'd', event: 'arrival' as const, on: D, accessible: false, blocking: ['c'] },
    {
      allocation_id: 'e',
      event: 'exit' as const,
      on: '2026-04-11',
      accessible: false,
      blocking: ['a'],
    },
  ];

  it('colours blocked, blocking and leaving boats of the date, never by colour only', () => {
    const s = accessibilityStyles(entries, D);
    expect(accessibilityOf(s, 'a')).toMatchObject({ key: 'leaving', badge: '1', outline: 'bad' });
    expect(accessibilityOf(s, 'b')).toMatchObject({
      key: 'blocked',
      glyph: 'lock',
      blockedBy: ['c', 'a'],
    });
    expect(accessibilityOf(s, 'c')).toMatchObject({ key: 'blocking', outline: 'bad' });
    expect(accessibilityOf(s, 'd')).toMatchObject({ key: 'arrival_blocked', glyph: 'arrowIn' });
    expect(accessibilityOf(s, 'e').key).toBe('ok');
    expect(accessibilityOf(s, 'zzz').key).toBe('ok');
  });

  it('counts blocked exits by date', () => {
    expect(Object.fromEntries(blockedExitsByDate(entries))).toEqual({ [D]: 1, '2026-04-11': 1 });
  });
});
