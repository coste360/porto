import type {
  PriceBand,
  PriceListDocument,
  PriceListIssue,
  PriceListSave,
  PriceListSettings,
  PriceRule,
  RatePeriod,
  Schemas,
} from '@porto/shared/api';

import { PERIOD_COLORS } from '../lib/enums';

// Price list document model of the editor (m2-plan §12 D15): the editable part of a draft as the API's
// PriceListSave; saved rows carry `id`, new rows a client `key`; the full document is saved with If-Match.

export type EditorDoc = PriceListSave;
export type RowRef = { id?: string; key?: string };

let keySeq = 0;

/** A fresh client key, unique in the session (pattern ^[A-Za-z0-9_:-]+$). */
export function newKey(prefix: 'p' | 'b' | 'r'): string {
  keySeq += 1;
  return `${prefix}:${Date.now().toString(36)}${keySeq.toString(36)}`;
}

/** Identity of a row: its id once saved, its key before. */
export function refOf(row: RowRef): string {
  return row.id ?? row.key ?? '';
}

export function sameRef(a: RowRef, b: RowRef): boolean {
  return refOf(a) !== '' && refOf(a) === refOf(b);
}

/** Engine settings defaults (m2-plan §2.3). */
export function defaultSettings(): PriceListSettings {
  return {
    dimension_source: 'vessel',
    length_rounding: { step_cm: 10, mode: 'up' },
    min_beam_ratio: { num: 1, den: 3 },
    split: { '*': 'pro_rata_daily' },
    rounding: { '*': 'none' },
    deposits: {},
    season_window: null,
    primary_items: {},
    publish_matrix: false,
  };
}

function stripRule(r: PriceRule): PriceRule {
  // seq is read-only (assigned by the server on save).
  const { seq: _seq, ...rest } = r;
  return rest;
}

/** The editable part of a server document. */
export function fromDocument(d: PriceListDocument): EditorDoc {
  return {
    code: d.code,
    name: d.name,
    valid_from: d.valid_from,
    valid_to: d.valid_to,
    contract_types: [...d.contract_types],
    settings: { ...defaultSettings(), ...d.settings },
    periods: d.periods.map((p) => ({ ...p, ranges: p.ranges.map((r) => ({ ...r })) })),
    bands: d.bands.map((b) => ({ ...b })),
    rules: d.rules.map(stripRule),
  };
}

function prune<T>(value: T): T {
  if (Array.isArray(value)) return value.map(prune) as T;
  if (value && typeof value === 'object') {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value)) {
      if (v === undefined) continue;
      out[k] = prune(v);
    }
    return out as T;
  }
  return value;
}

/**
 * Save body: every row keeps its id, or gets a key when it has none; no read-only fields; no undefined
 * values. The order of periods, bands and rules is the document order (band order is meaningful).
 */
export function toSaveBody(doc: EditorDoc): PriceListSave {
  const withRef = <T extends RowRef>(row: T, prefix: 'p' | 'b' | 'r'): T =>
    row.id ? { ...row, key: undefined } : { ...row, key: row.key ?? newKey(prefix) };
  return prune({
    ...doc,
    periods: doc.periods.map((p) => withRef(p, 'p')),
    bands: doc.bands.map((b) => withRef(b, 'b')),
    rules: doc.rules.map((r) => withRef(stripRule(r), 'r')),
  });
}

/** Stable comparison of two documents (key order independent). */
export function canonical(value: unknown): string {
  const sort = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(sort);
    if (v && typeof v === 'object') {
      return Object.fromEntries(
        Object.entries(v as Record<string, unknown>)
          .filter(([, x]) => x !== undefined)
          .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
          .map(([k, x]) => [k, sort(x)]),
      );
    }
    return v;
  };
  return JSON.stringify(sort(value));
}

export function isDirty(doc: EditorDoc, saved: EditorDoc): boolean {
  return canonical(doc) !== canonical(saved);
}

