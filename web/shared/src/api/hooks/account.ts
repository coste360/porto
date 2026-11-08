// Hooks of the customer area of the booking SPA (m8-plan §12.2). Keys live under queryKeys.account(slug); the
// session token travels in the Porto-Customer-Session header (CUSTOMER_SESSION_HEADER of ./onlineBooking).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { unwrap, uploadBinary } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import { CUSTOMER_SESSION_HEADER, retryBookingErrors } from './onlineBooking';
import type { BookingDocument, BookingDocumentType } from './onlineBooking';
import type { components } from '../schema';

type S = components['schemas'];

export type AccountLoginInput = S['AccountLoginInput'];
export type AccountSession = S['AccountSession'];
export type AccountVessel = S['AccountVessel'];
export type AccountBookingSummary = S['AccountBookingSummary'];
export type AccountBooking = S['AccountBooking'];
export type AccountReceipt = S['AccountReceipt'];
export type CancellationPreview = S['CancellationPreview'];
export type AccountCancelInput = S['AccountCancelInput'];
export type AccountCancelResult = S['AccountCancelResult'];
export type DateChangeInput = S['DateChangeInput'];
export type DateChangePreview = S['DateChangePreview'];
export type DateChangeResult = S['DateChangeResult'];
export type AccountPayBalanceResult = S['AccountPayBalanceResult'];

const sessionHeader = (session: string) => ({ [CUSTOMER_SESSION_HEADER]: session });

/** Asks for a login link. The answer is the same whether the email is known or not. */
export function useRequestAccountLink(slug: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: AccountLoginInput) =>
      unwrap(api.POST('/api/public/v1/{slug}/account/login', { params: { path: { slug } }, body })),
  });
}

/** Exchanges the link token of the URL fragment for a session token. */
export function useOpenAccountSession(slug: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (token: string) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/account/session', {
          params: { path: { slug } },
          body: { token },
        }),
      ),
  });
}

/** Revokes the session (logout); the cached customer data are dropped. */
export function useCloseAccountSession(slug: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (session: string) =>
      unwrap(
        api.DELETE('/api/public/v1/{slug}/account/session', {
          params: { path: { slug }, header: sessionHeader(session) },
        }),
      ),
    onSettled: () => qc.removeQueries({ queryKey: queryKeys.account(slug) }),
  });
}

export function useAccountVessels(slug: string, session: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.accountVessels(slug),
    enabled: Boolean(session),
    retry: retryBookingErrors,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/account/vessels', {
          params: { path: { slug }, header: sessionHeader(session ?? '') },
        }),
      ),
  });
}

export function useAccountBookings(slug: string, session: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.accountBookings(slug),
    enabled: Boolean(session),
    retry: retryBookingErrors,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/account/bookings', {
          params: { path: { slug }, header: sessionHeader(session ?? '') },
        }),
      ),
  });
}

export function useAccountBooking(slug: string, session: string | null, contractId: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.accountBooking(slug, contractId ?? ''),
    enabled: Boolean(session && contractId),
    retry: retryBookingErrors,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/account/bookings/{contractId}', {
          params: {
            path: { slug, contractId: contractId ?? '' },
            header: sessionHeader(session ?? ''),
          },
        }),
      ),
  });
}

/** Cancellation preview from the contract's policy snapshot (fetched when the dialog opens). */
export function useAccountCancellation(
  slug: string,
  session: string | null,
  contractId: string,
  enabled: boolean,
) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.accountBooking(slug, contractId), 'cancellation'] as const,
    enabled: Boolean(session) && enabled,
    retry: retryBookingErrors,
    gcTime: 0,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/account/bookings/{contractId}/cancellation', {
          params: { path: { slug, contractId }, header: sessionHeader(session ?? '') },
        }),
      ),
  });
}

function useInvalidateAccount(slug: string) {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: queryKeys.accountBookings(slug) });
}

/** Cancels with the refund the customer saw (409 refund_changed when it moved). */
export function useCancelAccountBooking(slug: string, session: string, contractId: string) {
  const api = useApi();
  const invalidate = useInvalidateAccount(slug);
  return useMutation({
    mutationFn: (v: { body: AccountCancelInput; idempotencyKey: string }) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/account/bookings/{contractId}/cancel', {
          params: {
            path: { slug, contractId },
            header: { ...sessionHeader(session), 'Idempotency-Key': v.idempotencyKey },
          },
          body: v.body,
        }),
      ),
    onSuccess: invalidate,
  });
}

/** Price effect of new dates (writes nothing). */
export function usePreviewAccountDateChange(slug: string, session: string, contractId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: DateChangeInput) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/account/bookings/{contractId}/change-dates/preview', {
          params: { path: { slug, contractId }, header: sessionHeader(session) },
          body,
        }),
      ),
  });
}

/** Changes the dates with the total the customer saw (409 price_changed when it moved). */
export function useChangeAccountDates(slug: string, session: string, contractId: string) {
  const api = useApi();
  const invalidate = useInvalidateAccount(slug);
  return useMutation({
    mutationFn: (v: { body: DateChangeInput; idempotencyKey: string }) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/account/bookings/{contractId}/change-dates', {
          params: {
            path: { slug, contractId },
            header: { ...sessionHeader(session), 'Idempotency-Key': v.idempotencyKey },
          },
          body: v.body,
        }),
      ),
    onSuccess: invalidate,
  });
}

/** Checkout of the open residual; the caller navigates the top window to checkout_url. */
export function usePayAccountBalance(slug: string, session: string, contractId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (idempotencyKey: string) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/account/bookings/{contractId}/pay-balance', {
          params: {
            path: { slug, contractId },
            header: { ...sessionHeader(session), 'Idempotency-Key': idempotencyKey },
          },
        }),
      ),
  });
}

/** Short-lived signed URL of one of the customer's documents. */
export function useAccountDocumentURL(slug: string, session: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (documentId: string) =>
      unwrap(
        api.GET('/api/public/v1/{slug}/account/documents/{documentId}/url', {
          params: { path: { slug, documentId }, header: sessionHeader(session) },
        }),
      ),
  });
}

/** Uploads a document for one of the customer's bookings (raw body, PDF, PNG or JPEG). */
export function useUploadAccountDocument(
  slug: string,
  session: string,
  contractId: string,
  fetchImpl?: typeof fetch,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { file: File; type: BookingDocumentType }) => {
      const path =
        `/api/public/v1/${encodeURIComponent(slug)}/account/bookings/` +
        `${encodeURIComponent(contractId)}/documents?type=${encodeURIComponent(v.type)}`;
      const { data } = await uploadBinary<BookingDocument>(path, v.file, {
        method: 'POST',
        contentType: v.file.type,
        headers: {
          ...sessionHeader(session),
          'X-File-Name': v.file.name.replace(/[^\w.\- ]/g, '_').slice(0, 255),
        },
        readCsrf: () => undefined,
        ...(fetchImpl ? { fetch: fetchImpl } : {}),
      });
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.accountBooking(slug, contractId) }),
  });
}
