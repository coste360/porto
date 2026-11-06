// Berth CSV import hooks (m1-plan §5.5, D23). The wizard reads the map with its own getMap query under
// queryKeys.map (same key and data shape as the editor's), and runs importBerthsCsv as dry run or apply.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ifMatch, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

/** The map document the import is placed on (piers, existing codes, version for If-Match). */
export function useImportTargetMap(marinaId: string, mapId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.map(marinaId, mapId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/maps/{mapId}', {
          params: { path: { marinaId, mapId } },
        }),
      ),
  });
}

export type BerthImportVars = {
  mapId: string;
  request: S['BerthImportRequest'];
  /** Map version for If-Match; required when applying (dry_run false). */
  version?: number;
};

/** importBerthsCsv. A dry run changes nothing; an apply invalidates every map query of the marina. */
export function useImportBerths(marinaId: string) {
  const api = useApi();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: BerthImportVars) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/maps/{mapId}/berth-import', {
          params: {
            path: { marinaId, mapId: vars.mapId },
            ...(vars.version !== undefined
              ? { header: { 'If-Match': ifMatch(vars.version) } }
              : {}),
          },
          body: vars.request,
        }),
      ),
    onSuccess: (_report, vars) => {
      if (!vars.request.dry_run) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.maps(marinaId) });
      }
    },
  });
}
