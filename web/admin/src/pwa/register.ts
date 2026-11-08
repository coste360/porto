// Service worker registration and update prompt of the admin panel (m6b-plan §3.2). Loaded by index.html as its
// own module (the vite plugin in vite.config.ts adds the script tag), so that it works on every screen.
//
// Not registered in the desktop window (local mode needs none, remote mode caches the shell in Go), on a desktop
// edition served on loopback, or in development. A new build waits; the page shows a persistent toast
// "Nuova versione disponibile · Aggiorna": on click the waiting worker takes over and the page reloads once it
// controls it. The offline queue lives in IndexedDB and survives the update.
import { toast } from 'sonner';

import { isDesktopWindow } from '@porto/shared/desktop/bridge';
import { detectLocale, initI18n } from '@porto/shared/i18n';

export const SW_URL = '/admin/sw.js';
export const SW_SCOPE = '/admin/';

export type SwStatus = 'unsupported' | 'skipped' | 'registered' | 'failed';

function setStatus(s: SwStatus) {
  document.documentElement.dataset.portoSw = s;
}

function isLoopback(host: string): boolean {
  return host === 'localhost' || host === '[::1]' || /^127\./.test(host);
}

async function desktopEdition(): Promise<boolean> {
  try {
    const res = await fetch('/api/admin/v1/meta', { credentials: 'same-origin' });
    if (!res.ok) return false;
    const meta = (await res.json()) as { edition?: string };
    return meta.edition === 'desktop';
  } catch {
    return false;
  }
}

/** Asks the waiting worker to take over, then reloads when it controls the page. */
export function applyUpdate(reg: ServiceWorkerRegistration): void {
  const waiting = reg.waiting;
  if (!waiting) return;
  navigator.serviceWorker.addEventListener('controllerchange', () => window.location.reload(), {
    once: true,
  });
  waiting.postMessage({ type: 'SKIP_WAITING' });
}

function announce(reg: ServiceWorkerRegistration) {
  document.documentElement.dataset.portoSwUpdate = 'waiting';
  const i18n = initI18n({ lng: detectLocale(), namespaces: ['offline'] });
  toast(i18n.t('offline:update.available'), {
    id: 'porto-sw-update',
    duration: Infinity,
    action: { label: i18n.t('offline:update.action'), onClick: () => applyUpdate(reg) },
  });
}

/** Watches a registration for a new worker installed while an older one controls the page. */
export function watchUpdates(reg: ServiceWorkerRegistration): void {
  if (reg.waiting && navigator.serviceWorker.controller) announce(reg);
  reg.addEventListener('updatefound', () => {
    const installing = reg.installing;
    installing?.addEventListener('statechange', () => {
      if (installing.state === 'installed' && navigator.serviceWorker.controller) announce(reg);
    });
  });
}

export async function registerServiceWorker(): Promise<SwStatus> {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
    setStatus('unsupported');
    return 'unsupported';
  }
  if (import.meta.env.DEV || isDesktopWindow()) {
    setStatus('skipped');
    return 'skipped';
  }
  if (isLoopback(window.location.hostname) && (await desktopEdition())) {
    setStatus('skipped');
    return 'skipped';
  }
  try {
    const reg = await navigator.serviceWorker.register(SW_URL, { scope: SW_SCOPE });
    watchUpdates(reg);
    // Check for a new build when the page comes back to the foreground.
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') void reg.update().catch(() => undefined);
    });
    setStatus('registered');
    return 'registered';
  } catch {
    // e.g. an untrusted certificate on the local network (m6b-plan §3.3): the queue still works while open.
    setStatus('failed');
    return 'failed';
  }
}

if (typeof window !== 'undefined' && !import.meta.env.VITEST) {
  window.addEventListener('load', () => void registerServiceWorker());
}
