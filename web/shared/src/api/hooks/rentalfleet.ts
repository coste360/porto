// Hooks of the M11 rental fleet (m11-plan §14.1, owner FLEET): units, skippers, formulas, extras, settings and the
// stateless quote. Keys live under queryKeys.rental(marinaId), invalidated by rental.changed; every write here is
// online (configuration and fleet registry), so the mutations invalidate the whole rental subtree.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type RentalCategory = S['RentalCategory'];
export type RentalUnit = S['RentalUnit'];
export type RentalUnitInput = S['RentalUnitInput'];
export type RentalUnitPatch = S['RentalUnitPatch'];
export type RentalUnitStatus = S['RentalUnitStatus'];
export type RentalLicenceMode = S['RentalLicenceMode'];
export type RentalFuelType = S['RentalFuelType'];
export type RentalOwnership = S['RentalOwnership'];
export type RentalSkipperPatch = S['RentalSkipperPatch'];
export type RentalSkipperHoursRow = S['RentalSkipperHoursRow'];
export type RentalFormulaPatch = S['RentalFormulaPatch'];
export type RentalFormulaKind = S['RentalFormulaKind'];
export type RentalSkipperMode = S['RentalSkipperMode'];
export type RentalFuelMode = S['RentalFuelMode'];
export type RentalExtraPatch = S['RentalExtraPatch'];
export type RentalExtraChargeMode = S['RentalExtraChargeMode'];
export type RentalBand = S['RentalBand'];
export type RentalChecklistTemplateItem = S['RentalChecklistTemplateItem'];
export type RentalQuoteOption = S['RentalQuoteOption'];
export type RentalSkipper = S['RentalSkipper'];
export type RentalSkipperInput = S['RentalSkipperInput'];
export type RentalFormula = S['RentalFormula'];
export type RentalFormulaInput = S['RentalFormulaInput'];
export type RentalExtra = S['RentalExtra'];
export type RentalExtraInput = S['RentalExtraInput'];
export type RentalSettings = S['RentalSettings'];
export type RentalSettingsInput = S['RentalSettingsInput'];
export type RentalQuoteInput = S['RentalQuoteInput'];
export type RentalQuoteResult = S['RentalQuoteResult'];
export type RentalPriceLine = S['RentalPriceLine'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

export function useRentalUnits(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalUnits(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/rental/units`, { params: { path: { marinaId } } })),
  });
}

export function useRentalSkippers(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalSkippers(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/rental/skippers`, { params: { path: { marinaId } } })),
  });
}

export function useRentalFormulas(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalFormulas(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/rental/formulas`, { params: { path: { marinaId } } })),
  });
}

export function useRentalExtras(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalExtras(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/rental/extras`, { params: { path: { marinaId } } })),
  });
}

export function useRentalSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalSettings(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/rental/settings`, { params: { path: { marinaId } } })),
  });
}

function invalidate(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.rental(marinaId) });
}

// ---- units (rental.manage) ----

export function useCreateRentalUnit(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RentalUnitInput) =>
      unwrap(
        api.POST(`${M}/rental/units`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateRentalUnit(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: RentalUnitPatch }) =>
      unwrap(
        api.PATCH(`${M}/rental/units/{rentalUnitId}`, {
          params: {
            path: { marinaId, rentalUnitId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- skippers (rental.manage) ----

export function useCreateRentalSkipper(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RentalSkipperInput) =>
      unwrap(
        api.POST(`${M}/rental/skippers`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateRentalSkipper(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: RentalSkipperPatch }) =>
      unwrap(
        api.PATCH(`${M}/rental/skippers/{rentalSkipperId}`, {
          params: {
            path: { marinaId, rentalSkipperId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function usePutRentalSkipperHours(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; hours: RentalSkipperHoursRow[] }) =>
      unwrap(
        api.PUT(`${M}/rental/skippers/{rentalSkipperId}/hours`, {
          params: {
            path: { marinaId, rentalSkipperId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { hours: v.hours },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- formulas, extras, settings (marina.configure) ----

export function useCreateRentalFormula(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RentalFormulaInput) =>
      unwrap(
        api.POST(`${M}/rental/formulas`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateRentalFormula(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: RentalFormulaPatch }) =>
      unwrap(
        api.PATCH(`${M}/rental/formulas/{rentalFormulaId}`, {
          params: {
            path: { marinaId, rentalFormulaId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useCreateRentalExtra(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: RentalExtraInput) =>
      unwrap(
        api.POST(`${M}/rental/extras`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateRentalExtra(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: RentalExtraPatch }) =>
      unwrap(
        api.PATCH(`${M}/rental/extras/{rentalExtraId}`, {
          params: {
            path: { marinaId, rentalExtraId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** Saves the settings; version 0 is the first save (defaults never saved). */
export function usePutRentalSettings(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { version: number; body: RentalSettingsInput }) =>
      unwrap(
        api.PUT(`${M}/rental/settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- quote (stateless, rental.manage) ----

/** Prices a unit and range: one formula, or every formula sorted by total (formula_id omitted). */
export function useRentalQuote(marinaId: string, input: RentalQuoteInput | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rentalQuote(marinaId, input),
    enabled: input !== null,
    queryFn: () =>
      unwrap(
        api.POST(`${M}/rental/quote`, {
          params: { path: { marinaId } },
          body: input as RentalQuoteInput,
        }),
      ),
  });
}

/** The quote as a mutation (dialogs that price on demand). */
export function useQuoteRental(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: RentalQuoteInput) =>
      unwrap(api.POST(`${M}/rental/quote`, { params: { path: { marinaId } }, body })),
  });
}
