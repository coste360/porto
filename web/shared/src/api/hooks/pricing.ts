// Pricing catalogue and price list hooks (m2-plan §4.3, §4.4, §7.2): pricing settings, revenue classes, items,
// berth categories, price list documents, validation, activation, archive and renewal. Every key sits under
// queryKeys.pricing(marinaId), so `pricing.changed` (SSE) and every mutation here invalidate the whole area.
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, type ApiClient } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const PAGE_SIZE = 50;
const MAX_PAGE = 200;

// ---------------------------------------------------------------------------------------------------------
// Problems with pricing members

export type PriceListConflict = S['PriceListRef'];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Validation issues carried by a price_list_invalid problem (empty for any other error). */
export function priceListIssuesOf(error: unknown): S['PriceListIssue'][] {
  return error instanceof ProblemError ? error.priceListIssues : [];
}

/** Overlapping active lists of a price_list_overlap problem. */
export function overlapConflictsOf(error: unknown): PriceListConflict[] {
  if (!(error instanceof ProblemError)) return [];
  const list = error.details.conflicts;
  return Array.isArray(list)
    ? list.filter(
        (c): c is PriceListConflict =>
          isRecord(c) && typeof c.id === 'string' && typeof c.code === 'string',
      )
    : [];
}

/** Number of references of an in_use problem, when the server reports it. */
export function inUseReferencesOf(error: unknown): number | undefined {
  if (!(error instanceof ProblemError)) return undefined;
  const n = error.details.references;
  return typeof n === 'number' ? n : undefined;
}

type FetchResult = { data?: unknown; error?: unknown; response: Response };
type DataOf<R extends FetchResult> = Exclude<R['data'], undefined>;

/** Like `unwrap`, typed for the pricing calls (errors keep the pricing members of the problem document). */
async function call<R extends FetchResult>(promise: Promise<R>): Promise<DataOf<R>> {
  let result: R;
  try {
    result = await promise;
  } catch {
    throw new ProblemError({ type: 'internal_error', status: 0 });
  }
  if (result.error !== undefined || !result.response.ok) {
    throw ProblemError.from(result.response.status, result.error, result.response.headers);
  }
  return result.data as DataOf<R>;
}

function invalidatePricing(queryClient: QueryClient, marinaId: string) {
  return queryClient.invalidateQueries({ queryKey: queryKeys.pricing(marinaId) });
}

/** Mutation hook with pricing invalidation on success. */
function usePricingMutation<V, R>(marinaId: string, fn: (api: ApiClient, vars: V) => Promise<R>) {
  const api = useApi();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => fn(api, vars),
    onSuccess: () => invalidatePricing(queryClient, marinaId),
  });
}

// ---------------------------------------------------------------------------------------------------------
// Pricing settings

export function usePricingSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.pricingSettings(marinaId),
    queryFn: () => call(api.GET(`${M}/pricing-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdatePricingSettings(marinaId: string) {
  return usePricingMutation(
    marinaId,
    (api, vars: { version: number; body: S['PricingSettingsUpdate'] }) =>
      call(
        api.PUT(`${M}/pricing-settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(vars.version) } },
          body: vars.body,
        }),
      ),
  );
}

// ---------------------------------------------------------------------------------------------------------
// Revenue classes

export function useRevenueClasses(marinaId: string, opts: { includeArchived?: boolean } = {}) {
  const api = useApi();
  const includeArchived = opts.includeArchived === true;
  return useQuery({
    queryKey: queryKeys.revenueClasses(marinaId, includeArchived ? { archived: '1' } : {}),
    queryFn: () =>
      call(
        api.GET(`${M}/revenue-classes`, {
          params: {
            path: { marinaId },
            query: includeArchived ? { include_archived: true } : {},
          },
        }),
      ),
  });
}

