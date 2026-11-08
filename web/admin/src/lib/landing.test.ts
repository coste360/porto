import { describe, expect, it } from 'vitest';

import type { Permission, Role } from '@porto/shared/api';

import { canUseDirection, canUseDock, landingFor } from './landing';

const M = 'm-1';

// The role matrix of spec 26.1 (internal/domain/auth/roles.go), only the permissions the landing reads plus a
// few others, so a change of the server matrix that matters here shows up as a failing row.
const ROLE_PERMISSIONS: Record<Role, Permission[]> = {
  owner: [
    'marina.configure',
    'presence.write',
    'transits.desk',
    'map.view',
    'reports.all',
    'contracts.read',
  ],
  director: ['marina.configure', 'presence.write', 'transits.desk', 'map.view', 'reports.all'],
  office: ['presence.write', 'transits.desk', 'map.view', 'contracts.read'],
  accounting: ['reports.all', 'map.view', 'payments.collect', 'contracts.read'],
  dock: ['presence.write', 'transits.desk', 'map.view', 'payments.collect_cash', 'contracts.read'],
  yard_mgr: ['yard.manage', 'yard.timesheets', 'storage.manage', 'map.view'],
  yard: ['yard.timesheets', 'maintenance.report', 'map.view'],
  rental: ['rental.manage', 'payments.collect', 'map.view'],
  viewer: ['reports.all', 'map.view', 'marina.read'],
};

const PHONE: Record<Role, string> = {
  owner: `/m/${M}/dock`,
  director: `/m/${M}/dock`,
  office: `/m/${M}/dock`,
  accounting: `/m/${M}/direction`,
  dock: `/m/${M}/dock`,
  yard_mgr: `/m/${M}`,
  yard: `/m/${M}`,
  rental: `/m/${M}`,
  viewer: `/m/${M}/direction`,
};

describe('landingFor', () => {
  const rows = (Object.keys(ROLE_PERMISSIONS) as Role[]).flatMap((role) => [
    [role, 'mobile', PHONE[role]] as const,
    [role, 'desktop', `/m/${M}`] as const,
  ]);
  it.each(rows)('%s on %s', (role, kind, expected) => {
    expect(landingFor(ROLE_PERMISSIONS[role], kind, M)).toBe(expected);
  });

  const cases: [string, Permission[], string][] = [
    ['presence only', ['presence.write'], `/m/${M}/dock`],
    ['map and desk', ['map.view', 'transits.desk'], `/m/${M}/dock`],
    ['map only', ['map.view'], `/m/${M}`],
    ['configure only', ['marina.configure'], `/m/${M}/direction`],
    ['no permissions', [], `/m/${M}`],
  ];
  it.each(cases)('phone: %s', (_name, permissions, expected) => {
    expect(landingFor(permissions, 'mobile', M)).toBe(expected);
  });

  it('encodes the marina id', () => {
    expect(landingFor([], 'desktop', 'a/b')).toBe('/m/a%2Fb');
    expect(landingFor(['presence.write'], 'mobile', 'a/b')).toBe('/m/a%2Fb/dock');
  });

  it('tells which phone screens a user may open', () => {
    expect(canUseDirection(ROLE_PERMISSIONS.owner)).toBe(true);
    expect(canUseDirection(ROLE_PERMISSIONS.dock)).toBe(false);
    expect(canUseDock(ROLE_PERMISSIONS.dock)).toBe(true);
    expect(canUseDock(ROLE_PERMISSIONS.yard)).toBe(true);
    expect(canUseDock(['reports.all'])).toBe(false);
  });
});
