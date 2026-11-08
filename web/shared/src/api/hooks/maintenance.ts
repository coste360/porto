// Hooks of maintenance: equipment, plans, tasks, field reports, worklist and the maintenance lens (m9-plan §12.2,
// owner UTILITIES). Keys live under queryKeys.maintenance(marinaId), invalidated by maintenance.changed; the
// worklist is the offline allowlist slot activities.assigned. Field reports (with their photo) and outcomes go
// through the offline queue (kinds maintenance.report and maintenance.outcome, m9-plan §10); scheduling, release
// and cancellation book or free nights and stay online.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary } from '../client';
import { useApi } from '../context';
import { enqueue, enqueueAndWait } from '../../offline/index';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type Equipment = S['Equipment'];
export type EquipmentKind = S['EquipmentKind'];
export type EquipmentInput = S['EquipmentInput'];
export type EquipmentPatch = S['EquipmentPatch'];
export type MaintenanceElementType = S['MaintenanceElementType'];
export type MaintenanceElementRef = S['MaintenanceElementRef'];
export type MaintenanceIntervention = S['MaintenanceIntervention'];
export type MaintenancePeriodicity = S['MaintenancePeriodicity'];
export type ChecklistEntry = S['ChecklistEntry'];
export type ChecklistResult = S['ChecklistResult'];
export type MaintenancePlan = S['MaintenancePlan'];
export type MaintenancePlanInput = S['MaintenancePlanInput'];
export type MaintenancePlanPatch = S['MaintenancePlanPatch'];
export type MaintenanceTask = S['MaintenanceTask'];
export type MaintenanceTaskStatus = S['MaintenanceTaskStatus'];
export type MaintenancePriority = S['MaintenancePriority'];
export type MaintenanceOutcome = S['MaintenanceOutcome'];
export type MaintenanceTaskCreate = S['MaintenanceTaskCreate'];
export type MaintenanceTaskPatch = S['MaintenanceTaskPatch'];
export type MaintenanceScheduleInput = S['MaintenanceScheduleInput'];
export type MaintenanceConflict = S['MaintenanceConflict'];
export type MaintenanceCompleteInput = S['MaintenanceCompleteInput'];
export type MaintenanceReportInput = S['MaintenanceReportInput'];
export type MaintenanceWorklist = S['MaintenanceWorklist'];
export type MaintenanceMap = S['MaintenanceMap'];
export type MaintenanceMapEntry = S['MaintenanceMapEntry'];
export type MaintenanceFile = S['MaintenanceFile'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const marinaPath = (marinaId: string) => `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}`;

/** Filters are stored in the query key as strings only (stable keys, no undefined values). */
function compact(
  filters: Record<string, string | boolean | undefined | null>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) {
    if (v === undefined || v === null || v === '' || v === false) continue;
    out[k] = String(v);
  }
  return out;
}

function signedInUser(qc: QueryClient): string | undefined {
  return qc.getQueryData<S['Me'] | null>(queryKeys.me)?.user.id;
}

function invalidate(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.maintenance(marinaId) });
}

// ---- equipment ----

export type EquipmentFilters = { kind?: EquipmentKind; active?: boolean };