/** Applies the id_map of a save response to rows that still carry the key (local edits after the save). */
export function applyIdMap(doc: EditorDoc, idMap: Record<string, string>): EditorDoc {
  const map = <T extends RowRef>(row: T): T =>
    !row.id && row.key && idMap[row.key] ? { ...row, id: idMap[row.key], key: undefined } : row;
  return {
    ...doc,
    periods: doc.periods.map(map),
    bands: doc.bands.map(map),
    rules: doc.rules.map(map),
  };
}

// ---------------------------------------------------------------------------------------------------------
// Bands

export type BandRange = { code: string; fromCm: number; toCm: number | null };

/**
 * "da–a" of every band (§12 D17): inclusive upper bounds; a band starts 1 cm above the previous one (the
 * first at 0); a band without max_cm is open-ended. Bands keep the document order.
 */
export function bandRanges(bands: PriceBand[]): BandRange[] {
  let prev: number | null = null;
  return bands.map((b) => {
    const fromCm: number = prev === null ? 0 : prev + 1;
    const toCm = b.max_cm ?? null;
    if (toCm !== null) prev = toCm;
    return { code: b.code, fromCm, toCm };
  });
}

/** Renames the band at `index` and every length_band amount keyed by its old code. */
export function renameBand(doc: EditorDoc, index: number, to: string): EditorDoc {
  const from = doc.bands[index]?.code;
  if (from === undefined || from === to) return doc;
  // Another band with the old code keeps the amounts (duplicates are reported by the validation).
  const shared = doc.bands.some((b, i) => i !== index && b.code === from);
  return {
    ...doc,
    bands: doc.bands.map((b, i) => (i === index ? { ...b, code: to } : b)),
    rules:
      from === '' || shared
        ? doc.rules
        : doc.rules.map((r) =>
            r.basis === 'length_band' && r.amounts && from in r.amounts
              ? { ...r, amounts: renameKey(r.amounts, from, to) }
              : r,
          ),
  };
}

function renameKey<V>(obj: Record<string, V>, from: string, to: string): Record<string, V> {
  return Object.fromEntries(Object.entries(obj).map(([k, v]) => [k === from ? to : k, v]));
}

/** Renames the period at `index` and every rule scope that names its old code. */
export function renamePeriod(doc: EditorDoc, index: number, to: string): EditorDoc {
  const from = doc.periods[index]?.code;
  if (from === undefined || from === to) return doc;
  const shared = doc.periods.some((p, i) => i !== index && p.code === from);
  return {
    ...doc,
    periods: doc.periods.map((p, i) => (i === index ? { ...p, code: to } : p)),
    rules:
      from === '' || shared
        ? doc.rules
        : doc.rules.map((r) =>
            r.scope?.periods?.includes(from)
              ? {
                  ...r,
                  scope: { ...r.scope, periods: r.scope.periods.map((c) => (c === from ? to : c)) },
                }
              : r,
          ),
  };
}

// ---------------------------------------------------------------------------------------------------------
// Periods and the year timeline

const pad = (n: number) => String(n).padStart(2, '0');

function isoOf(year: number, month: number, day: number): string {
  return `${year}-${pad(month)}-${pad(day)}`;
}

export function daysInMonth(year: number, month: number): number {
  return new Date(Date.UTC(year, month, 0)).getUTCDate();
}

/** Period of a night: an event covering it, else a season (§2.4 step 1); first match in document order. */
export function periodOfNight(periods: RatePeriod[], date: string): RatePeriod | null {
  const covers = (p: RatePeriod) => p.ranges.some((r) => r.from <= date && date <= r.to);
  return (
    periods.find((p) => p.kind === 'event' && covers(p)) ??
    periods.find((p) => p.kind === 'season' && covers(p)) ??
    null
  );
}

/** Colour of a period: its own, else by position. */
export function periodColor(periods: RatePeriod[], p: RatePeriod): Schemas['PeriodColor'] {
  if (p.color) return p.color;
  const i = Math.max(0, periods.indexOf(p));
  return PERIOD_COLORS[i % PERIOD_COLORS.length]!;
}

