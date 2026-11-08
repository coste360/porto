// Hooks of the public booking page (m8-plan §12.1, booking SPA). Keys live under queryKeys.publicBooking(slug),
// never under a marina. Tokens (Porto-Booking-Token, Porto-Status-Token, Porto-Customer-Session) travel only in
// headers and live in sessionStorage (memory fallback, owned by the booking app), never in the URL path or the
// query cache keys.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { unwrap, uploadBinary } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type BookingConfig = S['BookingConfig'];
export type BookingCategory = S['BookingCategory'];
export type BookingServiceOption = S['BookingServiceOption'];
export type BookingAvailability = S['BookingAvailability'];
export type BookingCategoryAvailability = S['BookingCategoryAvailability'];
export type BookingVesselInput = S['BookingVesselInput'];
export type BookingQuoteInput = S['BookingQuoteInput'];
export type BookingQuote = S['BookingQuote'];
export type BookingCreate = S['BookingCreate'];
export type BookingCreated = S['BookingCreated'];
export type Booking = S['Booking'];
export type BookingDetailsInput = S['BookingDetailsInput'];
export type BookingDocument = S['BookingDocument'];
export type BookingDocumentType = S['BookingDocumentType'];
export type BookingPayInput = S['BookingPayInput'];
export type BookingPayResult = S['BookingPayResult'];
export type BookingStatusView = S['BookingStatusView'];
export type OnlineBookingStatus = S['OnlineBookingStatus'];
export type CancellationTier = S['CancellationTier'];
export type BookingBlockReason = S['BookingBlockReason'];
export type BookingPriceLine = S['BookingPriceLine'];
export type BookingServiceInput = S['BookingServiceInput'];
export type HullType = S['HullType'];

/** Header names of the public booking credentials (m8-plan §0.7). */
export const BOOKING_TOKEN_HEADER = 'Porto-Booking-Token';
export const STATUS_TOKEN_HEADER = 'Porto-Status-Token';
export const CUSTOMER_SESSION_HEADER = 'Porto-Customer-Session';

/** A 4xx (unknown slug, booking disabled, expired token) is final: no retries. */
export function retryBookingErrors(count: number, error: unknown) {
  return !(error instanceof ProblemError && error.status < 500) && count < 2;
}

/** Public configuration of the booking page (cacheable 60 s server side). */
export function useBookingConfig(slug: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.bookingConfig(slug),
    retry: retryBookingErrors,
    staleTime: 60_000,
    queryFn: () =>
      unwrap(api.GET('/api/public/v1/{slug}/booking/config', { params: { path: { slug } } })),
  });
}

/** Query of the availability search (step "Quando e barca"). */
export type BookingSearchParams = {
  arrival: string;
  departure: string;
  loa_cm?: number;
  beam_cm?: number;
  draft_cm?: number;
  hull_type?: HullType;
  vessel_id?: string;
};

/** Availability per category; disabled while params is null. The session header only goes with vessel_id. */
export function useBookingAvailability(
  slug: string,
  params: BookingSearchParams | null,
  customerSession?: string,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.bookingAvailability(slug, { ...(params ?? {}) }),
    enabled: params !== null,
    retry: retryBookingErrors,
    staleTime: 30_000,
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/booking/availability', {
          params: {
            path: { slug },
            query: params as BookingSearchParams,
            ...(params?.vessel_id && customerSession
              ? { header: { [CUSTOMER_SESSION_HEADER]: customerSession } }
              : {}),
          },
        }),
      ),
  });
}

/** Binding-free price of a stay (writes nothing); the previous quote stays visible while a new one loads. */
export function useBookingQuote(slug: string, input: BookingQuoteInput | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.bookingQuote(slug, { ...(input ?? {}) }),
    enabled: input !== null,
    retry: retryBookingErrors,
    staleTime: 30_000,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.POST('/api/public/v1/{slug}/booking/quote', {
          params: { path: { slug } },
          body: input as BookingQuoteInput,
        }),
      ),
  });
}

/** Holds a berth. The Idempotency-Key is created once per user action and reused on retries. */
export function useCreateBooking(slug: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { body: BookingCreate; idempotencyKey: string; customerSession?: string }) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/bookings', {
          params: {
            path: { slug },
            header: {
              'Idempotency-Key': v.idempotencyKey,
              ...(v.customerSession ? { [CUSTOMER_SESSION_HEADER]: v.customerSession } : {}),
            },
          },
          body: v.body,
        }),
      ),
  });
}

/** Reference of a booking held by this browser: its id and the token returned by createBooking. */
export type BookingRef = { slug: string; bookingId: string; token: string };

