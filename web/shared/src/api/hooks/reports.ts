// Query hooks of the M7 report pages (m7-plan §10.2, owner REPORTS): useReport(key, search) over
// GET .../reports/{key} and useReportExport (same query + format=csv|xlsx, the caller saves the blob through
// lib/download). Keys live under queryKeys.reports(marinaId). Reports read every area and are not invalidated by
// live updates: a short staleTime and refetch on focus instead.
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query';

import { unwrap } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';
import { REPORT_STALE_TIME } from './dashboard';
import { fetchExport } from './contractsM4';

type S = components['schemas'];

export type ReportKey = S['ReportKey'];
export type ReportFormat = S['ReportFormat'];
export type ReportUnit = S['ReportUnit'];
export type ReportEnvelope = S['ReportEnvelope'];
export type ReportRef = S['ReportRef'];
export type ReportNote = S['ReportNote'];
export type ReportPageInfo = S['ReportPageInfo'];
export type OccupancyReport = S['OccupancyReport'];
export type EfficiencyReport = S['EfficiencyReport'];
export type AllocationWasteReport = S['AllocationWasteReport'];
export type WasteSuggestion = S['WasteSuggestion'];
export type LostDemandReport = S['LostDemandReport'];
export type CustomersReport = S['CustomersReport'];
export type ProductionReport = S['ProductionReport'];
export type AccrualsReport = S['AccrualsReport'];
export type ChargesReport = S['ChargesReport'];
export type PaymentsReport = S['PaymentsReport'];
export type RevenueClassesReport = S['RevenueClassesReport'];
export type ReceivablesReport = S['ReceivablesReport'];
export type PresencesReport = S['PresencesReport'];
export type RightsReport = S['RightsReport'];
export type CashReport = S['CashReport'];
export type VesselsReport = S['VesselsReport'];
export type BerthRevenueReport = S['BerthRevenueReport'];
export type StorageReport = S['StorageReport'];
export type MaintenanceReport = S['MaintenanceReport'];
export type UtilitiesReport = S['UtilitiesReport'];
export type YardReportResponse = S['YardReport'];

/** The JSON response of each report key (comparison has its own hook in dashboard.ts). */
export type ReportResponses = {
  occupancy: OccupancyReport;
  efficiency: EfficiencyReport;
  'allocation-waste': AllocationWasteReport;
  'lost-demand': LostDemandReport;
  customers: CustomersReport;
  production: ProductionReport;
  accruals: AccrualsReport;
  charges: ChargesReport;
  payments: PaymentsReport;
  'revenue-classes': RevenueClassesReport;
  receivables: ReceivablesReport;
  presences: PresencesReport;
  rights: RightsReport;
  cash: CashReport;
  vessels: VesselsReport;
  'berth-revenue': BerthRevenueReport;
  // M9 (m9-plan §11)
  storage: StorageReport;
  maintenance: MaintenanceReport;
  utilities: UtilitiesReport;
  // M10 (m10-plan §12)
  yard: YardReportResponse;
};

/** A report served by GET .../reports/{key} (every key but comparison and the reserved rental). */
export type ServedReportKey = keyof ReportResponses;

/** Any report response (the page renders them through the catalog, field by field). */
export type AnyReport = ReportResponses[ServedReportKey];

/** The report query string: string values only, empty values dropped (lib/reportLinks ReportSearch). */
export type ReportQuery = Record<string, string>;

/** The request path of a report (one typed operation per key; the path is the same shape for all). */
function reportPath(key: ServedReportKey) {
  return `/api/admin/v1/marinas/{marinaId}/reports/${key}` as '/api/admin/v1/marinas/{marinaId}/reports/charges';
}

function compact(q: ReportQuery): ReportQuery {
  const out: ReportQuery = {};
  for (const [k, v] of Object.entries(q)) if (v !== '') out[k] = v;
  return out;
}

/** One report page (JSON). `enabled` false while the filter is invalid or the role lacks the permission. */
export function useReport<K extends ServedReportKey>(
  marinaId: string,
  key: K,
  query: ReportQuery,
  enabled = true,
) {
  const api = useApi();
  const q = compact(query);
  delete q.format;
  return useQuery({
    queryKey: queryKeys.report(marinaId, key, q),
    enabled: enabled && !!marinaId,
    staleTime: REPORT_STALE_TIME,
    refetchOnWindowFocus: true,
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(
        api.GET(reportPath(key), { params: { path: { marinaId }, query: q as never } }),
      ) as unknown as Promise<ReportResponses[K]>,
  });
}

/** CSV or Excel of a report: the same query with format (the server ignores page and returns every record). */
export function useReportExport(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { key: ServedReportKey; query: ReportQuery; format: 'csv' | 'xlsx' }) => {
      const q = compact(v.query);
      delete q.page;
      delete q.page_size;
      return fetchExport(
        api.GET(reportPath(v.key), {
          params: { path: { marinaId }, query: { ...q, format: v.format } as never },
          parseAs: 'blob',
        }),
      );
    },
  });
}

/** The attachment name the server uses: porto-<key>-<from>-<to>.<ext>. */
export function reportFileName(key: string, from: string, to: string, ext: 'csv' | 'xlsx'): string {
  return `porto-${key}-${from}-${to}.${ext}`;
}
