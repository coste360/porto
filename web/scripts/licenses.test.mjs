import { describe, expect, it } from 'vitest';

import { check } from './licenses.mjs';

describe('licenses', () => {
  it('accepts the allowlist and rejects the rest', () => {
    expect(
      check([
        { name: 'a', version: '1', license: 'MIT' },
        { name: 'b', version: '1', license: '(MIT OR GPL-3.0)' },
        { name: 'c', version: '1', license: 'GPL-3.0' },
        { name: 'd', version: '1', license: { type: 'Apache-2.0' } },
        { name: '@porto/x', version: '0', private: true },
      ]),
    ).toEqual(['c@1: GPL-3.0']);
  });

  it('accepts AND only when every combined licence is allowed', () => {
    expect(
      check([
        { name: 'e', version: '1', license: 'MIT AND ISC AND BSD-3-Clause' },
        { name: 'f', version: '1', license: 'MIT AND GPL-3.0' },
      ]),
    ).toEqual(['f@1: MIT AND GPL-3.0']);
  });

  it('allows OFL-1.1 only for the font', () => {
    expect(
      check([{ name: '@fontsource/titillium-web', version: '5', license: 'OFL-1.1' }]),
    ).toEqual([]);
    expect(check([{ name: 'other-font', version: '1', license: 'OFL-1.1' }])).toEqual([
      'other-font@1: OFL-1.1',
    ]);
  });
});
