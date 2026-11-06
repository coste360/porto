import { describe, expect, it } from 'vitest';

import { MIN_RECT_SIDE_CM } from '@porto/shared/map';

import { buildGenerator, generatorDefaults } from './generators';

describe('buildGenerator minimum rectangle side', () => {
  it('refuses berth sides below MIN_RECT_SIDE_CM and accepts the minimum', () => {
    const base = { ...generatorDefaults('pier'), origin: { x: 0, y: 0 } };
    const tooSmall = buildGenerator(
      'pier',
      { ...base, berth_width_cm: MIN_RECT_SIDE_CM - 1 },
      'g-',
    );
    expect('errors' in tooSmall && tooSmall.errors).toContain('berth_width_cm');

    const taper = buildGenerator('pier', { ...base, berth_length_end_cm: 1 }, 'g-');
    expect('errors' in taper && taper.errors).toContain('berth_length_end_cm');

    const ok = buildGenerator(
      'pier',
      { ...base, berth_width_cm: MIN_RECT_SIDE_CM, berth_length_end_cm: 0 },
      'g-',
    );
    expect('params' in ok).toBe(true);
  });

  it('refuses land grid items below MIN_RECT_SIDE_CM', () => {
    const base = { ...generatorDefaults('land_grid'), origin: { x: 0, y: 0 } };
    const res = buildGenerator('land_grid', { ...base, item_length_cm: 1 }, 'l-');
    expect('errors' in res && res.errors).toContain('item_length_cm');
  });
});
