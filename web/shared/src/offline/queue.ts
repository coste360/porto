// The operation queue (m6b-plan §4.3): pure planning (ordering, lanes, dependencies, expiry) plus the in-memory
// snapshot of the "ops" store, shared by the hooks (useSyncExternalStore) and the runner, and kept in step with
// other tabs through a BroadcastChannel.
//
// Queue first (D2): every allowlisted mutation goes through enqueue, online too. The Idempotency-Key is fixed at
// enqueue and reused on every attempt; the server deduplicates (INV-83). The queue never decides anything the
// server decides (INV-01): a queued operation is a request the server may refuse, and a refusal is shown to the
// user (conflict), never resolved automatically.
import { OFFLINE_OPERATION_KINDS, type OfflineOpKind } from './allowlist';
import { dbDelete, dbGet, dbGetAll, dbPut, offlineStorageAvailable, type StoredBlob } from './db';
import type { NewOp, OpState, QueuedOp } from './types';
import { OfflineForbiddenError, OfflineQuotaError } from './types';

/** An op older than this is never resent with its key (the server keeps offline keys 7 days, D3). */
export const OP_EXPIRY_MS = 6 * 24 * 60 * 60 * 1000;
/** Done ops stay visible ("Inviata") and resolvable by dependents for this long. */
export const DONE_RETENTION_MS = 60 * 60 * 1000;
/** Free space required to queue a photo. */
export const MIN_FREE_BYTES = 20 * 1024 * 1024;
export const MAX_PHOTO_BYTES = 8 * 1024 * 1024;
export const MAX_PHOTO_PX = 2048;

const TERMINAL: ReadonlySet<OpState> = new Set(['done']);
/** States that hold their lane: nothing behind them is sent until the user acts. */
const LANE_STOPPERS: ReadonlySet<OpState> = new Set(['conflict', 'blocked', 'expired']);

// ---- pure planning ----

function byAge(a: QueuedOp, b: QueuedOp): number {
  return a.createdAt === b.createdAt
    ? a.id.localeCompare(b.id)
    : a.createdAt.localeCompare(b.createdAt);
}

/** Value at a dotted path of a JSON value ("presence.id"). */
export function pick(value: unknown, path: string): unknown {
  let cur: unknown = value;
  for (const part of path.split('.')) {
    if (cur === null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[part];
  }
  return cur;
}

/** The request path with the {param} placeholders bound to the parent's stored result, or null when a binding
 * is missing. */
export function resolvePath(op: QueuedOp, parent: QueuedOp | undefined): string | null {
  let path = op.request.path;
  if (op.dependsOn) {
    if (!parent || parent.state !== 'done') return null;
    for (const [param, from] of Object.entries(op.dependsOn.bind)) {
      const v = pick(parent.result, from);
      if (typeof v !== 'string' || v === '') return null;
      path = path.split(`{${param}}`).join(encodeURIComponent(v));
    }
  }
  return /\{[^}]+\}/.test(path) ? null : path;
}

export type Plan = {
  /** Ops whose state the plan changes (expired, blocked). */
  updates: QueuedOp[];
  /** The next op to send now, if any. */
  next?: QueuedOp;
  /** When the next retrying op becomes due (ms epoch), if nothing is due now. */
  wakeAt?: number;
};

/**
 * Decides what the runner does next for `userId` at `now`: marks expired ops (older than 6 days and not done),
 * blocks dependents of a conflicted, blocked, expired or discarded parent, then picks the oldest sendable op. One
 * request at a time, oldest first; a lane whose head is conflict/blocked/expired stops, other lanes continue; a
 * retrying op keeps its lane until its next attempt is due (order within a lane is preserved). Ops of other users
 * are never sent (they wait for their user or a discard).
 */
