// Hooks of utility meters, readings, consumptions, charging and settings (m9-plan §12.2, owner UTILITIES). Keys
// live under queryKeys.utilities(marinaId), invalidated by utilities.changed. Readings and their photo go through
// the offline queue (kind utility.reading, m9-plan §10): the photo is a dependent upload bound to the reading id.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { enqueue } from '../../offline/index';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type MeterKind = S['MeterKind'];
export type UtilityUnit = S['UtilityUnit'];
export type UtilityMeter = S['UtilityMeter'];
export type UtilityMeterInput = S['UtilityMeterInput'];
export type UtilityMeterPatch = S['UtilityMeterPatch'];
export type MeterReading = S['MeterReading'];
export type MeterReadingCreate = S['MeterReadingCreate'];
export type MeterReadingStatus = S['MeterReadingStatus'];
export type MeterAnomalyCode = S['MeterAnomalyCode'];
export type UtilityConsumption = S['UtilityConsumption'];
export type UtilityConsumptionStatus = S['UtilityConsumptionStatus'];
export type UtilityChargeRunResult = S['UtilityChargeRunResult'];
export type UtilitySettings = S['UtilitySettings'];
export type UtilitySettingsInput = S['UtilitySettingsInput'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const marinaPath = (marinaId: string) => `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}`;

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
  return qc.invalidateQueries({ queryKey: queryKeys.utilities(marinaId) });
}

// ---- meters ----

export type UtilityMeterFilters = {
  kind?: MeterKind;
  pedestal_id?: string;
  resource_id?: string;
  active?: boolean;
  /** "dock": the active power and water meters of the dock round (kept offline). */
  route?: 'dock';
  q?: string;
};

export function useUtilityMeters(marinaId: string, filters: UtilityMeterFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.utilityMeters(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/utility-meters`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useCreateUtilityMeter(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: UtilityMeterInput) =>
      unwrap(
        api.POST(`${M}/utility-meters`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateUtilityMeter(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: UtilityMeterPatch }) =>
      unwrap(
        api.PATCH(`${M}/utility-meters/{meterId}`, {
          params: { path: { marinaId, meterId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- readings ----

export type MeterReadingFilters = {
  meter_id?: string;
  status?: MeterReadingStatus;
  from?: string;
  to?: string;
};

export function useMeterReadings(marinaId: string, filters: MeterReadingFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.meterReadings(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/meter-readings`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

/**
 * Records a reading through the offline queue (kind utility.reading), with an optional photo queued as a
 * dependent upload. Online it resolves with the reading; offline it resolves with null once queued (the queue
 * sends it later; the client_ref makes a replay return the stored reading, INV-83).
 */
export function useRecordMeterReading(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    // Queue first (spec 29.2): the op is stored at once, also offline; a paused mutation would lose it.
    networkMode: 'always',
    mutationFn: async (v: {
      meterId: string;
      meterCode: string;
      value: string;
      note?: string;
      photo?: Blob;
    }): Promise<MeterReading | null> => {
      const userId = signedInUser(qc) ?? '';
      const lane = `meter:${v.meterId}`;
      const body: MeterReadingCreate = {
        meter_id: v.meterId,
        value: v.value,
        client_ref: newIdempotencyKey(),
        ...(v.note ? { note: v.note } : {}),
      };
      const reading = await enqueue({
        kind: 'utility.reading',
        userId,
        marinaId,
        lane,
        request: {
          operationId: 'recordMeterReading',
          path: `${marinaPath(marinaId)}/meter-readings`,
          body,
        },
        label: { key: 'utilities:queue.reading', params: { code: v.meterCode } },
      });
      if (v.photo) {
        await enqueue({
          kind: 'utility.reading',
          userId,
          marinaId,
          lane,
          dependsOn: { opId: reading.id, bind: { readingId: 'id' } },
          request: {
            operationId: 'uploadMeterReadingPhoto',
            path: `${marinaPath(marinaId)}/meter-readings/{readingId}/photo`,
            contentType: v.photo.type || 'image/jpeg',
          },
          blob: v.photo,
          label: { key: 'utilities:queue.photo', params: { code: v.meterCode } },
        });
      }
      return null;
    },
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useConfirmMeterReading(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/meter-readings/{readingId}/confirm`, {
          params: {
            path: { marinaId, readingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useVoidMeterReading(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason: string }) =>
      unwrap(
        api.POST(`${M}/meter-readings/{readingId}/void`, {
          params: {
            path: { marinaId, readingId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { reason: v.reason },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Fetches a 5-minute link to a reading photo. */
export function useMeterReadingPhotoLink(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (readingId: string) =>
      unwrap(
        api.GET(`${M}/meter-readings/{readingId}/photo`, {
          params: { path: { marinaId, readingId } },
        }),
      ),
  });
}

// ---- consumptions and charging ----

export type UtilityConsumptionFilters = {
  status?: UtilityConsumptionStatus;
  contract_id?: string;
  meter_id?: string;
  kind?: MeterKind;
  from?: string;
  to?: string;
};

export function useUtilityConsumptions(marinaId: string, filters: UtilityConsumptionFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.utilityConsumptions(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/utility-consumptions`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useExcludeUtilityConsumption(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason: string }) =>
      unwrap(
        api.POST(`${M}/utility-consumptions/{consumptionId}/exclude`, {
          params: {
            path: { marinaId, consumptionId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { reason: v.reason },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useAssignUtilityConsumption(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; contractId: string }) =>
      unwrap(
        api.POST(`${M}/utility-consumptions/{consumptionId}/assign`, {
          params: {
            path: { marinaId, consumptionId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { contract_id: v.contractId },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Charges the pending consumptions now (every contract due today, or one contract). */
export function useRunUtilityCharges(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (contractId?: string) =>
      unwrap(
        api.POST(`${M}/utility-charges/run`, {
          params: {
            path: { marinaId },
            query: contractId ? { contract_id: contractId } : {},
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
        }),
      ),
    onSuccess: () => {
      void invalidate(qc, marinaId);
      void qc.invalidateQueries({ queryKey: ['marina', marinaId, 'billing'] });
    },
  });
}

// ---- settings ----

export function useUtilitySettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.utilitySettings(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/utility-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateUtilitySettings(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { version: number; body: UtilitySettingsInput }) =>
      unwrap(
        api.PUT(`${M}/utility-settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}
