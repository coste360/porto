import { describe, expect, it } from 'vitest';

import { NAV_ITEMS, UPCOMING_MODULES, visibleNavItems } from './nav';

describe('nav', () => {
  it('shows everything to an owner', () => {
    const ids = visibleNavItems([
      'marina.read',
      'marina.configure',
      'users.manage',
      'devices.manage',
      'audit.read',
    ]).map((i) => i.id);
    expect(ids).toEqual(NAV_ITEMS.map((i) => i.id));
  });

  it('filters by permission', () => {
    expect(visibleNavItems(['marina.read', 'devices.pair_self']).map((i) => i.id)).toEqual([
      'dashboard',
      'devices',
    ]);
    expect(visibleNavItems([])).toEqual([]);
  });

  it('lists the future modules', () => {
    expect(UPCOMING_MODULES.length).toBeGreaterThan(0);
  });
});
