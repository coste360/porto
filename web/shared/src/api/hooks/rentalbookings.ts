// Hooks of the M11 rental bookings (m11-plan §14.2, owner BOOKING): planner, availability, bookings, blocks and the
// day view. Keys live under queryKeys.rental(marinaId), invalidated by rental.changed. Every booking write is
// online (never queued offline, INV-71).
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type RentalSlotPoint = S['RentalSlotPoint'];
export type RentalBooking = S['RentalBooking'];
export type RentalBookingStatus = S['RentalBookingStatus'];
export type RentalBookingInput = S['RentalBookingInput'];
export type RentalBookingPatch = S['RentalBookingPatch'];
export type RentalTransitionInput = S['RentalTransitionInput'];
export type RentalPlanner = S['RentalPlanner'];
export type RentalPlannerRow = S['RentalPlannerRow'];
export type RentalPlannerSegment = S['RentalPlannerSegment'];
export type RentalBlock = S['RentalBlock'];
export type RentalBlockInput = S['RentalBlockInput'];
export type RentalDay = S['RentalDay'];
export type RentalDayEntry = S['RentalDayEntry'];
export type RentalPlannerView = 'hours' | 'half_days' | 'days';

const M = '/api/admin/v1/marinas/{marinaId}' as const;

export function useRentalPlanner(
  marinaId: string,
  from: string,
  to: string,
  view: RentalPlannerView = 'days',
  category?: S['RentalCategory'],
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalPlanner(marinaId, from, to, view, category),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/planner`, {
          params: {
            path: { marinaId },
            query: { from, to, view, ...(category ? { category } : {}) },
          },
        }),
      ),
  });
}

export function useRentalBooking(marinaId: string, bookingId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalBooking(marinaId, bookingId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/bookings/{rentalBookingId}`, {
          params: { path: { marinaId, rentalBookingId: bookingId } },
        }),
      ),
  });
}

/** Day view (date undefined = today in the marina time zone; the key keeps 'today' for the offline cache). */
export function useRentalDay(marinaId: string, date?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalDay(marinaId, date ?? 'today'),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/day`, { params: { path: { marinaId }, query: date ? { date } : {} } }),
      ),
  });
}

export type RentalBookingList = S['RentalBookingList'];
export type RentalAvailability = S['RentalAvailability'];
export type RentalBlockPatch = S['RentalBlockPatch'];
export type RentalBookingQuoteInput = S['RentalQuoteInput'];
export type RentalBookingQuoteOption = S['RentalQuoteOption'];

function invalidateRental(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.rental(marinaId) });
}

export type RentalBookingFilters = {
  from?: string;
  to?: string;
  status?: RentalBookingStatus;
  unit_id?: string;
  customer_id?: string;
  cursor?: string;
};

export function useRentalBookings(marinaId: string, filters: RentalBookingFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalBookings(marinaId, filters),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/bookings`, {
          params: { path: { marinaId }, query: { ...filters, limit: 100 } },
        }),
      ),
  });
}

export function useRentalBlocks(marinaId: string, from: string, to: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalBlocks(marinaId, { from, to }),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/blocks`, { params: { path: { marinaId }, query: { from, to } } }),
      ),
  });
}

/** Stateless quote of a unit and range for the booking dialog: every formula (sorted by total) or one. */
export function useRentalBookingQuote(marinaId: string, input: RentalBookingQuoteInput | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalQuote(marinaId, input ?? undefined),
    enabled: input !== null,
    queryFn: () =>
      unwrap(api.POST(`${M}/rental/quote`, { params: { path: { marinaId } }, body: input! })),
  });
}

export function useCreateRentalBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RentalBookingInput) =>
      unwrap(
        api.POST(`${M}/rental/bookings`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidateRental(qc, marinaId),
  });
}

export function useUpdateRentalBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: RentalBookingPatch }) =>
      unwrap(
        api.PATCH(`${M}/rental/bookings/{rentalBookingId}`, {
          params: {
            path: { marinaId, rentalBookingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidateRental(qc, marinaId),
  });
}

/** Moves a booking from the planner: reads its current version, then reschedules it (unit and range). */
export function useMoveRentalBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: {
      id: string;
      unit_id: string;
      start: RentalSlotPoint;
      end: RentalSlotPoint;
    }) => {
      const b = await unwrap(
        api.GET(`${M}/rental/bookings/{rentalBookingId}`, {
          params: { path: { marinaId, rentalBookingId: v.id } },
        }),
      );
      return unwrap(
        api.PATCH(`${M}/rental/bookings/{rentalBookingId}`, {
          params: {
            path: { marinaId, rentalBookingId: v.id },
            header: { 'If-Match': ifMatch(b.version) },
          },
          body: { unit_id: v.unit_id, start: v.start, end: v.end },
        }),
      );
    },
    onSettled: () => invalidateRental(qc, marinaId),
  });
}

export function useTransitionRentalBooking(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: RentalTransitionInput }) =>
      unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/transition`, {
          params: {
            path: { marinaId, rentalBookingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidateRental(qc, marinaId),
  });
}

export function useCreateRentalBlock(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RentalBlockInput) =>
      unwrap(
        api.POST(`${M}/rental/blocks`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidateRental(qc, marinaId),
  });
}

export function useDeleteRentalBlock(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/rental/blocks/{rentalBlockId}`, {
          params: {
            path: { marinaId, rentalBlockId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidateRental(qc, marinaId),
  });
}
