import type { Permission } from '@porto/shared/api';

export type MarinaRoutePath =
  | '/m/$marinaId'
  | '/m/$marinaId/settings'
  | '/m/$marinaId/team'
  | '/m/$marinaId/api-keys'
  | '/m/$marinaId/devices'
  | '/m/$marinaId/webhooks'
  | '/m/$marinaId/audit';

export type NavItem = {
  id: string;
  to: MarinaRoutePath;
  labelKey: string;
  /** Any of these permissions shows the item (the server enforces them anyway). */
  anyOf: Permission[];
};

export const NAV_ITEMS: NavItem[] = [
  { id: 'dashboard', to: '/m/$marinaId', labelKey: 'common:nav.dashboard', anyOf: ['marina.read'] },
  {
    id: 'settings',
    to: '/m/$marinaId/settings',
    labelKey: 'common:nav.settings',
    anyOf: ['marina.configure'],
  },
  { id: 'team', to: '/m/$marinaId/team', labelKey: 'common:nav.team', anyOf: ['users.manage'] },
  {
    id: 'api-keys',
    to: '/m/$marinaId/api-keys',
    labelKey: 'common:nav.apiKeys',
    anyOf: ['users.manage'],
  },
  {
    id: 'devices',
    to: '/m/$marinaId/devices',
    labelKey: 'common:nav.devices',
    anyOf: ['devices.manage', 'devices.pair_self'],
  },
  {
    id: 'webhooks',
    to: '/m/$marinaId/webhooks',
    labelKey: 'common:nav.webhooks',
    anyOf: ['users.manage'],
  },
  { id: 'audit', to: '/m/$marinaId/audit', labelKey: 'common:nav.audit', anyOf: ['audit.read'] },
];

/** Modules of the next milestones, listed disabled in the sidebar. */
export const UPCOMING_MODULES = [
  'common:nav.modules.map',
  'common:nav.modules.customers',
  'common:nav.modules.contracts',
  'common:nav.modules.charges',
  'common:nav.modules.storage',
  'common:nav.modules.yard',
  'common:nav.modules.rental',
  'common:nav.modules.booking',
  'common:nav.modules.reports',
] as const;

export function visibleNavItems(permissions: readonly Permission[]): NavItem[] {
  return NAV_ITEMS.filter((item) => item.anyOf.some((p) => permissions.includes(p)));
}

export function navItem(id: string): NavItem {
  const item = NAV_ITEMS.find((i) => i.id === id);
  if (!item) throw new Error(`unknown nav item ${id}`);
  return item;
}
