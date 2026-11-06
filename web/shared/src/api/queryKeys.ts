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
};

export type QueryKey = readonly unknown[];

/** SSE topics (plan §4.5) and the query keys each one invalidates. */
export function keysForTopic(topic: string, marinaId: string): QueryKey[] {
  switch (topic) {
    case 'marina.changed':
      return [queryKeys.marina(marinaId), queryKeys.organizations, queryKeys.me];
    case 'team.changed':
      return [queryKeys.team(marinaId), queryKeys.me];
    default:
      return [];
  }
}
