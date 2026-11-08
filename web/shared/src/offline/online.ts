// Online status (m6b-plan §4.1): the browser events, plus what the queue learns from its own requests (a failed
// fetch means offline even when navigator.onLine says otherwise, e.g. a captive Wi-Fi or a server down), plus a
// HEAD /healthz probe every 15 s while offline. Also whether a service worker controls the page.
import { useSyncExternalStore } from 'react';

export type OnlineStatus = { online: boolean; lastOnlineAt?: string; swActive: boolean };

export const PROBE_INTERVAL_MS = 15_000;
export const HEALTH_PATH = '/healthz';

type Listener = () => void;
const listeners = new Set<Listener>();
let failed = false;
let lastOnlineAt: string | undefined;
let cached: OnlineStatus | null = null;
let probeTimer: ReturnType<typeof setInterval> | null = null;
let wired = false;

function browserOnline(): boolean {
  return typeof navigator === 'undefined' ? true : navigator.onLine;
}

function swActive(): boolean {
  return typeof navigator !== 'undefined' && !!navigator.serviceWorker?.controller;
}

/** True when the device can reach the server, as far as we know. */
export function isOnline(): boolean {
  return browserOnline() && !failed;
}

function changed() {
  cached = null;
  if (isOnline()) lastOnlineAt = new Date().toISOString();
  syncProbe();
  for (const l of listeners) l();
}

/** The queue calls this after a network error (status 0). */
export function reportNetworkFailure(): void {
  if (failed) return;
  failed = true;
  changed();
}

/** The queue (or the probe) calls this after any answer of the server. */
export function reportNetworkSuccess(): void {
  if (!failed) {
    lastOnlineAt = new Date().toISOString();
    return;
  }
  failed = false;
  changed();
}

/** One probe: any HTTP answer of /healthz means the server is reachable. */
export async function probe(fetchImpl: typeof fetch = fetch): Promise<boolean> {
  try {
    await fetchImpl(HEALTH_PATH, { method: 'HEAD', cache: 'no-store', credentials: 'omit' });
    reportNetworkSuccess();
    return true;
  } catch {
    return false;
  }
}

function syncProbe() {
  const needed = listeners.size > 0 && browserOnline() && failed;
  if (needed && !probeTimer) {
    probeTimer = setInterval(() => void probe(), PROBE_INTERVAL_MS);
  } else if (!needed && probeTimer) {
    clearInterval(probeTimer);
    probeTimer = null;
  }
}

function wire() {
  if (wired || typeof window === 'undefined') return;
  wired = true;
  window.addEventListener('online', () => {
    // The browser is back on a network: trust it until a request fails again.
    failed = false;
    changed();
  });
  window.addEventListener('offline', changed);
  navigator.serviceWorker?.addEventListener?.('controllerchange', changed);
}

export function subscribeOnline(l: Listener): () => void {
  wire();
  listeners.add(l);
  syncProbe();
  return () => {
    listeners.delete(l);
    syncProbe();
  };
}

export function getOnlineStatus(): OnlineStatus {
  if (!cached) {
    const online = isOnline();
    if (online && !lastOnlineAt) lastOnlineAt = new Date().toISOString();
    cached = {
      online,
      swActive: swActive(),
      ...(lastOnlineAt ? { lastOnlineAt } : {}),
    };
  }
  return cached;
}

const SERVER: OnlineStatus = { online: true, swActive: false };

/** Online status for React. */
export function useOnlineStatus(): OnlineStatus {
  return useSyncExternalStore(subscribeOnline, getOnlineStatus, () => SERVER);
}

/** Test hook. */
export function resetOnlineForTests(): void {
  failed = false;
  lastOnlineAt = undefined;
  cached = null;
  if (probeTimer) clearInterval(probeTimer);
  probeTimer = null;
}
