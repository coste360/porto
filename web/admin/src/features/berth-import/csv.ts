// Browser-side CSV helpers of the berth import wizard (m1-plan §5.5, D23). The server parses the file again
// and is authoritative; these only drive the preview, the column mapping and the report download.
import type { Schemas } from '@porto/shared/api';

export type Delimiter = NonNullable<Schemas['BerthImportRequest']['delimiter']>;
export type MappingField = keyof Schemas['BerthImportMapping'];
export type ColumnMapping = Partial<Record<MappingField, number>>;

/** 4 MiB (contract x-porto-max-body-bytes of importBerthsCsv) and 5 000 rows (D23). */
export const CSV_MAX_BYTES = 4 * 1024 * 1024;
export const CSV_MAX_ROWS = 5000;

export const DELIMITER_CHARS: Record<Delimiter, string> = { comma: ',', semicolon: ';', tab: '\t' };

export const MAPPING_FIELDS = [
  'code',
  'pier',
  'length',
  'width',
  'depth',
  'price_class',
  'side',
  'mooring_type',
] as const satisfies readonly MappingField[];
export const REQUIRED_FIELDS: readonly MappingField[] = ['code', 'pier', 'length', 'width'];

/** Parses CSV text (RFC 4180: quoted fields, doubled quotes, CRLF or LF). Blank lines are skipped. */
export function parseCsv(text: string, delimiter: Delimiter): string[][] {
  const sep = DELIMITER_CHARS[delimiter];
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  const src = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
  const pushRow = () => {
    row.push(field);
    field = '';
    if (row.length > 1 || row[0]!.trim() !== '') rows.push(row);
    row = [];
  };
  for (let i = 0; i < src.length; i++) {
    const ch = src[i]!;
    if (quoted) {
      if (ch === '"') {
        if (src[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
        }
      } else {
        field += ch;
      }
      continue;
    }
    if (ch === '"' && field === '') quoted = true;
    else if (ch === sep) {
      row.push(field);
      field = '';
    } else if (ch === '\n') pushRow();
    else if (ch === '\r') {
      if (src[i + 1] === '\n') i++;
      pushRow();
    } else field += ch;
  }
  if (field !== '' || row.length > 0) pushRow();
  return rows;
}

/** Picks the delimiter that splits the first lines into the most, and the same number of, columns. */
export function detectDelimiter(text: string): Delimiter {
  const lines = text
    .replace(/^\uFEFF/, '')
    .split(/\r?\n/)
    .filter((l) => l.trim() !== '')
    .slice(0, 10);
  let best: Delimiter = 'comma';
  let bestScore = -1;
  for (const d of ['semicolon', 'tab', 'comma'] as const) {
    const counts = parseCsv(lines.join('\n'), d).map((r) => r.length);
    if (counts.length === 0) continue;
    const first = counts[0]!;
    if (first < 2) continue;
    const consistent = counts.every((c) => c === first);
    const score = first * (consistent ? 2 : 1);
    if (score > bestScore) {
      best = d;
      bestScore = score;
    }
  }
  return best;
}

/** Lower case, accents folded, punctuation as spaces. */
export function normalizeHeader(h: string): string {
  return h
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();
}

// Italian and English header names; the first matching field wins, exact names before prefixes.
const HEADER_NAMES: Record<MappingField, string[]> = {
  code: [
    'codice',
    'code',
    'posto',
    'posto barca',
    'numero',
    'numero posto',
    'n',
    'nr',
    'berth',
    'berth code',
    'id',
  ],
  pier: ['pontile', 'pier', 'molo', 'banchina', 'dock', 'pontoon', 'struttura'],
  length: ['lunghezza', 'length', 'lung', 'loa', 'l', 'lunghezza m', 'length m'],
  width: ['larghezza', 'width', 'larg', 'baglio', 'beam', 'w', 'larghezza m', 'width m'],
  depth: ['profondita', 'depth', 'fondale', 'pescaggio', 'draft', 'draught'],
  price_class: ['classe', 'classe tariffaria', 'price class', 'class', 'categoria', 'tariffa'],
  side: ['lato', 'side'],
  mooring_type: ['ormeggio', 'tipo ormeggio', 'mooring', 'mooring type', 'tipo'],
};

/** Guesses the column of each field from the header row. */
export function guessMapping(header: string[]): ColumnMapping {
  const norm = header.map(normalizeHeader);
  const used = new Set<number>();
  const mapping: ColumnMapping = {};
  const assign = (field: MappingField, match: (h: string, names: string[]) => boolean) => {
    if (mapping[field] !== undefined) return;
    const idx = norm.findIndex((h, i) => !used.has(i) && match(h, HEADER_NAMES[field]));
    if (idx >= 0) {
      mapping[field] = idx;
      used.add(idx);
    }
  };
  for (const f of MAPPING_FIELDS) assign(f, (h, names) => names.includes(h));
  for (const f of MAPPING_FIELDS) {
    assign(f, (h, names) => names.some((n) => n.length >= 3 && h.startsWith(n)));
  }
  return mapping;
}

/** Applies a mapping remembered by header names to the current header row. */
export function mappingFromNames(
  header: string[],
  saved: Partial<Record<MappingField, string>>,
): ColumnMapping {
  const norm = header.map(normalizeHeader);
  const mapping: ColumnMapping = {};
  for (const f of MAPPING_FIELDS) {
    const name = saved[f];
    if (name === undefined) continue;
    const idx = norm.indexOf(normalizeHeader(name));
    if (idx >= 0) mapping[f] = idx;
  }
  return mapping;
}

export function mappingToNames(
  header: string[],
  mapping: ColumnMapping,
): Partial<Record<MappingField, string>> {
  const out: Partial<Record<MappingField, string>> = {};
  for (const f of MAPPING_FIELDS) {
    const idx = mapping[f];
    if (idx !== undefined && header[idx] !== undefined) out[f] = header[idx];
  }
  return out;
}

/** The contract mapping, or null while a required field has no column. */
export function toContractMapping(mapping: ColumnMapping): Schemas['BerthImportMapping'] | null {
  if (REQUIRED_FIELDS.some((f) => mapping[f] === undefined)) return null;
  const out: Schemas['BerthImportMapping'] = {
    code: mapping.code!,
    pier: mapping.pier!,
    length: mapping.length!,
    width: mapping.width!,
  };
  for (const f of ['depth', 'price_class', 'side', 'mooring_type'] as const) {
    if (mapping[f] !== undefined) out[f] = mapping[f];
  }
  return out;
}

/** Distinct pier codes of the data rows in the mapped column, in file order. */
export function piersInFile(rows: string[][], mapping: ColumnMapping): string[] {
  const idx = mapping.pier;
  if (idx === undefined) return [];
  const out: string[] = [];
  for (const r of rows) {
    const p = (r[idx] ?? '').trim();
    if (p && !out.includes(p)) out.push(p);
  }
  return out;
}

const csvCell = (v: string | number) => {
  const s = String(v);
  return /[",\r\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

/** The dry-run or apply report as a CSV (one line per rejected field, duplicate and planned berth). */
export function reportToCsv(report: Schemas['BerthImportReport']): string {
  const lines = [['result', 'row', 'code', 'pier', 'side', 'field', 'detail']];
  for (const r of report.rejected) {
    for (const e of r.errors) lines.push(['rejected', String(r.row), '', '', '', e.field, e.code]);
  }
  for (const d of report.duplicates) {
    lines.push(['duplicate', String(d.row), d.code, '', '', 'code', d.reason]);
  }
  for (const p of report.planned) {
    lines.push(['planned', String(p.row), p.code, p.pier, p.side, '', '']);
  }
  return lines.map((l) => l.map(csvCell).join(',')).join('\r\n') + '\r\n';
}
