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
