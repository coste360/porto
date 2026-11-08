// TanStack Query hooks and request helpers of the harbour map (m1-plan §4.3, §5). Every map key lives under
// queryKeys.maps(marinaId), which the map.changed SSE topic invalidates.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary, type ApiClient } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type {
  BerthGroup,
  GenerateRequest,
  GenerateResult,
  HarborSettings,
  MapBackgroundFile,
  MapDocument,
  MapExport,
  MapSaveRequest,
  MapSaveResult,
  MapSummary,
  MapValidationReport,
  Polygon,
  Schemas,
} from '../schema';

const M = '/api/admin/v1/marinas/{marinaId}' as const;

export type MapCreate = Schemas['MapCreate'];
export type BerthGroupCreate = Schemas['BerthGroupCreate'];
export type BerthGroupPatch = Schemas['BerthGroupPatch'];
export type HarborSettingsPatch = Schemas['HarborSettingsPatch'];
export type ConcessionGeojsonRequest = Schemas['ConcessionGeojsonRequest'];

/** URL of a map background image (served with a sandbox CSP; drawn only through `<image>`). */
export function mapBackgroundUrl(marinaId: string, mapId: string, fileId: string): string {
  return `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/maps/${encodeURIComponent(mapId)}/background/${encodeURIComponent(fileId)}`;
}

// ---- maps ----

export function useMaps(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maps(marinaId),
    queryFn: async (): Promise<MapSummary[]> =>
      (await unwrap(api.GET(`${M}/maps`, { params: { path: { marinaId } } }))).items,
  });
}

export function fetchMap(api: ApiClient, marinaId: string, mapId: string): Promise<MapDocument> {
  return unwrap(api.GET(`${M}/maps/{mapId}`, { params: { path: { marinaId, mapId } } }));
}

export function useMap(marinaId: string, mapId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.map(marinaId, mapId),
    enabled: !!mapId,
    queryFn: () => fetchMap(api, marinaId, mapId),
  });
}

export function useCreateMap(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ body, idempotencyKey }: { body: MapCreate; idempotencyKey?: string }) =>
      unwrap(
        api.POST(`${M}/maps`, {
          params: {
            path: { marinaId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.maps(marinaId), exact: true }),
  });
}

export function useReorderMaps(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (mapIds: string[]) =>
      unwrap(
        api.POST(`${M}/maps/reorder`, {
          params: { path: { marinaId } },
          body: { map_ids: mapIds },
        }),
      ),
    onMutate: (mapIds) => {
      const key = queryKeys.maps(marinaId);
      const prev = qc.getQueryData<MapSummary[]>(key);
      if (prev) {
        const byId = new Map(prev.map((m) => [m.id, m]));
        qc.setQueryData(
          key,
          mapIds.flatMap((id, i) => {
            const m = byId.get(id);
            return m ? [{ ...m, sort_order: i }] : [];
          }),
        );
      }
      return { prev };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prev) qc.setQueryData(queryKeys.maps(marinaId), ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.maps(marinaId), exact: true }),
  });
}

export function useImportMap(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: MapExport) =>
      unwrap(
        api.POST(`${M}/maps/import`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.maps(marinaId), exact: true }),
  });
}

