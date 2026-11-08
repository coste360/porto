// Hooks of the storage planner, scenarios, movements and storage contracts (m9-plan §12.1, owner PLANNER). Keys
// live under queryKeys.storage(marinaId), invalidated by storage.changed and occupancy.changed. Every mutation is
// online only (29.2: allocation changes, scenarios and movement completion never go through the offline queue).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { assertOnline } from '../../offline/index';
import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type StorageArea = S['StorageArea'];
export type StoragePlan = S['StoragePlan'];
export type StoragePlanItem = S['StoragePlanItem'];
export type StorageGate = S['StorageGate'];
export type StorageObstacle = S['StorageObstacle'];
export type StorageAccessEntry = S['StorageAccessEntry'];
export type StorageAccessibility = S['StorageAccessibility'];
export type StorageLayoutItemInput = S['StorageLayoutItemInput'];
export type StorageArrangeRequest = S['StorageArrangeRequest'];
export type StorageArrangeResult = S['StorageArrangeResult'];
export type StorageEvaluateRequest = S['StorageEvaluateRequest'];
export type StorageEvaluation = S['StorageEvaluation'];
export type StorageIndicators = S['StorageIndicators'];
export type StorageItemIssue = S['StorageItemIssue'];
export type StorageScenario = S['StorageScenario'];
export type StorageScenarioStatus = S['StorageScenarioStatus'];
export type StorageScenarioDetail = S['StorageScenarioDetail'];
export type StorageScenarioItem = S['StorageScenarioItem'];
export type StorageScenarioMove = S['StorageScenarioMove'];
export type StorageScenarioComparison = S['StorageScenarioComparison'];
export type StorageScenarioConflict = S['StorageScenarioConflict'];
export type StorageApplyResult = S['StorageApplyResult'];
export type StorageTrayCandidate = S['StorageTrayCandidate'];
export type StorageMovement = S['StorageMovement'];
export type StorageMovementList = S['StorageMovementList'];
export type StorageMoveEnd = S['StorageMoveEnd'];
export type StorageContract = S['StorageContract'];
export type StorageContractCreate = S['StorageContractCreate'];
export type StorageContractPatch = S['StorageContractPatch'];
export type StorageContractDetails = S['StorageContractDetails'];
export type StorageSupport = S['StorageSupport'];
export type StorageSupportOwner = S['StorageSupportOwner'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

/** Invalidates every storage query of the marina (plans, scenarios, movements, contract details). */
function useInvalidateStorage(marinaId: string) {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: queryKeys.storage(marinaId) });
}

export function useStorageAreas(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageAreas(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/storage/areas`, { params: { path: { marinaId } } })),
  });
}

/** Plan and timeline of an area for a night window (server default today .. today + 30). */
export function useStoragePlan(marinaId: string, areaId: string, from?: string, to?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storagePlan(marinaId, areaId, from, to),
    enabled: !!areaId,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/areas/{areaId}/plan`, {
          params: {
            path: { marinaId, areaId },
            query: { ...(from ? { from } : {}), ...(to ? { to } : {}) },
          },
        }),
      ),
  });
}

/** Accessibility of the boats present on a date (extraction order, blocked boats and their blockers). */
export function useStorageAccessibility(marinaId: string, areaId: string, date?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageAccessibility(marinaId, areaId, date),
    enabled: !!areaId,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/areas/{areaId}/accessibility`, {
          params: { path: { marinaId, areaId }, query: date ? { date } : {} },
        }),
      ),
  });
}

/** Preset: the optimizer proposal for the area (nothing is saved). */
export function useArrangeStorageArea(marinaId: string, areaId: string) {
  const api = useApi();
  return useMutation({
    onMutate: () => assertOnline('arrangeStorageArea', 'allocation'),
    mutationFn: (body: StorageArrangeRequest) =>
      unwrap(
        api.POST(`${M}/storage/areas/{areaId}/arrange`, {
          params: { path: { marinaId, areaId } },
          body,
        }),
      ),
  });
}

/** Indicators, accessibility and per-item issues of a draft layout (nothing is saved). */
export function useEvaluateStorageLayout(marinaId: string, areaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: StorageEvaluateRequest) =>
      unwrap(
        api.POST(`${M}/storage/areas/{areaId}/evaluate`, {
          params: { path: { marinaId, areaId } },
          body,
        }),
      ),
  });
}

export function useStorageContract(marinaId: string, contractId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageContract(marinaId, contractId),
    enabled: !!contractId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/contracts/{contractId}`, {
          params: { path: { marinaId, contractId: contractId! } },
        }),
      ),
  });
}

