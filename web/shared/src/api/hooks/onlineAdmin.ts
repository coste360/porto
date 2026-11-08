// Hooks of the desk and configuration pages of online booking (m8-plan §12.3, admin app). Online bookings and
// channel reservations live under the desk root (invalidated by contract.changed); settings, texts, quotas and
// channels under queryKeys.onlineConfig(marinaId).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type OnlineBookingSettings = S['OnlineBookingSettings'];
export type OnlineBookingSettingsInput = S['OnlineBookingSettingsInput'];
export type OnlineApprovalRules = S['OnlineApprovalRules'];
export type BookingRules = S['BookingRules'];
export type BookingRequiredFields = S['BookingRequiredFields'];
export type ArrivalInfo = S['ArrivalInfo'];
export type BookingText = S['BookingText'];
export type BookingTextKind = S['BookingTextKind'];
export type BookingTextPublish = S['BookingTextPublish'];
export type OnlineQuota = S['OnlineQuota'];
export type OnlineQuotaKind = S['OnlineQuotaKind'];
export type OnlineQuotasUpdate = S['OnlineQuotasUpdate'];
export type OnlineQuotaUsage = S['OnlineQuotaUsage'];
export type OnlineBookingSummary = S['OnlineBookingSummary'];
export type OnlineBookingPage = S['OnlineBookingPage'];
export type OnlineBookingDetail = S['OnlineBookingDetail'];
export type OnlineBookingReject = S['OnlineBookingReject'];
export type OnlinePaymentState = S['OnlinePaymentState'];
export type DocumentVerification = S['DocumentVerification'];
export type BookingChannel = S['BookingChannel'];
export type BookingChannelUpdate = S['BookingChannelUpdate'];
export type ChannelReservation = S['ChannelReservation'];
export type ChannelReservationPage = S['ChannelReservationPage'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

export type OnlineBookingFilters = {
  status?: S['OnlineBookingStatus'] | undefined;
  channel?: S['OnlineChannel'] | undefined;
  from?: string | undefined;
  to?: string | undefined;
};

function compact(f: OnlineBookingFilters): OnlineBookingFilters {
  const out: OnlineBookingFilters = {};
  if (f.status) out.status = f.status;
  if (f.channel) out.channel = f.channel;
  if (f.from) out.from = f.from;
  if (f.to) out.to = f.to;
  return out;
}

// ---- desk: online bookings ----

export function useOnlineBookings(marinaId: string, filters: OnlineBookingFilters = {}) {
  const api = useApi();
  const query = compact(filters);
  return useQuery({
    queryKey: queryKeys.onlineBookings(marinaId, { ...query }),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/online-bookings`, {
          params: { path: { marinaId }, query: { ...query, limit: 200 } },
        }),
      ),
  });
}

export function useOnlineBooking(marinaId: string, contractId: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.onlineBooking(marinaId, contractId ?? ''),
    enabled: Boolean(contractId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/online-bookings/{contractId}`, {
          params: { path: { marinaId, contractId: contractId ?? '' } },
        }),
      ),
  });
}

function useDeskMutation<V, R>(
  marinaId: string,
  fn: (api: ReturnType<typeof useApi>, v: V) => Promise<R>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: V) => fn(api, v),
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: queryKeys.onlineBookings(marinaId) }),
        qc.invalidateQueries({ queryKey: queryKeys.onlineBooking(marinaId) }),
        qc.invalidateQueries({ queryKey: queryKeys.desk(marinaId) }),
      ]),
  });
}

