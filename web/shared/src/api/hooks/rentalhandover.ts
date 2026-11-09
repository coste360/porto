// Hooks of the M11 rental handover (m11-plan §14.3, owner HANDOVER): check-out and check-in drafts, identity,
// contract signature, deposit, damages, check-in preview and commit, quick cash. Keys live under
// queryKeys.rental(marinaId), invalidated by rental.changed (and billing.changed for the charges). Only the
// checklist and the boat photos are queued offline (kind rental.checklist, INV-71); every commit, the identity, the
// signature, the deposit, the damages and the quick cash are online only (assertOnline).
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary } from '../client';
import { useApi } from '../context';
import { assertOnline, enqueueAndWait } from '../../offline/index';
import type { NewOp } from '../../offline/types';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type RentalPhase = S['RentalPhase'];
export type RentalHandover = S['RentalHandover'];
export type RentalHandoverFile = S['RentalHandoverFile'];
export type RentalChecklistEntry = S['RentalChecklistEntry'];
export type RentalChecklistInput = S['RentalChecklistInput'];
export type RentalIdentityInput = S['RentalIdentityInput'];
export type RentalSignInput = S['RentalSignInput'];
export type RentalSignature = S['RentalSignature'];
export type RentalDeposit = S['RentalDeposit'];
export type RentalDepositInput = S['RentalDepositInput'];
export type RentalDamage = S['RentalDamage'];
export type RentalDamageInput = S['RentalDamageInput'];
export type RentalDamagePatch = S['RentalDamagePatch'];
export type RentalCheckInInput = S['RentalCheckInInput'];
export type RentalCheckInPreview = S['RentalCheckInPreview'];
export type RentalQuickCashInput = S['RentalQuickCashInput'];
export type RentalQuickCashResult = S['RentalQuickCashResult'];
export type RentalFileKind = S['RentalFileKind'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const marinaPath = (marinaId: string) => `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}`;
const bookingPath = (marinaId: string, bookingId: string) =>
  `${marinaPath(marinaId)}/rental/bookings/${encodeURIComponent(bookingId)}`;

function signedInUser(qc: QueryClient): string | undefined {
  return qc.getQueryData<S['Me'] | null>(queryKeys.me)?.user.id;
}

function invalidate(qc: QueryClient, marinaId: string) {
  void qc.invalidateQueries({ queryKey: queryKeys.rental(marinaId) });
  void qc.invalidateQueries({ queryKey: queryKeys.billing(marinaId) });
}

export function useRentalHandover(marinaId: string, bookingId: string, phase: RentalPhase) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalHandover(marinaId, bookingId, phase),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/bookings/{rentalBookingId}/handovers/{phase}`, {
          params: { path: { marinaId, rentalBookingId: bookingId, phase } },
        }),
      ),
    enabled: !!bookingId,
  });
}

export function useRentalDeposit(marinaId: string, bookingId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalDeposit(marinaId, bookingId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/rental/bookings/{rentalBookingId}/deposit`, {
          params: { path: { marinaId, rentalBookingId: bookingId } },
        }),
      ),
    enabled: !!bookingId,
  });
}

/**
 * Saves checklist, fuel, engine hours and notes through the offline queue (kind rental.checklist): stored at once,
 * also offline, replayed with one effect (client_ref). Without a signed-in user (tests) it is sent directly.
 */
