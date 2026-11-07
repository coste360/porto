// Parsing of amounts typed by staff into integer cents, mirrored from Go money.ParseCents (m5-plan §9.2): '.' or
// ',' as decimal separator, at most two decimals, optional sign, no thousands separators or exponents. Floats are
// never used: the digits are combined with BigInt and the result must be a safe integer.

const PATTERN = /^([+-]?)(\d+)(?:[.,](\d{1,2}))?$/;

/** Returns the cents of a decimal amount, or null when the text is not a valid amount. */
export function parseCents(input: string): number | null {
  const m = PATTERN.exec(input.trim());
  if (!m) return null;
  const sign = m[1] ?? '';
  const units = m[2] ?? '0';
  const frac = m[3] ?? '';
  const cents = BigInt(units) * 100n + BigInt(frac.padEnd(2, '0') || '0');
  const signed = sign === '-' ? -cents : cents;
  if (signed > BigInt(Number.MAX_SAFE_INTEGER) || signed < -BigInt(Number.MAX_SAFE_INTEGER))
    return null;
  return Number(signed);
}

/** Formats cents for an input field ("1234,56" in Italian, "1234.56" in English), without thousands separators. */
export function centsToInput(cents: number, locale: string = 'it'): string {
  const neg = cents < 0;
  const abs = BigInt(Math.abs(cents));
  const units = abs / 100n;
  const frac = (abs % 100n).toString().padStart(2, '0');
  const sep = locale.startsWith('it') ? ',' : '.';
  return `${neg ? '-' : ''}${units.toString()}${sep}${frac}`;
}
