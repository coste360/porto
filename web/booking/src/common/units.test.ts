import { describe, expect, it } from 'vitest';

import { cmToFeetText, cmToMetresText, formatLength } from './units';

describe('units', () => {
  it('shows metres with two decimals from integer cm', () => {
    expect(cmToMetresText(1150, 'it')).toBe('11,50');
    expect(cmToMetresText(1205, 'en')).toBe('12.05');
    expect(cmToMetresText(90, 'it')).toBe('0,90');
  });

  it('converts cm to feet for display only (1 ft = 30.48 cm)', () => {
    expect(cmToFeetText(3048, 'en')).toBe('100.0');
    expect(cmToFeetText(1219, 'it')).toBe('40,0');
    expect(cmToFeetText(1150, 'it')).toBe('37,7');
  });

  it('formats a length with or without feet', () => {
    expect(formatLength(1150, 'it', true)).toBe('11,50 m (37,7 ft)');
    expect(formatLength(1150, 'en', false)).toBe('11.50 m');
  });
});