export function useCreateStorageContract(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    onMutate: () => assertOnline('createStorageContract', 'allocation'),
    mutationFn: ({
      body,
      idempotencyKey,
    }: {
      body: StorageContractCreate;
      idempotencyKey?: string;
    }) =>
      unwrap(
        api.POST(`${M}/storage/contracts`, {
          params: {
            path: { marinaId },
            header: { 'Idempotency-Key': idempotencyKey ?? newIdempotencyKey() },
          },
          body,
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.storage(marinaId) });
      void qc.invalidateQueries({ queryKey: queryKeys.contracts(marinaId) });
    },
  });
}

export function useUpdateStorageContract(marinaId: string, contractId: string) {
  const api = useApi();
  const invalidate = useInvalidateStorage(marinaId);
  return useMutation({
    mutationFn: ({ body, version }: { body: StorageContractPatch; version: number }) =>
      unwrap(
        api.PATCH(`${M}/storage/contracts/{contractId}`, {
          params: { path: { marinaId, contractId }, header: { 'If-Match': ifMatch(version) } },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useStorageScenarios(
  marinaId: string,
  areaId?: string,
  status?: StorageScenarioStatus,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageScenarios(marinaId, { area_id: areaId, status }),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/scenarios`, {
          params: {
            path: { marinaId },
            query: {
              ...(areaId ? { area_id: areaId } : {}),
              ...(status ? { status } : {}),
              limit: 200,
            },
          },
        }),
      ),
  });
}

export function useCreateStorageScenario(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateStorage(marinaId);
  return useMutation({
    onMutate: () => assertOnline('createStorageScenario', 'online_only'),
    mutationFn: (body: S['StorageScenarioCreate']) =>
      unwrap(
        api.POST(`${M}/storage/scenarios`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useStorageScenario(marinaId: string, scenarioId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageScenario(marinaId, scenarioId),
    enabled: !!scenarioId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/scenarios/{scenarioId}`, {
          params: { path: { marinaId, scenarioId: scenarioId! } },
        }),
      ),
  });
}

/** Allocations of other resources the "Da collocare" tray of a draft scenario accepts. */
export function useStorageTrayCandidates(marinaId: string, scenarioId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.storageScenario(marinaId, scenarioId), 'tray-candidates'],
    enabled: enabled && !!scenarioId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/scenarios/{scenarioId}/tray-candidates`, {
          params: { path: { marinaId, scenarioId } },
        }),
      ),
  });
}

/** Writes the scenario detail an action returned into the cache, then invalidates the rest. */
function useScenarioMutation<V>(
  marinaId: string,
  scenarioId: string,
  operationId: string,
  run: (v: V) => Promise<StorageScenarioDetail>,
) {
  const qc = useQueryClient();
  return useMutation({
    onMutate: () => assertOnline(operationId, 'online_only'),
    mutationFn: run,
    onSuccess: (d) => {
      qc.setQueryData(queryKeys.storageScenario(marinaId, scenarioId), d);
      void qc.invalidateQueries({ queryKey: queryKeys.storageScenarios(marinaId) });
      void qc.invalidateQueries({ queryKey: queryKeys.storageCompare(marinaId, scenarioId) });
      void qc.invalidateQueries({
        queryKey: [...queryKeys.storageScenario(marinaId, scenarioId), 'tray-candidates'],
      });
    },
  });
}

export function useUpdateStorageScenario(marinaId: string, scenarioId: string) {
  const api = useApi();
  return useScenarioMutation(
    marinaId,
    scenarioId,
    'updateStorageScenario',
    (v: { body: S['StorageScenarioPatch']; version: number }) =>
      unwrap(
        api.PATCH(`${M}/storage/scenarios/{scenarioId}`, {
          params: { path: { marinaId, scenarioId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
  );
}

export function useSaveStorageScenarioItems(marinaId: string, scenarioId: string) {
  const api = useApi();
  return useScenarioMutation(
    marinaId,
    scenarioId,
    'saveStorageScenarioItems',
    (v: { items: StorageLayoutItemInput[]; version: number }) =>
      unwrap(
        api.PUT(`${M}/storage/scenarios/{scenarioId}/items`, {
          params: { path: { marinaId, scenarioId }, header: { 'If-Match': ifMatch(v.version) } },
          body: { items: v.items },
        }),
      ),
  );
}

export function useAddStorageScenarioItem(marinaId: string, scenarioId: string) {
  const api = useApi();
  return useScenarioMutation(
    marinaId,
    scenarioId,
    'addStorageScenarioItem',
    (v: { allocationId: string; version: number }) =>
      unwrap(
        api.POST(`${M}/storage/scenarios/{scenarioId}/items`, {
          params: { path: { marinaId, scenarioId }, header: { 'If-Match': ifMatch(v.version) } },
          body: { allocation_id: v.allocationId },
        }),
      ),
  );
}

export function useRemoveStorageScenarioItem(marinaId: string, scenarioId: string) {
  const api = useApi();
  return useScenarioMutation(
    marinaId,
    scenarioId,
    'removeStorageScenarioItem',
    (v: { allocationId: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/storage/scenarios/{scenarioId}/items/{allocationId}`, {
          params: {
            path: { marinaId, scenarioId, allocationId: v.allocationId },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
  );
}

export function useOptimizeStorageScenario(marinaId: string, scenarioId: string) {
  const api = useApi();
  return useScenarioMutation(
    marinaId,
    scenarioId,
    'optimizeStorageScenario',
    (v: { body: S['StorageOptimizeRequest']; version: number }) =>
      unwrap(
        api.POST(`${M}/storage/scenarios/{scenarioId}/optimize`, {
          params: { path: { marinaId, scenarioId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
  );
}

export function useDiscardStorageScenario(marinaId: string, scenarioId: string) {
  const api = useApi();
  const invalidate = useInvalidateStorage(marinaId);
  return useMutation({
    onMutate: () => assertOnline('discardStorageScenario', 'online_only'),
    mutationFn: (version: number) =>
      unwrap(
        api.DELETE(`${M}/storage/scenarios/{scenarioId}`, {
          params: { path: { marinaId, scenarioId }, header: { 'If-Match': ifMatch(version) } },
        }),
      ),
    onSuccess: invalidate,
  });
}

/** Comparison with another scenario of the same area or with the real state (`with` omitted or "real"). */
export function useCompareStorageScenario(
  marinaId: string,
  scenarioId: string,
  withId?: string,
  date?: string,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageCompare(marinaId, scenarioId, withId, date),
    enabled: !!scenarioId,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/scenarios/{scenarioId}/compare`, {
          params: {
            path: { marinaId, scenarioId },
            query: { ...(withId ? { with: withId } : {}), ...(date ? { date } : {}) },
          },
        }),
      ),
  });
}

/** Applies a draft scenario (all or nothing); a 409 scenario_conflicts carries details.conflicts. */
export function useApplyStorageScenario(marinaId: string, scenarioId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    onMutate: () => assertOnline('applyStorageScenario', 'allocation'),
    mutationFn: ({ version, idempotencyKey }: { version: number; idempotencyKey?: string }) =>
      unwrap(
        api.POST(`${M}/storage/scenarios/{scenarioId}/apply`, {
          params: {
            path: { marinaId, scenarioId },
            header: {
              'If-Match': ifMatch(version),
              'Idempotency-Key': idempotencyKey ?? newIdempotencyKey(),
            },
          },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.storage(marinaId) });
      void qc.invalidateQueries({ queryKey: queryKeys.ops(marinaId) });
    },
  });
}

export function useStorageMovements(marinaId: string, date?: string, areaId?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.storageMovements(marinaId, date, areaId),
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/storage/movements`, {
          params: {
            path: { marinaId },
            query: { ...(date ? { date } : {}), ...(areaId ? { area_id: areaId } : {}) },
          },
        }),
      ),
  });
}

/** Marks a movement done (online only, D18). */
export function useCompleteStorageMovement(marinaId: string) {
  const api = useApi();
  const invalidate = useInvalidateStorage(marinaId);
  return useMutation({
    onMutate: () => assertOnline('completeStorageMovement', 'online_only'),
    mutationFn: ({ movementId, version }: { movementId: string; version: number }) =>
      unwrap(
        api.POST(`${M}/storage/movements/{movementId}/done`, {
          params: { path: { marinaId, movementId }, header: { 'If-Match': ifMatch(version) } },
        }),
      ),
    onSuccess: invalidate,
  });
}
