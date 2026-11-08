// Hooks of the M10 yard (m10-plan §14, owner QUOTE): catalogue (work types, qualifications, item yard settings,
// variables, tables, packages, settings, formula test) and yard quotes (preview, create, update, recompute, send,
// accept, decline). Keys live under queryKeys.yard(marinaId), invalidated by yard.changed.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

export type WorkType = S['WorkType'];
export type WorkTypeInput = S['WorkTypeInput'];
export type WorkTypePatch = S['WorkTypePatch'];
export type YardQualification = S['YardQualification'];
export type YardQualificationInput = S['YardQualificationInput'];
export type YardQualificationPatch = S['YardQualificationPatch'];
export type YardItem = S['YardItem'];
export type YardItemSettingsInput = S['YardItemSettingsInput'];
export type FormulaVariable = S['FormulaVariable'];
export type FormulaVariableInput = S['FormulaVariableInput'];
export type FormulaVariablePatch = S['FormulaVariablePatch'];
export type FormulaValue = S['FormulaValue'];
export type FormulaTable = S['FormulaTable'];
export type FormulaTableInput = S['FormulaTableInput'];
export type FormulaTablePatch = S['FormulaTablePatch'];
export type FormulaTableRow = S['FormulaTableRow'];
export type WorkPackage = S['WorkPackage'];
export type WorkPackageInput = S['WorkPackageInput'];
export type WorkPackageVersionInput = S['WorkPackageVersionInput'];
export type WorkPackageLine = S['WorkPackageLine'];
export type FormulaValidateInput = S['FormulaValidateInput'];
export type FormulaValidateResult = S['FormulaValidateResult'];
export type YardSettings = S['YardSettings'];
export type YardSettingsInput = S['YardSettingsInput'];
export type YardQuote = S['YardQuote'];
export type YardQuoteStatus = S['YardQuoteStatus'];
export type YardBillingMode = S['YardBillingMode'];
export type YardQuoteInput = S['YardQuoteInput'];
export type YardQuotePatch = S['YardQuotePatch'];
export type YardQuotePreview = S['YardQuotePreview'];
export type YardQuoteLine = S['YardQuoteLine'];
export type YardLineOverride = S['YardLineOverride'];
export type YardManualLine = S['YardManualLine'];
export type YardVariableInput = S['YardVariableInput'];
export type YardQuoteSendResult = S['YardQuoteSendResult'];
export type YardQuoteAcceptResult = S['YardQuoteAcceptResult'];
export type FormulaIssue = S['FormulaIssue'];
export type PublicYardQuote = S['PublicYardQuote'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

/** Filters are stored in the query key as strings only (stable keys, no undefined values). */
function compact(
  filters: Record<string, string | boolean | undefined | null>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) {
    if (v === undefined || v === null || v === '') continue;
    out[k] = String(v);
  }
  return out;
}

function invalidate(qc: QueryClient, marinaId: string) {
  return qc.invalidateQueries({ queryKey: queryKeys.yard(marinaId) });
}

/** The formula issues of a formula_invalid (or no_price) problem, else []. */
export function formulaIssuesOf(err: unknown): FormulaIssue[] {
  if (!(err instanceof ProblemError)) return [];
  const raw = err.details['issues'];
  if (!Array.isArray(raw)) return [];
  return raw.filter(
    (i): i is FormulaIssue =>
      typeof i === 'object' && i !== null && typeof (i as { code?: unknown }).code === 'string',
  );
}

// ---- work types ----

