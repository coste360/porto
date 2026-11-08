import type { Schemas } from '@porto/shared/api';

export const ONLINE_STATUSES: Schemas['OnlineBookingStatus'][] = [
  'held',
  'pending_payment',
  'awaiting_approval',
  'confirmed',
  'arrived',
  'departed',
  'cancelled',
  'expired',
  'no_show',
];

export const ONLINE_CHANNELS: Schemas['OnlineChannel'][] = ['online', 'external'];

export const BOOKING_DOCUMENT_TYPES: Schemas['BookingDocumentType'][] = [
  'insurance_policy',
  'boating_licence',
  'navigation_licence',
  'registration_certificate',
  'identity_card',
];

export const TEXT_KINDS: Schemas['BookingTextKind'][] = ['terms', 'privacy', 'marketing'];

/** Remaining time as "2 h 05 min" / "12 min" (minutes rounded down); "" when past. */
export function remaining(atIso: string, nowMs: number): string {
  const ms = Date.parse(atIso) - nowMs;
  if (!(ms > 0)) return '';
  const min = Math.floor(ms / 60000);
  const h = Math.floor(min / 60);
  return h > 0 ? `${h} h ${String(min % 60).padStart(2, '0')} min` : `${min} min`;
}

/** Parses "key=value" lines (blank lines and lines without "=" are ignored). */
export function parsePairs(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split('\n')) {
    const i = line.indexOf('=');
    if (i <= 0) continue;
    const k = line.slice(0, i).trim();
    const v = line.slice(i + 1).trim();
    if (k) out[k] = v;
  }
  return out;
}

export function formatPairs(m: Record<string, string>): string {
  return Object.entries(m)
    .map(([k, v]) => `${k}=${v}`)
    .join('\n');
}

/** Embed snippets of the booking page (m8-plan §10): the script widget, a plain iframe and the direct link. */
export function embedSnippets(origin: string, slug: string, lang: string) {
  const page = `${origin}/b/${slug}`;
  return {
    script: `<div data-porto="${slug}" data-lang="${lang}"></div>\n<script src="${origin}/embed.js" async></script>`,
    iframe: `<iframe src="${page}?embed=1&lang=${lang}" allow="payment" title="${slug}" loading="lazy" width="100%" height="900"></iframe>`,
    link: page,
  };
}
