// Query and mutation hooks of the operational map (M3, m3-plan §8): day view, map occupancy, allocation,
// placement check, moves, swaps, layout saves, compaction, undo, blocks. Query keys come from queryKeys.ts (ops
// root); SSE (occupancy.changed, presence.changed) invalidates them. Mutations invalidate the ops and desk roots
// on success so the acting tab does not wait for the stream.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, type ApiClient } from '../client';
import { useApi } from '../context';
import { assertOnline } from '../../offline/index';
import { queryKeys } from '../queryKeys';
import type { Schemas } from '../schema';

const M = '/api/admin/v1/marinas/{marinaId}' as const;

export type DayViewData = Schemas['DayView'];
export type DayElementData = Schemas['DayElement'];
export type AreaPlacementData = Schemas['AreaPlacement'];
export type MapOccupancyData = Schemas['MapOccupancy'];
export type MapAllocationData = Schemas['MapAllocation'];
export type AllocationData = Schemas['Allocation'];
export type AllocationDetailData = Schemas['AllocationDetail'];
export type PlacementInputData = Schemas['PlacementInput'];
export type PlacementCheckRequestData = Schemas['PlacementCheckRequest'];
export type PlacementCheckData = Schemas['PlacementCheck'];
export type MoveRequestData = Schemas['MoveRequest'];
export type SwapRequestData = Schemas['SwapRequest'];
export type MoveResultData = Schemas['MoveResult'];
export type AreaLayoutSaveData = Schemas['AreaLayoutSave'];
export type CompactionRequestData = Schemas['CompactionRequest'];
export type CompactionProposalData = Schemas['CompactionProposal'];
export type BlockCreateData = Schemas['BlockCreate'];
export type OccupancySettingsData = Schemas['OccupancySettings'];

// ---- reads ----

export function fetchDayView(
  api: ApiClient,
  marinaId: string,
  mapId: string,
  date?: string,
): Promise<DayViewData> {
  return unwrap(
    api.GET(`${M}/maps/{mapId}/day`, {
      params: { path: { marinaId, mapId }, query: date ? { date } : {} },
    }),
  );
}

/** Day view of a map on a night (default today on the server). Keeps the previous day while a new one loads. */
export function useDayView(marinaId: string, mapId: string | undefined, date: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.dayView(marinaId, mapId ?? '', date ?? 'today'),
    queryFn: () => fetchDayView(api, marinaId, mapId!, date),
    enabled: !!mapId,
    placeholderData: keepPreviousData,
  });
}

export function fetchMapOccupancy(
  api: ApiClient,
  marinaId: string,
  mapId: string,
  from: string,
  to: string,
  resourceIds?: readonly string[],
): Promise<MapOccupancyData> {
  return unwrap(
    api.GET(`${M}/maps/{mapId}/occupancy`, {
      params: {
        path: { marinaId, mapId },
        query: {
          from,
          to,
          ...(resourceIds && resourceIds.length > 0 ? { resource_ids: resourceIds.join(',') } : {}),
        },
      },
    }),
  );
}

/** Allocations of a map intersecting [from, to] (drag preview busy index, planner). */
export function useMapOccupancy(
  marinaId: string,
  mapId: string | undefined,
  from: string | undefined,
  to: string | undefined,
  enabled = true,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.mapOccupancy(marinaId, mapId ?? '', from ?? '', to ?? ''),
    queryFn: () => fetchMapOccupancy(api, marinaId, mapId!, from!, to!),
    enabled: enabled && !!mapId && !!from && !!to,
    staleTime: 15_000,
  });
}

export function fetchAllocation(
  api: ApiClient,
  marinaId: string,
  allocationId: string,
): Promise<AllocationDetailData> {
  return unwrap(
    api.GET(`${M}/allocations/{allocationId}`, { params: { path: { marinaId, allocationId } } }),
  );
}

export function useAllocation(marinaId: string, allocationId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.allocation(marinaId, allocationId ?? ''),
    queryFn: () => fetchAllocation(api, marinaId, allocationId!),
    enabled: !!allocationId,
  });
}

