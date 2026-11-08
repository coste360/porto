import type { ReportQuery } from '@porto/shared/api';
import type { ReportSearch } from '@porto/shared/lib/reportLinks';

// Search params of a report page (m7-plan §10.1): exactly the report query string, string values only, so a
// drill link (lib/reportLinks) reproduces the request and the page URL is the request. Owner REPORTS.

/** Keys the page never sends to the API (none today: every search key is a report parameter). */
const UI_ONLY: readonly string[] = [];

/** Keeps the non-empty string (and finite number/boolean) values of the URL search. */
export function validateReportSearch(search: Record<string, unknown>): ReportSearch {
  const out: ReportSearch = {};
  for (const [k, v] of Object.entries(search)) {
    if (typeof v === 'string' && v !== '') out[k] = v;
    else if (typeof v === 'number' && Number.isFinite(v)) out[k] = String(v);
    else if (typeof v === 'boolean') out[k] = String(v);
  }
  return out;
}

/**
 * Values that the router's JSON search serializer would quote (`page=%222%22`) because they read as a number or
 * boolean, turned into that number or boolean so the URL stays `page=2`. Only exact round trips are converted
 * ("007" stays a quoted string); `validateReportSearch` turns them back into strings when the URL is read.
 */
export function urlSearchValues(search: ReportSearch): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {};
  for (const [k, v] of Object.entries(search)) {
    if (v === 'true' || v === 'false') out[k] = v === 'true';
    else if (/^-?\d+$/.test(v) && String(Number(v)) === v && Number.isSafeInteger(Number(v)))
      out[k] = Number(v);
    else out[k] = v;
  }
  return out;
}

/** Route search middleware of the report pages: keeps the page URL equal to the report query string. */
export function reportSearchMiddleware({
  search,
  next,
}: {
  search: ReportSearch;
  next: (search: ReportSearch) => ReportSearch;
}): ReportSearch {
  // The router stringifies what the middleware returns; the route type stays the validated (string) search.
  return urlSearchValues(next(search)) as unknown as ReportSearch;
}

/**
 * The API query of a page search: the search itself, with the period defaulted (year to date) when absent, so
 * the request the page shows is explicit and the CSV/Excel names carry the dates.
 */
export function reportQuery(
  search: ReportSearch,
  defaults: { from: string; to: string },
): ReportQuery {
  const q: ReportQuery = {};
  for (const [k, v] of Object.entries(search)) if (!UI_ONLY.includes(k) && v !== '') q[k] = v;
  q.from = search.from ?? defaults.from;
  q.to = search.to ?? defaults.to;
  return q;
}

/**
 * Applies a patch to a search: undefined or '' removes the key. Any change other than the page itself goes back
 * to the first page (totals never depend on the page, the records do).
 */
export function patchSearch(
  search: ReportSearch,
  patch: Record<string, string | undefined>,
): ReportSearch {
  const next: ReportSearch = { ...search };
  for (const [k, v] of Object.entries(patch)) {
    if (v === undefined || v === '') delete next[k];
    else next[k] = v;
  }
  if (!('page' in patch)) delete next.page;
  if (next.page === '1') delete next.page;
  return next;
}

/** Current page (1-based) of a search. */
export function pageOf(search: ReportSearch): number {
  const n = Number(search.page ?? '1');
  return Number.isInteger(n) && n > 0 ? n : 1;
}
