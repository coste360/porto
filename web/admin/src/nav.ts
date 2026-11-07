import type { Permission } from '@porto/shared/api';

export type MarinaRoutePath =
  | '/m/$marinaId'
  | '/m/$marinaId/settings'
  | '/m/$marinaId/team'
  | '/m/$marinaId/api-keys'
  | '/m/$marinaId/devices'
  | '/m/$marinaId/webhooks'
  | '/m/$marinaId/audit'
  | '/m/$marinaId/maps'
  | '/m/$marinaId/customers'
  | '/m/$marinaId/vessels'
  | '/m/$marinaId/deadlines'
  | '/m/$marinaId/harbor-settings'
  | '/m/$marinaId/price-lists'
  | '/m/$marinaId/items'
  | '/m/$marinaId/pricing/classifications'
  | '/m/$marinaId/pricing/simulator'
  | '/m/$marinaId/pricing/settings'
  | '/m/$marinaId/ops'
  | '/m/$marinaId/planner'
  | '/m/$marinaId/transits'
  | '/m/$marinaId/quotes'
  | '/m/$marinaId/contracts'
  | '/m/$marinaId/inquiries'
  | '/m/$marinaId/dock'
  | '/m/$marinaId/occupancy-settings'
  | '/m/$marinaId/installments'
  | '/m/$marinaId/renewals'
  | '/m/$marinaId/charters'
  | '/m/$marinaId/waitlist'
  | '/m/$marinaId/rights'
  | '/m/$marinaId/transfers'
  | '/m/$marinaId/mandates'
  | '/m/$marinaId/fees'
  | '/m/$marinaId/common-charges'
  | '/m/$marinaId/holder-installments'
  | '/m/$marinaId/concession'
  | '/m/$marinaId/signatures'
  | '/m/$marinaId/communications'
  | '/m/$marinaId/imports'
  | '/m/$marinaId/templates'
  | '/m/$marinaId/settings/contracts'
  | '/m/$marinaId/settings/signature';

export type NavItem = {
  id: string;
  to: MarinaRoutePath;
  labelKey: string;
  /** Any of these permissions shows the item (the server enforces them anyway). */
  anyOf: Permission[];
};

