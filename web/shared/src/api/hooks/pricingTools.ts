// Pricing tool hooks (m2-plan §4.5, §7.2): simulatePrice, getPriceMatrix and getPriceCoverage, plus the read
// queries the simulator, matrix and coverage pages need (price lists, items, berth categories, berths). Every
// key sits under queryKeys.pricing(marinaId) (or the maps root for berths), so `pricing.changed` invalidates
// them. Keys shared with the price list pages get a trailing 'tools' segment: the cached value is always the
// raw response of this file's query function and never collides with another hook's shape.
import { useMutation, useQuery } from '@tanstack/react-query';

import { unwrap } from '../client';
import { useApi } from '../context';
import { ProblemError } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

/** Machine-readable details of a pricing problem (no_price, promo_not_applicable); ids and codes only. */
export type PricingProblemDetails = {
  item_id?: string;
  contract_type?: string;
  night?: string;
  unit?: string;
  category?: string;
  band?: string;
  reason?: string;
  [key: string]: unknown;
};

/** `details` of a problem thrown by useSimulatePrice, or null. */
export function pricingProblemDetails(error: unknown): PricingProblemDetails | null {
  return error instanceof ProblemError && Object.keys(error.details).length > 0
    ? (error.details as PricingProblemDetails)
    : null;
}

/** Prices a stay (POST pricing/simulate). The web never computes prices: the breakdown comes from the server. */
export function useSimulatePrice(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: async (body: S['PricingSimulateRequest']): Promise<S['PriceBreakdown']> => {
      let result;
      try {
        result = await api.POST('/api/admin/v1/marinas/{marinaId}/pricing/simulate', {
          params: { path: { marinaId } },
          body,
        });
      } catch {
        throw new ProblemError({ type: 'internal_error', status: 0 });
      }
      if (result.error !== undefined || !result.response.ok || result.data === undefined) {
        throw ProblemError.from(result.response.status, result.error, result.response.headers);
      }
      return result.data;
    },
  });
}

export type PriceMatrixParams = {
  contract_type?: S['ContractType'];
  item_id?: string;
  rows?: 'bands' | 'categories';
  channel?: S['Channel'];
  customer_category?: S['CustomerCategory'];
  quoted_on?: string;
};

function compact(params: Record<string, string | undefined>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(params)) if (v) out[k] = v;
  return out;
}

/** The price matrix of a list for one contract type (each cell is the engine result of its probe). */
export function usePriceMatrix(
  marinaId: string,
  priceListId: string,
  params: PriceMatrixParams = {},
  enabled = true,
) {
  const api = useApi();
  const query = compact(params) as PriceMatrixParams;
  return useQuery({
    queryKey: queryKeys.priceMatrix(marinaId, priceListId, query),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/matrix', {
          params: { path: { marinaId, priceListId }, query },
        }),
      ),
  });
}

/** Coverage check of a list: combinations of contract type, category/band and period without a price. */
export function usePriceCoverage(marinaId: string, priceListId: string, quotedOn?: string) {
  const api = useApi();
  const query = compact({ quoted_on: quotedOn });
  return useQuery({
    queryKey: queryKeys.priceCoverage(marinaId, priceListId, query),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}/coverage', {
          params: { path: { marinaId, priceListId }, query },
        }),
      ),
  });
}

/** Price list summaries for the tool pages (list select of the simulator). */
export function usePricingToolPriceLists(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.priceLists(marinaId), 'tools'],
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/price-lists', {
          params: { path: { marinaId } },
        }),
      ),
  });
}

/** One price list document, read by the tool pages (rule names, periods, bands, validity). */
export function usePricingToolPriceList(marinaId: string, priceListId: string | null) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.priceList(marinaId, priceListId ?? ''), 'tools'],
    enabled: !!priceListId,
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/price-lists/{priceListId}', {
          params: { path: { marinaId, priceListId: priceListId ?? '' } },
        }),
      ),
  });
}

/** Items of the marina (first page of 200), for the item and services selects of the tool pages. */
export function usePricingToolItems(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.items(marinaId), 'tools'],
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/items', {
          params: { path: { marinaId }, query: { limit: 200 } },
        }),
      ),
  });
}

/** Berth categories and the unknown codes in use, for the category selects of the tool pages. */
export function usePricingToolBerthCategories(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.berthCategories(marinaId), 'tools'],
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/berth-categories', {
          params: { path: { marinaId } },
        }),
      ),
  });
}

/** Berths matching a code search (first page of 50), for the resource combobox of the simulator. */
export function usePricingToolBerths(marinaId: string, q: string, enabled = true) {
  const api = useApi();
  const query = compact({ q: q.trim() });
  return useQuery({
    queryKey: [...queryKeys.berths(marinaId, query), 'tools'],
    enabled,
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/berths', {
          params: { path: { marinaId }, query: { limit: 50, ...query } },
        }),
      ),
  });
}
