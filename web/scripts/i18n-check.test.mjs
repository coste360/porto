import { describe, expect, it } from 'vitest';

import { checkUsages, compareLocales, findKeyLiterals, flatten, run } from './i18n-check.mjs';

describe('i18n-check', () => {
  it('flattens nested keys', () => {
    expect([...flatten({ a: { b: 'x' }, c: 'y' }).keys()]).toEqual(['a.b', 'c']);
  });

  it('reports missing, extra and empty keys', () => {
    const errors = compareLocales({
      it: { common: { a: 'A', b: 'B' } },
      en: { common: { a: 'A', c: 'C', b: ' ' } },
    });
    expect(errors.some((e) => e.includes('"c" is not in it'))).toBe(true);
    expect(errors.some((e) => e.includes('empty or non-string value for "b"'))).toBe(true);

    const missing = compareLocales({ it: { common: { a: 'A' } }, en: { common: {} } });
    expect(missing).toEqual(['en/common.json: missing "a"']);
  });

  it('finds key literals and ignores interpolated ones', () => {
    const text = "t('common:a.b'); t(`errors:${x}`); z.string().min(1, 'auth:login.email');";
    expect(findKeyLiterals(text, ['common', 'errors', 'auth'])).toEqual([
      { ns: 'common', key: 'a.b' },
      { ns: 'auth', key: 'login.email' },
    ]);
  });

  it('reports unknown keys', () => {
    const errors = checkUsages([['a.tsx', "t('common:nope')"]], { common: { yes: 'Y' } });
    expect(errors).toEqual(['a.tsx: unknown i18n key "common:nope"']);
  });

  it('passes on the repository', () => {
    expect(run()).toEqual([]);
  });
});
