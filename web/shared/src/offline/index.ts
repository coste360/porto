// Public API of the offline module (m6b-plan §4.1). Other features (PHONE, REMOTE) import only from
// '@porto/shared/offline'.
//
//   db.ts        IndexedDB "porto-offline" (meta, cache, ops, blobs)
//   persist.ts   allowlisted query cache, 48 h, per user and marina
//   allowlist.ts operations and queries allowed offline
//   queue.ts     enqueue, planning (ordering, lanes, dependencies, expiry), shared snapshot
//   runner.ts    single runner per origin (Web Locks), sending and classification
//   classify.ts  PURE: answer → done | retry | conflict | auth
//   online.ts    online status (events, failed requests, /healthz probe)
//   overlay.ts   PURE: queued ops over the cached day data
//   purge.ts     logout
import { useMemo, useSyncExternalStore } from 'react';

import { isOnline } from './online';
import { offlineStorageAvailable } from './db';
import { discardOp, enqueueOp, getOps, kick, loadOps, retryOpAsNew, subscribeOps } from './queue';
import { getRunner } from './runner';
import { OfflineForbiddenError, OfflineQueuedError, type NewOp, type QueuedOp } from './types';
import { ProblemError } from '../api/problem';

export type { AllowlistEntry, OfflineOpKind } from './allowlist';
export {
  OFFLINE_OPERATIONS,
  OFFLINE_OPERATION_KINDS,
  OFFLINE_QUERY_ALLOWLIST,
  isPersistable,
} from './allowlist';
export type { NewOp, OpState, QueuedOp, OfflineReasonCode } from './types';
export {
  OfflineForbiddenError,
  OfflineQueuedError,
  OfflineQuotaError,
  isQueuedOffline,
} from './types';
export { useOnlineStatus, isOnline, probe as probeServer, type OnlineStatus } from './online';
export { purgeOfflineData } from './purge';
export { purgeCache, restoreBundle, startPersister, type OfflineContext } from './persist';
export { overlayOf, residualAfterQueued, stateAfterQueued, type Overlay } from './overlay';
export { preparePhoto } from './queue';
export { sendQueueNow } from './runner';
export { OfflineSchemaNewerError, isReadOnly as offlineSchemaNewer } from './db';

/** A new Idempotency-Key (generated once, at enqueue time). */
export function newIdempotencyKey(): string {
  return crypto.randomUUID();
}

/** Adds an allowlisted operation to the queue (online too: queue first, D2) and starts sending. Throws
 * OfflineForbiddenError for anything offline may not do and OfflineQuotaError when the device is full. */
export async function enqueue(op: NewOp): Promise<QueuedOp> {
  return enqueueOp(op);
}

const FINAL: ReadonlySet<QueuedOp['state']> = new Set(['done', 'conflict', 'blocked', 'expired']);

/**
 * Waits for a queued op: resolves with its stored result when done; rejects with a ProblemError when the server
 * refused it (conflict, the op is discarded: the caller shows the problem) and with OfflineQueuedError when it is
 * still queued after `timeoutMs` or the device is offline (the queue sends it later).
 */
export function waitForOp(id: string, timeoutMs = 15_000): Promise<unknown> {
  return new Promise((resolve, reject) => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    let unsubscribe: () => void = () => {};
    const finish = (fn: () => void) => {
      if (timer) clearTimeout(timer);
      unsubscribe();
      fn();
    };
    const check = () => {
      const op = getOps().find((o) => o.id === id);
      if (!op) return;
      if (op.state === 'done') {
        finish(() => resolve(op.result));
      } else if (FINAL.has(op.state)) {
        const p = op.problem;
        finish(() => {
          void discardOp(op.id);
          reject(
            new ProblemError({
              type: p?.type ?? 'conflict',
              status: p?.status ?? 409,
              details: (p?.detail as Record<string, unknown> | undefined) ?? {},
            }),
          );
        });
      } else if (op.attempts > 0 && !isOnline()) {
        finish(() => reject(new OfflineQueuedError(op)));
      }
    };
    unsubscribe = subscribeOps(check);
    timer = setTimeout(() => {
      const op = getOps().find((o) => o.id === id);
      finish(() =>
        op?.state === 'done'
          ? resolve(op.result)
          : op
            ? reject(new OfflineQueuedError(op))
            : reject(new Error('offline: op lost')),
      );
    }, timeoutMs);
    if (!isOnline()) {
      const op = getOps().find((o) => o.id === id);
      if (op) finish(() => reject(new OfflineQueuedError(op)));
      return;
    }
    check();
  });
}

/** enqueue + waitForOp: the queue-backed mutation hooks use it (online it behaves like a direct call). */
export async function enqueueAndWait<T>(op: NewOp, timeoutMs?: number): Promise<T> {
  const q = await enqueueOp(op);
  return (await waitForOp(q.id, timeoutMs)) as T;
}

