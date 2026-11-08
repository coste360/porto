import { describe, expect, it } from 'vitest';

import { ENTRY_LIMIT_BYTES, allJsFiles, firstLoadFiles, measure } from './budget.mjs';

// A Vite manifest shaped like the booking build: the entry imports a shared vendor chunk statically and loads the
// booking flow, the account area and the links lazily.
const manifest = {
  'index.html': {
    file: 'assets/index-a.js',
    isEntry: true,
    imports: ['_vendor-b.js'],
    dynamicImports: ['src/booking/index.tsx', 'src/account/index.tsx'],
    css: ['assets/index-a.css'],
  },
  '_vendor-b.js': { file: 'assets/vendor-b.js', imports: ['_tiny-c.js'] },
  '_tiny-c.js': { file: 'assets/tiny-c.js', imports: ['_vendor-b.js'] },
  'src/booking/index.tsx': {
    file: 'assets/booking-d.js',
    isDynamicEntry: true,
    imports: ['_vendor-b.js'],
  },
  'src/account/index.tsx': { file: 'assets/account-e.js', isDynamicEntry: true },
  'index.css': { file: 'assets/index-a.css' },
};

describe('budget', () => {
  it('counts the entry and its static imports only, once each', () => {
    expect(firstLoadFiles(manifest)).toEqual([
      'assets/index-a.js',
      'assets/vendor-b.js',
      'assets/tiny-c.js',
    ]);
    expect(allJsFiles(manifest)).toHaveLength(5);
  });

  it('passes under the limits and fails over them', () => {
    const small = measure(manifest, () => 10_000);
    expect(small).toMatchObject({ entry: 30_000, total: 50_000, ok: true });
    const bigLazy = measure(manifest, (f) => (f.includes('account') ? 300_000 : 10_000));
    expect(bigLazy.ok).toBe(true);
    const bigEntry = measure(manifest, (f) => (f.includes('vendor') ? ENTRY_LIMIT_BYTES : 10));
    expect(bigEntry).toMatchObject({ ok: false, errors: ['entry'] });
    const bigTotal = measure(manifest, (f) =>
      f.includes('booking') || f.includes('account') ? 250_000 : 10,
    );
    expect(bigTotal.errors).toEqual(['total']);
  });
});
