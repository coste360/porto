/** Display formatting. Inputs are integers (cents, cm) or ISO strings: no float arithmetic. */

/** Formats an RFC 3339 instant in the given locale and IANA timezone. */
export function formatDateTime(instant: string, locale: string, timeZone?: string): string {
  const date = new Date(instant);
  if (Number.isNaN(date.getTime())) return instant;
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'short',
    timeStyle: 'short',
    ...(timeZone ? { timeZone } : {}),
  }).format(date);
}

/** `YYYY-MM-DD` → `dd/MM/yyyy`, without going through Date (no timezone shift). */
export function formatIsoDate(isoDate: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(isoDate);
  if (!match) return isoDate;
  return `${match[3]}/${match[2]}/${match[1]}`;
}

function splitInt(
  value: number,
  scale: number,
): { negative: boolean; whole: string; frac: string } {
  if (!Number.isSafeInteger(value)) throw new RangeError(`not an integer: ${value}`);
  const negative = value < 0;
  const abs = Math.abs(value);
  const digits = String(scale).length - 1;
  const whole = Math.trunc(abs / scale);
  const frac = abs - whole * scale;
  return { negative, whole: String(whole), frac: String(frac).padStart(digits, '0') };
}

function groupThousands(whole: string, separator: string): string {
  return whole.replace(/\B(?=(\d{3})+(?!\d))/g, separator);
}

function separators(locale: string): { decimal: string; group: string } {
  const parts = new Intl.NumberFormat(locale).formatToParts(12345.6);
  return {
    decimal: parts.find((p) => p.type === 'decimal')?.value ?? ',',
    group: parts.find((p) => p.type === 'group')?.value ?? '.',
  };
}

/** Integer centimetres as metres with two decimals: 1250 → "12,50 m" (it). */
export function formatCm(cm: number, locale = 'it'): string {
  const { negative, whole, frac } = splitInt(cm, 100);
  const { decimal, group } = separators(locale);
  return `${negative ? '-' : ''}${groupThousands(whole, group)}${decimal}${frac} m`;
}

/** Integer cents as a currency amount: 123456 → "1.234,56 €" (it). */
export function formatCents(cents: number, currency = 'EUR', locale = 'it'): string {
  const { negative, whole, frac } = splitInt(cents, 100);
  // Intl only formats the symbol and layout; the digits come from integer math.
  const parts = new Intl.NumberFormat(locale, { style: 'currency', currency }).formatToParts(0);
  const { decimal, group } = separators(locale);
  const amount = `${groupThousands(whole, group)}${decimal}${frac}`;
  const symbol = parts.find((p) => p.type === 'currency')?.value ?? currency;
  const symbolFirst =
    parts.findIndex((p) => p.type === 'currency') < parts.findIndex((p) => p.type === 'integer');
  const sign = negative ? '-' : '';
  return symbolFirst ? `${sign}${symbol}${amount}` : `${sign}${amount} ${symbol}`;
}

/**
 * Parses a decimal string ("12,5", "-12.3456") to an integer scaled by 10^decimals, rounding half-even on the
 * extra digits. Digit-string arithmetic only (no float). Returns null for anything that is not a plain decimal.
 */
function parseScaled(text: string, decimals: number): number | null {
  const match = /^\s*([+-]?)(\d+)(?:[.,](\d*))?\s*$/.exec(text);
  if (!match) return null;
  const [, sign, whole, fracRaw = ''] = match;
  const kept = fracRaw.slice(0, decimals).padEnd(decimals, '0');
  const rest = fracRaw.slice(decimals);
  let value = BigInt(whole + kept);
  if (rest !== '' && /[1-9]/.test(rest)) {
    const first = rest.charCodeAt(0) - 48;
    const tail = /[1-9]/.test(rest.slice(1));
    const odd = value % 2n === 1n;
    if (first > 5 || (first === 5 && (tail || odd))) value += 1n;
  }
  if (sign === '-') value = -value;
  const n = Number(value);
  return Number.isSafeInteger(n) ? n : null;
}

/** Metres typed by the user ("12,50", "12.5", "7") → integer centimetres; at most two decimals, else null. */
export function parseMetresToCm(text: string): number | null {
  const match = /^\s*[+-]?\d+(?:[.,](\d*))?\s*$/.exec(text);
  if (!match || (match[1]?.length ?? 0) > 2) return null;
  return parseScaled(text, 2);
}