export type QueueView = {
  ops: QueuedOp[];
  /** Ops of the marina still to be sent (pending, sending, retrying, blocked). */
  pending: number;
  /** Ops of the marina waiting for the user (conflict, expired). */
  conflicts: number;
  sendNow(): void;
  discard(id: string): Promise<void>;
  retryAsNew(id: string, patch?: unknown): Promise<void>;
};

const NO_OPS: readonly QueuedOp[] = [];
const PENDING: ReadonlySet<QueuedOp['state']> = new Set([
  'pending',
  'sending',
  'retrying',
  'blocked',
]);
const NEEDS_USER: ReadonlySet<QueuedOp['state']> = new Set(['conflict', 'expired']);

/** Counts of a list of ops. */
export function countOps(ops: readonly QueuedOp[]): { pending: number; conflicts: number } {
  let pending = 0;
  let conflicts = 0;
  for (const o of ops) {
    if (PENDING.has(o.state)) pending++;
    else if (NEEDS_USER.has(o.state)) conflicts++;
  }
  return { pending, conflicts };
}

/** The queue of a marina (every user: ops of another user can only be discarded). */
export function useQueue(marinaId: string): QueueView {
  const all = useSyncExternalStore(subscribeOps, getOps, () => NO_OPS);
  return useMemo(() => {
    const ops = all.filter((o) => o.marinaId === marinaId);
    return {
      ops,
      ...countOps(ops),
      sendNow: () => kick(),
      discard: (id: string) => discardOp(id),
      retryAsNew: async (id: string, patch?: unknown) => {
        await retryOpAsNew(id, patch);
      },
    };
  }, [all, marinaId]);
}

/** Unsent ops of a user in a marina (the marina switch is refused while there are any). */
export function unsentOps(userId: string, marinaId?: string): QueuedOp[] {
  return getOps().filter(
    (o) =>
      o.userId === userId &&
      (marinaId === undefined || o.marinaId === marinaId) &&
      o.state !== 'done',
  );
}

/** True when the user may leave the marina (no unsent ops there). */
export function canSwitchMarina(userId: string, fromMarinaId: string): boolean {
  return unsentOps(userId, fromMarinaId).length === 0;
}

/**
 * Route guard of the marina switch (D9, INV-84): the marina whose unsent ops forbid entering targetMarinaId (any
 * other marina of the user with ops not done), or null. It loads the queue first, so a deep link, the browser
 * history or the organization page are refused like the switcher.
 */
export async function marinaBlockingEntry(
  userId: string,
  targetMarinaId: string,
): Promise<string | null> {
  if (!offlineStorageAvailable()) return null;
  try {
    await loadOps();
  } catch {
    return null;
  }
  return unsentOps(userId).find((o) => o.marinaId !== targetMarinaId)?.marinaId ?? null;
}

let switchBlockedNotices = 0;
const switchBlockedListeners = new Set<() => void>();

/** Records that the guard refused a marina switch (the marina layout shows the toast). */
export function noteMarinaSwitchBlocked(): void {
  switchBlockedNotices++;
  for (const l of switchBlockedListeners) l();
}

/** Takes the pending refusal notice (true once per refusal). */
export function takeMarinaSwitchBlocked(): boolean {
  if (switchBlockedNotices === 0) return false;
  switchBlockedNotices = 0;
  return true;
}

/** Changes whenever a refusal is noted (re-render trigger for the toast). */
export function useMarinaSwitchBlocked(): number {
  return useSyncExternalStore(
    (l) => {
      switchBlockedListeners.add(l);
      return () => switchBlockedListeners.delete(l);
    },
    () => switchBlockedNotices,
    () => 0,
  );
}

// ---- session ----

type SessionState = { userId: string | null; authRequired: boolean };
let session: SessionState = { userId: null, authRequired: false };
const sessionListeners = new Set<() => void>();

function setSession(next: SessionState) {
  session = next;
  for (const l of sessionListeners) l();
}

/** Tells the queue who is signed in (null: nobody). Only that user's ops are sent; a 401 pauses the runner until
 * the same user signs in again. Called by the API provider. */
export function setOfflineSession(
  userId: string | null,
  events: { onDone?: (op: QueuedOp) => void; onAuthRequired?: () => void } = {},
): void {
  const runner = getRunner();
  runner.setEvents({
    onDone: events.onDone,
    onAuthRequired: () => {
      setSession({ userId: session.userId, authRequired: true });
      events.onAuthRequired?.();
    },
  });
  runner.setUser(userId);
  setSession({
    userId,
    authRequired: userId === null ? session.authRequired : false,
  });
}

/** The signed-in user of the queue and whether it waits for a new sign-in. */
export function useOfflineSession(): SessionState {
  return useSyncExternalStore(
    (l) => {
      sessionListeners.add(l);
      return () => sessionListeners.delete(l);
    },
    () => session,
    () => session,
  );
}

/** Throws OfflineForbiddenError when offline: guard of the online-only mutation hooks (m6b-plan §4.2). */
export function assertOnline(
  operationId: string,
  reason?: 'allocation' | 'document' | 'online_only',
): void {
  if (!isOnline()) throw new OfflineForbiddenError(operationId, reason);
}
