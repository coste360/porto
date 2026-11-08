// Query hooks of the M7 manager dashboard and period comparison (m7-plan §10.2, owner DASHBOARD): useDashboard
// over GET .../dashboard and useComparison over GET .../reports/comparison (keys queryKeys.dashboard and
// queryKeys.comparison). Reports are not refreshed by SSE: a short staleTime and refetch on focus instead.
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query';

import { unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';
import { fetchExport } from './contractsM4';

type S = components['schemas'];

export type Dashboard = S['Dashboard'];
export type Tile = S['Tile'];
export type TileKey = S['TileKey'];
export type TileTrend = S['TileTrend'];
export type TileValue = S['TileValue'];
export type TileSeriesPoint = S['TileSeriesPoint'];
export type TileBreakdown = S['TileBreakdown'];
export type ReportAlign = S['ReportAlign'];
export type ReportGranularity = S['ReportGranularity'];
export type ComparisonIndicator = S['ComparisonIndicator'];
export type ComparisonResult = S['ComparisonResult'];
export type ComparisonPoint = S['ComparisonPoint'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;

/** Reports read every area and are not invalidated by live updates (m7-plan §10.2). */
export const REPORT_STALE_TIME = 30_000;

/** Query of GET .../dashboard (string values only; empty values are dropped). */
export type DashboardQuery = {
  from?: string;
  to?: string;
  compare?: 'previous_year' | 'none';
  align?: ReportAlign;
  map_id?: string;
  map_kind?: string;
  structure_id?: string;
  group_id?: string;
  category?: string;
};

/** Query of GET .../reports/comparison. */
export type ComparisonQuery = {
  indicator: ComparisonIndicator;
  from?: string;
  to?: string;
  years_back?: string;
  align?: ReportAlign;
  granularity?: ReportGranularity;
  lang?: string;
  map_id?: string;
  map_kind?: string;
  structure_id?: string;
  group_id?: string;
  category?: string;
};

/** Drops undefined and empty values so query keys are stable. */
export function compactQuery<T extends Record<string, string | undefined>>(
  q: T,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(q)) if (v !== undefined && v !== '') out[k] = v;
  return out;
}

/** The manager dashboard tiles the user may see. `enabled` false for roles without report permissions. */
export function useDashboard(marinaId: string, query: DashboardQuery = {}, enabled = true) {
  const api = useApi();
  const q = compactQuery(query);
  return useQuery({
    queryKey: queryKeys.dashboard(marinaId, q),
    enabled: enabled && !!marinaId,
    staleTime: REPORT_STALE_TIME,
    refetchOnWindowFocus: true,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/dashboard`, {
          params: { path: { marinaId }, query: q as never },
        }),
      ),
  });
}

/** One indicator compared with the previous years (R11). */
export function useComparison(marinaId: string, query: ComparisonQuery, enabled = true) {
  const api = useApi();
  const q = compactQuery(query);
  return useQuery({
    queryKey: queryKeys.comparison(marinaId, q),
    enabled: enabled && !!marinaId,
    staleTime: REPORT_STALE_TIME,
    refetchOnWindowFocus: true,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/reports/comparison`, {
          params: { path: { marinaId }, query: q as never },
        }),
        // The operation also serves CSV/Excel; with format absent the body is always the JSON result.
      ) as Promise<ComparisonResult>,
  });
}

/** CSV or Excel of a comparison (same query + format). */
export function useComparisonFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { query: ComparisonQuery; format: 'csv' | 'xlsx' }) =>
      fetchExport(
        api.GET(`${M}/reports/comparison`, {
          params: {
            path: { marinaId },
            query: { ...compactQuery(v.query), format: v.format } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}
