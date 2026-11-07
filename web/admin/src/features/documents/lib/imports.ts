// CSV import wizard (spec 11.6, m4-plan §3.7, D27): entity → file → column mapping → dry-run report → apply →
// undo. The browser parses only for the mapping preview; the server parses again and is authoritative.
import type { Schemas } from '@porto/shared/api';

import {
  CSV_MAX_BYTES,
  CSV_MAX_ROWS,
  detectDelimiter,
  normalizeHeader,
  parseCsv,
} from '../../berth-import/csv';

export type ImportEntity = Schemas['ImportEntity'];
export type Delimiter = NonNullable<Schemas['ImportInput']['delimiter']>;

export { CSV_MAX_BYTES, CSV_MAX_ROWS, detectDelimiter, parseCsv };

/**
 * Target fields per entity (keys `imports:m4.fields.<field>`), exactly the names the server handlers accept
 * (`Fields()` of each csvimport handler); `required` are the ones the wizard asks before the dry run (the server
 * checks the rest, e.g. a customer by fiscal code or id).
 */
export const IMPORT_FIELDS: Record<ImportEntity, { required: string[]; optional: string[] }> = {
  customers_vessels: {
    required: [],
    optional: [
      'kind',
      'first_name',
      'last_name',
      'company_name',
      'fiscal_code',
      'vat_number',
      'email',
      'phone',
      'address_line1',
      'postal_code',
      'city',
      'province',
      'country',
      'locale',
      'vessel_name',
      'vessel_registration',
      'vessel_loa_cm',
      'vessel_beam_cm',
      'vessel_draft_cm',
      'vessel_hull_type',
    ],
  },
  contracts: {
    required: ['ref', 'berth_code', 'term', 'starts_on', 'total'],
    optional: [
      'customer_fiscal_code',
      'customer_id',
      'vessel_registration',
      'vessel_id',
      'ends_on',
      'due_on',
      'amount',
      'status',
    ],
  },
  rights: {
    required: ['berth_code', 'kind', 'from_on', 'holder_tax_id', 'share_millis'],
    optional: ['to_on', 'title_deed', 'title_number', 'title_notary', 'title_date'],
  },
  deadlines: {
    required: ['kind', 'due_on'],
    optional: ['customer_fiscal_code', 'vessel_registration', 'note'],
  },
};

/** Permission needed to import an entity (m4-plan §7.3.5). */
export const IMPORT_PERMISSION: Record<ImportEntity, Schemas['Permission']> = {
  customers_vessels: 'customers.write',
  contracts: 'contracts.write',
  rights: 'rights.manage',
  deadlines: 'customers.write',
};

export type Mapping = Record<string, number | undefined>;

/** Guesses the mapping by normalised header names equal to the field name (or to its words). */
export function guessImportMapping(entity: ImportEntity, header: readonly string[]): Mapping {
  const fields = [...IMPORT_FIELDS[entity].required, ...IMPORT_FIELDS[entity].optional];
  const norm = header.map((h) => normalizeHeader(h).replace(/\s+/g, '_'));
  const out: Mapping = {};
  for (const f of fields) {
    const i = norm.indexOf(f);
    if (i >= 0) out[f] = i;
  }
  return out;
}

export function missingRequired(entity: ImportEntity, mapping: Mapping): string[] {
  return IMPORT_FIELDS[entity].required.filter((f) => mapping[f] === undefined);
}

/** API mapping: field → column index (0-based), only mapped fields. */
export function toApiMapping(mapping: Mapping): Record<string, number> {
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(mapping)) if (v !== undefined) out[k] = v;
  return out;
}

export type FileCheck =
  | { ok: true; delimiter: Delimiter; rows: string[][] }
  | { ok: false; error: 'tooLarge' | 'tooManyRows' | 'empty' };

export function checkCsvText(text: string, bytes: number, delimiter?: Delimiter): FileCheck {
  if (bytes > CSV_MAX_BYTES) return { ok: false, error: 'tooLarge' };
  const d = delimiter ?? detectDelimiter(text);
  const rows = parseCsv(text, d);
  if (rows.length === 0) return { ok: false, error: 'empty' };
  if (rows.length > CSV_MAX_ROWS + 1) return { ok: false, error: 'tooManyRows' };
  return { ok: true, delimiter: d, rows };
}

/** One-line summary counts of a report. */
export function reportSummary(r: Schemas['ImportReport']) {
  return {
    rows: r.rows,
    valid: r.valid,
    rejected: r.rejected.length,
    duplicates: r.duplicates.length,
    // The server applies a file all or nothing (one transaction): every row must be valid.
    applicable: r.valid > 0 && r.rejected.length === 0,
  };
}
