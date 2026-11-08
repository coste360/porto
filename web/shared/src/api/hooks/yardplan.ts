// Hooks of the M10 yard planner (m10-plan §14.3, owner PLANNER): employees, calendars, absences, planner grid,
// assignments, workload, at-risk orders and the auto-plan proposal. Keys live under queryKeys.yard(marinaId),
// invalidated by yard.changed. Everything here is online: assignments are never queued offline (spec 29.2).
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type Employee = S['Employee'];
export type EmployeeInput = S['EmployeeInput'];
export type EmployeePatch = S['EmployeePatch'];
export type EmployeeCalendarDay = S['EmployeeCalendarDay'];
export type Absence = S['Absence'];
export type AbsenceInput = S['AbsenceInput'];
export type AbsenceKind = S['AbsenceKind'];
export type Planner = S['Planner'];
export type PlannerRow = S['PlannerRow'];
export type PlannerDay = S['PlannerDay'];
export type PlannerTask = S['PlannerTask'];
export type Assignment = S['Assignment'];
export type AssignmentChange = S['AssignmentChange'];
export type AssignmentBatchInput = S['AssignmentBatchInput'];
export type AssignmentBatchResult = S['AssignmentBatchResult'];
export type YardLoad = S['YardLoad'];
export type YardLoadRow = S['YardLoadRow'];
export type YardLoadCell = S['YardLoadCell'];
export type AtRiskOrder = S['AtRiskOrder'];
export type AtRiskOrderList = S['AtRiskOrderList'];
export type YardPlanProposal = S['YardPlanProposal'];
export type YardPlanProposalInput = S['YardPlanProposalInput'];
export type YardLoadGroupBy = YardLoad['group_by'];
export type YardLoadPeriod = YardLoad['period'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

function invalidateYard(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.yard(marinaId) });
}

// ---- employees ----

export type EmployeeFilters = { active?: boolean; department?: string; qualification_id?: string };

export function useEmployees(marinaId: string, filters: EmployeeFilters = {}) {
  const api = useApi();
  const key: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) if (v !== undefined && v !== '') key[k] = String(v);
  return useQuery({
    queryKey: queryKeys.employees(marinaId, key),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/employees`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useEmployee(marinaId: string, employeeId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.employee(marinaId, employeeId),
    enabled: !!employeeId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/employees/{employeeId}`, {
          params: { path: { marinaId, employeeId: employeeId! } },
        }),
      ),
  });
}

export function useCreateEmployee(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: EmployeeInput) =>
      unwrap(
        api.POST(`${M}/yard/employees`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useUpdateEmployee(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: EmployeePatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/employees/{employeeId}`, {
          params: {
            path: { marinaId, employeeId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function usePutEmployeeCalendar(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; days: EmployeeCalendarDay[] }) =>
      unwrap(
        api.PUT(`${M}/yard/employees/{employeeId}/calendar`, {
          params: {
            path: { marinaId, employeeId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { days: v.days },
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useAbsences(marinaId: string, employeeId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.absences(marinaId, employeeId),
    enabled: !!employeeId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/employees/{employeeId}/absences`, {
          params: { path: { marinaId, employeeId: employeeId! } },
        }),
      ),
  });
}

export function useCreateAbsence(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { employeeId: string; body: AbsenceInput }) =>
      unwrap(
        api.POST(`${M}/yard/employees/{employeeId}/absences`, {
          params: {
            path: { marinaId, employeeId: v.employeeId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useDeleteAbsence(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { employeeId: string; absenceId: string }) => {
      const res = await api.DELETE(`${M}/yard/employees/{employeeId}/absences/{absenceId}`, {
        params: { path: { marinaId, employeeId: v.employeeId, absenceId: v.absenceId } },
      });
      if (res.error) throw res.error;
    },
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

// ---- planner ----

export function usePlanner(marinaId: string, from?: string, to?: string, department?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.planner(marinaId, from, to, department),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/planner`, {
          params: {
            path: { marinaId },
            query: {
              ...(from ? { from } : {}),
              ...(to ? { to } : {}),
              ...(department ? { department } : {}),
            },
          },
        }),
      ),
  });
}

export function usePutAssignments(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AssignmentBatchInput) =>
      unwrap(api.PUT(`${M}/yard/planner/assignments`, { params: { path: { marinaId } }, body })),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useDeleteAssignment(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { id: string; version: number }) => {
      const res = await api.DELETE(`${M}/yard/planner/assignments/{assignmentId}`, {
        params: {
          path: { marinaId, assignmentId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
      });
      if (res.error) throw res.error;
    },
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useYardLoad(
  marinaId: string,
  filters: { group_by?: YardLoadGroupBy; period?: YardLoadPeriod; from?: string; to?: string } = {},
) {
  const api = useApi();
  const key: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) if (v) key[k] = v;
  return useQuery({
    queryKey: queryKeys.yardLoad(marinaId, key),
    queryFn: () =>
      unwrap(api.GET(`${M}/yard/load`, { params: { path: { marinaId }, query: key } })),
  });
}

export function useAtRiskOrders(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.atRiskOrders(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/at-risk`, { params: { path: { marinaId } } })),
  });
}

/** The auto-plan proposal: a read-only POST (nothing is written until confirmed through usePutAssignments). */
export function useProposeYardPlan(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: YardPlanProposalInput) =>
      unwrap(api.POST(`${M}/yard/planner/proposal`, { params: { path: { marinaId } }, body })),
  });
}

/** Yard qualifications as options of the planner screens (code and labels; the catalogue owns their editing). */
export function useQualificationOptions(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardQualifications(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/qualifications`, { params: { path: { marinaId } } })),
  });
}