const tokenHeader = (token: string) => ({ [BOOKING_TOKEN_HEADER]: token });

export function useBooking(ref: BookingRef | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.booking(ref?.slug ?? '', ref?.bookingId ?? ''),
    enabled: ref !== null,
    retry: retryBookingErrors,
    gcTime: 0,
    queryFn: () => {
      const r = ref as BookingRef;
      return unwrap(
        api.GET('/api/public/v1/{slug}/bookings/{bookingId}', {
          params: { path: { slug: r.slug, bookingId: r.bookingId }, header: tokenHeader(r.token) },
        }),
      );
    },
  });
}

/** Abandons a held booking (releases the hold). */
export function useAbandonBooking() {
  const api = useApi();
  return useMutation({
    mutationFn: (r: BookingRef) =>
      unwrap(
        api.DELETE('/api/public/v1/{slug}/bookings/{bookingId}', {
          params: { path: { slug: r.slug, bookingId: r.bookingId }, header: tokenHeader(r.token) },
        }),
      ),
  });
}

export function useSubmitBookingDetails(ref: BookingRef) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: BookingDetailsInput) =>
      unwrap(
        api.PUT('/api/public/v1/{slug}/bookings/{bookingId}/details', {
          params: {
            path: { slug: ref.slug, bookingId: ref.bookingId },
            header: tokenHeader(ref.token),
          },
          body,
        }),
      ),
    onSuccess: (b) => qc.setQueryData(queryKeys.booking(ref.slug, ref.bookingId), b),
  });
}

/** Content types accepted by the document upload (raw body, like the M4 customer links). */
export const BOOKING_DOCUMENT_TYPES = ['application/pdf', 'image/png', 'image/jpeg'] as const;
export const BOOKING_DOCUMENT_MAX_BYTES = 10 * 1024 * 1024;

export function useUploadBookingDocument(ref: BookingRef, fetchImpl?: typeof fetch) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { file: File; type: BookingDocumentType }) => {
      const path =
        `/api/public/v1/${encodeURIComponent(ref.slug)}/bookings/` +
        `${encodeURIComponent(ref.bookingId)}/documents?type=${encodeURIComponent(v.type)}`;
      const { data } = await uploadBinary<BookingDocument>(path, v.file, {
        method: 'POST',
        contentType: v.file.type,
        headers: {
          ...tokenHeader(ref.token),
          'X-File-Name': v.file.name.replace(/[^\w.\- ]/g, '_').slice(0, 255),
        },
        readCsrf: () => undefined,
        ...(fetchImpl ? { fetch: fetchImpl } : {}),
      });
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.booking(ref.slug, ref.bookingId) }),
  });
}

export function useStartBookingPayment(ref: BookingRef) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { body: BookingPayInput; idempotencyKey: string }) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/bookings/{bookingId}/pay', {
          params: {
            path: { slug: ref.slug, bookingId: ref.bookingId },
            header: { ...tokenHeader(ref.token), 'Idempotency-Key': v.idempotencyKey },
          },
          body: v.body,
        }),
      ),
  });
}

/** Statuses after which the return page stops polling. */
export function isBookingStatusFinal(s: BookingStatusView): boolean {
  if (s.verifying) return false;
  return s.status !== 'pending_payment';
}

/**
 * Status of a booking for the return page, with the booking token (same browser) or the read-only status token
 * (from the URL fragment). Polls every pollMs while the payment is being verified.
 */
export function useBookingStatus(
  slug: string,
  bookingId: string,
  creds: { bookingToken?: string | undefined; statusToken?: string | undefined },
  pollMs = 2500,
) {
  const api = useApi();
  const enabled = Boolean(creds.bookingToken || creds.statusToken);
  return useQuery({
    queryKey: queryKeys.bookingStatus(slug, bookingId),
    enabled,
    retry: retryBookingErrors,
    gcTime: 0,
    refetchInterval: (q) => {
      const d = q.state.data;
      if (q.state.error) return false;
      return d && isBookingStatusFinal(d) ? false : pollMs;
    },
    queryFn: () =>
      unwrap(
        api.GET('/api/public/v1/{slug}/bookings/{bookingId}/status', {
          params: {
            path: { slug, bookingId },
            header: {
              ...(creds.bookingToken ? { [BOOKING_TOKEN_HEADER]: creds.bookingToken } : {}),
              ...(creds.statusToken ? { [STATUS_TOKEN_HEADER]: creds.statusToken } : {}),
            },
          },
        }),
      ),
  });
}