export function useCreateRevenueClass(marinaId: string) {
  return usePricingMutation(marinaId, (api, body: S['RevenueClassCreate']) =>
    call(
      api.POST(`${M}/revenue-classes`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useUpdateRevenueClass(marinaId: string) {
  return usePricingMutation(
    marinaId,
    (api, vars: { id: string; version: number; patch: S['RevenueClassPatch'] }) =>
      call(
        api.PATCH(`${M}/revenue-classes/{revenueClassId}`, {
          params: {
            path: { marinaId, revenueClassId: vars.id },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useDeleteRevenueClass(marinaId: string) {
  return usePricingMutation(marinaId, (api, vars: { id: string; version: number }) =>
    call(
      api.DELETE(`${M}/revenue-classes/{revenueClassId}`, {
        params: {
          path: { marinaId, revenueClassId: vars.id },
          header: { 'If-Match': ifMatch(vars.version) },
        },
      }),
    ),
  );
}

// ---------------------------------------------------------------------------------------------------------
// Items

export type ItemFilters = { q?: string; type?: S['ItemType']; active?: boolean };

export function useItems(marinaId: string, filters: ItemFilters = {}) {
  const api = useApi();
  const key: Record<string, string> = {};
  if (filters.q) key.q = filters.q;
  if (filters.type) key.type = filters.type;
  if (filters.active !== undefined) key.active = String(filters.active);
  return useInfiniteQuery({
    queryKey: queryKeys.items(marinaId, key),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      call(
        api.GET(`${M}/items`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...(filters.q ? { q: filters.q } : {}),
              ...(filters.type ? { type: filters.type } : {}),
              ...(filters.active !== undefined ? { active: filters.active } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** Every item of the marina (all pages), for the selects of the price list editor. */
export function useAllItems(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.items(marinaId, { all: '1' }),
    queryFn: async () => {
      const out: S['Item'][] = [];
      let cursor: string | undefined;
      // A marina has at most a few hundred items; the loop stops on the last page.
      for (let page = 0; page < 50; page++) {
        const res = await call(
          api.GET(`${M}/items`, {
            params: {
              path: { marinaId },
              query: { limit: MAX_PAGE, ...(cursor ? { cursor } : {}) },
            },
          }),
        );
        out.push(...res.items);
        if (!res.next_cursor) break;
        cursor = res.next_cursor;
      }
      return out;
    },
  });
}

export function useItem(marinaId: string, itemId: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.item(marinaId, itemId ?? ''),
    enabled: itemId !== null,
    queryFn: () =>
      call(
        api.GET(`${M}/items/{itemId}`, { params: { path: { marinaId, itemId: itemId ?? '' } } }),
      ),
  });
}

export function useCreateItem(marinaId: string) {
  return usePricingMutation(marinaId, (api, body: S['ItemCreate']) =>
    call(
      api.POST(`${M}/items`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useUpdateItem(marinaId: string) {
  return usePricingMutation(
    marinaId,
    (api, vars: { id: string; version: number; patch: S['ItemPatch'] }) =>
      call(
        api.PATCH(`${M}/items/{itemId}`, {
          params: {
            path: { marinaId, itemId: vars.id },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useDeleteItem(marinaId: string) {
  return usePricingMutation(marinaId, (api, vars: { id: string; version: number }) =>
    call(
      api.DELETE(`${M}/items/{itemId}`, {
        params: {
          path: { marinaId, itemId: vars.id },
          header: { 'If-Match': ifMatch(vars.version) },
        },
      }),
    ),
  );
}

// ---------------------------------------------------------------------------------------------------------
// Berth categories

/** Customer categories (fixed enum) with the number of customers in each; counts only. */
export function useCustomerCategoryCounts(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.customerCategoryCounts(marinaId),
    queryFn: () => call(api.GET(`${M}/customer-categories`, { params: { path: { marinaId } } })),
  });
}

export function useBerthCategories(marinaId: string, opts: { includeArchived?: boolean } = {}) {
  const api = useApi();
  const includeArchived = opts.includeArchived === true;
  return useQuery({
    queryKey: queryKeys.berthCategories(marinaId, includeArchived ? { archived: '1' } : {}),
    queryFn: () =>
      call(
        api.GET(`${M}/berth-categories`, {
          params: {
            path: { marinaId },
            query: includeArchived ? { include_archived: true } : {},
          },
        }),
      ),
  });
}

export function useCreateBerthCategory(marinaId: string) {
  return usePricingMutation(marinaId, (api, body: S['BerthCategoryCreate']) =>
    call(
      api.POST(`${M}/berth-categories`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useUpdateBerthCategory(marinaId: string) {
  return usePricingMutation(
    marinaId,
    (api, vars: { id: string; version: number; patch: S['BerthCategoryPatch'] }) =>
      call(
        api.PATCH(`${M}/berth-categories/{categoryId}`, {
          params: {
            path: { marinaId, categoryId: vars.id },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useDeleteBerthCategory(marinaId: string) {
  return usePricingMutation(marinaId, (api, vars: { id: string; version: number }) =>
    call(
      api.DELETE(`${M}/berth-categories/{categoryId}`, {
        params: {
          path: { marinaId, categoryId: vars.id },
          header: { 'If-Match': ifMatch(vars.version) },
        },
      }),
    ),
  );
}

export function useSyncBerthCategories(marinaId: string) {
  return usePricingMutation(marinaId, (api, _vars: void) =>
    call(
      api.POST(`${M}/berth-categories/sync`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
      }),
    ),
  );
}

// ---------------------------------------------------------------------------------------------------------
// Price lists

export type PriceListFilters = {
  status?: S['PriceListStatus'];
  contractType?: S['ContractType'];
  on?: string;
};

export function usePriceLists(marinaId: string, filters: PriceListFilters = {}) {
  const api = useApi();
  const key: Record<string, string> = {};
  if (filters.status) key.status = filters.status;
  if (filters.contractType) key.contract_type = filters.contractType;
  if (filters.on) key.on = filters.on;
  return useQuery({
    queryKey: queryKeys.priceLists(marinaId, key),
    queryFn: () =>
      call(
        api.GET(`${M}/price-lists`, {
          params: {
            path: { marinaId },
            query: {
              ...(filters.status ? { status: filters.status } : {}),
              ...(filters.contractType ? { contract_type: filters.contractType } : {}),
              ...(filters.on ? { on: filters.on } : {}),
            },
          },
        }),
      ),
  });
}

export function fetchPriceList(
  api: ApiClient,
  marinaId: string,
  priceListId: string,
): Promise<S['PriceListDocument']> {
  return call(
    api.GET(`${M}/price-lists/{priceListId}`, { params: { path: { marinaId, priceListId } } }),
  );
}

export function usePriceList(marinaId: string, priceListId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.priceList(marinaId, priceListId),
    queryFn: () => fetchPriceList(api, marinaId, priceListId),
  });
}

export function useCreatePriceList(marinaId: string) {
  return usePricingMutation(marinaId, (api, body: S['PriceListCreate']) =>
    call(
      api.POST(`${M}/price-lists`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useSavePriceList(marinaId: string, priceListId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ version, body }: { version: number; body: S['PriceListSave'] }) =>
      call(
        api.PUT(`${M}/price-lists/{priceListId}`, {
          params: {
            path: { marinaId, priceListId },
            header: { 'If-Match': ifMatch(version) },
          },
          body,
        }),
      ),
    onSuccess: (result) => {
      qc.setQueryData(queryKeys.priceList(marinaId, priceListId), result.document);
      void qc.invalidateQueries({ queryKey: queryKeys.priceLists(marinaId) });
    },
  });
}

/** Dry-run validation of an unsaved document (no body = the saved document). */
export function validatePriceList(
  api: ApiClient,
  marinaId: string,
  priceListId: string,
  body?: S['PriceListSave'],
  signal?: AbortSignal,
): Promise<S['PriceListValidation']> {
  return call(
    api.POST(`${M}/price-lists/{priceListId}/validate`, {
      params: { path: { marinaId, priceListId } },
      ...(body ? { body } : {}),
      signal,
    }),
  );
}

export function useDeletePriceList(marinaId: string) {
  return usePricingMutation(marinaId, (api, vars: { id: string; version: number }) =>
    call(
      api.DELETE(`${M}/price-lists/{priceListId}`, {
        params: {
          path: { marinaId, priceListId: vars.id },
          header: { 'If-Match': ifMatch(vars.version) },
        },
      }),
    ),
  );
}

export function useActivatePriceList(marinaId: string) {
  return usePricingMutation(
    marinaId,
    (api, vars: { id: string; version: number; effectiveFrom?: string }) =>
      call(
        api.POST(`${M}/price-lists/{priceListId}/activate`, {
          params: {
            path: { marinaId, priceListId: vars.id },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.effectiveFrom ? { effective_from: vars.effectiveFrom } : {},
        }),
      ),
  );
}

export function useArchivePriceList(marinaId: string) {
  return usePricingMutation(marinaId, (api, vars: { id: string; version: number }) =>
    call(
      api.POST(`${M}/price-lists/{priceListId}/archive`, {
        params: {
          path: { marinaId, priceListId: vars.id },
          header: { 'If-Match': ifMatch(vars.version) },
        },
      }),
    ),
  );
}

/** Renewal preview (`dry_run: true`): the unsaved document and the matrix diff per contract type. */
export function usePreviewRenewal(marinaId: string, priceListId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: async (body: Omit<S['RenewalRequest'], 'dry_run'>) =>
      (await call(
        api.POST(`${M}/price-lists/{priceListId}/renewal`, {
          params: { path: { marinaId, priceListId } },
          body: { ...body, dry_run: true },
        }),
      )) as S['RenewalPreview'],
  });
}

/** Renewal apply: creates the new draft and returns it. */
export function useApplyRenewal(marinaId: string, priceListId: string) {
  return usePricingMutation(
    marinaId,
    async (api, vars: { body: Omit<S['RenewalRequest'], 'dry_run'>; idempotencyKey: string }) =>
      (await call(
        api.POST(`${M}/price-lists/{priceListId}/renewal`, {
          params: {
            path: { marinaId, priceListId },
            header: { 'Idempotency-Key': vars.idempotencyKey },
          },
          body: { ...vars.body, dry_run: false },
        }),
      )) as S['PriceListDocument'],
  );
}
