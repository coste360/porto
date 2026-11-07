import { describe, expect, it } from 'vitest';

import doc from '../../../../testdata/money/parse.json';
import { centsToInput, parseCents } from './parse';

describe('parseCents', () => {
  it('agrees with Go money.ParseCents on the shared vectors', () => {
    for (const v of doc.vectors) {
      expect(parseCents(v.input), v.input).toBe(v.cents);
    }
  });

  it('formats cents for the input field', () => {
    expect(centsToInput(123456)).toBe('1234,56');
    expect(centsToInput(-5, 'en')).toBe('-0.05');
    expect(parseCents(centsToInput(98765))).toBe(98765);
  });
});
