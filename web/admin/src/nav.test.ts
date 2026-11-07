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
      'pricing.read',
      'pricing.configure',
      'contracts.read',
      'presence.write',
      'contracts.write',
      'rights.manage',
      'templates.configure',
      'communications.send',
      'providers.configure',
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
    expect(visibleNavItems(['map.view']).map((i) => i.id)).toEqual(['ops', 'planner', 'maps']);
    expect(visibleNavItems(['customers.read']).map((i) => i.id)).toEqual([
      'customers',
      'vessels',
      'deadlines',
    ]);
    expect(visibleNavItems(['harbor.configure']).map((i) => i.id)).toEqual([
      'harbor-settings',
      'occupancy-settings',
    ]);
  });

  it('shows the M2 pricing pages by permission', () => {
    expect(visibleNavItems(['pricing.read']).map((i) => i.id)).toEqual([
      'price-lists',
      'items',
      'pricing-classifications',
      'pricing-simulator',
    ]);
    expect(visibleNavItems(['pricing.configure']).map((i) => i.id)).toEqual(['pricing-settings']);
  });

  it('keeps charges upcoming and lists contracts from M3', () => {
    expect(UPCOMING_MODULES).not.toContain('common:nav.modules.contracts');
    expect(UPCOMING_MODULES).toContain('common:nav.modules.charges');
  });

  it('shows the M3 desk pages by permission', () => {
    expect(visibleNavItems(['contracts.read']).map((i) => i.id)).toEqual([
      'transits',
      'quotes',
      'contracts',
      'inquiries',
      'installments',
      'renewals',
      'charters',
      'waitlist',
      'signatures',
    ]);
    expect(visibleNavItems(['presence.write']).map((i) => i.id)).toEqual(['dock']);
  });

  it('lists the future modules without the M1 ones', () => {
    expect(UPCOMING_MODULES.length).toBeGreaterThan(0);
    expect(UPCOMING_MODULES).not.toContain('common:nav.modules.map');
    expect(UPCOMING_MODULES).not.toContain('common:nav.modules.customers');
  });

  it('shows the M4 pages by permission', () => {
    expect(visibleNavItems(['rights.manage']).map((i) => i.id)).toEqual([
      'rights',
      'transfers',
      'mandates',
      'fees',
      'common-charges',
      'holder-installments',
      'concession',
      'imports',
    ]);
    expect(visibleNavItems(['templates.configure']).map((i) => i.id)).toEqual(['templates']);
    expect(visibleNavItems(['communications.send']).map((i) => i.id)).toEqual(['communications']);
    expect(visibleNavItems(['providers.configure']).map((i) => i.id)).toEqual([
      'signature-settings',
    ]);
    expect(visibleNavItems(['contracts.write']).map((i) => i.id)).toEqual([
      'imports',
      'contract-settings',
    ]);
  });
});
