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
