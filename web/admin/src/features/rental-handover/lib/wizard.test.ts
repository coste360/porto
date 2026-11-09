import {
  canCommit,
  checkOutSteps,
  CHECK_IN_STEPS,
  firstOpen,
  parseHours,
  parseRentalQr,
  retainOf,
  stepDone,
} from './wizard';

describe('rental handover wizard', () => {
  it('adds the licence step only when the unit needs one', () => {
    expect(checkOutSteps(true)).toEqual([
      'identity',
      'licence',
      'contract',
      'deposit',
      'boat',
      'summary',
    ]);
    expect(checkOutSteps(false)).not.toContain('licence');
  });

  it('gates the steps on the server missing list', () => {
    const missing = ['contract', 'photos'] as const;
    expect(stepDone('identity', missing)).toBe(true);
    expect(stepDone('contract', missing)).toBe(false);
    expect(stepDone('boat', missing)).toBe(false);
    expect(firstOpen(checkOutSteps(true), missing)).toBe('contract');
    expect(firstOpen(checkOutSteps(true), [])).toBe('summary');
    expect(firstOpen(CHECK_IN_STEPS, ['fuel'])).toBe('boat');
    expect(canCommit([])).toBe(true);
    expect(canCommit(['deposit'])).toBe(false);
    expect(canCommit(undefined)).toBe(false);
  });

  it('computes the retention of a decision within the deposit', () => {
    expect(retainOf('release', 50000, 18000)).toBe(0);
    expect(retainOf('retain_all', 50000, 0)).toBe(50000);
    expect(retainOf('retain', 50000, 18000)).toBe(18000);
    expect(retainOf('retain', 50000, 90000)).toBe(50000);
  });

  it('parses engine hours and the rental QR', () => {
    expect(parseHours('120,5')).toBe(120500);
    expect(parseHours('125')).toBe(125000);
    expect(parseHours('1,2345')).toBeNull();
    expect(parseHours('abc')).toBeNull();
    expect(parseRentalQr('PR1:01900000-0000-7000-8000-000000000000')).toBe(
      '01900000-0000-7000-8000-000000000000',
    );
    expect(parseRentalQr('PT1:01900000-0000-7000-8000-000000000000')).toBeNull();
  });
});
