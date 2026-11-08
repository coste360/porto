// Hooks of the M10 yard work orders (m10-plan §14.2, owner ORDERS): orders, board, tasks, time entries (office side:
// list, manual entry, approval), materials, external costs, final account and billings. Keys live under
// queryKeys.yard(marinaId), invalidated by yard.changed. The phone screen queues its timers, hours, progress and
// photos itself (features/phone/yard, offline kinds yard.timesheet and yard.activity_done); the office calls here are
// online.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type WorkOrder = S['WorkOrder'];
export type WorkOrderStatus = S['WorkOrderStatus'];
export type WorkOrderAction = S['WorkOrderAction'];
export type WorkOrderLine = S['WorkOrderLine'];
export type WorkOrderInput = S['WorkOrderInput'];
export type WorkOrderPatch = S['WorkOrderPatch'];
export type YardBoard = S['YardBoard'];
export type YardBoardCard = S['YardBoardCard'];
export type WorkTask = S['WorkTask'];
export type WorkTaskInput = S['WorkTaskInput'];
export type WorkTaskPatch = S['WorkTaskPatch'];
export type WorkTaskStatus = S['WorkTaskStatus'];
export type WorkTaskFile = S['WorkTaskFile'];
export type Timesheet = S['Timesheet'];
export type TimesheetStatus = S['TimesheetStatus'];
export type TimeEntryInput = S['TimeEntryInput'];
export type StockMovement = S['StockMovement'];
export type StockMovementInput = S['StockMovementInput'];
export type ExternalCost = S['ExternalCost'];
export type ExternalCostInput = S['ExternalCostInput'];
export type ExternalCostPatch = S['ExternalCostPatch'];
export type FinalAccount = S['FinalAccount'];
export type FinalAccountLine = S['FinalAccountLine'];
export type WorkOrderBilling = S['WorkOrderBilling'];
export type WorkOrderBillingInput = S['WorkOrderBillingInput'];
export type WorkOrderBillingResult = S['WorkOrderBillingResult'];
export type MyYardWork = S['MyYardWork'];
export type YardReport = S['YardReport'];

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

function invalidate(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.yard(marinaId) });
}

// ---- orders ----

export type WorkOrderFilters = {
  status?: WorkOrderStatus;
  open_only?: boolean;
  customer_id?: string;
  vessel_id?: string;
  q?: string;
};

export function useWorkOrders(marinaId: string, filters: WorkOrderFilters = {}) {
  const api = useApi();
  const f = compact(filters);
  return useQuery({
    queryKey: queryKeys.workOrders(marinaId, f),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-orders`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useYardBoard(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardBoard(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/board`, { params: { path: { marinaId } } })),
  });
}

