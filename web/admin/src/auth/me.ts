import { queryOptions, type QueryClient } from '@tanstack/react-query';

import {
  ProblemError,
  queryKeys,
  unwrap,
  type ApiClient,
  type Me,
  type MeMarina,
  type Permission,
} from '@porto/shared/api';

/** The current principal, or null when not signed in (401 is an answer, not an error). */
export function meQueryOptions(api: ApiClient) {
  return queryOptions({
    queryKey: queryKeys.me,
    queryFn: async (): Promise<Me | null> => {
      try {
        return await unwrap(api.GET('/api/admin/v1/auth/me'));
      } catch (error) {
        if (error instanceof ProblemError && error.status === 401) return null;
        throw error;
      }
    },
    staleTime: 30_000,
  });
}

export function ensureMe(queryClient: QueryClient, api: ApiClient): Promise<Me | null> {
  return queryClient.ensureQueryData(meQueryOptions(api));
}

/** Fetches the principal again, ignoring the cached copy (a marina created since, a cache restored offline). */
export function refreshMe(queryClient: QueryClient, api: ApiClient): Promise<Me | null> {
  return queryClient.fetchQuery({ ...meQueryOptions(api), staleTime: 0 });
}

export function findMarina(me: Me | null | undefined, marinaId: string): MeMarina | undefined {
  return me?.marinas.find((m) => m.id === marinaId);
}

export function hasPermission(marina: MeMarina | undefined, permission: Permission): boolean {
  return marina?.permissions.includes(permission) ?? false;
}

/** Organizations where the user holds the org-level owner grant. */
export function ownedOrganizations(me: Me | null | undefined) {
  return me?.organizations.filter((o) => o.role === 'owner') ?? [];
}
