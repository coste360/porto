// Offline read cache (m6b-plan §4.2): the allowlisted, successful queries of the current user and marina are
// dehydrated into IndexedDB (store "cache", one bundle per `userId:marinaId`) and hydrated at start, for 48
// hours. Never persisted: anything off the allowlist (auth, desktop, devices, keys, webhooks, audit, exports),
// failed queries, and any data that carries a signed link (photo or document URLs with a signature). Bundles of
// other marinas of the user are deleted when a new one is saved (marina switch).
import { dehydrate, hydrate, type DehydratedState, type QueryClient } from '@tanstack/react-query';

import { isPersistable } from './allowlist';
import { dbDelete, dbGet, dbGetAll, dbMayExist, dbPut, offlineStorageAvailable } from './db';

export const CACHE_MAX_AGE_MS = 48 * 60 * 60 * 1000;
export const SAVE_DEBOUNCE_MS = 1_500;
const SCHEMA = 'c1';

export type OfflineContext = { userId: string; marinaId: string };

export type CacheBundle = {
  id: string; // userId:marinaId
  buster: string;
  savedAt: number;
  state: DehydratedState;
};

export function bundleId(ctx: OfflineContext): string {
  return `${ctx.userId}:${ctx.marinaId}`;
}

export function busterOf(appVersion: string, ctx: OfflineContext): string {
  return `${SCHEMA}:${appVersion}:${ctx.userId}:${ctx.marinaId}`;
}

/** Signed links (FileLink, photo and document URLs) must never be stored. */
const SIGNED = /[?&](sig|signature|token)=|\/file\?exp=/;

function carriesSignedLink(data: unknown): boolean {
  try {
    return SIGNED.test(JSON.stringify(data) ?? '');
  } catch {
    return true;
  }
}

/** The persistable part of the cache for ctx (pure apart from reading the client). */
export function dehydrateFor(qc: QueryClient, ctx: OfflineContext): DehydratedState {
  return dehydrate(qc, {
    shouldDehydrateQuery: (q) =>
      q.state.status === 'success' &&
      q.state.data !== undefined &&
      isPersistable(q.queryKey, ctx.marinaId) &&
      !carriesSignedLink(q.state.data),
    shouldDehydrateMutation: () => false,
  });
}

/** Saves the bundle of ctx and deletes the other bundles of the same user. */
export async function saveBundle(
  qc: QueryClient,
  ctx: OfflineContext,
  appVersion: string,
  now: number,
): Promise<CacheBundle> {
  const bundle: CacheBundle = {
    id: bundleId(ctx),
    buster: busterOf(appVersion, ctx),
    savedAt: now,
    state: dehydrateFor(qc, ctx),
  };
  await dbPut('cache', bundle, bundle.id);
  await dbPut('meta', { userId: ctx.userId, marinaId: ctx.marinaId, appVersion }, 'last');
  const all = await dbGetAll<CacheBundle>('cache');
  for (const b of all) {
    if (b.id !== bundle.id) await dbDelete('cache', b.id);
  }
  return bundle;
}

/** Drops entries older than 48 hours from a dehydrated state. */
export function freshOnly(state: DehydratedState, now: number): DehydratedState {
  return {
    ...state,
    queries: state.queries.filter((q) => now - q.state.dataUpdatedAt <= CACHE_MAX_AGE_MS),
    mutations: [],
  };
}

/** Hydrates the last saved bundle (same app version, not older than 48 h). Returns its context, or null. */
export async function restoreBundle(
  qc: QueryClient,
  appVersion: string,
  now: number,
): Promise<OfflineContext | null> {
  if (!offlineStorageAvailable()) return null;
  try {
    const last = await dbGet<OfflineContext & { appVersion: string }>('meta', 'last');
    if (!last) return null;
    const ctx = { userId: last.userId, marinaId: last.marinaId };
    const bundle = await dbGet<CacheBundle>('cache', bundleId(ctx));
    if (!bundle || bundle.buster !== busterOf(appVersion, ctx)) return null;
    if (now - bundle.savedAt > CACHE_MAX_AGE_MS) {
      await dbDelete('cache', bundle.id);
      return null;
    }
    const state = freshOnly(bundle.state, now);
    hydrate(qc, state);
    // Restored data is a fallback, never fresher than the server: mark it stale so every screen refetches it
    // when mounted (online it is replaced at once; offline the restored copy stays on screen).
    for (const q of state.queries)
      void qc.invalidateQueries({ queryKey: q.queryKey, exact: true, refetchType: 'none' });
    return ctx;
  } catch {
    return null;
  }
}

/** Deletes every cached bundle and the last context (401 of a revoked device: the ops are kept). */
export async function purgeCache(): Promise<void> {
  if (!offlineStorageAvailable()) return;
  try {
    if (!(await dbMayExist())) return; // nothing stored (or logout deleted it): never re-create it
    const all = await dbGetAll<CacheBundle>('cache');
    for (const b of all) await dbDelete('cache', b.id);
    await dbDelete('meta', 'last');
  } catch {
    // nothing stored
  }
}

/**
 * Persists the cache while the app runs: saves (debounced) after every query update, when ctx() tells the user
 * and marina. Returns the unsubscribe function.
 */
export function startPersister(
  qc: QueryClient,
  opts: {
    appVersion: string;
    context: () => OfflineContext | null;
    now?: () => number;
    onSaved?: (b: CacheBundle) => void;
    debounceMs?: number;
  },
): () => void {
  if (!offlineStorageAvailable()) return () => {};
  let timer: ReturnType<typeof setTimeout> | null = null;
  const now = opts.now ?? (() => Date.now());
  const flush = () => {
    timer = null;
    const ctx = opts.context();
    if (!ctx) return;
    void saveBundle(qc, ctx, opts.appVersion, now())
      .then((b) => opts.onSaved?.(b))
      .catch(() => undefined);
  };
  const unsubscribe = qc.getQueryCache().subscribe((e) => {
    if (e.type !== 'updated' || e.action.type !== 'success') return;
    if (timer) clearTimeout(timer);
    timer = setTimeout(flush, opts.debounceMs ?? SAVE_DEBOUNCE_MS);
  });
  // Leaving the page (navigation, tab hidden): save now, best effort, rather than lose the debounced save.
  const onHide = () => {
    if (!timer) return;
    clearTimeout(timer);
    flush();
  };
  const onVisibility = () => {
    if (document.visibilityState === 'hidden') onHide();
  };
  if (typeof window !== 'undefined') {
    window.addEventListener('pagehide', onHide);
    document.addEventListener('visibilitychange', onVisibility);
  }
  return () => {
    unsubscribe();
    if (timer) clearTimeout(timer);
    if (typeof window !== 'undefined') {
      window.removeEventListener('pagehide', onHide);
      document.removeEventListener('visibilitychange', onVisibility);
    }
  };
}