export function useEquipmentList(marinaId: string, filters: EquipmentFilters = {}) {
  const api = useApi();
  const f = compact(filters);
  return useQuery({
    queryKey: queryKeys.equipment(marinaId, f),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/equipment`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useCreateEquipment(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: EquipmentInput) =>
      unwrap(
        api.POST(`${M}/equipment`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateEquipment(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: EquipmentPatch }) =>
      unwrap(
        api.PATCH(`${M}/equipment/{equipmentId}`, {
          params: {
            path: { marinaId, equipmentId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- plans ----

export function useMaintenancePlans(
  marinaId: string,
  filters: { active?: boolean; element_type?: MaintenanceElementType } = {},
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maintenancePlans(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maintenance/plans`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useMaintenancePlan(marinaId: string, planId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maintenancePlan(marinaId, planId),
    enabled: !!planId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maintenance/plans/{planId}`, {
          params: { path: { marinaId, planId: planId! } },
        }),
      ),
  });
}

export function useCreateMaintenancePlan(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: MaintenancePlanInput) =>
      unwrap(
        api.POST(`${M}/maintenance/plans`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateMaintenancePlan(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: MaintenancePlanPatch }) =>
      unwrap(
        api.PATCH(`${M}/maintenance/plans/{planId}`, {
          params: { path: { marinaId, planId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- tasks ----

export type MaintenanceTaskFilters = {
  status?: MaintenanceTaskStatus;
  priority?: MaintenancePriority;
  element_type?: MaintenanceElementType;
  element_id?: string;
  plan_id?: string;
  assignee_user_id?: string;
  overdue_only?: boolean;
};

export function useMaintenanceTasks(marinaId: string, filters: MaintenanceTaskFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maintenanceTasks(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maintenance/tasks`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useMaintenanceTask(marinaId: string, taskId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maintenanceTask(marinaId, taskId),
    enabled: !!taskId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maintenance/tasks/{taskId}`, {
          params: { path: { marinaId, taskId: taskId! } },
        }),
      ),
  });
}

export function useCreateMaintenanceTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: MaintenanceTaskCreate) =>
      unwrap(
        api.POST(`${M}/maintenance/tasks`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateMaintenanceTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: MaintenanceTaskPatch }) =>
      unwrap(
        api.PATCH(`${M}/maintenance/tasks/{taskId}`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Schedules nights; a 409 maintenance_conflicts carries details.conflicts (MaintenanceConflict[]). Online only. */
export function useScheduleMaintenanceTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: MaintenanceScheduleInput }) =>
      unwrap(
        api.POST(`${M}/maintenance/tasks/{taskId}/schedule`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useStartMaintenanceTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/maintenance/tasks/{taskId}/start`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useReleaseMaintenanceTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; lastNight?: string }) =>
      unwrap(
        api.POST(`${M}/maintenance/tasks/{taskId}/release`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.lastNight ? { last_night: v.lastNight } : {},
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useCancelMaintenanceTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason: string }) =>
      unwrap(
        api.POST(`${M}/maintenance/tasks/{taskId}/cancel`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: { reason: v.reason },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/**
 * Records the outcome of a task through the offline queue (kind maintenance.outcome): online it resolves with the
 * task, offline it rejects with OfflineQueuedError and the queue sends it later. The client_ref makes a replay
 * answer the stored result (a completion by someone else is 409 task_already_done).
 */
export function useCompleteMaintenanceTask(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    // Queue first (spec 29.2): the op is stored at once, also offline; a paused mutation would lose it.
    networkMode: 'always',
    mutationFn: async (v: {
      id: string;
      title: string;
      body: Omit<MaintenanceCompleteInput, 'client_ref'>;
    }) => {
      const userId = signedInUser(qc) ?? '';
      const body: MaintenanceCompleteInput = { ...v.body, client_ref: newIdempotencyKey() };
      return enqueueAndWait<MaintenanceTask>({
        kind: 'maintenance.outcome',
        userId,
        marinaId,
        lane: `task:${v.id}`,
        request: {
          operationId: 'completeMaintenanceTask',
          path: `${marinaPath(marinaId)}/maintenance/tasks/${encodeURIComponent(v.id)}/complete`,
          body,
        },
        label: { key: 'maintenance:queue.outcome', params: { title: v.title } },
      });
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/**
 * Reports a problem from the field through the offline queue (kind maintenance.report), with an optional photo
 * queued as a dependent upload (bound to the task id of the report's result). It never schedules or occupies.
 */
export function useReportMaintenance(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    // Queue first (spec 29.2): the op is stored at once, also offline; a paused mutation would lose it.
    networkMode: 'always',
    mutationFn: async (v: { body: Omit<MaintenanceReportInput, 'client_ref'>; photo?: Blob }) => {
      const userId = signedInUser(qc) ?? '';
      const ref = newIdempotencyKey();
      const lane = `report:${ref}`;
      const report = await enqueue({
        kind: 'maintenance.report',
        userId,
        marinaId,
        lane,
        request: {
          operationId: 'reportMaintenance',
          path: `${marinaPath(marinaId)}/maintenance/reports`,
          body: { ...v.body, client_ref: ref },
        },
        label: { key: 'maintenance:queue.report', params: { title: v.body.title } },
      });
      if (v.photo) {
        await enqueue({
          kind: 'maintenance.report',
          userId,
          marinaId,
          lane,
          dependsOn: { opId: report.id, bind: { taskId: 'id' } },
          request: {
            operationId: 'uploadMaintenancePhoto',
            path: `${marinaPath(marinaId)}/maintenance/tasks/{taskId}/files`,
            contentType: v.photo.type || 'image/jpeg',
          },
          blob: v.photo,
          label: { key: 'maintenance:queue.photo', params: { title: v.body.title } },
        });
      }
      return report;
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Attaches a file to a task: photos through the queue, documents (PDF) online. */
export function useUploadMaintenanceFile(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { taskId: string; title: string; file: File }) => {
      const path = `${marinaPath(marinaId)}/maintenance/tasks/${encodeURIComponent(v.taskId)}/files`;
      if (v.file.type.startsWith('image/')) {
        return enqueueAndWait<MaintenanceFile>({
          kind: 'maintenance.report',
          userId: signedInUser(qc) ?? '',
          marinaId,
          lane: `task:${v.taskId}`,
          request: { operationId: 'uploadMaintenancePhoto', path, contentType: v.file.type },
          blob: v.file,
          label: { key: 'maintenance:queue.photo', params: { title: v.title } },
        });
      }
      return (
        await uploadBinary<MaintenanceFile>(path, v.file, {
          method: 'POST',
          contentType: v.file.type,
          headers: { 'Idempotency-Key': newIdempotencyKey() },
        })
      ).data;
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Fetches a 5-minute link to a task file. */
export function useMaintenanceFileLink(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { taskId: string; fileId: string }) =>
      unwrap(
        api.GET(`${M}/maintenance/tasks/{taskId}/files/{fileId}/link`, {
          params: { path: { marinaId, taskId: v.taskId, fileId: v.fileId } },
        }),
      ),
  });
}

export function useMaintenanceWorklist(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maintenanceWorklist(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/maintenance/worklist`, { params: { path: { marinaId } } })),
  });
}

export function useMaintenanceMap(marinaId: string, mapId: string | undefined, date?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.maintenanceMap(marinaId, mapId, date),
    enabled: !!mapId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maintenance/map`, {
          params: { path: { marinaId }, query: { map_id: mapId, ...(date ? { date } : {}) } },
        }),
      ),
  });
}
