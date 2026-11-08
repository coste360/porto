import type { TFunction } from 'i18next';

import {
  formatBp,
  formatCents,
  formatCm,
  formatCm2,
  formatInteger,
  formatIsoDate,
  formatQtyMilli,
  formatReportValue,
  type ReportValueUnit,
} from '@porto/shared/lib/format';

import type { CellKind, ColumnDef } from './catalog';

// Formatting of report values at the edge (integers in state): one function for the records, the Totale row, the
// summary cards and the groups, so the Totale cell and a dashboard tile show the same text for the same integer.

export type Row = Record<string, unknown>;

/** Text of a month YYYY-MM in the locale ("mar 2026"). */
export function formatMonth(month: string, locale: string): string {
  const m = /^(\d{4})-(\d{2})$/.exec(month);
  if (!m) return month;
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, 1));
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(d);
}

/** The unit of the 'unit' columns: the result's totals.unit (berth_night when absent). */
export function unitOf(totals: Row | undefined): ReportValueUnit {
  const u = totals?.unit;
  return typeof u === 'string' ? (u as ReportValueUnit) : 'berth_night';
}

/**
 * Formats one value: null/undefined → "—" for numbers (hidden amount, unpriced, ratio without denominator) and ""
 * for text. Enumerations go through their translation group, falling back to the code.
 */
export function formatCell(
  value: unknown,
  kind: CellKind,
  opts: { t: TFunction; locale: string; unit: ReportValueUnit; values?: string },
): string {
  const { t, locale } = opts;
  if (value === null || value === undefined || value === '') {
    return kind === 'text' || kind === 'enum' || kind === 'date' || kind === 'month' ? '' : '—';
  }
  switch (kind) {
    case 'text':
      return String(value);
    case 'enum':
      return opts.values
        ? t(`${opts.values}.${String(value)}`, { defaultValue: String(value) })
        : String(value);
    case 'date':
      return formatIsoDate(String(value));
    case 'month':
      return formatMonth(String(value), locale);
    case 'bool':
      return t(`reports:values.bool.${value === true || value === 'true' ? 'true' : 'false'}`);
  }
  const n = Number(value);
  if (!Number.isFinite(n)) return String(value);
  switch (kind) {
    case 'cents':
      return formatCents(n, 'EUR', locale);
    case 'bp':
      return formatBp(n, locale);
    case 'cm':
      return formatCm(n, locale);
    case 'cm2':
      return formatCm2(n, locale);
    case 'milli':
      return formatQtyMilli(n, locale);
    case 'unit':
    case 'unit_area':
      return formatReportValue(n, opts.unit, locale);
    default:
      return formatInteger(n, locale);
  }
}

/** Whether a column is numeric (right-aligned, tabular figures). */
export function isNumeric(kind: CellKind): boolean {
  return !['text', 'enum', 'date', 'month', 'bool'].includes(kind);
}

/** The Totale row: the totals field named by each column (the first cell carries the label). */
export function totalRow(
  columns: readonly ColumnDef[],
  totals: Row | undefined,
): (unknown | undefined)[] {
  return columns.map((c) => (c.total ? totals?.[c.total] : undefined));
}
