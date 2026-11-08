// Test helpers of the offline module (imported by *.test.ts only): a fresh fake IndexedDB per test, a controllable
// navigator.onLine, op builders and a scripted fetch.
import { IDBFactory } from 'fake-indexeddb';

import { resetDbForTests } from './db';
import { resetOnlineForTests } from './online';
import { queueDeps, resetQueueMemory } from './queue';
import { setRunnerForTests } from './runner';
import type { NewOp, QueuedOp } from './types';

let online = true;

/** Fresh database, memory and online status. */
export function resetOffline(): void {
  globalThis.indexedDB = new IDBFactory();
  resetDbForTests();
  resetQueueMemory();
  resetOnlineForTests();
  setRunnerForTests(null);
  setOnline(true);
}

export function setOnline(v: boolean): void {
  online = v;
  Object.defineProperty(window.navigator, 'onLine', { configurable: true, get: () => online });
}

let seq = 0;

/** Deterministic ids and clock for queue mutations. */
export function fixQueueClock(start = Date.parse('2027-06-05T09:00:00Z')): {
  advance: (ms: number) => void;
  now: () => number;
} {
  let t = start;
  queueDeps.now = () => t;
  queueDeps.newId = () => `00000000-0000-4000-8000-${String(++seq).padStart(12, '0')}`;
  queueDeps.online = () => online;
  return { advance: (ms) => (t += ms), now: () => t };
}

export const M = '01a1b84d-26ff-776e-b32d-5c50858aa748';
export const USER = 'user-1';

export function presenceNewOp(
  vessel: string,
  event: 'arrived' | 'departed' = 'arrived',
  user = USER,
): NewOp {
  return {
    kind: 'presence.record',
    userId: user,
    marinaId: M,
    lane: `vessel:${vessel}`,
    request: {
      operationId: 'recordPresence',
      path: `/api/admin/v1/marinas/${M}/presences`,
      body: {
        vessel_id: vessel,
        event,
        allocation_id: `a-${vessel}`,
        expected_resource_id: `b-${vessel}`,
      },
    },
    label: { key: 'offline:queue.label.arrived', params: { vessel } },
  };
}

export function photoNewOp(vessel: string, parentOpId: string): NewOp {
  return {
    kind: 'presence.photo',
    userId: USER,
    marinaId: M,
    lane: `vessel:${vessel}`,
    dependsOn: { opId: parentOpId, bind: { presenceId: 'presence.id' } },
    request: {
      operationId: 'uploadPresencePhoto',
      path: `/api/admin/v1/marinas/${M}/presences/{presenceId}/photo`,
      contentType: 'image/jpeg',
    },
    blob: new Blob([new Uint8Array([0xff, 0xd8, 0xff, 1, 2, 3])], { type: 'image/jpeg' }),
    label: { key: 'offline:queue.label.photo', params: { vessel } },
  };
}

export function cashNewOp(charge: string, amount = 1000): NewOp {
  return {
    kind: 'payment.cash',
    userId: USER,
    marinaId: M,
    lane: `charge:${charge}`,
    request: {
      operationId: 'recordPayment',
      path: `/api/admin/v1/marinas/${M}/payments`,
      body: {
        kind: 'payment',
        method: 'cash',
        amount_cents: amount,
        matches: [{ charge_id: charge, amount_cents: amount }],
      },
    },
    label: { key: 'offline:queue.label.cash', params: { amount: '10,00 €' } },
    issuesDocument: false,
  };
}

/** A QueuedOp literal for the pure planner. */
export function op(id: string, patch: Partial<QueuedOp> = {}): QueuedOp {
  return {
    id,
    kind: 'presence.record',
    key: `key-${id}`,
    userId: USER,
    marinaId: M,
    createdAt: `2027-06-05T09:00:${id.padStart(2, '0')}Z`,
    lane: `vessel:${id}`,
    request: { operationId: 'recordPresence', path: '/p' },
    state: 'pending',
    attempts: 0,
    label: { key: 'k', params: {} },
    ...patch,
  };
}

export type Call = { url: string; headers: Headers; body: unknown };

/** A fetch answering from a script of responses (or a function of the call). */
export function scriptedFetch(
  answer: (call: Call, n: number) => Response | Error | Promise<Response | Error>,
) {
  const calls: Call[] = [];
  const fn = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    const raw = init?.body;
    let body: unknown = raw;
    if (typeof raw === 'string') body = JSON.parse(raw);
    const call = { url: String(input), headers, body };
    calls.push(call);
    const r = await answer(call, calls.length);
    if (r instanceof Error) throw r;
    return r;
  }) as typeof fetch;
  return { fetch: fn, calls };
}

export function json(
  status: number,
  body: unknown,
  headers: Record<string, string> = {},
): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      ...headers,
    },
  });
}

export function problem(
  status: number,
  type: string,
  extra: Record<string, unknown> = {},
): Response {
  return json(status, { type, title: type, status, ...extra });
}