export function useSaveHandoverChecklist(marinaId: string) {
  const qc = useQueryClient();
  const api = useApi();
  return useMutation({
    networkMode: 'always',
    mutationFn: async (v: {
      bookingId: string;
      phase: RentalPhase;
      label: string;
      body: Omit<RentalChecklistInput, 'client_ref'>;
    }) => {
      const body: RentalChecklistInput = { ...v.body, client_ref: newIdempotencyKey() };
      const userId = signedInUser(qc);
      if (!userId) {
        return unwrap(
          api.PUT(`${M}/rental/bookings/{rentalBookingId}/handovers/{phase}/checklist`, {
            params: { path: { marinaId, rentalBookingId: v.bookingId, phase: v.phase } },
            body,
          }),
        );
      }
      // The checklist is a PUT: the queue replays it with request.method.
      const request: NewOp['request'] = {
        operationId: 'saveHandoverChecklist',
        path: `${bookingPath(marinaId, v.bookingId)}/handovers/${v.phase}/checklist`,
        body,
        method: 'PUT',
      };
      return enqueueAndWait<RentalHandover>({
        kind: 'rental.checklist',
        userId,
        marinaId,
        lane: `rental:${v.bookingId}`,
        request,
        label: { key: 'rentalHandover:queue.checklist', params: { booking: v.label } },
      });
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Uploads a handover photo: boat photos through the offline queue, identity, licence and damage photos online. */
export function useUploadHandoverPhoto(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    networkMode: 'always',
    mutationFn: async (v: {
      bookingId: string;
      phase: RentalPhase;
      kind: RentalFileKind;
      file: Blob;
      damageId?: string;
      label: string;
    }) => {
      const ref = newIdempotencyKey();
      const q = new URLSearchParams({ kind: v.kind, client_ref: ref });
      if (v.damageId) q.set('damage_id', v.damageId);
      const path = `${bookingPath(marinaId, v.bookingId)}/handovers/${v.phase}/photos?${q.toString()}`;
      const contentType = v.file.type || 'image/jpeg';
      const userId = signedInUser(qc);
      if (v.kind === 'boat' && userId) {
        return enqueueAndWait<RentalHandoverFile>({
          kind: 'rental.checklist',
          userId,
          marinaId,
          lane: `rental:${v.bookingId}`,
          request: { operationId: 'uploadHandoverPhoto', path, contentType },
          blob: v.file,
          label: { key: 'rentalHandover:queue.photo', params: { booking: v.label } },
        });
      }
      assertOnline('uploadHandoverPhoto');
      return (
        await uploadBinary<RentalHandoverFile>(path, v.file, {
          method: 'POST',
          contentType,
          headers: { 'Idempotency-Key': ref },
        })
      ).data;
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function usePutHandoverIdentity(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; body: RentalIdentityInput }) => {
      assertOnline('putHandoverIdentity');
      return unwrap(
        api.PUT(`${M}/rental/bookings/{rentalBookingId}/identity`, {
          params: { path: { marinaId, rentalBookingId: v.bookingId } },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useSignRentalContract(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; body: RentalSignInput }) => {
      assertOnline('signRentalContract');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/contract/sign`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useStartRentalDeposit(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; body: RentalDepositInput }) => {
      assertOnline('startRentalDeposit', 'document');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/deposit`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useCheckOutRental(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; version: number; actualAt?: string; note?: string }) => {
      assertOnline('checkOutRental', 'allocation');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/check-out`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'If-Match': ifMatch(v.version), 'Idempotency-Key': newIdempotencyKey() },
          },
          body: {
            ...(v.actualAt ? { actual_at: v.actualAt } : {}),
            ...(v.note ? { note: v.note } : {}),
          },
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useCreateRentalDamage(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; body: RentalDamageInput }) => {
      assertOnline('createRentalDamage');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/damages`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateRentalDamage(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { damageId: string; version: number; body: RentalDamagePatch }) => {
      assertOnline('updateRentalDamage');
      return unwrap(
        api.PATCH(`${M}/rental/damages/{rentalDamageId}`, {
          params: {
            path: { marinaId, rentalDamageId: v.damageId },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useDeleteRentalDamage(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { damageId: string; version: number }) => {
      assertOnline('deleteRentalDamage');
      await unwrap(
        api.DELETE(`${M}/rental/damages/{rentalDamageId}`, {
          params: {
            path: { marinaId, rentalDamageId: v.damageId },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Stateless return totals (a mutation: it is recomputed on every change of the inputs, never cached). */
export function usePreviewRentalCheckIn(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { bookingId: string; body: RentalCheckInInput }) =>
      unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/check-in/preview`, {
          params: { path: { marinaId, rentalBookingId: v.bookingId } },
          body: v.body,
        }),
      ),
  });
}

export function useCheckInRental(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; version: number; body: RentalCheckInInput }) => {
      assertOnline('checkInRental', 'allocation');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/check-in`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'If-Match': ifMatch(v.version), 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useCloseRental(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; version: number }) => {
      assertOnline('closeRental');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/close`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Quick cash (online only: receipt-mode charges issue a commercial document, 29.2). */
export function useQuickCashRental(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { bookingId: string; body: RentalQuickCashInput }) => {
      assertOnline('quickCashRental', 'document');
      return unwrap(
        api.POST(`${M}/rental/bookings/{rentalBookingId}/quick-cash`, {
          params: {
            path: { marinaId, rentalBookingId: v.bookingId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      );
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Same-origin URL of a rental PDF: the server answers with a redirect to a 5-minute link signed for the caller. */
export function rentalPdfUrl(
  marinaId: string,
  bookingId: string,
  which: 'contract' | RentalPhase,
): string {
  const base = bookingPath(marinaId, bookingId);
  return which === 'contract' ? `${base}/contract/pdf` : `${base}/handovers/${which}/pdf`;
}
