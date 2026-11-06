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
      'map.view',
      'customers.read',
      'harbor.configure',
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

  it('shows the M1 modules by permission', () => {
    expect(visibleNavItems(['map.view']).map((i) => i.id)).toEqual(['maps']);
    expect(visibleNavItems(['customers.read']).map((i) => i.id)).toEqual([
      'customers',
      'vessels',
      'deadlines',
    ]);
    expect(visibleNavItems(['harbor.configure']).map((i) => i.id)).toEqual(['harbor-settings']);
  });

  it('lists the future modules without the M1 ones', () => {
    expect(UPCOMING_MODULES.length).toBeGreaterThan(0);
    expect(UPCOMING_MODULES).not.toContain('common:nav.modules.map');
    expect(UPCOMING_MODULES).not.toContain('common:nav.modules.customers');
  });
});
