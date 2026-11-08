// Calls of the yard phone screen (m10-plan §7, §14.3) to ORDERS' time entry and task operations, exactly as in the
// contract. Reads use the query keys of queryKeys.ts so that the offline cache allowlist matches them
// (activities.yard). Timer start/stop, manual hours, progress/checklist and photos go through the offline queue
// (kinds yard.timesheet and yard.activity_done, D25): the queue stores them at once, also offline, and replays
// them with their client_ref; none creates, moves or releases an allocation or a booking (INV-71).
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import {
  queryKeys,
  unwrap,
  useApi,
  type MyYardWork,
  type Timesheet,
  type components,
} from '@porto/shared/api';
import { enqueue, newIdempotencyKey } from '@porto/shared/offline';

type S = components['schemas'];

export type MyYardAssignment = S['MyYardAssignment'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const marinaPath = (marinaId: string) => `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}`;

function signedInUser(qc: QueryClient): string {
  return qc.getQueryData<S['Me'] | null>(queryKeys.me)?.user.id ?? '';
}

function invalidateYard(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.yard(marinaId) });
}

/** The caller's assignments (±7 days) and running timer (getMyYardWork, cached offline). */
export function useMyYardWork(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.myYardWork(marinaId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/my-work`, { params: { path: { marinaId } } }),
      ) as Promise<MyYardWork>,
  });
}

/** Starts the timer on a task (queued: kind yard.timesheet). The running entry comes back when online. */
export function useStartTimer(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: 'always',
    mutationFn: async (v: { taskId: string; title: string; startedAt: string }) => {
      const ref = newIdempotencyKey();
      const op = await enqueue({
        kind: 'yard.timesheet',
        userId: signedInUser(qc),
        marinaId,
        lane: `timer:${ref}`,
        request: {
          operationId: 'startTimer',
          path: `${marinaPath(marinaId)}/timesheets/timer`,
          body: { client_ref: ref, task_id: v.taskId, started_at: v.startedAt },
        },
        label: { key: 'yardPlanner:phone.queue.start', params: { title: v.title } },
      });
      return { opId: op.id, lane: op.lane };
    },
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

/**
 * Stops a timer (queued: kind yard.timesheet). With a timesheet id (the running entry is known) it is sent as is;
 * with the op of a start still queued it depends on it (the id is bound from the start's result).
 */
export function useStopTimer(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: 'always',
    mutationFn: async (v: {
      timesheetId?: string;
      startOp?: { opId: string; lane: string };
      title: string;
      endedAt: string;
    }) => {
      const ref = newIdempotencyKey();
      const body = { client_ref: ref, ended_at: v.endedAt };
      if (v.startOp) {
        return enqueue({
          kind: 'yard.timesheet',
          userId: signedInUser(qc),
          marinaId,
          lane: v.startOp.lane,
          dependsOn: { opId: v.startOp.opId, bind: { timesheetId: 'id' } },
          request: {
            operationId: 'stopTimer',
            path: `${marinaPath(marinaId)}/timesheets/{timesheetId}/stop`,
            body,
          },
          label: { key: 'yardPlanner:phone.queue.stop', params: { title: v.title } },
        });
      }
      return enqueue({
        kind: 'yard.timesheet',
        userId: signedInUser(qc),
        marinaId,
        lane: `timesheet:${v.timesheetId}`,
        request: {
          operationId: 'stopTimer',
          path: `${marinaPath(marinaId)}/timesheets/${encodeURIComponent(v.timesheetId ?? '')}/stop`,
          body,
        },
        label: { key: 'yardPlanner:phone.queue.stop', params: { title: v.title } },
      });
    },
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

/** Records manual hours on a task (queued: kind yard.timesheet). */
export function useCreateTimeEntry(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: 'always',
    mutationFn: (v: { taskId: string; title: string; workDate: string; hoursMilli: number }) => {
      const ref = newIdempotencyKey();
      return enqueue({
        kind: 'yard.timesheet',
        userId: signedInUser(qc),
        marinaId,
        lane: `task:${v.taskId}`,
        request: {
          operationId: 'createTimeEntry',
          path: `${marinaPath(marinaId)}/timesheets`,
          body: {
            client_ref: ref,
            task_id: v.taskId,
            work_date: v.workDate,
            hours_milli: v.hoursMilli,
          },
        },
        label: { key: 'yardPlanner:phone.queue.hours', params: { title: v.title } },
      });
    },
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

/** Progress, checklist and done of a task (queued: kind yard.activity_done; replays never lower progress). */
export function useUpdateTaskProgress(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: 'always',
    mutationFn: (v: {
      taskId: string;
      title: string;
      progressBp: number;
      checklist: { key: string; done: boolean }[];
      recordedAt: string;
    }) => {
      const ref = newIdempotencyKey();
      return enqueue({
        kind: 'yard.activity_done',
        userId: signedInUser(qc),
        marinaId,
        lane: `task:${v.taskId}`,
        request: {
          operationId: 'updateTaskProgress',
          path: `${marinaPath(marinaId)}/yard/tasks/${encodeURIComponent(v.taskId)}/progress`,
          body: {
            client_ref: ref,
            progress_bp: v.progressBp,
            done: v.progressBp >= 10000,
            checklist: v.checklist,
            recorded_at: v.recordedAt,
          },
        },
        label: { key: 'yardPlanner:phone.queue.progress', params: { title: v.title } },
      });
    },
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

/** A camera photo of a task (queued: kind yard.activity_done, the bytes kept in the queue until sent). */
export function useUploadTaskPhoto(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: 'always',
    mutationFn: (v: { taskId: string; title: string; photo: Blob }) =>
      enqueue({
        kind: 'yard.activity_done',
        userId: signedInUser(qc),
        marinaId,
        lane: `task:${v.taskId}`,
        request: {
          operationId: 'uploadTaskPhoto',
          path: `${marinaPath(marinaId)}/yard/tasks/${encodeURIComponent(v.taskId)}/photos`,
          contentType: v.photo.type || 'image/jpeg',
        },
        blob: v.photo,
        label: { key: 'yardPlanner:phone.queue.photo', params: { title: v.title } },
      }),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

/** Submitted time entries to approve (yard.manage, online). */
export function useSubmittedTimesheets(marinaId: string, enabled: boolean) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.timesheets(marinaId, { status: 'submitted' }),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/timesheets`, {
          params: { path: { marinaId }, query: { status: 'submitted', limit: 200 } },
        }),
      ),
  });
}

/** Approves time entries in batch (yard.manage, online only: approval freezes the cost). */
export function useApproveTimesheets(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (entries: Pick<Timesheet, 'id' | 'version'>[]) =>
      unwrap(
        api.POST(`${M}/timesheets/approve`, {
          params: { path: { marinaId } },
          body: { entries: entries.map((e) => ({ id: e.id, version: e.version })) },
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}