/** Occupancy settings (absent days threshold for the Presenze lens, arrival grace). */
export function useOccupancySettingsForOps(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.occupancySettings(marinaId),
    queryFn: (): Promise<OccupancySettingsData> =>
      unwrap(api.GET(`${M}/occupancy-settings`, { params: { path: { marinaId } } })),
    enabled,
    staleTime: 60_000,
  });
}

/** Advisory placement check (fit, conflicts, price effect); nothing is written. */
export function checkPlacement(
  api: ApiClient,
  marinaId: string,
  body: PlacementCheckRequestData,
): Promise<PlacementCheckData> {
  return unwrap(api.POST(`${M}/occupancies/check`, { params: { path: { marinaId } }, body }));
}

export function useCheckPlacement(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: PlacementCheckRequestData) => checkPlacement(api, marinaId, body),
  });
}

export function proposeCompaction(
  api: ApiClient,
  marinaId: string,
  areaId: string,
  body: CompactionRequestData,
): Promise<CompactionProposalData> {
  return unwrap(
    api.POST(`${M}/dynamic-areas/{areaId}/compact`, {
      params: { path: { marinaId, areaId } },
      body,
    }),
  );
}

export function useProposeCompaction(marinaId: string, areaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: CompactionRequestData) => proposeCompaction(api, marinaId, areaId, body),
  });
}

// ---- writes ----

function useInvalidateOps(marinaId: string) {
  const qc = useQueryClient();
  return () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: queryKeys.ops(marinaId) }),
      qc.invalidateQueries({ queryKey: queryKeys.desk(marinaId) }),
    ]);
}

type WithKey<T> = { body: T; idempotencyKey?: string };

export function useMoveAllocation(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateOps(marinaId);
  return useMutation({
    onMutate: () => assertOnline('moveAllocation', 'allocation'),
    mutationFn: ({ body, idempotencyKey }: WithKey<MoveRequestData>) =>
      unwrap(
        api.POST(`${M}/occupancies/move`, {
          params: {
            path: { marinaId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useSwapAllocations(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateOps(marinaId);
  return useMutation({
    onMutate: () => assertOnline('swapAllocations', 'allocation'),
    mutationFn: ({ body, idempotencyKey }: WithKey<SwapRequestData>) =>
      unwrap(
        api.POST(`${M}/occupancies/swap`, {
          params: {
            path: { marinaId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useSaveAreaLayout(marinaId: string, areaId: string) {
  const api = useApi();
  const invalidate = useInvalidateOps(marinaId);
  return useMutation({
    onMutate: () => assertOnline('saveAreaLayout', 'allocation'),
    mutationFn: ({ body, idempotencyKey }: WithKey<AreaLayoutSaveData>) =>
      unwrap(
        api.POST(`${M}/dynamic-areas/{areaId}/layout`, {
          params: {
            path: { marinaId, areaId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useUndoMove(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateOps(marinaId);
  return useMutation({
    onMutate: () => assertOnline('undoMove', 'allocation'),
    mutationFn: ({ undoId, idempotencyKey }: { undoId: string; idempotencyKey?: string }) =>
      unwrap(
        api.POST(`${M}/move-undo/{undoId}`, {
          params: {
            path: { marinaId, undoId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useCreateBlock(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateOps(marinaId);
  return useMutation({
    onMutate: () => assertOnline('createBlock', 'allocation'),
    mutationFn: ({ body, idempotencyKey }: WithKey<BlockCreateData>) =>
      unwrap(
        api.POST(`${M}/blocks`, {
          params: {
            path: { marinaId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useDeleteBlock(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateOps(marinaId);
  return useMutation({
    onMutate: () => assertOnline('deleteBlock', 'allocation'),
    mutationFn: ({ allocationId, version }: { allocationId: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/blocks/{allocationId}`, {
          params: { path: { marinaId, allocationId }, header: { 'If-Match': ifMatch(version) } },
        }),
      ),
    onSuccess: invalidate,
  });
}
