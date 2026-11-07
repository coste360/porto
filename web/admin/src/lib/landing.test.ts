import { describe, expect, it } from 'vitest';

import type { Permission } from '@porto/shared/api';

import { landingFor } from './landing';

const M = 'm-1';

describe('landingFor', () => {
  const cases: [string, Permission[], 'mobile' | 'desktop', string][] = [
    ['dock phone', ['presence.write', 'map.view'], 'mobile', `/m/${M}/dock`],
    ['map viewer phone', ['map.view'], 'mobile', `/m/${M}/dock`],
    ['presence only phone', ['presence.write'], 'mobile', `/m/${M}/dock`],
    ['office phone without quay permissions', ['customers.read'], 'mobile', `/m/${M}`],
    ['dock user on a desktop', ['presence.write', 'map.view'], 'desktop', `/m/${M}`],
    ['no permissions', [], 'mobile', `/m/${M}`],
  ];
  it.each(cases)('%s', (_name, permissions, kind, expected) => {
    expect(landingFor(permissions, kind, M)).toBe(expected);
  });

  it('encodes the marina id', () => {
    expect(landingFor([], 'desktop', 'a/b')).toBe('/m/a%2Fb');
  });
});