export function useWorkTypes(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardWorkTypes(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/work-types`, { params: { path: { marinaId } } })),
  });
}

export function useCreateWorkType(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WorkTypeInput) =>
      unwrap(
        api.POST(`${M}/yard/work-types`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateWorkType(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: WorkTypePatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/work-types/{workTypeId}`, {
          params: {
            path: { marinaId, workTypeId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- qualifications ----

export function useYardQualifications(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardQualifications(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/qualifications`, { params: { path: { marinaId } } })),
  });
}

export function useCreateYardQualification(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: YardQualificationInput) =>
      unwrap(
        api.POST(`${M}/yard/qualifications`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateYardQualification(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: YardQualificationPatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/qualifications/{qualificationId}`, {
          params: {
            path: { marinaId, qualificationId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- items ----

export type YardItemFilters = {
  q?: string;
  category?: string;
  item_type?: S['ItemType'];
  yard_enabled?: boolean;
};

export function useYardItems(marinaId: string, filters: YardItemFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardItems(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/items`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function usePutYardItemSettings(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { itemId: string; version: number; body: YardItemSettingsInput }) =>
      unwrap(
        api.PUT(`${M}/yard/items/{itemId}`, {
          params: {
            path: { marinaId, itemId: v.itemId },
            header: v.version > 0 ? { 'If-Match': ifMatch(v.version) } : {},
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- variables ----

export function useFormulaVariables(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.formulaVariables(marinaId),
    queryFn: () =>
      unwrap(api.GET(`${M}/yard/formula-variables`, { params: { path: { marinaId } } })),
  });
}

export function useCreateFormulaVariable(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: FormulaVariableInput) =>
      unwrap(
        api.POST(`${M}/yard/formula-variables`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateFormulaVariable(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: FormulaVariablePatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/formula-variables/{variableId}`, {
          params: {
            path: { marinaId, variableId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useDeleteFormulaVariable(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.DELETE(`${M}/yard/formula-variables/{variableId}`, {
          params: {
            path: { marinaId, variableId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- tables ----

export function useFormulaTables(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.formulaTables(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/formula-tables`, { params: { path: { marinaId } } })),
  });
}

export function useCreateFormulaTable(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: FormulaTableInput) =>
      unwrap(
        api.POST(`${M}/yard/formula-tables`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateFormulaTable(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: FormulaTablePatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/formula-tables/{tableId}`, {
          params: { path: { marinaId, tableId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- packages ----

export function useWorkPackages(
  marinaId: string,
  filters: { active?: boolean; work_type_id?: string } = {},
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.workPackages(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-packages`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useWorkPackage(marinaId: string, packageId: string | undefined, version?: number) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.workPackage(marinaId, packageId, version),
    enabled: !!packageId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/work-packages/{packageId}`, {
          params: {
            path: { marinaId, packageId: packageId ?? '' },
            query: version ? { version } : {},
          },
        }),
      ),
  });
}

export function useCreateWorkPackage(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: WorkPackageInput) =>
      unwrap(
        api.POST(`${M}/yard/work-packages`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useSaveWorkPackageVersion(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: WorkPackageVersionInput }) =>
      unwrap(
        api.POST(`${M}/yard/work-packages/{packageId}/versions`, {
          params: {
            path: { marinaId, packageId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useSetWorkPackageActive(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; active: boolean }) =>
      unwrap(
        api.PUT(`${M}/yard/work-packages/{packageId}/active`, {
          params: {
            path: { marinaId, packageId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { active: v.active },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- formula test and settings ----

export function useValidateFormula(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: FormulaValidateInput) =>
      unwrap(api.POST(`${M}/yard/formulas/validate`, { params: { path: { marinaId } }, body })),
  });
}

export function useYardSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardSettings(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/yard/settings`, { params: { path: { marinaId } } })),
  });
}

export function usePutYardSettings(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { version: number; body: YardSettingsInput }) =>
      unwrap(
        api.PUT(`${M}/yard/settings`, {
          params: {
            path: { marinaId },
            header: v.version > 0 ? { 'If-Match': ifMatch(v.version) } : {},
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

// ---- quotes ----

export type YardQuoteFilters = {
  status?: YardQuoteStatus;
  kind?: S['YardQuoteKind'];
  customer_id?: string;
  vessel_id?: string;
  work_order_id?: string;
};

export function useYardQuotes(marinaId: string, filters: YardQuoteFilters = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardQuotes(marinaId, compact(filters)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/quotes`, {
          params: { path: { marinaId }, query: { ...filters, limit: 200 } },
        }),
      ),
  });
}

export function useYardQuote(marinaId: string, quoteId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.yardQuote(marinaId, quoteId),
    enabled: !!quoteId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/yard/quotes/{yardQuoteId}`, {
          params: { path: { marinaId, yardQuoteId: quoteId ?? '' } },
        }),
      ),
  });
}

/** The stateless preview of every wizard step (nothing written). */
export function usePreviewYardQuote(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: YardQuoteInput) =>
      unwrap(api.POST(`${M}/yard/quotes/preview`, { params: { path: { marinaId } }, body })),
  });
}

export function useCreateYardQuote(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: YardQuoteInput) =>
      unwrap(
        api.POST(`${M}/yard/quotes`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useUpdateYardQuote(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; body: YardQuotePatch }) =>
      unwrap(
        api.PATCH(`${M}/yard/quotes/{yardQuoteId}`, {
          params: {
            path: { marinaId, yardQuoteId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useRecomputeYardQuote(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/yard/quotes/{yardQuoteId}/recompute`, {
          params: {
            path: { marinaId, yardQuoteId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useSendYardQuote(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; manual?: boolean }) =>
      unwrap(
        api.POST(`${M}/yard/quotes/{yardQuoteId}/send`, {
          params: {
            path: { marinaId, yardQuoteId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: { manual: v.manual ?? false },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useAcceptYardQuote(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number }) =>
      unwrap(
        api.POST(`${M}/yard/quotes/{yardQuoteId}/accept`, {
          params: {
            path: { marinaId, yardQuoteId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

export function useDeclineYardQuote(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; version: number; reason?: string }) =>
      unwrap(
        api.POST(`${M}/yard/quotes/{yardQuoteId}/decline`, {
          params: {
            path: { marinaId, yardQuoteId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.reason ? { reason: v.reason } : {},
        }),
      ),
    onSuccess: () => invalidate(qc, marinaId),
  });
}

/** URL of the PDF of a quote (same-origin GET with the session cookie). */
export function yardQuotePdfUrl(marinaId: string, quoteId: string): string {
  return `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/yard/quotes/${encodeURIComponent(quoteId)}/pdf`;
}

// ---- customer link (booking SPA) ----

/** Accepts a yard quote from its magic link (the work order is created by the marina). */
export function useAcceptPublicYardQuote(ref: { slug: string; token: string }) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (acceptConditions: boolean) =>
      unwrap(
        api.POST('/api/public/v1/{slug}/links/{token}/yard-quote/accept', {
          params: { path: ref },
          body: { accept_conditions: acceptConditions },
        }),
      ),
    onSettled: () => qc.invalidateQueries({ queryKey: queryKeys.publicLink(ref.slug, ref.token) }),
  });
}
