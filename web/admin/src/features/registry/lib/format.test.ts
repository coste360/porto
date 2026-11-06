import { describe, expect, it } from 'vitest';

import { formatShare, parseLeadDays, parseSharePercent } from './format';

describe('owner shares', () => {
  it.each([
    ['100', 1000],
    ['60', 600],
    ['33,3', 333],
    ['33.3', 333],
    [' 0,1 ', 1],
    ['0', null],
    ['100,1', null],
    ['33,33', null],
    ['abc', null],
    ['', null],
  ])('parses %j → %s', (text, millis) => {
    expect(parseSharePercent(text)).toBe(millis);
  });

  it('formats per-mille as percent', () => {
    expect(formatShare(1000)).toBe('100');
    expect(formatShare(333)).toBe('33,3');
    expect(formatShare(333, 'en')).toBe('33.3');
    expect(formatShare(null)).toBe('');
  });
});

describe('parseLeadDays', () => {
  it('returns unique days, descending', () => {
    expect(parseLeadDays('15, 60;15 30')).toEqual([60, 30, 15]);
    expect(parseLeadDays('')).toEqual([]);
  });
  it('refuses non-integers and values above 730', () => {
    expect(parseLeadDays('1.5')).toBeNull();
    expect(parseLeadDays('731')).toBeNull();
    expect(parseLeadDays('-3')).toBeNull();
  });
});
