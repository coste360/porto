import type {
  ComparisonIndicator,
  ComparisonResult,
  ReportAlign,
  ReportGranularity,
} from '@porto/shared/api';
import { formatIsoDate } from '@porto/shared/lib/format';

// Pure helpers of the period comparison page (m7-plan §5, §10.1).

export const INDICATORS = [
  'production',
  'charged',
  'collected',
  'occupancy',
  'efficiency',
  'berth_revenue_per_m',
  'transit_avg_night',
  'new_customers',
  'renewal_rate',
  'arrivals',
  'departures',
  'lost_demand_count',
  'lost_demand_value',
] as const satisfies readonly ComparisonIndicator[];

/** Indicators readable with reports.operational only (m7-plan D17); every other one needs reports.all. */
export const OPERATIONAL_INDICATORS: readonly ComparisonIndicator[] = [
  'occupancy',
  'efficiency',
  'arrivals',
  'departures',
  'lost_demand_count',
];

export function allowedIndicators(full: boolean, operational: boolean): ComparisonIndicator[] {
  if (full) return [...INDICATORS];
  if (operational) return [...OPERATIONAL_INDICATORS];
  return [];
}

export function isIndicator(v: unknown): v is ComparisonIndicator {
  return typeof v === 'string' && (INDICATORS as readonly string[]).includes(v);
}

export const ALIGNS = ['date', 'weekday'] as const satisfies readonly ReportAlign[];
export const GRANULARITIES = [
  'day',
  'week',
  'month',
] as const satisfies readonly ReportGranularity[];
export const MAX_YEARS_BACK = 5;

/** Parsed search of the comparison page (defaults applied by the caller). */
export type ComparisonSearch = {
  indicator?: ComparisonIndicator;
  from?: string;
  to?: string;
  years_back?: number;
  align?: ReportAlign;
  granularity?: ReportGranularity;
};

/** Reads the string search of the page (unknown values dropped, so a hand-edited URL still renders). */
export function parseComparisonSearch(search: Record<string, string>): ComparisonSearch {
  const out: ComparisonSearch = {};
  if (isIndicator(search.indicator)) out.indicator = search.indicator;
  if (/^\d{4}-\d{2}-\d{2}$/.test(search.from ?? '')) out.from = search.from;
  if (/^\d{4}-\d{2}-\d{2}$/.test(search.to ?? '')) out.to = search.to;
  const years = Number(search.years_back);
  if (Number.isInteger(years) && years >= 1 && years <= MAX_YEARS_BACK) out.years_back = years;
  if ((ALIGNS as readonly string[]).includes(search.align ?? ''))
    out.align = search.align as ReportAlign;
  if ((GRANULARITIES as readonly string[]).includes(search.granularity ?? ''))
    out.granularity = search.granularity as ReportGranularity;
  return out;
}

/** One row of the delta table / point of the chart: the current bucket and the aligned previous value. */
export type ComparisonRow = {
  bucket: string;
  current: number | null;
  previous: number | null;
  /** current − previous when both exist (integers in the indicator unit). */
  delta: number | null;
};

/**
 * Rows of the series in server order. The previous value of a row is the one the server aligned to that
 * bucket (by date or weekday), never the previous series by index.
 */
export function comparisonRows(result: ComparisonResult): ComparisonRow[] {
  return result.series.map((p) => {
    const current = p.current ?? null;
    const previous = p.previous ?? null;
    return {
      bucket: p.bucket,
      current,
      previous,
      delta: current !== null && previous !== null ? current - previous : null,
    };
  });
}

/** Label of a bucket: the day, the Monday of the ISO week, or the month ("nov 2026"). */
export function bucketLabel(
  bucket: string,
  granularity: ReportGranularity,
  locale: string,
): string {
  if (granularity !== 'month') return formatIsoDate(bucket);
  const m = /^(\d{4})-(\d{2})-\d{2}$/.exec(bucket);
  if (!m) return bucket;
  const date = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, 1));
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(date);
}

/** File name of an export, as the server names it. */
export function comparisonFileName(from: string, to: string, format: 'csv' | 'xlsx'): string {
  return `porto-comparison-${from}-${to}.${format}`;
}
