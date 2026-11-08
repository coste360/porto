// Customer-area session of this tab (m8-plan §7.1): the session token lives in sessionStorage (memory fallback,
// ../common/storage), per marina, and is dropped at its absolute expiry. The login token travels only in the URL
// fragment and is wiped from the address bar before anything else happens.
import { readJSON, removeItem, writeJSON } from '../common/storage';

export type StoredSession = { token: string; expiresAt: string };

const key = (slug: string) => `porto:account:${slug}`;

function isSession(v: unknown): v is StoredSession {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as StoredSession).token === 'string' &&
    typeof (v as StoredSession).expiresAt === 'string'
  );
}

/** The stored session of the marina, or null when missing or past its absolute expiry. */
export function readSession(slug: string, nowMs = Date.now()): string | null {
  const s = readJSON(key(slug), isSession);
  if (!s) return null;
  if (Date.parse(s.expiresAt) <= nowMs) {
    removeItem(key(slug));
    return null;
  }
  return s.token;
}

export function writeSession(slug: string, s: StoredSession): void {
  writeJSON(key(slug), s);
}

export function clearSession(slug: string): void {
  removeItem(key(slug));
}

/** Reads the login token from a fragment like "#t=<token>" (null when missing or malformed). Pure. */
export function fragmentToken(hash: string): string | null {
  const token = new URLSearchParams(hash.replace(/^#/, '')).get('t');
  return token && /^[A-Za-z0-9]{16,128}$/.test(token) ? token : null;
}

/** Wipes the fragment from the address bar: the token must not stay in the history or be copied with the URL. */
export function wipeFragment(
  loc: Pick<Location, 'hash' | 'pathname' | 'search'> = window.location,
  hist: Pick<History, 'replaceState'> = window.history,
): void {
  if (loc.hash) hist.replaceState(null, '', loc.pathname + loc.search);
}

/** Path of the customer area (list, or one booking). */
export function accountPath(slug: string, contractId?: string | null): string {
  return `/b/${slug}/account${contractId ? `/${contractId}` : ''}`;
}