export const NAV_ITEMS: NavItem[] = [
  { id: 'dashboard', to: '/m/$marinaId', labelKey: 'common:nav.dashboard', anyOf: ['marina.read'] },
  { id: 'ops', to: '/m/$marinaId/ops', labelKey: 'common:nav.ops', anyOf: ['map.view'] },
  {
    id: 'planner',
    to: '/m/$marinaId/planner',
    labelKey: 'common:nav.planner',
    anyOf: ['map.view'],
  },
  {
    id: 'transits',
    to: '/m/$marinaId/transits',
    labelKey: 'common:nav.transits',
    anyOf: ['contracts.read'],
  },
  {
    id: 'quotes',
    to: '/m/$marinaId/quotes',
    labelKey: 'common:nav.quotes',
    anyOf: ['contracts.read'],
  },
  {
    id: 'contracts',
    to: '/m/$marinaId/contracts',
    labelKey: 'common:nav.contracts',
    anyOf: ['contracts.read'],
  },
  {
    id: 'inquiries',
    to: '/m/$marinaId/inquiries',
    labelKey: 'common:nav.inquiries',
    anyOf: ['contracts.read'],
  },
  // M4 (m4-plan §8.1)
  {
    id: 'installments',
    to: '/m/$marinaId/installments',
    labelKey: 'common:nav.installments',
    anyOf: ['contracts.read'],
  },
  {
    id: 'renewals',
    to: '/m/$marinaId/renewals',
    labelKey: 'common:nav.renewals',
    anyOf: ['contracts.read'],
  },
  {
    id: 'charters',
    to: '/m/$marinaId/charters',
    labelKey: 'common:nav.charters',
    anyOf: ['contracts.read'],
  },
  {
    id: 'waitlist',
    to: '/m/$marinaId/waitlist',
    labelKey: 'common:nav.waitlist',
    anyOf: ['contracts.read'],
  },
  {
    id: 'rights',
    to: '/m/$marinaId/rights',
    labelKey: 'common:nav.rights',
    anyOf: ['rights.manage'],
  },
  {
    id: 'transfers',
    to: '/m/$marinaId/transfers',
    labelKey: 'common:nav.transfers',
    anyOf: ['rights.manage'],
  },
  {
    id: 'mandates',
    to: '/m/$marinaId/mandates',
    labelKey: 'common:nav.mandates',
    anyOf: ['rights.manage'],
  },
  {
    id: 'fees',
    to: '/m/$marinaId/fees',
    labelKey: 'common:nav.fees',
    anyOf: ['rights.manage'],
  },
  {
    id: 'common-charges',
    to: '/m/$marinaId/common-charges',
    labelKey: 'common:nav.commonCharges',
    anyOf: ['rights.manage'],
  },
  {
    id: 'holder-installments',
    to: '/m/$marinaId/holder-installments',
    labelKey: 'common:nav.holderInstallments',
    anyOf: ['rights.manage'],
  },
  {
    id: 'concession',
    to: '/m/$marinaId/concession',
    labelKey: 'common:nav.concession',
    anyOf: ['rights.manage'],
  },
  {
    id: 'signatures',
    to: '/m/$marinaId/signatures',
    labelKey: 'common:nav.signatures',
    anyOf: ['contracts.read'],
  },
  {
    id: 'communications',
    to: '/m/$marinaId/communications',
    labelKey: 'common:nav.communications',
    anyOf: ['communications.send'],
  },
  {
    id: 'imports',
    to: '/m/$marinaId/imports',
    labelKey: 'common:nav.imports',
    anyOf: ['customers.write', 'contracts.write', 'rights.manage'],
  },
  {
    id: 'templates',
    to: '/m/$marinaId/templates',
    labelKey: 'common:nav.templates',
    anyOf: ['templates.configure'],
  },
  {
    id: 'contract-settings',
    to: '/m/$marinaId/settings/contracts',
    labelKey: 'common:nav.contractSettings',
    anyOf: ['contracts.write'],
  },
  {
    id: 'signature-settings',
    to: '/m/$marinaId/settings/signature',
    labelKey: 'common:nav.signatureSettings',
    anyOf: ['providers.configure'],
  },
  { id: 'dock', to: '/m/$marinaId/dock', labelKey: 'common:nav.dock', anyOf: ['presence.write'] },
  { id: 'maps', to: '/m/$marinaId/maps', labelKey: 'common:nav.maps', anyOf: ['map.view'] },
  {
    id: 'customers',
    to: '/m/$marinaId/customers',
    labelKey: 'common:nav.customers',
    anyOf: ['customers.read'],
  },
  {
    id: 'vessels',
    to: '/m/$marinaId/vessels',
    labelKey: 'common:nav.vessels',
    anyOf: ['customers.read'],
  },
  {
    id: 'deadlines',
    to: '/m/$marinaId/deadlines',
    labelKey: 'common:nav.deadlines',
    anyOf: ['customers.read'],
  },
  {
    id: 'price-lists',
    to: '/m/$marinaId/price-lists',
    labelKey: 'common:nav.priceLists',
    anyOf: ['pricing.read'],
  },
  { id: 'items', to: '/m/$marinaId/items', labelKey: 'common:nav.items', anyOf: ['pricing.read'] },
  {
    id: 'pricing-classifications',
    to: '/m/$marinaId/pricing/classifications',
    labelKey: 'common:nav.pricingClassifications',
    anyOf: ['pricing.read'],
  },
  {
    id: 'pricing-simulator',
    to: '/m/$marinaId/pricing/simulator',
    labelKey: 'common:nav.pricingSimulator',
    anyOf: ['pricing.read'],
  },
  {
    id: 'harbor-settings',
    to: '/m/$marinaId/harbor-settings',
    labelKey: 'common:nav.harborSettings',
    anyOf: ['harbor.configure'],
  },
  {
    id: 'occupancy-settings',
    to: '/m/$marinaId/occupancy-settings',
    labelKey: 'common:nav.occupancySettings',
    anyOf: ['harbor.configure'],
  },
  {
    id: 'pricing-settings',
    to: '/m/$marinaId/pricing/settings',
    labelKey: 'common:nav.pricingSettings',
    anyOf: ['pricing.configure'],
  },
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
