// Holders' shares of a right (spec 14.1, INV-40) and millesimal tables (spec 14.5, INV-41) in the browser: the live
// Σ‰ indicator, input parsing and the CSV of a table. Shares are integer millis (1..1000 per holder, Σ = 1000),
// millesimal entries integer millis ≥ 0 per berth with Σ = 1000 to activate. The server validates again.
import type { Schemas } from '@porto/shared/api';

import { detectDelimiter, parseCsv } from '../../berth-import/csv';
import type { CustomerChoice } from '../../desk/components/CustomerField';

export const TOTAL_MILLIS = 1000;

/** Parses a holder share typed in millesimi: an integer 0..1000 ("250" → 250); decimals are refused (null). */
export function parseMillis(text: string): number | null {
  const t = text.trim();
  if (!/^\d{1,4}$/.test(t)) return null;
  const n = Number(t);
  return n <= TOTAL_MILLIS ? n : null;
}

export type HolderRow = { customerId: string; millis: number | null };

export type SharesCheck = {
  sum: number;
  remaining: number;
  /** Keys under `rights:holders.errors.<key>`. */
  errors: ('none' | 'duplicate' | 'share_range' | 'sum' | 'customer')[];
  ok: boolean;
};

/** Live check of the holders of a right: every share 1..1000, distinct customers, Σ = 1000 exactly. */
export function checkShares(rows: readonly HolderRow[]): SharesCheck {
  const errors: SharesCheck['errors'] = [];
  let sum = 0;
  if (rows.length === 0) errors.push('none');
  const seen = new Set<string>();
  let dup = false;
  let range = false;
  let missingCustomer = false;
  for (const r of rows) {
    if (!r.customerId) missingCustomer = true;
    else if (seen.has(r.customerId)) dup = true;
    else seen.add(r.customerId);
    if (r.millis === null || r.millis < 1 || r.millis > TOTAL_MILLIS) range = true;
    else sum += r.millis;
  }
  if (missingCustomer) errors.push('customer');
  if (dup) errors.push('duplicate');
  if (range) errors.push('share_range');
  if (rows.length > 0 && sum !== TOTAL_MILLIS) errors.push('sum');
  return { sum, remaining: TOTAL_MILLIS - sum, errors, ok: errors.length === 0 };
}

/** Equal shares for n holders with the remainder on the first ones (n ≤ 1000), Σ = 1000 exactly. */
export function equalShares(n: number): number[] {
  if (n <= 0) return [];
  const base = Math.floor(TOTAL_MILLIS / n);
  const rest = TOTAL_MILLIS - base * n;
  return Array.from({ length: n }, (_, i) => base + (i < rest ? 1 : 0));
}

export function toHolderShares(rows: readonly HolderRow[]): Schemas['HolderShare'][] {
  return rows.map((r) => ({ customer_id: r.customerId, share_millis: r.millis ?? 0 }));
}

// ---- millesimal grid ----

export type GridRow = { berthId: string; berthCode: string; text: string };

export type GridCheck = {
  sum: number;
  remaining: number;
  /** Indexes of rows whose text is not a valid integer ≥ 0. */
  invalid: number[];
  complete: boolean;
};

export function parseEntryMillis(text: string): number | null {
  const t = text.trim();
  if (t === '') return 0;
  if (!/^\d{1,4}$/.test(t)) return null;
  return Number(t);
}

/** Live Σ of the grid editor; the table can be activated only when complete (Σ = 1000, INV-41). */
export function checkGrid(rows: readonly GridRow[]): GridCheck {
  let sum = 0;
  const invalid: number[] = [];
  rows.forEach((r, i) => {
    const m = parseEntryMillis(r.text);
    if (m === null) invalid.push(i);
    else sum += m;
  });
  return {
    sum,
    remaining: TOTAL_MILLIS - sum,
    invalid,
    complete: invalid.length === 0 && sum === TOTAL_MILLIS,
  };
}

export function gridEntries(rows: readonly GridRow[]): Schemas['MillesimalEntry'][] {
  return rows
    .map((r) => ({ berth_id: r.berthId, millis: parseEntryMillis(r.text) ?? 0 }))
    .filter((e) => e.millis > 0);
}

export type CsvEntriesResult = {
  /** Berth code → millis, in file order. */
  entries: { code: string; millis: number }[];
  /** 1-based data rows refused, with the reason key `commonCharges:csv.errors.<code>`. */
  rejected: { row: number; code: 'columns' | 'millis' | 'duplicate' }[];
};

/**
 * Parses the CSV of a millesimal table: two columns, berth code and millis (header optional, detected when the
 * second cell of the first row is not a number). Separator auto-detected (`;` in Italian Excel).
 */
export function parseMillesimalCsv(text: string): CsvEntriesResult {
  const rows = parseCsv(text, detectDelimiter(text));
  const out: CsvEntriesResult = { entries: [], rejected: [] };
  const seen = new Set<string>();
  const first = rows[0];
  const hasHeader = first !== undefined && parseEntryMillis(first[1] ?? '') === null;
  rows.slice(hasHeader ? 1 : 0).forEach((r, i) => {
    const row = i + 1;
    const code = (r[0] ?? '').trim();
    if (r.length < 2 || !code) {
      out.rejected.push({ row, code: 'columns' });
      return;
    }
    const millis = parseEntryMillis(r[1] ?? '');
    if (millis === null) {
      out.rejected.push({ row, code: 'millis' });
      return;
    }
    const key = code.toUpperCase();
    if (seen.has(key)) {
      out.rejected.push({ row, code: 'duplicate' });
      return;
    }
    seen.add(key);
    out.entries.push({ code, millis });
  });
  return out;
}

/** Applies parsed CSV entries to the grid by berth code (case-insensitive); unknown codes are returned. */
export function applyCsvToGrid(
  rows: readonly GridRow[],
  entries: readonly { code: string; millis: number }[],
): { rows: GridRow[]; unknown: string[] } {
  const byCode = new Map(entries.map((e) => [e.code.toUpperCase(), e.millis]));
  const known = new Set(rows.map((r) => r.berthCode.toUpperCase()));
  const next = rows.map((r) => {
    const m = byCode.get(r.berthCode.toUpperCase());
    return m === undefined ? r : { ...r, text: String(m) };
  });
  return {
    rows: next,
    unknown: entries.filter((e) => !known.has(e.code.toUpperCase())).map((e) => e.code),
  };
}

// ---- holders editor rows ----

export type HolderDraft = { customer: CustomerChoice; text: string };

export function holderRows(rows: readonly HolderDraft[]): HolderRow[] {
  return rows.map((r) => ({ customerId: r.customer?.id ?? '', millis: parseMillis(r.text) }));
}
