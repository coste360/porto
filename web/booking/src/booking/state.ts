// State of the booking flow (m8-plan §12.1). Each step is a URL hash (#search, #berth, #extras, #details,
// #payment) so that back and forward work; the state lives in sessionStorage (memory fallback) per marina, so a
// reload, the return from the provider or the iframe reloaded by the host page resumes the same booking.
import type { BookingSearchParams, HullType } from '@porto/shared/api';

import { readJSON, removeItem, writeJSON } from '../common/storage';

export const STEPS = ['search', 'berth', 'extras', 'details', 'payment'] as const;
export type Step = (typeof STEPS)[number];

export type SearchInput = {
  arrival: string;
  departure: string;
  loa_cm: number;
  beam_cm: number;
  draft_cm?: number;
  hull_type?: HullType;
};

/** A booking held by this tab: id, token (never in a URL) and the binding idempotency key of its creation. */
export type HeldBooking = { id: string; token: string; reference: string };

export type FlowState = {
  search: SearchInput | null;
  category: string | null;
  /** Selected services: item id → quantity in milli units. */
  services: Record<string, number>;
  servicesTouched: boolean;
  promo: string;
  booking: HeldBooking | null;
  /** Idempotency-Key of the pending createBooking (kept for retries of the same action). */
  createKey: string | null;
  /** Idempotency-Key of the pending payment start. */
  payKey: string | null;
};

export const emptyState: FlowState = {
  search: null,
  category: null,
  services: {},
  servicesTouched: false,
  promo: '',
  booking: null,
  createKey: null,
  payKey: null,
};

export function stepFromHash(hash: string): Step | null {
  const h = hash.replace(/^#/, '');
  return (STEPS as readonly string[]).includes(h) ? (h as Step) : null;
}

/** The furthest step the state allows. */
export function maxStep(s: FlowState): Step {
  if (s.booking) return 'payment';
  if (s.search && s.category) return 'extras';
  if (s.search) return 'berth';
  return 'search';
}

/** The step to show for a requested one: never past maxStep, and never before details once a booking is held. */
export function clampStep(requested: Step | null, s: FlowState): Step {
  const max = STEPS.indexOf(maxStep(s));
  const want = requested ? STEPS.indexOf(requested) : max;
  return STEPS[Math.min(want, max)] ?? 'search';
}

export function searchParams(s: SearchInput): BookingSearchParams {
  return {
    arrival: s.arrival,
    departure: s.departure,
    loa_cm: s.loa_cm,
    beam_cm: s.beam_cm,
    ...(s.draft_cm ? { draft_cm: s.draft_cm } : {}),
    ...(s.hull_type ? { hull_type: s.hull_type } : {}),
  };
}

const key = (slug: string) => `porto:booking:${slug}`;

function isFlowState(v: unknown): v is FlowState {
  if (typeof v !== 'object' || v === null) return false;
  const o = v as Record<string, unknown>;
  return typeof o.services === 'object' && o.services !== null && typeof o.promo === 'string';
}

export function loadState(slug: string): FlowState {
  const s = readJSON(key(slug), isFlowState);
  return s ? { ...emptyState, ...s } : emptyState;
}

export function saveState(slug: string, s: FlowState): void {
  writeJSON(key(slug), s);
}

export function clearState(slug: string): void {
  removeItem(key(slug));
}

/** Nights between two YYYY-MM-DD dates (0 when invalid or not after). */
export function nightsBetween(arrival: string, departure: string): number {
  const a = Date.parse(`${arrival}T00:00:00Z`);
  const d = Date.parse(`${departure}T00:00:00Z`);
  if (Number.isNaN(a) || Number.isNaN(d) || d <= a) return 0;
  return Math.round((d - a) / 86_400_000);
}

/** YYYY-MM-DD plus n days. */
export function addDays(isoDate: string, n: number): string {
  const t = Date.parse(`${isoDate}T00:00:00Z`);
  return new Date(t + n * 86_400_000).toISOString().slice(0, 10);
}

/** Today's date in the browser's time zone (the server checks the marina's rules). */
export function todayIso(now = new Date()): string {
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, '0');
  const d = String(now.getDate()).padStart(2, '0');
  return `${y}-${m}-${d}`;
}