export function planNext(all: readonly QueuedOp[], now: number, userId: string | null): Plan {
  const updates: QueuedOp[] = [];
  const byId = new Map(all.map((o) => [o.id, o]));
  const state = (o: QueuedOp) => byId.get(o.id)?.state ?? o.state;
  const set = (o: QueuedOp, patch: Partial<QueuedOp>) => {
    const n = { ...byId.get(o.id)!, ...patch };
    byId.set(o.id, n);
    updates.push(n);
  };
  const sorted = [...all].sort(byAge);
  for (const o of sorted) {
    if (TERMINAL.has(o.state) || o.state === 'expired') continue;
    if (now - Date.parse(o.createdAt) > OP_EXPIRY_MS) set(o, { state: 'expired' });
  }
  // Dependencies: parents are older, so one pass in age order settles chains.
  for (const o of sorted) {
    const cur = byId.get(o.id)!;
    if (!cur.dependsOn || cur.state !== 'pending') continue;
    const parent = byId.get(cur.dependsOn.opId);
    if (
      !parent ||
      parent.state === 'conflict' ||
      parent.state === 'blocked' ||
      parent.state === 'expired'
    ) {
      set(cur, { state: 'blocked' });
    }
  }
  if (!userId) return { updates };
  const stopped = new Set<string>();
  let wakeAt: number | undefined;
  for (const o0 of sorted) {
    const o = byId.get(o0.id)!;
    if (o.userId !== userId || TERMINAL.has(state(o))) continue;
    if (stopped.has(o.lane)) continue;
    if (LANE_STOPPERS.has(o.state)) {
      stopped.add(o.lane);
      continue;
    }
    // This op is the head of its lane: whatever happens, nothing behind it goes first.
    stopped.add(o.lane);
    if (o.dependsOn && byId.get(o.dependsOn.opId)?.state !== 'done') continue;
    const due = o.nextAttemptAt ? Date.parse(o.nextAttemptAt) : 0;
    if (due > now) {
      wakeAt = wakeAt === undefined ? due : Math.min(wakeAt, due);
      continue;
    }
    return { updates, next: o };
  }
  return { updates, ...(wakeAt !== undefined ? { wakeAt } : {}) };
}

/** Done ops nobody waits for any more, older than the retention. */
export function prunable(all: readonly QueuedOp[], now: number): QueuedOp[] {
  const needed = new Set(
    all.filter((o) => o.dependsOn && o.state !== 'done').map((o) => o.dependsOn!.opId),
  );
  return all.filter(
    (o) =>
      o.state === 'done' &&
      !needed.has(o.id) &&
      now - Date.parse(o.updatedAt ?? o.createdAt) > DONE_RETENTION_MS,
  );
}

/** Rejects what offline may not do (29.2, INV-71): only the allowlisted operations, in their offline form. */
/** Whether a cash payment is fully matched: every match positive and their sum equal to the amount. */
export function amountEqualsMatches(amount: unknown, matches: readonly unknown[]): boolean {
  if (typeof amount !== 'number' || !Number.isSafeInteger(amount) || amount < 1) return false;
  let sum = 0;
  for (const m of matches) {
    const cents = (m as { amount_cents?: unknown } | null)?.amount_cents;
    if (typeof cents !== 'number' || !Number.isSafeInteger(cents) || cents < 1) return false;
    sum += cents;
  }
  return sum === amount;
}

function nonEmpty(v: unknown): boolean {
  return typeof v === 'string' && v.trim() !== '';
}

export function validateNewOp(op: NewOp): void {
  const kind = (OFFLINE_OPERATION_KINDS as Record<string, OfflineOpKind>)[op.request.operationId];
  if (kind === undefined || kind !== op.kind) {
    throw new OfflineForbiddenError(op.request.operationId, 'online_only');
  }
  if (!op.userId || !op.marinaId || !op.lane) {
    throw new OfflineForbiddenError(op.request.operationId, 'online_only');
  }
  const body = (op.request.body ?? {}) as Record<string, unknown>;
  switch (op.kind) {
    case 'presence.record':
      // An arrival without allocation creates a transit: never offline.
      if (
        (body.event !== 'arrived' && body.event !== 'departed') ||
        typeof body.allocation_id !== 'string' ||
        !body.allocation_id ||
        typeof body.expected_resource_id !== 'string' ||
        !body.expected_resource_id
      ) {
        throw new OfflineForbiddenError(op.request.operationId, 'allocation');
      }
      break;
    case 'presence.photo':
      if (!op.blob || op.blob.size === 0 || op.blob.size > MAX_PHOTO_BYTES) {
        throw new OfflineForbiddenError(op.request.operationId, 'online_only');
      }
      break;
    // M9 (m9-plan §10, INV-71): readings, field reports and outcomes never touch allocations; each carries the
    // client_ref the server dedupes on, and the photo uploads carry their bytes and depend on their parent op.
    case 'utility.reading':
    case 'maintenance.report':
      if (
        op.request.operationId === 'uploadMeterReadingPhoto' ||
        op.request.operationId === 'uploadMaintenancePhoto'
      ) {
        if (
          !op.blob ||
          op.blob.size === 0 ||
          op.blob.size > MAX_PHOTO_BYTES ||
          !op.blob.type.startsWith('image/')
        ) {
          throw new OfflineForbiddenError(op.request.operationId, 'online_only');
        }
        break;
      }
      if (
        !nonEmpty(body.client_ref) ||
        (op.kind === 'utility.reading' && (!nonEmpty(body.meter_id) || !nonEmpty(body.value))) ||
        (op.kind === 'maintenance.report' && !nonEmpty(body.title))
      ) {
        throw new OfflineForbiddenError(op.request.operationId, 'online_only');
      }
      break;
    case 'maintenance.outcome':
      if (!nonEmpty(body.client_ref) || !nonEmpty(body.outcome)) {
        throw new OfflineForbiddenError(op.request.operationId, 'online_only');
      }
      break;
    case 'payment.cash': {
      const matches = body.matches;
      if (
        body.method !== 'cash' ||
        body.kind !== 'payment' ||
        body.auto_match !== undefined ||
        !Array.isArray(matches) ||
        matches.length === 0 ||
        // Collection on existing documents only: the whole amount is matched, no unmatched credit (INV-87).
        !amountEqualsMatches(body.amount_cents, matches) ||
        // A receipt-mode charge numbers a commercial document: online only (unknown mode counts as receipt).
        op.issuesDocument !== false
      ) {
        throw new OfflineForbiddenError(op.request.operationId, 'document');
      }
      break;
    }
  }
}