export function useApproveOnlineBooking(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: { contractId: string; version: number }) =>
    unwrap(
      api.POST(`${M}/online-bookings/{contractId}/approve`, {
        params: {
          path: { marinaId, contractId: v.contractId },
          header: { 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useRejectOnlineBooking(marinaId: string) {
  return useDeskMutation(
    marinaId,
    (api, v: { contractId: string; version: number; body: OnlineBookingReject }) =>
      unwrap(
        api.POST(`${M}/online-bookings/{contractId}/reject`, {
          params: {
            path: { marinaId, contractId: v.contractId },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
  );
}

export function useSendOnlineBookingPaymentLink(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: { contractId: string; version: number }) =>
    unwrap(
      api.POST(`${M}/online-bookings/{contractId}/payment-link`, {
        params: {
          path: { marinaId, contractId: v.contractId },
          header: { 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useVerifyDocument(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: { documentId: string; verified: boolean }) =>
    unwrap(
      api.POST(`${M}/documents/{documentId}/verify`, {
        params: { path: { marinaId, documentId: v.documentId } },
        body: { verified: v.verified },
      }),
    ),
  );
}

// ---- configuration: settings, texts, quotas ----

export function useOnlineBookingSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.onlineSettings(marinaId),
    queryFn: () =>
      unwrap(api.GET(`${M}/online-booking/settings`, { params: { path: { marinaId } } })),
  });
}

function useConfigMutation<V, R>(
  marinaId: string,
  fn: (api: ReturnType<typeof useApi>, v: V) => Promise<R>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: V) => fn(api, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.onlineConfig(marinaId) }),
  });
}

/** Saves the settings; version 0 saves the defaults for the first time. */
export function useUpdateOnlineBookingSettings(marinaId: string) {
  return useConfigMutation(
    marinaId,
    (api, v: { version: number; body: OnlineBookingSettingsInput }) =>
      unwrap(
        api.PUT(`${M}/online-booking/settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
  );
}

export function useBookingTexts(marinaId: string, kind?: BookingTextKind) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.bookingTexts(marinaId, kind),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/online-booking/texts`, {
          params: { path: { marinaId }, query: kind ? { kind } : {} },
        }),
      ),
  });
}

export function usePublishBookingText(marinaId: string) {
  return useConfigMutation(marinaId, (api, body: BookingTextPublish) =>
    unwrap(
      api.POST(`${M}/online-booking/texts`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useOnlineQuotas(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.onlineQuotas(marinaId),
    queryFn: () =>
      unwrap(api.GET(`${M}/online-booking/quotas`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateOnlineQuotas(marinaId: string) {
  return useConfigMutation(marinaId, (api, body: OnlineQuotasUpdate) =>
    unwrap(api.PUT(`${M}/online-booking/quotas`, { params: { path: { marinaId } }, body })),
  );
}

export function useOnlineQuotaUsage(marinaId: string, from: string, to: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.onlineQuotaUsage(marinaId, from, to),
    enabled: Boolean(from && to),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/online-booking/quotas/usage`, {
          params: { path: { marinaId }, query: { from, to } },
        }),
      ),
  });
}

// ---- channels ----

export function useBookingChannels(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.bookingChannels(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/booking-channels`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateBookingChannel(marinaId: string) {
  return useConfigMutation(
    marinaId,
    (api, v: { code: string; version: number; body: BookingChannelUpdate }) =>
      unwrap(
        api.PUT(`${M}/booking-channels/{channelCode}`, {
          params: {
            path: { marinaId, channelCode: v.code },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
  );
}

export function useTestBookingChannel(marinaId: string) {
  return useConfigMutation(marinaId, (api, code: string) =>
    unwrap(
      api.POST(`${M}/booking-channels/{channelCode}/test`, {
        params: { path: { marinaId, channelCode: code } },
      }),
    ),
  );
}

export function useSyncBookingChannel(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (code: string) =>
      unwrap(
        api.POST(`${M}/booking-channels/{channelCode}/sync`, {
          params: { path: { marinaId, channelCode: code } },
        }),
      ),
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: queryKeys.onlineConfig(marinaId) }),
        qc.invalidateQueries({ queryKey: queryKeys.channelReservations(marinaId) }),
      ]),
  });
}

export function useChannelReservations(marinaId: string, channel?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.channelReservations(marinaId, channel ? { channel } : undefined),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/channel-reservations`, {
          params: { path: { marinaId }, query: { ...(channel ? { channel } : {}), limit: 50 } },
        }),
      ),
  });
}
