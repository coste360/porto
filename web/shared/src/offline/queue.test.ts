import { marinaBlockingEntry, noteMarinaSwitchBlocked, takeMarinaSwitchBlocked } from './index';
import { OfflineForbiddenError } from './types';
import {
  OP_EXPIRY_MS,
  enqueueOp,
  getOps,
  planNext,
  prunable,
  readBlob,
  reloadOps,
  resetQueueMemory,
  resolvePath,
  retryOpAsNew,
  saveOps,
  validateNewOp,
} from './queue';
import { resetDbForTests } from './db';
import {
  M,
  USER,
  cashNewOp,
  fixQueueClock,
  op,
  photoNewOp,
  presenceNewOp,
  resetOffline,
  setOnline,
} from './testkit';

const NOW = Date.parse('2027-06-05T10:00:00Z');

describe('planNext', () => {
  it('sends the oldest op first, one at a time', () => {
    const p = planNext([op('3'), op('1'), op('2')], NOW, USER);
    expect(p.next?.id).toBe('1');
  });

  it('keeps the order within a lane and lets other lanes go when a lane is stopped', () => {
    const ops = [
      op('1', { lane: 'vessel:a', state: 'conflict' }),
      op('2', { lane: 'vessel:a' }),
      op('3', { lane: 'vessel:b' }),
    ];
    expect(planNext(ops, NOW, USER).next?.id).toBe('3');
  });

  it('waits for a retrying head until it is due, without overtaking it in its lane', () => {
    const ops = [
      op('1', { lane: 'vessel:a', state: 'retrying', nextAttemptAt: '2027-06-05T10:00:05Z' }),
      op('2', { lane: 'vessel:a' }),
    ];
    const p = planNext(ops, NOW, USER);
    expect(p.next).toBeUndefined();
    expect(p.wakeAt).toBe(Date.parse('2027-06-05T10:00:05Z'));
    expect(planNext(ops, NOW + 5_000, USER).next?.id).toBe('1');
  });

  it('sends a photo only after its presence is done, and binds the presence id', () => {
    const parent = op('1', { lane: 'vessel:a' });
    const photo = op('2', {
      lane: 'vessel:a',
      kind: 'presence.photo',
      dependsOn: { opId: '1', bind: { presenceId: 'presence.id' } },
      request: { operationId: 'uploadPresencePhoto', path: '/m/presences/{presenceId}/photo' },
    });
    expect(planNext([parent, photo], NOW, USER).next?.id).toBe('1');
    const done = { ...parent, state: 'done' as const, result: { presence: { id: 'p 9' } } };
    expect(planNext([done, photo], NOW, USER).next?.id).toBe('2');
    expect(resolvePath(photo, done)).toBe('/m/presences/p%209/photo');
    expect(resolvePath(photo, parent)).toBeNull();
    expect(resolvePath(photo, { ...done, result: {} })).toBeNull();
  });

  it('blocks the dependents of a conflicted or discarded parent', () => {
    const photo = op('2', { lane: 'vessel:a', dependsOn: { opId: '1', bind: {} } });
    const conflicted = planNext(
      [op('1', { lane: 'vessel:a', state: 'conflict' }), photo],
      NOW,
      USER,
    );
    expect(conflicted.updates.map((u) => [u.id, u.state])).toEqual([['2', 'blocked']]);
    const orphan = planNext([photo], NOW, USER);
    expect(orphan.updates.map((u) => [u.id, u.state])).toEqual([['2', 'blocked']]);
  });

  it('expires ops older than 6 days and never sends them', () => {
    const old = op('1', { createdAt: new Date(NOW - OP_EXPIRY_MS - 1).toISOString() });
    const p = planNext([old], NOW, USER);
    expect(p.updates[0]?.state).toBe('expired');
    expect(p.next).toBeUndefined();
    const young = op('2', { createdAt: new Date(NOW - OP_EXPIRY_MS + 60_000).toISOString() });
    expect(planNext([young], NOW, USER).next?.id).toBe('2');
  });

  it('never sends the ops of another user', () => {
    const p = planNext([op('1', { userId: 'someone-else' })], NOW, USER);
    expect(p.next).toBeUndefined();
    expect(planNext([op('1')], NOW, null).next).toBeUndefined();
  });

  it('prunes done ops after an hour unless a dependent still needs them', () => {
    const doneOld = op('1', { state: 'done', updatedAt: '2027-06-05T08:00:00Z' });
    const doneNeeded = op('2', { state: 'done', updatedAt: '2027-06-05T08:00:00Z' });
    const child = op('3', { dependsOn: { opId: '2', bind: {} } });
    const doneRecent = op('4', { state: 'done', updatedAt: '2027-06-05T09:59:00Z' });
    expect(prunable([doneOld, doneNeeded, child, doneRecent], NOW).map((o) => o.id)).toEqual(['1']);
  });
});