export function useDeleteMap(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ mapId, version }: { mapId: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/maps/{mapId}`, {
          params: { path: { marinaId, mapId }, header: { 'If-Match': ifMatch(version) } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.maps(marinaId), exact: true }),
  });
}

/** PUT the whole document with If-Match (m1-plan §11 D1). */
export function saveMap(
  api: ApiClient,
  marinaId: string,
  mapId: string,
  version: number,
  body: MapSaveRequest,
): Promise<MapSaveResult> {
  return unwrap(
    api.PUT(`${M}/maps/{mapId}`, {
      params: { path: { marinaId, mapId }, header: { 'If-Match': ifMatch(version) } },
      body,
    }),
  );
}

export function useSaveMap(marinaId: string, mapId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ version, body }: { version: number; body: MapSaveRequest }) =>
      saveMap(api, marinaId, mapId, version, body),
    onSuccess: (result) => {
      qc.setQueryData(queryKeys.map(marinaId, mapId), result.document);
      void qc.invalidateQueries({ queryKey: queryKeys.maps(marinaId), exact: true });
    },
  });
}

/** Dry-run validation of an unsaved document (the editor's validation panel). */
export function validateMap(
  api: ApiClient,
  marinaId: string,
  mapId: string,
  body: MapSaveRequest,
  signal?: AbortSignal,
): Promise<MapValidationReport> {
  return unwrap(
    api.POST(`${M}/maps/{mapId}/validate`, { params: { path: { marinaId, mapId } }, body, signal }),
  );
}

/** Generator preview: elements with keys only, never saved (m1-plan §11 D2). */
export function generateMapElements(
  api: ApiClient,
  marinaId: string,
  mapId: string,
  body: GenerateRequest,
): Promise<GenerateResult> {
  return unwrap(
    api.POST(`${M}/maps/{mapId}/generate`, { params: { path: { marinaId, mapId } }, body }),
  );
}

export function useGenerateMapElements(marinaId: string, mapId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: GenerateRequest) => generateMapElements(api, marinaId, mapId, body),
  });
}

/** Content types accepted for a map background. */
export const BACKGROUND_TYPES = ['image/png', 'image/jpeg', 'image/svg+xml'] as const;
export const MAX_BACKGROUND_BYTES = 15 * 1024 * 1024;

export function useUploadMapBackground(marinaId: string, mapId: string) {
  return useMutation({
    mutationFn: async (file: File): Promise<MapBackgroundFile> =>
      (
        await uploadBinary<MapBackgroundFile>(
          `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/maps/${encodeURIComponent(mapId)}/background`,
          file,
          { method: 'POST', contentType: file.type || 'application/octet-stream' },
        )
      ).data,
  });
}

export function useConvertConcessionGeojson(marinaId: string, mapId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: async (body: ConcessionGeojsonRequest): Promise<Polygon> =>
      (
        await unwrap(
          api.POST(`${M}/maps/{mapId}/concession/geojson`, {
            params: { path: { marinaId, mapId } },
            body,
          }),
        )
      ).outline,
  });
}

/** Map export as a JSON value (`porto-map/1` or a GeoJSON FeatureCollection). */
export async function exportMap(
  api: ApiClient,
  marinaId: string,
  mapId: string,
  format: 'json' | 'geojson',
): Promise<unknown> {
  return unwrap(
    api.GET(`${M}/maps/{mapId}/export`, {
      params: { path: { marinaId, mapId }, query: { format } },
    }),
  );
}

// ---- berth groups ----

export function useBerthGroups(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.berthGroups(marinaId),
    queryFn: async (): Promise<BerthGroup[]> =>
      (await unwrap(api.GET(`${M}/berth-groups`, { params: { path: { marinaId } } }))).items,
  });
}

export function useCreateBerthGroup(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: BerthGroupCreate) =>
      unwrap(
        api.POST(`${M}/berth-groups`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.berthGroups(marinaId) }),
  });
}

export function useUpdateBerthGroup(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ group, patch }: { group: BerthGroup; patch: BerthGroupPatch }) =>
      unwrap(
        api.PATCH(`${M}/berth-groups/{groupId}`, {
          params: {
            path: { marinaId, groupId: group.id },
            header: { 'If-Match': ifMatch(group.version) },
          },
          body: patch,
        }),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.berthGroups(marinaId) }),
  });
}

export function useDeleteBerthGroup(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (group: BerthGroup) =>
      unwrap(
        api.DELETE(`${M}/berth-groups/{groupId}`, {
          params: {
            path: { marinaId, groupId: group.id },
            header: { 'If-Match': ifMatch(group.version) },
          },
        }),
      ),
    // Members disappear from every map document too.
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.maps(marinaId) }),
  });
}

// ---- harbour settings ----

export function useHarborSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.harborSettings(marinaId),
    queryFn: (): Promise<HarborSettings> =>
      unwrap(api.GET(`${M}/harbor-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateHarborSettings(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ version, patch }: { version: number; patch: HarborSettingsPatch }) =>
      unwrap(
        api.PATCH(`${M}/harbor-settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(version) } },
          body: patch,
        }),
      ),
    onSuccess: (data) => qc.setQueryData(queryKeys.harborSettings(marinaId), data),
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.harborSettings(marinaId) }),
  });
}