// ---- the shared snapshot ----

type Listener = () => void;

const listeners = new Set<Listener>();
let snapshot: readonly QueuedOp[] = [];
let loaded: Promise<void> | null = null;
/** Bumped by resetQueueMemory: a read of the old database that finishes later is ignored. */
let generation = 0;
let channel: BroadcastChannel | null = null;
const kickers = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

function broadcast() {
  try {
    channel?.postMessage({ type: 'ops-changed' });
  } catch {
    // closed channel
  }
}

function ensureChannel() {
  if (channel || typeof BroadcastChannel === 'undefined') return;
  channel = new BroadcastChannel('porto-offline');
  channel.onmessage = () => {
    void reloadOps();
  };
}

/** Re-reads the ops store (another tab changed it, or the runner starts a drain). */
export async function reloadOps(): Promise<readonly QueuedOp[]> {
  if (!offlineStorageAvailable()) return snapshot;
  const gen = generation;
  try {
    const ops = await dbGetAll<QueuedOp>('ops');
    if (gen !== generation) return snapshot;
    snapshot = ops.sort(byAge);
  } catch {
    // keep the last snapshot
  }
  emit();
  return snapshot;
}

/** Loads the ops once per page. */
export function loadOps(): Promise<void> {
  ensureChannel();
  if (!loaded) loaded = reloadOps().then(() => undefined);
  return loaded;
}

export function subscribeOps(l: Listener): () => void {
  listeners.add(l);
  void loadOps();
  return () => listeners.delete(l);
}

export function getOps(): readonly QueuedOp[] {
  return snapshot;
}

/** Writes changed ops (store and snapshot) and tells the other tabs. */
export async function saveOps(changed: readonly QueuedOp[]): Promise<void> {
  if (changed.length === 0) return;
  const map = new Map(snapshot.map((o) => [o.id, o]));
  for (const o of changed) {
    await dbPut('ops', o);
    map.set(o.id, o);
  }
  snapshot = [...map.values()].sort(byAge);
  emit();
  broadcast();
}

async function removeOps(ids: readonly string[]): Promise<void> {
  if (ids.length === 0) return;
  for (const id of ids) {
    await dbDelete('ops', id);
    await dbDelete('blobs', id);
  }
  const drop = new Set(ids);
  snapshot = snapshot.filter((o) => !drop.has(o.id));
  emit();
  broadcast();
}

/** Runners register here so that enqueue and sendNow start a drain. */
export function onKick(fn: () => void): () => void {
  kickers.add(fn);
  return () => kickers.delete(fn);
}

export function kick(): void {
  for (const k of kickers) k();
}

/** Forgets the in-memory snapshot (after purgeOfflineData, and between tests). */
export function resetQueueMemory(): void {
  generation++;
  snapshot = [];
  loaded = null;
  emit();
}

// ---- photos ----

/** Re-encodes a photo as JPEG, at most 2048 px on the long side (strips EXIF and GPS), when the browser can;
 * otherwise returns it unchanged. */
export async function preparePhoto(blob: Blob): Promise<Blob> {
  if (typeof createImageBitmap !== 'function' || typeof OffscreenCanvas === 'undefined')
    return blob;
  try {
    const bmp = await createImageBitmap(blob);
    const scale = Math.min(1, MAX_PHOTO_PX / Math.max(bmp.width, bmp.height));
    const w = Math.max(1, Math.round(bmp.width * scale));
    const h = Math.max(1, Math.round(bmp.height * scale));
    const canvas = new OffscreenCanvas(w, h);
    const ctx = canvas.getContext('2d');
    if (!ctx) return blob;
    ctx.drawImage(bmp, 0, 0, w, h);
    bmp.close();
    const out = await canvas.convertToBlob({ type: 'image/jpeg', quality: 0.85 });
    return out.size > 0 && out.size <= MAX_PHOTO_BYTES ? out : blob;
  } catch {
    return blob;
  }
}

