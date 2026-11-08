// The runner (m6b-plan §4.3): sends the queued operations of the signed-in user, one at a time, oldest first, as
// planned by queue.planNext, and classifies each answer (classify.ts). A single runner per origin: it holds the
// Web Lock "porto-offline-runner" while it drains, so two tabs never send the same op; without the Locks API
// (old engines) the tab drains alone, the BroadcastChannel keeps the snapshots in step and the server's
// idempotency makes a rare double send harmless (INV-83).
//
// Every attempt carries the op's Idempotency-Key. Porto-Offline-Queued-At (the enqueue instant) marks an offline
// replay: it is sent when the op was enqueued offline and on every attempt after the first, so that the server
// applies the offline rules (OfflineGuard, INV-71) and keeps the key 7 days.
import { ProblemError } from '../api/problem';
import { readCookie } from '../lib/cookies';
import { classify } from './classify';
import { dbDelete } from './db';
import { reportNetworkFailure, reportNetworkSuccess } from './online';
import {
  dropOps,
  getOps,
  kick,
  onKick,
  planNext,
  prunable,
  readBlob,
  reloadOps,
  resolvePath,
  saveOps,
} from './queue';
import type { QueuedOp } from './types';

export const LOCK_NAME = 'porto-offline-runner';
export const OFFLINE_QUEUED_AT_HEADER = 'Porto-Offline-Queued-At';
export const IDEMPOTENCY_HEADER = 'Idempotency-Key';
export const PERIODIC_MS = 30_000;

type LockManagerLike = {
  request: (
    name: string,
    options: { ifAvailable: boolean },
    cb: (lock: unknown) => Promise<void>,
  ) => Promise<unknown>;
};

export type RunnerDeps = {
  fetch: typeof fetch;
  now: () => number;
  random: () => number;
  locks?: LockManagerLike | undefined;
  readCsrf: () => string | undefined;
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (t: unknown) => void;
};

export type RunnerEvents = {
  /** An op finished: the caller invalidates the queries it touched. */
  onDone?: (op: QueuedOp) => void;
  /** 401: the runner paused until the same user signs in again. */
  onAuthRequired?: () => void;
};

function defaultDeps(): RunnerDeps {
  const locks =
    typeof navigator !== 'undefined' && 'locks' in navigator
      ? (navigator.locks as unknown as LockManagerLike)
      : undefined;
  return {
    fetch: (...a) => fetch(...a),
    now: () => Date.now(),
    random: () => Math.random(),
    locks,
    readCsrf: () => (typeof document === 'undefined' ? undefined : readCookie('porto_csrf')),
    setTimer: (fn, ms) => setTimeout(fn, ms),
    clearTimer: (t) => clearTimeout(t as ReturnType<typeof setTimeout>),
  };
}

export type SendResult = {
  status: number;
  problemType?: string;
  retryAfter?: string | null;
  body?: unknown;
  details?: unknown;
};

export class Runner {
  private deps: RunnerDeps;
  private events: RunnerEvents;
  private userId: string | null = null;
  private paused = false;
  private draining: Promise<void> | null = null;
  private again = false;
  private wake: unknown = null;
  private unkick: () => void;

  constructor(deps: Partial<RunnerDeps> = {}, events: RunnerEvents = {}) {
    this.deps = { ...defaultDeps(), ...deps };
    this.events = events;
    this.unkick = onKick(() => this.kick());
  }

  setEvents(events: RunnerEvents): void {
    this.events = events;
  }

  /** The signed-in user (null: nobody). A new sign-in of a user resumes a paused runner; ops of other users are
   * never sent. */
  setUser(userId: string | null): void {
    const resumed = userId !== null && (userId !== this.userId || this.paused);
    this.userId = userId;
    if (resumed) this.paused = false;
    if (resumed) this.kick();
  }

  get isPaused(): boolean {
    return this.paused;
  }

  get user(): string | null {
    return this.userId;
  }

  dispose(): void {
    this.unkick();
    if (this.wake) this.deps.clearTimer(this.wake);
    this.wake = null;
  }

  /** Starts a drain (or schedules another pass after the current one). */
  kick(): void {
    if (this.draining) {
      this.again = true;
      return;
    }
    this.draining = this.drainWithLock().finally(() => {
      this.draining = null;
      if (this.again) {
        this.again = false;
        this.kick();
      }
    });
  }

  /** Resolves when the current drain (if any) ended. */
  async idle(): Promise<void> {
    while (this.draining) await this.draining;
  }

