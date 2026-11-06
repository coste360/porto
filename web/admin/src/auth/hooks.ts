import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { queryKeys, unwrap, useApi, type Permission } from '@porto/shared/api';

import { findMarina, hasPermission, meQueryOptions } from './me';

const marinaRouteApi = getRouteApi('/auth/m/$marinaId');

export function useMe() {
  const api = useApi();
  return useQuery(meQueryOptions(api));
}

/** Marina of the current /m/$marinaId route, from the live `me` query. */
export function useCurrentMarina() {
  const { marinaId } = marinaRouteApi.useParams();
  const { data: me } = useMe();
  const marina = findMarina(me, marinaId);
  return {
    marinaId,
    marina,
    me: me ?? null,
    can: (permission: Permission) => hasPermission(marina, permission),
  };
}

export function useLogout() {
  const api = useApi();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: () => unwrap(api.POST('/api/admin/v1/auth/logout')),
    onSettled: async () => {
      queryClient.setQueryData(queryKeys.me, null);
      queryClient.removeQueries({ predicate: (q) => q.queryKey[0] !== queryKeys.me[0] });
      await navigate({ to: '/login' });
    },
  });
}