/** WGS84 degrees × 10^7 → exact decimal string: 123456789 → "12.3456789". */
export function formatE7(value: number): string {
  const { negative, whole, frac } = splitInt(value, 10_000_000);
  return `${negative ? '-' : ''}${whole}.${frac}`;
}

/** Decimal degrees typed by the user ("45.4371908", "12,3") → degrees × 10^7, half-even beyond 7 decimals. */
export function parseDecimalToE7(text: string): number | null {
  return parseScaled(text, 7);
}

// ---- M2 pricing inputs (m2-plan §7.1): integers only, digit-string arithmetic, never floats. ----

function trimmedFraction(value: number, scale: number, locale: string): string {
  const { negative, whole, frac } = splitInt(value, scale);
  const { decimal, group } = separators(locale);
  const f = frac.replace(/0+$/, '');
  return `${negative ? '-' : ''}${groupThousands(whole, group)}${f ? decimal + f : ''}`;
}

/** Basis points as a percentage: 2200 → "22%", 1250 → "12,5%" (it), 5 → "0,05%". */
export function formatBp(bp: number, locale = 'it'): string {
  return `${trimmedFraction(bp, 100, locale)}%`;
}

/** Thousandths as a quantity: 1500 → "1,5" (it), 1234567 → "1.234,567", 2000 → "2". */
export function formatQtyMilli(milli: number, locale = 'it'): string {
  return trimmedFraction(milli, 1000, locale);
}

/**
 * Normalises a number typed with Italian or English separators to "<int>[.<frac>]", or null. With both "."
 * and "," the last one is the decimal separator and the other groups thousands; with one kind only, it is the
 * decimal separator when it appears once and is followed by at most maxDecimals digits, groups thousands
 * (blocks of three digits) when it appears at least twice, and is refused as ambiguous otherwise ("1.234").
 */
function normaliseDecimal(text: string, maxDecimals: number): string | null {
  const t = text.trim();
  const match = /^([+-]?)(\d[\d.,]*)$/.exec(t);
  if (!match) return null;
  const sign = match[1] ?? '';
  const body = match[2] ?? '';
  const lastDot = body.lastIndexOf('.');
  const lastComma = body.lastIndexOf(',');
  let intPart: string = body;
  let frac = '';
  let groupSep = '';
  if (lastDot >= 0 && lastComma >= 0) {
    const dec = lastDot > lastComma ? '.' : ',';
    groupSep = dec === '.' ? ',' : '.';
    const at = body.lastIndexOf(dec);
    intPart = body.slice(0, at);
    frac = body.slice(at + 1);
    if (intPart.includes(dec)) return null;
  } else if (lastDot >= 0 || lastComma >= 0) {
    const sep = lastDot >= 0 ? '.' : ',';
    const pieces = body.split(sep);
    const tail = pieces[pieces.length - 1] ?? '';
    if (pieces.length === 2 && tail.length <= maxDecimals) {
      intPart = pieces[0] ?? '';
      frac = tail;
    } else if (pieces.length > 2) {
      groupSep = sep;
    } else {
      return null;
    }
  }
  if (groupSep) {
    const groups = intPart.split(groupSep);
    if (groups.some((g, i) => (i === 0 ? !/^\d{1,3}$/.test(g) : !/^\d{3}$/.test(g)))) return null;
    intPart = groups.join('');
  }
  if (!/^\d+$/.test(intPart) || !/^\d*$/.test(frac) || frac.length > maxDecimals) return null;
  return `${sign}${intPart}${frac ? '.' + frac : ''}`;
}

/**
 * Euros typed by the user ("12,50", "€ 1.234,56", "1,234.56", "-3") → integer cents; at most two decimals,
 * else null.
 */
export function parseEuroToCents(text: string): number | null {
  const n = normaliseDecimal(text.replace(/€|EUR/gi, ''), 2);
  return n === null ? null : parseScaled(n, 2);
}

/** A percentage typed by the user ("22", "22,5 %", "-10%") → integer basis points; at most two decimals. */
export function parsePercentToBp(text: string): number | null {
  const n = normaliseDecimal(text.replace(/%/g, ''), 2);
  return n === null ? null : parseScaled(n, 2);
}

