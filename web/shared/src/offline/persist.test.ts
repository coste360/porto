import { QueryClient } from '@tanstack/react-query';

import { queryKeys } from '../api/queryKeys';
import { localDate } from './allowlist';
import { dbGetAll } from './db';
import {
  CACHE_MAX_AGE_MS,
  purgeCache,
  restoreBundle,
  saveBundle,
  type CacheBundle,
} from './persist';
import { purgeOfflineData } from './purge';
import { enqueueOp, getOps } from './queue';
import { M, USER, fixQueueClock, presenceNewOp, resetOffline } from './testkit';

const OTHER = '01a1b84d-0000-7000-8000-000000000000';
const ctx = { userId: USER, marinaId: M };

function seeded(now: number): QueryClient {
  const qc = new QueryClient();
  const set = (key: readonly unknown[], data: unknown) =>
    qc.setQueryData(key, data, { updatedAt: now });
  set(queryKeys.me, { user: { id: USER } });
  set(queryKeys.maps(M), { items: [] });
  set(queryKeys.dayView(M, 'map-1', localDate()), { elements: [] });
  set(queryKeys.dayView(M, 'map-1', '2020-01-01'), { elements: ['old'] });
  set(queryKeys.movements(M, localDate(1)), { arrivals: [] });
  set(queryKeys.presences(M, localDate()), { items: [] });
  set(queryKeys.collectibles(M, localDate()), { items: [] });
  set([...queryKeys.cashSession(M, 'current'), ''], { id: 's1' });
  set(queryKeys.deadlines(M, { due_before: '2027-07-05' }), { pages: [] });
  // Not on the allowlist, or carrying secrets / signed links:
  set(queryKeys.customers(M), { items: [{ name: 'Rossi' }] });
  set(queryKeys.audit(M), { items: [] });
  set(queryKeys.desktop.status, { mode: 'local' });
  set(queryKeys.apiKeys(M), { items: [] });
  set(queryKeys.presences(M, localDate()).concat('photo'), {
    url: `/api/admin/v1/marinas/${M}/presences/p1/photo/file?exp=1&sig=abc`,
  });
  set(queryKeys.maps(OTHER), { items: [] });
  return qc;
}

describe('offline query cache', () => {
  beforeEach(() => {
    resetOffline();
    fixQueueClock();
  });

  it('persists only allowlisted queries of the current marina, never signed links', async () => {
    const now = Date.now();
    const bundle = await saveBundle(seeded(now), ctx, 'v1', now);
    const keys = bundle.state.queries.map((q) => JSON.stringify(q.queryKey)).sort();
    expect(keys).toEqual(
      [
        queryKeys.me,
        queryKeys.maps(M),
        queryKeys.dayView(M, 'map-1', localDate()),
        queryKeys.movements(M, localDate(1)),
        queryKeys.presences(M, localDate()),
        queryKeys.collectibles(M, localDate()),
        [...queryKeys.cashSession(M, 'current'), ''],
        queryKeys.deadlines(M, { due_before: '2027-07-05' }),
      ]
        .map((k) => JSON.stringify(k))
        .sort(),
    );
    expect(JSON.stringify(bundle)).not.toContain('sig=');
    expect(JSON.stringify(bundle)).not.toContain('Rossi');
  });

  it('restores the bundle of the same build, drops entries older than 48 h, and keeps one marina', async () => {
    const now = Date.now();
    const qc = seeded(now);
    qc.setQueryData(
      queryKeys.map(M, 'map-1'),
      { id: 'map-1' },
      { updatedAt: now - CACHE_MAX_AGE_MS - 1 },
    );
    await saveBundle(qc, { userId: USER, marinaId: OTHER }, 'v1', now);
    await saveBundle(qc, ctx, 'v1', now);
    expect((await dbGetAll<CacheBundle>('cache')).map((b) => b.id)).toEqual([`${USER}:${M}`]);

    const fresh = new QueryClient();
    expect(await restoreBundle(fresh, 'v1', now + 60_000)).toEqual(ctx);
    expect(fresh.getQueryData(queryKeys.maps(M))).toEqual({ items: [] });
    // Restored data never counts as fresh: a mounted screen refetches it (a reload after a save shows the save).
    expect(fresh.getQueryState(queryKeys.maps(M))?.isInvalidated).toBe(true);
    expect(fresh.getQueryData(queryKeys.map(M, 'map-1'))).toBeUndefined();

    expect(await restoreBundle(new QueryClient(), 'v2', now)).toBeNull(); // another build
    expect(await restoreBundle(new QueryClient(), 'v1', now + CACHE_MAX_AGE_MS + 1)).toBeNull();
  });

  it('a 401 drops the cache but keeps the queued ops; logout deletes everything', async () => {
    const now = Date.now();
    await saveBundle(seeded(now), ctx, 'v1', now);
    await enqueueOp(presenceNewOp('v1'));
    await purgeCache();
    expect(await dbGetAll('cache')).toEqual([]);
    expect(await dbGetAll('ops')).toHaveLength(1);
    await purgeOfflineData();
    expect(getOps()).toEqual([]);
    const dbs = await indexedDB.databases();
    expect(dbs.map((d) => d.name)).not.toContain('porto-offline');
  });

  it('a purge of the cache after logout never re-creates the deleted database', async () => {
    const now = Date.now();
    await saveBundle(seeded(now), ctx, 'v1', now);
    await purgeOfflineData();
    await purgeCache(); // me = null right after the logout
    const dbs = await indexedDB.databases();
    expect(dbs.map((d) => d.name)).not.toContain('porto-offline');
  });
});
