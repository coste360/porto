// Client-side checks of Italian fiscal codes, VAT numbers and IBANs (m1-plan §4.4 "Customer rules", §5.6).
// They mirror the server rules so the form can warn early (both sides share the vectors in
// testdata/registry/fiscal-codes.json); the server checks again and is authoritative.

/** Upper case without spaces, dots and dashes (the stored form). */
export function normalizeCode(value: string): string {
  return value.replace(/[\s.-]+/g, '').toUpperCase();
}

// Value of a character in an odd (1-based) position of a personal fiscal code. Digits share the values of the
// letter with the same index (0 = A, 1 = B, ...).
const ODD_VALUES = [
  1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23,
];
// Omocodia letters replace digits when two codes would collide: L = 0 … V = 9.
const OMOCODIA = 'LMNPQRSTUV';
const MONTHS = 'ABCDEHLMPRST';

const isLetter = (c: string) => c >= 'A' && c <= 'Z';
const isDigit = (c: string) => c >= '0' && c <= '9';

function digitOrOmocodia(c: string): number | null {
  if (isDigit(c)) return c.charCodeAt(0) - 48;
  const i = OMOCODIA.indexOf(c);
  return i >= 0 ? i : null;
}

/**
 * A person's 16-character fiscal code: surname, name, year, month letter, day (1–31, +40 for women),
 * municipality and check character; omocodia letters accepted in the numeric positions.
 */
export function isValidPersonFiscalCode(value: string): boolean {
  const s = normalizeCode(value);
  if (s.length !== 16) return false;
  for (let i = 0; i < 6; i++) if (!isLetter(s[i]!)) return false;
  for (const i of [6, 7, 9, 10, 12, 13, 14]) if (digitOrOmocodia(s[i]!) === null) return false;
  if (!MONTHS.includes(s[8]!) || !isLetter(s[11]!) || !isLetter(s[15]!)) return false;
  let day = digitOrOmocodia(s[9]!)! * 10 + digitOrOmocodia(s[10]!)!;
  if (day > 40) day -= 40;
  if (day < 1 || day > 31) return false;
  let sum = 0;
  for (let i = 0; i < 15; i++) {
    const c = s[i]!;
    const v = isDigit(c) ? c.charCodeAt(0) - 48 : c.charCodeAt(0) - 65;
    // i is zero-based: even indexes are the odd (1st, 3rd, ...) positions.
    sum += i % 2 === 0 ? ODD_VALUES[v]! : v;
  }
  return String.fromCharCode(65 + (sum % 26)) === s[15];
}

/** 11 digits with the Italian VAT check digit (a company's numeric fiscal code uses the same rule). */
export function isValidItalianVatDigits(value: string): boolean {
  if (!/^\d{11}$/.test(value) || value.startsWith('0000000')) return false;
  let sum = 0;
  for (let i = 0; i < 11; i++) {
    let d = value.charCodeAt(i) - 48;
    if (i % 2 === 1) {
      d *= 2;
      if (d > 9) d -= 9;
    }
    sum += d;
  }
  return sum % 10 === 0;
}

/** A fiscal code for the customer kind: people 16 characters; companies 11 digits (VAT check) or 16. */
export function isValidFiscalCode(value: string, kind: 'person' | 'company'): boolean {
  const s = normalizeCode(value);
  if (kind === 'company' && s.length === 11) return isValidItalianVatDigits(s);
  return isValidPersonFiscalCode(s);
}

/** A VAT number: prefix IT or none → Italian check digit; another two-letter prefix → format only. */
export function isValidVatNumber(value: string): boolean {
  const s = normalizeCode(value);
  if (s.length >= 2 && isLetter(s[0]!) && isLetter(s[1]!)) {
    const rest = s.slice(2);
    if (s.startsWith('IT')) return isValidItalianVatDigits(rest);
    return /^[A-Z0-9]{2,13}$/.test(rest);
  }
  return isValidItalianVatDigits(s);
}

// IBAN length of common countries; other countries are checked for 15–34 characters.
const IBAN_LENGTHS: Record<string, number> = {
  AD: 24,
  AT: 20,
  BE: 16,
  BG: 22,
  CH: 21,
  CY: 28,
  CZ: 24,
  DE: 22,
  DK: 18,
  EE: 20,
  ES: 24,
  FI: 18,
  FR: 27,
  GB: 22,
  GR: 27,
  HR: 21,
  HU: 28,
  IE: 22,
  IS: 26,
  IT: 27,
  LI: 21,
  LT: 20,
  LU: 20,
  LV: 21,
  MC: 27,
  MT: 31,
  NL: 18,
  NO: 15,
  PL: 28,
  PT: 25,
  RO: 24,
  SE: 24,
  SI: 19,
  SK: 24,
  SM: 27,
  VA: 22,
};

/** ISO 13616 IBAN: country, check digits, country length, mod-97 = 1. */
export function isValidIban(value: string): boolean {
  const s = normalizeCode(value);
  if (!/^[A-Z]{2}\d{2}[A-Z0-9]{11,30}$/.test(s)) return false;
  const expected = IBAN_LENGTHS[s.slice(0, 2)];
  if (expected !== undefined && s.length !== expected) return false;
  let rest = 0;
  for (const c of s.slice(4) + s.slice(0, 4)) {
    // Letters expand to two digits (A = 10 ... Z = 35); the remainder stays small (no big numbers).
    rest = isDigit(c)
      ? (rest * 10 + c.charCodeAt(0) - 48) % 97
      : (rest * 100 + c.charCodeAt(0) - 55) % 97;
  }
  return rest === 1;
}
