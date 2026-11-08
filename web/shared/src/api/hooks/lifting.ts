// Hooks of the M10 lifting equipment (m10-plan §14.3, owner PLANNER): the day calendar, book, reschedule, cancel,
// complete and the operators of a machine. Keys live under queryKeys.yard(marinaId), invalidated by yard.changed.
// The day calendar is cached for the phone (allowlist slot activities.lifting); bookings move a machine and stay
// online (never queued offline, INV-71).
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type EquipmentBooking = S['EquipmentBooking'];
export type EquipmentBookingDay = S['EquipmentBookingDay'];
export type EquipmentBookingStatus = S['EquipmentBookingStatus'];
export type EquipmentBookingPurpose = S['EquipmentBookingPurpose'];
export type EquipmentBookingInput = S['EquipmentBookingInput'];
export type EquipmentBookingPatch = S['EquipmentBookingPatch'];
export type EquipmentOperators = S['EquipmentOperators'];
export type DayEquipment = EquipmentBookingDay['equipment'][number];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

function invalidateYard(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.yard(marinaId) });
}

export type BookingFilters = {
  equipment_id?: string;
  status?: EquipmentBookingStatus;
  work_order_id?: string;
};

/** Day calendar (date undefined = today in the marina time zone; the key keeps 'today' for the offline cache). */
export function useEquipmentBookings(
  marinaId: string,
  date?: string,
  filters: BookingFilters = {},
) {
  const api = useApi();
  const key: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) if (v) key[k] = v;
  return useQuery({
    queryKey: queryKeys.equipmentBookings(
      marinaId,
      date ?? 'today',
      Object.keys(key).length ? key : undefined,
    ),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/equipment-bookings`, {
          params: { path: { marinaId }, query: { ...(date ? { date } : {}), ...key } },
        }),
      ),
  });
}

export function useCreateEquipmentBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: EquipmentBookingInput) =>
      unwrap(
        api.POST(`${M}/equipment-bookings`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useUpdateEquipmentBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: EquipmentBookingPatch }) =>
      unwrap(
        api.PATCH(`${M}/equipment-bookings/{equipmentBookingId}`, {
          params: {
            path: { marinaId, equipmentBookingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useCancelEquipmentBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason?: string }) =>
      unwrap(
        api.POST(`${M}/equipment-bookings/{equipmentBookingId}/cancel`, {
          params: {
            path: { marinaId, equipmentBookingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.reason ? { reason: v.reason } : {},
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useCompleteEquipmentBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/equipment-bookings/{equipmentBookingId}/complete`, {
          params: {
            path: { marinaId, equipmentBookingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}

export function useEquipmentOperators(marinaId: string, equipmentId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.equipmentOperators(marinaId, equipmentId),
    enabled: !!equipmentId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/equipment/{equipmentId}/operators`, {
          params: { path: { marinaId, equipmentId: equipmentId! } },
        }),
      ),
  });
}

export function usePutEquipmentOperators(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { equipmentId: string; employeeIds: string[] }) =>
      unwrap(
        api.PUT(`${M}/equipment/{equipmentId}/operators`, {
          params: { path: { marinaId, equipmentId: v.equipmentId } },
          body: { employee_ids: v.employeeIds },
        }),
      ),
    onSuccess: () => invalidateYard(qc, marinaId),
  });
}
