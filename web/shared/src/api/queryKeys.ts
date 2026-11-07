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

  // M3 operational map: everything under ['marina', id, 'ops'] (invalidated by occupancy.changed and
  // presence.changed, narrowed by map id). Omitted trailing arguments give prefix keys for invalidation.
  ops: (marinaId: string) => ['marina', marinaId, 'ops'] as const,
  dayView: (marinaId: string, mapId?: string, date?: string) =>
    trim(['marina', marinaId, 'ops', 'day', mapId, date]),
  mapOccupancy: (marinaId: string, mapId?: string, from?: string, to?: string) =>
    trim(['marina', marinaId, 'ops', 'occupancy', mapId, from, to]),
  allocation: (marinaId: string, allocationId?: string) =>
    trim(['marina', marinaId, 'ops', 'allocation', allocationId]),
  occupancySettings: (marinaId: string) => ['marina', marinaId, 'ops', 'settings'] as const,
  closures: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'ops', 'closures', filters]),

  // M3 desk: quotes, contracts, transits, inquiries, releases, movements and presences under
  // ['marina', id, 'desk'] (invalidated by contract.changed, occupancy.changed and presence.changed).
  desk: (marinaId: string) => ['marina', marinaId, 'desk'] as const,
  quotes: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'quotes', filters]),
  quote: (marinaId: string, quoteId?: string) =>
    trim(['marina', marinaId, 'desk', 'quote', quoteId]),
  contracts: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'contracts', filters]),
  contract: (marinaId: string, contractId?: string) =>
    trim(['marina', marinaId, 'desk', 'contract', contractId]),
  transits: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'transits', filters]),
  inquiries: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'inquiries', filters]),
  releases: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'releases', filters]),
  movements: (marinaId: string, date?: string, mapId?: string) =>
    trim(['marina', marinaId, 'desk', 'movements', date, mapId]),
  presences: (marinaId: string, date?: string) =>
    trim(['marina', marinaId, 'desk', 'presences', date]),
};

/** Drops the trailing undefined segments, so a key with fewer arguments is a prefix of the full one. */
function trim(key: readonly unknown[]): readonly unknown[] {
  let end = key.length;
  while (end > 0 && key[end - 1] === undefined) end--;
  return key.slice(0, end);
}

export type QueryKey = readonly unknown[];

/** The data of an SSE message (internal/http/sse Message): ids, enums and dates only. */
export type StreamMessage = {
  topic: string;
  type: string;
  marina_id: string;
  entity_ids: string[];
  map_id?: string;
  resource_ids?: string[];
  nights?: { from: string; to: string };
};

/** Topics whose invalidation is narrowed by the message content and debounced (M3). */
export const NARROWED_TOPICS = [
  'occupancy.changed',
  'presence.changed',
  'contract.changed',
] as const;

/**
 * Query keys invalidated by one message (m3-plan §7.5). The M3 topics narrow the day view and the map occupancy
 * to the message's map (all maps when absent); other topics fall back to keysForTopic.
 */
export function keysForMessage(msg: StreamMessage, marinaId: string): QueryKey[] {
  const mapId = msg.map_id || undefined;
  switch (msg.topic) {
    case 'occupancy.changed':
      return [
        queryKeys.dayView(marinaId, mapId),
        queryKeys.mapOccupancy(marinaId, mapId),
        queryKeys.allocation(marinaId),
        queryKeys.movements(marinaId),
        queryKeys.releases(marinaId),
      ];
    case 'presence.changed':
      return [
        queryKeys.dayView(marinaId, mapId),
        queryKeys.movements(marinaId),
        queryKeys.presences(marinaId),
      ];
    case 'contract.changed':
      return [
        queryKeys.quotes(marinaId),
        queryKeys.quote(marinaId),
        queryKeys.contracts(marinaId),
        queryKeys.contract(marinaId),
        queryKeys.transits(marinaId),
        queryKeys.inquiries(marinaId),
        queryKeys.movements(marinaId),
        queryKeys.dayView(marinaId),
      ];
    default:
      return keysForTopic(msg.topic, marinaId);
  }
}

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
