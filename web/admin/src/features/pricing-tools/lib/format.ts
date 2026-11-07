import {
  formatBp,
  formatCents,
  formatCm,
  formatIsoDate,
  formatQtyMilli,
} from '@porto/shared/lib/format';

// Display of the integer values of a breakdown, a matrix and a trace step. Inputs are integers or the integer
// strings of trace params: nothing is computed here.

const INT = /^-?\d+$/;
const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;

/** Locale used by the shared number formatters for an i18next language ("it-IT" → "it"). */
export function numberLocale(lang: string): string {
  return lang.startsWith('en') ? 'en' : 'it';
}

export function money(cents: number | null | undefined, lang: string): string {
  return cents === null || cents === undefined ? '' : formatCents(cents, 'EUR', numberLocale(lang));
}

/** A trace param value shown by its key suffix: `_cents` money, `_cm` metres, `_bp` percent, `_milli` qty. */
export function formatTraceParam(key: string, value: string, lang: string): string {
  const locale = numberLocale(lang);
  if (INT.test(value)) {
    const n = Number(value);
    if (Number.isSafeInteger(n)) {
      if (key.endsWith('_cents')) return formatCents(n, 'EUR', locale);
      if (key.endsWith('_cm')) return formatCm(n, locale);
      if (key.endsWith('_bp')) return formatBp(n, locale);
      if (key.endsWith('_milli')) return formatQtyMilli(n, locale);
    }
  }
  if (ISO_DATE.test(value)) return formatIsoDate(value);
  return value;
}
