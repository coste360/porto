/**
 * Drill links of the manager dashboard (m7-plan §6, §10.2): a tile opens its report with exactly the parameters
 * the server put in `Tile.drill` plus the dashboard period. The dashboard never builds report parameters by hand.
 */
import type { components } from '../api/schema';

export type TileDrill = components['schemas']['TileDrill'];
export type ReportKey = components['schemas']['ReportKey'];

/** Search params of a report page: the report query string (string values only). */
export type ReportSearch = Record<string, string>;

/** Period of the dashboard, as `YYYY-MM-DD` dates. */
export type ReportPeriodSearch = { from: string; to: string };

/** Target of a drill link, ready for `<Link to params search>`. */
export type ReportLinkTarget = {
  to: '/m/$marinaId/reports/$report';
  params: { marinaId: string; report: ReportKey };
  search: ReportSearch;
};

/**
 * Search of a report page from a drill payload and a period. The dashboard period wins, except for a tile with
 * its own period (M9 storage_exits_blocked: the next 30 days), whose drill carries both from and to.
 */
export function drillSearch(drill: TileDrill, period: ReportPeriodSearch): ReportSearch {
  const search: ReportSearch = {};
  for (const [k, v] of Object.entries(drill.params)) {
    if (k !== 'from' && k !== 'to' && v !== '') search[k] = v;
  }
  const own = drill.params.from && drill.params.to;
  search.from = own ? drill.params.from! : period.from;
  search.to = own ? drill.params.to! : period.to;
  return search;
}

/** The report page a tile opens. */
export function reportLink(
  marinaId: string,
  drill: TileDrill,
  period: ReportPeriodSearch,
): ReportLinkTarget {
  return {
    to: '/m/$marinaId/reports/$report',
    params: { marinaId, report: drill.report },
    search: drillSearch(drill, period),
  };
}

/** The query string of a report request, keys sorted (stable query keys and export URLs). */
export function reportQueryString(search: ReportSearch): string {
  return Object.keys(search)
    .sort()
    .map((k) => `${encodeURIComponent(k)}=${encodeURIComponent(search[k] ?? '')}`)
    .join('&');
}