/**
 * Normalises a number typed with the locale's own separators to "<int>[.<frac>]", or null. Strict and the
 * exact inverse of the locale formatting: the decimal separator appears at most once, the group separator only
 * in the integer part and only between blocks of three digits. "1.500" in `it` is 1500, "1.5" in `it` is refused.
 */
function normaliseLocaleDecimal(text: string, maxDecimals: number, locale: string): string | null {
  const { decimal, group } = separators(locale);
  const t = text.trim();
  const match = /^([+-]?)(\d[\d.,\s\u00a0\u202f]*)$/.exec(t);
  if (!match) return null;
  const sign = match[1] ?? '';
  const body = match[2] ?? '';
  const pieces = body.split(decimal);
  if (pieces.length > 2) return null;
  const intRaw = pieces[0] ?? '';
  const frac = pieces[1] ?? '';
  let intPart = intRaw;
  if (intRaw.includes(group)) {
    const groups = intRaw.split(group);
    if (groups.some((g, i) => (i === 0 ? !/^\d{1,3}$/.test(g) : !/^\d{3}$/.test(g)))) return null;
    intPart = groups.join('');
  }
  if (!/^\d+$/.test(intPart) || !/^\d*$/.test(frac) || frac.length > maxDecimals) return null;
  if (pieces.length === 2 && frac === '') return null;
  return `${sign}${intPart}${frac ? '.' + frac : ''}`;
}

/**
 * A quantity typed by the user in the given locale → integer thousandths; at most three decimals. The exact
 * inverse of formatQtyMilli: in `it` "1,5" → 1500 and "1.500" → 1500000; in `en` "1.5" → 1500 and "1,500" →
 * 1500000. A separator that does not fit the locale (e.g. "1.5" in `it`) is refused rather than guessed.
 */
export function parseQtyToMilli(text: string, locale = 'it'): number | null {
  const n = normaliseLocaleDecimal(text, 3, locale);
  return n === null ? null : parseScaled(n, 3);
}

// ---- M7 reports (m7-plan §10.2): integers only. ----

/** Integer square centimetres as square metres with two decimals, half-even: 125000 → "12,50 m²" (it). */
export function formatCm2(cm2: number, locale = 'it'): string {
  if (!Number.isSafeInteger(cm2)) throw new RangeError(`not an integer: ${cm2}`);
  // 1 m² = 10 000 cm²: keep two decimals (hundredths of m² = 100 cm²), rounding half-even on the rest.
  const negative = cm2 < 0;
  const abs = BigInt(Math.abs(cm2));
  let q = abs / 100n;
  const r = abs % 100n;
  if (r > 50n || (r === 50n && q % 2n === 1n)) q += 1n;
  const { whole, frac } = splitInt(Number(q), 100);
  const { decimal, group } = separators(locale);
  return `${negative && q > 0n ? '-' : ''}${groupThousands(whole, group)}${decimal}${frac} m²`;
}

/** A whole number with the locale's grouping: 12345 → "12.345" (it). */
export function formatInteger(value: number, locale = 'it'): string {
  const { negative, whole } = splitInt(value, 1);
  const { group } = separators(locale);
  return `${negative ? '-' : ''}${groupThousands(whole, group)}`;
}

/** Units of report and tile values (OpenAPI ReportUnit). */
export type ReportValueUnit =
  | 'cents'
  | 'bp'
  | 'count'
  | 'cm'
  | 'cm2'
  | 'cents_per_m'
  | 'cents_per_night'
  | 'berth_night'
  | 'cm_night'
  | 'cm2_night';

/**
 * Formats a report or tile value by its unit; null (ratio without denominator, hidden or unpriced amount) → "—".
 * Per-metre and per-night amounts and unit-nights carry no suffix: the column or tile label names the unit.
 */
export function formatReportValue(
  value: number | null | undefined,
  unit: ReportValueUnit,
  locale = 'it',
): string {
  if (value === null || value === undefined) return '—';
  switch (unit) {
    case 'cents':
    case 'cents_per_m':
    case 'cents_per_night':
      return formatCents(value, 'EUR', locale);
    case 'bp':
      return formatBp(value, locale);
    case 'cm':
    case 'cm_night':
      return formatCm(value, locale);
    case 'cm2':
    case 'cm2_night':
      return formatCm2(value, locale);
    case 'count':
    case 'berth_night':
      return formatInteger(value, locale);
  }
}
