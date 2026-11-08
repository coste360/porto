// Pure helpers of the yard catalogue screens (m10-plan §14.1): formula issue messages, band table rows and the
// text of a quantity in milli. No API calls, no formatting of money here.
import type { FormulaIssue, FormulaTableRow } from '@porto/shared/api';
import { parseQtyToMilli } from '@porto/shared/lib/format';

/** Issue codes with a message in yardCatalog:issues (m10-plan §3.4 plus no_price and the test case). */
export const ISSUE_CODES = [
  'syntax',
  'unknown_variable',
  'unknown_function',
  'unknown_table',
  'arity',
  'type_mismatch',
  'enum_value_unknown',
  'formula_too_complex',
  'derived_cycle',
  'div_by_zero',
  'missing_variable',
  'overflow',
  'band_no_row',
  'negative_quantity',
  'negative_price',
  'out_of_range',
  'no_price',
  'test_case_mismatch',
] as const;

export type IssueCode = (typeof ISSUE_CODES)[number];

/** The i18n key of an issue code (unknown codes use the generic message). */
export function issueKey(code: string): string {
  return (ISSUE_CODES as readonly string[]).includes(code)
    ? `yardCatalog:issues.${code}`
    : 'yardCatalog:issues.unknown';
}

/** The issues of a field (exact name, or its prefix "lines[2]." for the lines of a package). */
export function issuesFor(issues: readonly FormulaIssue[], field: string): FormulaIssue[] {
  return issues.filter((i) => i.field === field || (i.field ?? '').startsWith(`${field}.`));
}

/** The 1-based column of an issue position in a formula (positions are byte offsets, ASCII formulas). */
export function column(pos: number | undefined): number | null {
  return pos === undefined ? null : pos + 1;
}

/** Parses the rows typed in the band table editor: "up to;value" per line, an empty "up to" is the open row. */
export function parseBandRows(text: string, locale = 'it'): FormulaTableRow[] | null {
  const rows: FormulaTableRow[] = [];
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const [upTo, value] = line.split(';').map((s) => s.trim());
    if (value === undefined) return null;
    const v = parseQtyToMilli(value, locale);
    if (v === null) return null;
    if (!upTo) {
      rows.push({ up_to_milli: null, value_milli: v });
      continue;
    }
    const u = parseQtyToMilli(upTo, locale);
    if (u === null) return null;
    rows.push({ up_to_milli: u, value_milli: v });
  }
  return rows.length ? rows : null;
}

/** A translated name for the API: English only when typed. */
export function i18nBody(it: string, en: string): { it: string; en?: string } {
  return en.trim() ? { it: it.trim(), en: en.trim() } : { it: it.trim() };
}
