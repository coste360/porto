import { QueryClientContext, type QueryClient } from '@tanstack/react-query';
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';

import { offlineStorageAvailable } from '../offline/db';
import { setOfflineSession } from '../offline/index';
import { purgeCache, restoreBundle, startPersister, type OfflineContext } from '../offline/persist';
import { purgeOfflineData } from '../offline/purge';
import { sendQueueNow } from '../offline/runner';
import type { QueuedOp } from '../offline/types';
import type { ApiClient } from './client';
import { queryKeys } from './queryKeys';
import type { Me } from './schema';

const ApiContext = createContext<ApiClient | null>(null);

/** Build version of the persisted cache (a new build drops the cached queries). */
const APP_VERSION: string =
  (import.meta.env?.VITE_PORTO_VERSION as string | undefined) ?? import.meta.env?.MODE ?? 'dev';

/** How long the first render waits for the offline cache before it goes on without it. */
const RESTORE_TIMEOUT_MS = 1_500;

/** Data attribute set on <html> once the offline cache was saved (e2e warm-up marker). */
export const OFFLINE_CACHE_MARKER = 'portoOfflineCache';

const MARINA_IN_PATH = /\/m\/([0-9a-fA-F-]{36})(?:\/|$)/;

function currentMarinaId(): string | null {
  if (typeof window === 'undefined') return null;
  return MARINA_IN_PATH.exec(window.location.pathname)?.[1]?.toLowerCase() ?? null;
}

function meOf(qc: QueryClient): Me | null | undefined {
  return qc.getQueryData<Me | null>(queryKeys.me);
}

function offlineContext(qc: QueryClient): OfflineContext | null {
  const userId = meOf(qc)?.user.id;
  const marinaId = currentMarinaId();
  return userId && marinaId ? { userId, marinaId } : null;
}

/** Queries refreshed after a queued op is sent (the SSE messages do the same for the other devices). */
function invalidateAfter(qc: QueryClient, op: QueuedOp) {
  const m = op.marinaId;
  const keys =
    op.kind === 'payment.cash'
      ? [queryKeys.billing(m)]
      : [queryKeys.desk(m), queryKeys.ops(m), queryKeys.billing(m)];
  for (const k of keys) void qc.invalidateQueries({ queryKey: k });
}

const wiredClients = new WeakSet<ApiClient>();

/** Logout deletes every offline record (INV-84): the request is seen here, whatever screen sends it. */
function wireLogout(client: ApiClient) {
  if (wiredClients.has(client) || typeof client.use !== 'function') return;
  wiredClients.add(client);
  client.use({
    async onResponse({ request }) {
      const path = new URL(request.url, 'http://local').pathname;
      if (request.method === 'POST' && path === '/api/admin/v1/auth/logout') {
        setOfflineSession(null);
        await purgeOfflineData();
      }
      return undefined;
    },
  });
}

/**
 * Offline wiring of the app (m6b-plan §4.2–4.3): restores the persisted query cache before the first render
 * (bounded wait), persists the allowlisted queries, tells the queue who is signed in, sends the queue when the
 * network comes back or the page becomes visible, purges on logout, and drops the cache (not the queued ops) when
 * the queue meets a 401.
 */
function useOfflineWiring(qc: QueryClient | undefined, client: ApiClient): boolean {
  const enabled = !!qc && offlineStorageAvailable();
  const [ready, setReady] = useState(!enabled);
  useEffect(() => {
    if (!enabled || !qc) return;
    let alive = true;
    const timer = setTimeout(() => alive && setReady(true), RESTORE_TIMEOUT_MS);
    void restoreBundle(qc, APP_VERSION, Date.now()).finally(() => {
      if (alive) setReady(true);
    });
    wireLogout(client);
    const stopPersist = startPersister(qc, {
      appVersion: APP_VERSION,
      context: () => offlineContext(qc),
      onSaved: () => {
        if (typeof document !== 'undefined')
          document.documentElement.dataset[OFFLINE_CACHE_MARKER] = 'saved';
      },
    });
    let lastUser: string | null | undefined;
    const syncSession = () => {
      const me = meOf(qc);
      if (me === undefined) return; // not loaded yet
      const userId = me?.user.id ?? null;
      if (userId === lastUser) return;
      lastUser = userId;
      // Signed out (expired session, revoked device: getMe answered 401): the cached marina data must not outlive
      // the session (INV-84). The queued ops stay, frozen until the same user signs in again.
      if (userId === null) void purgeCache();
      setOfflineSession(userId, {
        onDone: (op) => invalidateAfter(qc, op),
        onAuthRequired: () => void purgeCache(),
      });
    };
    syncSession();
    const unsubscribeMe = qc.getQueryCache().subscribe((e) => {
      if (e.query.queryKey.length === 1 && e.query.queryKey[0] === queryKeys.me[0]) syncSession();
    });
    const onVisible = () => {
      if (document.visibilityState === 'visible') sendQueueNow();
    };
    window.addEventListener('online', sendQueueNow);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      alive = false;
      clearTimeout(timer);
      stopPersist();
      unsubscribeMe();
      window.removeEventListener('online', sendQueueNow);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [enabled, qc, client]);
  return ready;
}

export function ApiProvider({ client, children }: { client: ApiClient; children: ReactNode }) {
  const qc = useContext(QueryClientContext);
  const ready = useOfflineWiring(qc, client);
  return <ApiContext.Provider value={client}>{ready ? children : null}</ApiContext.Provider>;
}

/** The API client of the current app (tests inject one with a fetch mock). */
export function useApi(): ApiClient {
  const client = useContext(ApiContext);
  if (!client) throw new Error('useApi must be used inside <ApiProvider>');
  return client;
}