export type TimelineDay = {
  date: string;
  day: number;
  period: string | null;
  color: Schemas['PeriodColor'] | null;
  inValidity: boolean;
};
export type TimelineMonth = { month: number; days: TimelineDay[] };

/** Twelve months of a year, each day with its period (string comparison of ISO dates, no time zones). */
export function yearTimeline(
  periods: RatePeriod[],
  year: number,
  validity: { from: string; to: string },
): TimelineMonth[] {
  const months: TimelineMonth[] = [];
  for (let m = 1; m <= 12; m++) {
    const days: TimelineDay[] = [];
    for (let d = 1; d <= daysInMonth(year, m); d++) {
      const date = isoOf(year, m, d);
      const p = periodOfNight(periods, date);
      days.push({
        date,
        day: d,
        period: p?.code ?? null,
        color: p ? periodColor(periods, p) : null,
        inValidity: validity.from <= date && date <= validity.to,
      });
    }
    months.push({ month: m, days });
  }
  return months;
}

/** Years touched by the validity range (at most six, for the timeline year selector). */
export function validityYears(from: string, to: string): number[] {
  const a = Number(from.slice(0, 4));
  const b = Number(to.slice(0, 4));
  if (!Number.isInteger(a) || !Number.isInteger(b) || b < a) return Number.isInteger(a) ? [a] : [];
  const out: number[] = [];
  for (let y = a; y <= Math.min(b, a + 5); y++) out.push(y);
  return out;
}

// ---------------------------------------------------------------------------------------------------------
// Rules

export function rulesOfStage(rules: PriceRule[], stage: Schemas['PricingStage']): PriceRule[] {
  return rules.filter((r) => r.stage === stage);
}

/** A new rule with the defaults of its stage. */
export function newRule(stage: Schemas['PricingStage'], name: string): PriceRule {
  const base: PriceRule = {
    key: newKey('r'),
    stage,
    name,
    active: true,
    priority: 0,
    stackable: stage !== 'base',
    scope: {},
  };
  if (stage === 'base') return { ...base, basis: 'per_meter', unit: 'night', stackable: false };
  if (stage === 'discount')
    return { ...base, discount_kind: 'automatic', effect: { percent_bp: 1000 } };
  return { ...base, effect: { percent_bp: 1000 } };
}

/** Replaces the row with the same identity, or appends it. */
export function upsertRule(doc: EditorDoc, rule: PriceRule): EditorDoc {
  const i = doc.rules.findIndex((r) => sameRef(r, rule));
  const rules = [...doc.rules];
  if (i >= 0) rules[i] = rule;
  else rules.push(rule);
  return { ...doc, rules };
}

export function removeRule(doc: EditorDoc, ref: RowRef): EditorDoc {
  return { ...doc, rules: doc.rules.filter((r) => !sameRef(r, ref)) };
}

/** A copy of a rule as a new row (new key, no id). */
export function duplicateRule(rule: PriceRule, suffix: string): PriceRule {
  const { id: _id, ...rest } = stripRule(rule);
  return { ...rest, key: newKey('r'), name: `${rule.name} ${suffix}`.slice(0, 120) };
}

// ---------------------------------------------------------------------------------------------------------
// Issues

export type IssueTarget = {
  tab: 'general' | 'periods' | 'bands' | 'rules';
  ref?: string;
  field?: string;
};

/** Where an issue points to in the editor (click → field). */
export function issueTarget(issue: PriceListIssue): IssueTarget {
  const { kind, id, field } = issue.ref;
  const at = (tab: IssueTarget['tab']): IssueTarget => ({
    tab,
    ...(id ? { ref: id } : {}),
    ...(field ? { field } : {}),
  });
  switch (kind) {
    case 'period':
      return at('periods');
    case 'band':
      return at('bands');
    case 'rule':
    case 'item':
      return at('rules');
    default:
      return at('general');
  }
}

export function issueCounts(issues: PriceListIssue[]): { errors: number; warnings: number } {
  return {
    errors: issues.filter((i) => i.severity === 'error').length,
    warnings: issues.filter((i) => i.severity === 'warning').length,
  };
}