  private async drainWithLock(): Promise<void> {
    if (!this.userId || this.paused) return;
    // The browser knows it has no network: wait for the online event (or the periodic pass).
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      this.schedule(undefined);
      return;
    }
    const locks = this.deps.locks;
    if (!locks) {
      await this.drain();
      return;
    }
    await locks.request(LOCK_NAME, { ifAvailable: true }, async (lock) => {
      if (!lock) return; // another tab is draining
      await this.drain();
    });
  }

  private schedule(at: number | undefined) {
    if (this.wake) this.deps.clearTimer(this.wake);
    this.wake = null;
    const now = this.deps.now();
    const ms = at === undefined ? PERIODIC_MS : Math.max(0, at - now);
    const pending = getOps().some((o) => o.state !== 'done');
    if (!pending) return;
    this.wake = this.deps.setTimer(
      () => {
        this.wake = null;
        this.kick();
      },
      Math.min(ms, PERIODIC_MS),
    );
  }

  /** Sends ops until nothing is due. */
  private async drain(): Promise<void> {
    let ops = await reloadOps();
    for (let guard = 0; guard < 1000; guard++) {
      if (!this.userId || this.paused) return;
      const now = this.deps.now();
      const plan = planNext(ops, now, this.userId);
      if (plan.updates.length)
        await saveOps(plan.updates.map((o) => ({ ...o, updatedAt: iso(now) })));
      const old = prunable(getOps(), now);
      if (old.length) await dropOps(old.map((o) => o.id));
      if (!plan.next) {
        this.schedule(plan.wakeAt);
        return;
      }
      const stop = await this.sendOne(plan.next);
      if (stop) {
        this.schedule(undefined);
        return;
      }
      ops = getOps();
    }
  }

  /** Sends one op and records the outcome. Returns true when the drain must stop (offline, 401). */
  private async sendOne(op: QueuedOp): Promise<boolean> {
    const parent = op.dependsOn ? getOps().find((o) => o.id === op.dependsOn!.opId) : undefined;
    const path = resolvePath(op, parent);
    const now = this.deps.now();
    if (!path) {
      await saveOps([{ ...op, state: 'blocked', updatedAt: iso(now) }]);
      return false;
    }
    const attempts = op.attempts + 1;
    await saveOps([{ ...op, state: 'sending', attempts, updatedAt: iso(now) }]);
    const res = await this.send(op, path, attempts);
    const at = this.deps.now();
    if (res.status === 0) reportNetworkFailure();
    else reportNetworkSuccess();
    const outcome = classify(res, attempts, this.deps.random());
    const cur = getOps().find((o) => o.id === op.id);
    if (!cur) return false; // discarded meanwhile
    const base = { ...cur, attempts, updatedAt: iso(at) };
    delete base.nextAttemptAt;
    switch (outcome.kind) {
      case 'done': {
        const doneOp: QueuedOp = { ...base, state: 'done', result: res.body };
        delete doneOp.problem;
        await saveOps([doneOp]);
        if (doneOp.request.blobId) await dropBlob(doneOp);
        this.events.onDone?.(doneOp);
        return false;
      }
      case 'retry':
        await saveOps([{ ...base, state: 'retrying', nextAttemptAt: iso(at + outcome.afterMs) }]);
        return res.status === 0;
      case 'auth':
        await saveOps([{ ...base, state: 'pending', attempts: op.attempts }]);
        this.paused = true;
        this.events.onAuthRequired?.();
        return true;
      case 'conflict':
        await saveOps([
          {
            ...base,
            state: 'conflict',
            problem: {
              status: res.status,
              type: res.problemType ?? 'internal_error',
              ...(res.details !== undefined ? { detail: res.details } : {}),
            },
          },
        ]);
        return false;
    }
  }

  private async send(op: QueuedOp, path: string, attempts: number): Promise<SendResult> {
    const headers = new Headers({
      Accept: 'application/json, application/problem+json',
      [IDEMPOTENCY_HEADER]: op.key,
    });
    if (op.queuedOffline || attempts > 1) headers.set(OFFLINE_QUEUED_AT_HEADER, op.createdAt);
    const csrf = this.deps.readCsrf();
    if (csrf) headers.set('X-CSRF-Token', csrf);
    let body: BodyInit | undefined;
    if (op.request.blobId) {
      const blob = await readBlob(op.request.blobId);
      if (!blob) return { status: 422, problemType: 'invalid_file' };
      headers.set('Content-Type', op.request.contentType ?? blob.type);
      body = new Blob([blob.bytes], { type: blob.type });
    } else if (op.request.body !== undefined) {
      headers.set('Content-Type', 'application/json');
      body = JSON.stringify(op.request.body);
    }
    let response: Response;
    try {
      response = await this.deps.fetch(path, {
        method: 'POST',
        headers,
        ...(body !== undefined ? { body } : {}),
        credentials: 'same-origin',
      });
    } catch {
      return { status: 0 };
    }
    let parsed: unknown;
    try {
      const text = await response.text();
      parsed = text ? JSON.parse(text) : undefined;
    } catch {
      parsed = undefined;
    }
    if (response.ok) return { status: response.status, body: parsed };
    const problem = ProblemError.from(response.status, parsed, response.headers);
    return {
      status: response.status,
      problemType: problem.type,
      retryAfter: response.headers.get('Retry-After'),
      ...(Object.keys(problem.details).length ? { details: problem.details } : {}),
    };
  }
}

async function dropBlob(op: QueuedOp) {
  if (op.request.blobId) await dbDelete('blobs', op.request.blobId);
}

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

let shared: Runner | null = null;

/** The runner of this page (created on first use). */
export function getRunner(): Runner {
  if (!shared) shared = new Runner();
  return shared;
}

/** Replaces the page runner (tests). */
export function setRunnerForTests(r: Runner | null): void {
  shared?.dispose();
  shared = r;
}

/** Starts a drain now ("Invia ora", online event, SSE reconnect, page visible). */
export function sendQueueNow(): void {
  kick();
}