describe('validateNewOp (INV-71 on the client)', () => {
  it('accepts the three offline operations in their offline form', () => {
    expect(() => validateNewOp(presenceNewOp('v1'))).not.toThrow();
    expect(() => validateNewOp(photoNewOp('v1', 'x'))).not.toThrow();
    expect(() => validateNewOp(cashNewOp('c1'))).not.toThrow();
  });

  it('refuses everything else', () => {
    const noAlloc = presenceNewOp('v1');
    noAlloc.request.body = { vessel_id: 'v1', event: 'arrived' };
    const haul = presenceNewOp('v1');
    haul.request.body = { ...(haul.request.body as object), event: 'hauled_out' };
    const card = cashNewOp('c1');
    card.request.body = { ...(card.request.body as object), method: 'card_pos' };
    const auto = cashNewOp('c1');
    auto.request.body = {
      kind: 'payment',
      method: 'cash',
      amount_cents: 1,
      auto_match: 'oldest_due_first',
    };
    const transit = {
      ...presenceNewOp('v1'),
      request: { operationId: 'createTransit', path: '/t' },
    };
    const mismatch = { ...cashNewOp('c1'), kind: 'presence.record' as const };
    const noPhoto = { ...photoNewOp('v1', 'x') };
    delete noPhoto.blob;
    for (const bad of [noAlloc, haul, card, auto, transit, mismatch, noPhoto]) {
      expect(() => validateNewOp(bad)).toThrow(OfflineForbiddenError);
    }
    try {
      validateNewOp(noAlloc);
    } catch (e) {
      expect((e as OfflineForbiddenError).reason).toBe('allocation');
    }
  });

  it('refuses cash on a receipt-mode charge, of unknown mode, or not matched in full (document)', () => {
    const receipt = { ...cashNewOp('c1'), issuesDocument: true };
    const unknown = { ...cashNewOp('c1') };
    delete unknown.issuesDocument;
    const credit = cashNewOp('c1');
    credit.request.body = { ...(credit.request.body as object), amount_cents: 1500 };
    const zero = cashNewOp('c1');
    zero.request.body = {
      ...(zero.request.body as object),
      amount_cents: 1000,
      matches: [
        { charge_id: 'c1', amount_cents: 1000 },
        { charge_id: 'c2', amount_cents: 0 },
      ],
    };
    for (const bad of [receipt, unknown, credit, zero]) {
      let error: unknown;
      try {
        validateNewOp(bad);
      } catch (e) {
        error = e;
      }
      expect(error).toBeInstanceOf(OfflineForbiddenError);
      expect((error as OfflineForbiddenError).reason).toBe('document');
    }
  });
});

describe('enqueue', () => {
  beforeEach(() => {
    resetOffline();
    fixQueueClock();
  });

  it('fixes the key at enqueue and keeps it across reloads', async () => {
    const q = await enqueueOp(presenceNewOp('v1'));
    expect(q.key).toMatch(/^[0-9a-f-]{36}$/);
    expect(q.state).toBe('pending');
    expect(q.queuedOffline).toBe(false);
    resetQueueMemory();
    resetDbForTests();
    const reloaded = await reloadOps();
    expect(reloaded.map((o) => [o.id, o.key])).toEqual([[q.id, q.key]]);
  });

  it('marks ops queued while offline and stores photo bytes apart', async () => {
    setOnline(false);
    const parent = await enqueueOp(presenceNewOp('v1'));
    const photo = await enqueueOp(photoNewOp('v1', parent.id));
    expect(parent.queuedOffline).toBe(true);
    expect(photo.request.blobId).toBe(photo.id);
    const blob = await readBlob(photo.id);
    expect(new Uint8Array(blob!.bytes)).toEqual(new Uint8Array([0xff, 0xd8, 0xff, 1, 2, 3]));
    expect(getOps()).toHaveLength(2);
  });

  it('keeps every op of concurrent enqueues in the snapshot (two photos taken in a row)', async () => {
    const parent = await enqueueOp(presenceNewOp('v1'));
    const [a, b] = await Promise.all([
      enqueueOp(photoNewOp('v1', parent.id)),
      enqueueOp(photoNewOp('v1', parent.id)),
    ]);
    const ids = getOps().map((o) => o.id);
    expect(ids).toContain(a.id);
    expect(ids).toContain(b.id);
    expect(ids).toHaveLength(3);
  });

  it('keeps a state saved during a reload (the reload does not bring back the older copy)', async () => {
    const q = await enqueueOp(presenceNewOp('v1'));
    const reload = reloadOps();
    await saveOps([{ ...q, state: 'done' }]);
    await reload;
    expect(getOps().find((o) => o.id === q.id)?.state).toBe('done');
  });

  it('retries as a new operation with a new key and moves the dependents', async () => {
    const parent = await enqueueOp(presenceNewOp('v1'));
    const photo = await enqueueOp(photoNewOp('v1', parent.id));
    const fresh = await retryOpAsNew(parent.id, { expected_resource_id: 'b-new' });
    expect(fresh).toBeDefined();
    expect(fresh!.key).not.toBe(parent.key);
    expect((fresh!.request.body as Record<string, unknown>).expected_resource_id).toBe('b-new');
    const ops = getOps();
    expect(ops.find((o) => o.id === parent.id)).toBeUndefined();
    expect(ops.find((o) => o.id === photo.id)?.dependsOn?.opId).toBe(fresh!.id);
  });
});

describe('marina switch guard (D9)', () => {
  beforeEach(() => {
    resetOffline();
    fixQueueClock();
  });

  it('names the marina with unsent ops after a reload, null for that marina or another user', async () => {
    setOnline(false);
    await enqueueOp(presenceNewOp('v1'));
    resetQueueMemory();
    const other = '01a1b84d-0000-7000-8000-000000000000';
    expect(await marinaBlockingEntry(USER, other)).toBe(M);
    expect(await marinaBlockingEntry(USER, M)).toBeNull();
    expect(await marinaBlockingEntry('someone-else', other)).toBeNull();
    noteMarinaSwitchBlocked();
    expect(takeMarinaSwitchBlocked()).toBe(true);
    expect(takeMarinaSwitchBlocked()).toBe(false);
  });
});
