/**
 * TanStack Query keys. Everything about one marina lives under ['marina', id] so that
 * SSE topics can invalidate a whole area at once (see sse.ts).
 */
export const queryKeys = {
  me: ['me'] as const,
  meta: ['meta'] as const,
  eventTypes: ['event-types'] as const,
  organizations: ['organizations'] as const,
  orgMarinas: (orgId: string) => ['organizations', orgId, 'marinas'] as const,
  orgOwners: (orgId: string) => ['organizations', orgId, 'owners'] as const,
  marina: (marinaId: string) => ['marina', marinaId, 'detail'] as const,
  team: (marinaId: string) => ['marina', marinaId, 'team'] as const,
  members: (marinaId: string) => ['marina', marinaId, 'team', 'members'] as const,
  apiKeys: (marinaId: string) => ['marina', marinaId, 'team', 'api-keys'] as const,
  devices: (marinaId: string) => ['marina', marinaId, 'team', 'devices'] as const,
  webhooks: (marinaId: string) => ['marina', marinaId, 'webhooks'] as const,
  webhookDeliveries: (marinaId: string, endpointId: string) =>
    ['marina', marinaId, 'webhooks', endpointId, 'deliveries'] as const,
  audit: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'audit', filters] as const,
  publicMarina: (slug: string) => ['public', slug] as const,

  // M1 harbour: everything under ['marina', id, 'maps'] (invalidated by map.changed).
  maps: (marinaId: string) => ['marina', marinaId, 'maps'] as const,
  map: (marinaId: string, mapId: string) => ['marina', marinaId, 'maps', 'detail', mapId] as const,
  berths: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'maps', 'berths', filters] as const,
  berthGroups: (marinaId: string) => ['marina', marinaId, 'maps', 'berth-groups'] as const,
  harborSettings: (marinaId: string) => ['marina', marinaId, 'maps', 'harbor-settings'] as const,

  // M1 registry: everything under ['marina', id, 'registry'] (invalidated by registry.changed).
  registry: (marinaId: string) => ['marina', marinaId, 'registry'] as const,
  customers: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'customers', filters] as const,
  customer: (marinaId: string, customerId: string) =>
    ['marina', marinaId, 'registry', 'customer', customerId] as const,
  vessels: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'vessels', filters] as const,
  vessel: (marinaId: string, vesselId: string) =>
    ['marina', marinaId, 'registry', 'vessel', vesselId] as const,
  documents: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'documents', filters] as const,
  deadlines: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'deadlines', filters] as const,
  deadlinePolicies: (marinaId: string) =>
    ['marina', marinaId, 'registry', 'deadline-policies'] as const,

  // M2 pricing: everything under ['marina', id, 'pricing'] (invalidated by pricing.changed).
  pricing: (marinaId: string) => ['marina', marinaId, 'pricing'] as const,
  pricingSettings: (marinaId: string) => ['marina', marinaId, 'pricing', 'settings'] as const,
  revenueClasses: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'revenue-classes', filters] as const,
  items: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'items', filters] as const,
  item: (marinaId: string, itemId: string) =>
    ['marina', marinaId, 'pricing', 'item', itemId] as const,
  customerCategoryCounts: (marinaId: string) =>
    ['marina', marinaId, 'pricing', 'customer-categories'] as const,
  berthCategories: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'berth-categories', filters] as const,
  priceLists: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'price-lists', filters] as const,
  priceList: (marinaId: string, priceListId: string) =>
    ['marina', marinaId, 'pricing', 'price-list', priceListId] as const,
  priceMatrix: (
    marinaId: string,
    priceListId: string,
    params: Record<string, string | undefined> = {},
  ) => ['marina', marinaId, 'pricing', 'price-list', priceListId, 'matrix', params] as const,
  priceCoverage: (
    marinaId: string,
    priceListId: string,
    params: Record<string, string | undefined> = {},
  ) => ['marina', marinaId, 'pricing', 'price-list', priceListId, 'coverage', params] as const,
};

export type QueryKey = readonly unknown[];

/** SSE topics (plan §4.5) and the query keys each one invalidates. */
export function keysForTopic(topic: string, marinaId: string): QueryKey[] {
  switch (topic) {
    case 'marina.changed':
      return [queryKeys.marina(marinaId), queryKeys.organizations, queryKeys.me];
    case 'team.changed':
      return [queryKeys.team(marinaId), queryKeys.me];
    case 'map.changed':
      // maps, map(id), berths, berth-groups and harbor-settings all live under the maps key.
      return [queryKeys.maps(marinaId)];
    case 'registry.changed':
      return [queryKeys.registry(marinaId)];
    case 'pricing.changed':
      // settings, revenue classes, items, categories, lists, matrices and coverage live under pricing.
      return [queryKeys.pricing(marinaId)];
    default:
      return [];
  }
}
