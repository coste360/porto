import type { TFunction } from 'i18next';

import { downloadFile } from '../../desktop/files';

/** Owner share in per-mille (1000 = 100 %) → "60" or "33,3" (one decimal at most). */
export function formatShare(millis: number | null | undefined, locale = 'it'): string {
  if (millis === null || millis === undefined || !Number.isSafeInteger(millis)) return '';
  const whole = Math.trunc(millis / 10);
  const tenth = millis - whole * 10;
  if (tenth === 0) return String(whole);
  return `${whole}${locale.startsWith('en') ? '.' : ','}${tenth}`;
}

/** "60", "33,3", "33.3" (percent, at most one decimal) → per-mille integer, or null. */
export function parseSharePercent(text: string): number | null {
  const m = /^\s*(\d{1,3})(?:[.,](\d))?\s*$/.exec(text);
  if (!m) return null;
  const value = Number(m[1]) * 10 + (m[2] ? Number(m[2]) : 0);
  return value >= 1 && value <= 1000 ? value : null;
}

/** "60, 15 30" → [60, 30, 15] (unique, descending), or null when a value is not a whole day count 0..730. */
export function parseLeadDays(text: string): number[] | null {
  const parts = text.split(/[\s,;]+/).filter(Boolean);
  const days: number[] = [];
  for (const p of parts) {
    if (!/^\d{1,3}$/.test(p)) return null;
    const n = Number(p);
    if (n > 730) return null;
    if (!days.includes(n)) days.push(n);
  }
  return days.sort((a, b) => b - a);
}

/** Customer name shown in the UI: anonymised customers have an empty name from the API (D15). */
export function customerName(
  t: TFunction,
  c: { display_name: string; anonymized_at: string | null },
): string {
  if (c.anonymized_at || c.display_name.trim() === '') return t('customers:anonymized');
  return c.display_name;
}

/** Saves a blob as a file (GDPR export, import report), through the desktop bridge when present. */
export function downloadBlob(filename: string, blob: Blob): void {
  downloadFile(filename, blob);
}