export function useWorkOrder(marinaId: string, workOrderId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.workOrder(marinaId, workOrderId),
    enabled: !!workOrderId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-orders/{workOrderId}`, {
          params: { path: { marinaId, workOrderId: workOrderId! } },
        }),
      ),
  });
}

export function useCreateWorkOrder(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WorkOrderInput) =>
      unwrap(
        api.POST(`${M}/yard/work-orders`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateWorkOrder(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: WorkOrderPatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/work-orders/{workOrderId}`, {
          params: {
            path: { marinaId, workOrderId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useTransitionWorkOrder(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; action: WorkOrderAction; reason?: string }) =>
      unwrap(
        api.POST(`${M}/yard/work-orders/{workOrderId}/transition`, {
          params: {
            path: { marinaId, workOrderId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { action: v.action, ...(v.reason ? { reason: v.reason } : {}) },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- tasks ----

export function useWorkTasks(marinaId: string, workOrderId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.workTasks(marinaId, workOrderId),
    enabled: !!workOrderId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-orders/{workOrderId}/tasks`, {
          params: { path: { marinaId, workOrderId: workOrderId! } },
        }),
      ),
  });
}

export function useCreateWorkTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { workOrderId: string; body: WorkTaskInput }) =>
      unwrap(
        api.POST(`${M}/yard/work-orders/{workOrderId}/tasks`, {
          params: {
            path: { marinaId, workOrderId: v.workOrderId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateWorkTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: WorkTaskPatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/tasks/{taskId}`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useDeleteWorkTask(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/yard/tasks/{taskId}`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useSetTaskDependencies(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; dependsOn: string[] }) =>
      unwrap(
        api.PUT(`${M}/yard/tasks/{taskId}/dependencies`, {
          params: { path: { marinaId, taskId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: { depends_on: v.dependsOn },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Progress, checklist marks and done of a task from the office (online; the phone queues the same call). */
export function useTaskProgress(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: {
      id: string;
      progressBp?: number;
      done?: boolean;
      checklist?: { key: string; done: boolean }[];
    }) =>
      unwrap(
        api.POST(`${M}/yard/tasks/{taskId}/progress`, {
          params: { path: { marinaId, taskId: v.id } },
          body: {
            client_ref: newIdempotencyKey(),
            ...(v.progressBp !== undefined ? { progress_bp: v.progressBp } : {}),
            ...(v.done !== undefined ? { done: v.done } : {}),
            ...(v.checklist ? { checklist: v.checklist } : {}),
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useTaskFiles(marinaId: string, taskId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.taskFiles(marinaId, taskId),
    enabled: !!taskId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/tasks/{taskId}/files`, {
          params: { path: { marinaId, taskId: taskId! } },
        }),
      ),
  });
}

/** Attaches a photo or document to a task (online from the office). */
export function useUploadTaskFile(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { taskId: string; file: File }) =>
      (
        await uploadBinary<WorkTaskFile>(
          `${marinaPath(marinaId)}/yard/tasks/${encodeURIComponent(v.taskId)}/photos`,
          v.file,
          {
            method: 'POST',
            contentType: v.file.type,
            headers: { 'Idempotency-Key': newIdempotencyKey() },
          },
        )
      ).data,
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Fetches a 5-minute link to a task file. */
export function useTaskFileLink(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { taskId: string; fileId: string }) =>
      unwrap(
        api.GET(`${M}/yard/tasks/{taskId}/files/{fileId}/link`, {
          params: { path: { marinaId, taskId: v.taskId, fileId: v.fileId } },
        }),
      ),
  });
}

// ---- time entries (office) ----

export type TimesheetFilters = {
  employee_id?: string;
  work_order_id?: string;
  status?: TimesheetStatus;
  from?: string;
  to?: string;
};

export function useTimesheets(marinaId: string, filters: TimesheetFilters = {}, enabled = true) {
  const api = useApi();
  const f = compact(filters);
  return useQuery({
    queryKey: queryKeys.timesheets(marinaId, f),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/timesheets`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

/** A manual entry recorded by the office (online). */
export function useCreateTimeEntry(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Omit<TimeEntryInput, 'client_ref'>) => {
      const ref = newIdempotencyKey();
      return unwrap(
        api.POST(`${M}/timesheets`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': ref } },
          body: { ...body, client_ref: ref },
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useApproveTimeEntries(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (entries: { id: string; version: number }[]) =>
      unwrap(
        api.POST(`${M}/timesheets/approve`, { params: { path: { marinaId } }, body: { entries } }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useRejectTimeEntry(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason: string }) =>
      unwrap(
        api.POST(`${M}/timesheets/{timesheetId}/reject`, {
          params: {
            path: { marinaId, timesheetId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { reason: v.reason },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useVoidTimeEntry(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason?: string }) =>
      unwrap(
        api.POST(`${M}/timesheets/{timesheetId}/void`, {
          params: {
            path: { marinaId, timesheetId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.reason ? { reason: v.reason } : {},
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- materials and external costs ----

export function useStockMovements(marinaId: string, workOrderId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.stockMovements(marinaId, compact({ work_order_id: workOrderId })),
    enabled: !!workOrderId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/stock-movements`, {
          params: { path: { marinaId }, query: { work_order_id: workOrderId, limit: 200 } },
        }),
      ),
  });
}

export function useCreateStockMovement(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: StockMovementInput) =>
      unwrap(
        api.POST(`${M}/stock-movements`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useExternalCosts(marinaId: string, workOrderId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.externalCosts(marinaId, workOrderId),
    enabled: !!workOrderId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-orders/{workOrderId}/external-costs`, {
          params: { path: { marinaId, workOrderId: workOrderId! } },
        }),
      ),
  });
}

export function useCreateExternalCost(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { workOrderId: string; body: ExternalCostInput }) =>
      unwrap(
        api.POST(`${M}/yard/work-orders/{workOrderId}/external-costs`, {
          params: {
            path: { marinaId, workOrderId: v.workOrderId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useDeleteExternalCost(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/yard/external-costs/{externalCostId}`, {
          params: {
            path: { marinaId, externalCostId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- final account and billings ----

export function useFinalAccount(marinaId: string, workOrderId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.finalAccount(marinaId, workOrderId),
    enabled: !!workOrderId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-orders/{workOrderId}/account`, {
          params: { path: { marinaId, workOrderId: workOrderId! } },
        }),
      ) as Promise<FinalAccount>,
  });
}

export function useWorkOrderBillings(marinaId: string, workOrderId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.workOrderBillings(marinaId, workOrderId),
    enabled: !!workOrderId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-orders/{workOrderId}/billings`, {
          params: { path: { marinaId, workOrderId: workOrderId! } },
        }),
      ),
  });
}

/** Previews (dryRun) or creates a billing: progress, final or adjustment through the M5 charges. */
export function useWorkOrderBilling(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { workOrderId: string; body: WorkOrderBillingInput; dryRun: boolean }) =>
      unwrap(
        api.POST(`${M}/yard/work-orders/{workOrderId}/billings`, {
          params: {
            path: { marinaId, workOrderId: v.workOrderId },
            query: v.dryRun ? { dry_run: true } : {},
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      ) as Promise<WorkOrderBillingResult>,
    onSuccess: (_d, v) => (v.dryRun ? undefined : invalidate(qc, marinaId)),
  });
}