async function ensureSpace(): Promise<void> {
  const storage = typeof navigator === 'undefined' ? undefined : navigator.storage;
  if (!storage?.estimate) return;
  try {
    const { quota, usage } = await storage.estimate();
    if (quota !== undefined && usage !== undefined && quota - usage < MIN_FREE_BYTES) {
      throw new OfflineQuotaError();
    }
  } catch (err) {
    if (err instanceof OfflineQuotaError) throw err;
  }
}

let persistAsked = false;

function askPersistence() {
  if (persistAsked) return;
  persistAsked = true;
  try {
    void navigator.storage?.persist?.();
  } catch {
    // not supported
  }
}

// ---- mutations ----

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

/** Clock and id sources (tests replace them). */
export const queueDeps = {
  now: () => Date.now(),
  newId: () => crypto.randomUUID(),
  online: () => (typeof navigator === 'undefined' ? true : navigator.onLine),
};

/** Adds an allowlisted operation (queue first, D2). The Idempotency-Key is generated here, once. */
export async function enqueueOp(op: NewOp): Promise<QueuedOp> {
  validateNewOp(op);
  if (!offlineStorageAvailable())
    throw new OfflineForbiddenError(op.request.operationId, 'online_only');
  await loadOps();
  const now = queueDeps.now();
  const id = queueDeps.newId();
  const queued: QueuedOp = {
    id,
    kind: op.kind,
    key: queueDeps.newId(),
    userId: op.userId,
    marinaId: op.marinaId,
    createdAt: iso(now),
    updatedAt: iso(now),
    lane: op.lane,
    ...(op.dependsOn ? { dependsOn: op.dependsOn } : {}),
    request: { ...op.request },
    state: 'pending',
    attempts: 0,
    queuedOffline: !queueDeps.online(),
    label: op.label,
  };
  if (op.blob) {
    await ensureSpace();
    askPersistence();
    const photo = await preparePhoto(op.blob);
    const stored: StoredBlob = {
      id,
      type: photo.type || 'image/jpeg',
      bytes: await photo.arrayBuffer(),
    };
    await dbPut('blobs', stored);
    queued.request = { ...queued.request, blobId: id, contentType: stored.type };
  } else {
    askPersistence();
  }
  await saveOps([queued]);
  kick();
  return queued;
}

export async function readBlob(id: string): Promise<StoredBlob | undefined> {
  return dbGet<StoredBlob>('blobs', id);
}

/** Removes an op (the user discards it) and its photo; dependents become blocked on the next plan. */
export async function discardOp(id: string): Promise<void> {
  await removeOps([id]);
  kick();
}

/** Removes ops (pruning, purge). */
export async function dropOps(ids: readonly string[]): Promise<void> {
  await removeOps(ids);
}

/**
 * Re-queues a conflicted, blocked or expired op as a new operation: new id, new key, now as queued instant,
 * optionally a shallow patch of the JSON body (e.g. the new berth). Dependents follow the new op. Never automatic.
 */
export async function retryOpAsNew(id: string, patch?: unknown): Promise<QueuedOp | undefined> {
  await loadOps();
  const old = snapshot.find((o) => o.id === id);
  if (!old) return undefined;
  const now = queueDeps.now();
  const nid = queueDeps.newId();
  const body =
    patch && typeof patch === 'object' && old.request.body && typeof old.request.body === 'object'
      ? { ...(old.request.body as Record<string, unknown>), ...(patch as Record<string, unknown>) }
      : old.request.body;
  const fresh: QueuedOp = {
    ...old,
    id: nid,
    key: queueDeps.newId(),
    createdAt: iso(now),
    updatedAt: iso(now),
    request: { ...old.request, ...(body !== undefined ? { body } : {}) },
    state: 'pending',
    attempts: 0,
    queuedOffline: !queueDeps.online(),
  };
  delete fresh.problem;
  delete fresh.result;
  delete fresh.nextAttemptAt;
  if (old.request.blobId) {
    const blob = await readBlob(old.request.blobId);
    if (blob) {
      await dbPut('blobs', { ...blob, id: nid });
      fresh.request = { ...fresh.request, blobId: nid };
    }
  }
  const children = snapshot
    .filter((o) => o.dependsOn?.opId === id)
    .map((o): QueuedOp => ({
      ...o,
      dependsOn: { ...o.dependsOn!, opId: nid },
      state: o.state === 'blocked' ? 'pending' : o.state,
      updatedAt: iso(now),
    }));
  await saveOps([fresh, ...children]);
  await removeOps([id]);
  kick();
  return fresh;
}
