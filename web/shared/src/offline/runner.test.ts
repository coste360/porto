import { enqueueOp, getOps } from './queue';
import { LOCK_NAME, OFFLINE_QUEUED_AT_HEADER, Runner, type RunnerDeps } from './runner';
import { getOnlineStatus } from './online';
import {
  USER,
  cashNewOp,
  fixQueueClock,
  json,
  photoNewOp,
  presenceNewOp,
  problem,
  resetOffline,
  scriptedFetch,
  setOnline,
} from './testkit';

type Clock = ReturnType<typeof fixQueueClock>;

function makeRunner(clock: Clock, fetch: typeof globalThis.fetch, extra: Partial<RunnerDeps> = {}) {
  const r = new Runner({
    fetch,
    now: clock.now,
    random: () => 0.5,
    locks: undefined,
    readCsrf: () => 'csrf-1',
    setTimer: () => 0,
    clearTimer: () => undefined,
    ...extra,
  });
  return r;
}

async function drained(r: Runner) {
  r.kick();
  await r.idle();
}

const presenceResult = { presence: { id: 'presence-1' } };

describe('runner', () => {
  let clock: Clock;
  beforeEach(() => {
    resetOffline();
    clock = fixQueueClock();
  });

  it('queue first: sends at once online, without the replay header on the first attempt', async () => {
    const { fetch, calls } = scriptedFetch(() => json(201, presenceResult));
    const r = makeRunner(clock, fetch);
    r.setUser(USER);
    const q = await enqueueOp(presenceNewOp('v1'));
    await r.idle();
    expect(calls).toHaveLength(1);
    expect(calls[0]!.headers.get('Idempotency-Key')).toBe(q.key);
    expect(calls[0]!.headers.get('X-CSRF-Token')).toBe('csrf-1');
    expect(calls[0]!.headers.get(OFFLINE_QUEUED_AT_HEADER)).toBeNull();
    const done = getOps().find((o) => o.id === q.id)!;
    expect(done.state).toBe('done');
    expect(done.result).toEqual(presenceResult);
    r.dispose();
  });

  it('replays with the method of the request, POST by default', async () => {
    const { fetch, calls } = scriptedFetch(() => json(200, {}));
    const r = makeRunner(clock, fetch);
    r.setUser(USER);
    const put = presenceNewOp('v1');
    await enqueueOp({ ...put, request: { ...put.request, method: 'PUT' } });
    await enqueueOp(presenceNewOp('v2'));
    await drained(r);
    expect(calls.map((c) => c.method)).toEqual(['PUT', 'POST']);
    r.dispose();
  });

  it('a lost response is retried with the same key and the replay header; the second answer settles it', async () => {
    let n = 0;
    const { fetch, calls } = scriptedFetch(() =>
      ++n === 1 ? new TypeError('network') : json(201, presenceResult),
    );
    const r = makeRunner(clock, fetch);
    r.setUser(USER);
    const q = await enqueueOp(presenceNewOp('v1'));
    await r.idle();
    const after = getOps().find((o) => o.id === q.id)!;
    expect(after.state).toBe('retrying');
    expect(Date.parse(after.nextAttemptAt!) - clock.now()).toBe(1_000);
    expect(getOnlineStatus().online).toBe(false);
    clock.advance(1_000);
    await drained(r);
    expect(calls.map((c) => c.headers.get('Idempotency-Key'))).toEqual([q.key, q.key]);
    expect(calls[1]!.headers.get(OFFLINE_QUEUED_AT_HEADER)).toBe(q.createdAt);
    expect(getOps().find((o) => o.id === q.id)!.state).toBe('done');
    expect(getOnlineStatus().online).toBe(true);
    r.dispose();
  });

  it('ops queued offline carry Porto-Offline-Queued-At from the first attempt', async () => {
    setOnline(false);
    const q = await enqueueOp(cashNewOp('c1'));
    setOnline(true);
    const { fetch, calls } = scriptedFetch(() => json(201, { id: 'pay-1' }));
    const r = makeRunner(clock, fetch);
    r.setUser(USER);
    await r.idle();
    expect(calls[0]!.headers.get(OFFLINE_QUEUED_AT_HEADER)).toBe(q.createdAt);
    r.dispose();
  });

  it('honours Retry-After and keeps the key on 409 idempotency_in_progress', async () => {
    let n = 0;
    const { fetch, calls } = scriptedFetch(() =>
      ++n === 1 ? problem(409, 'idempotency_in_progress') : json(201, presenceResult),
    );
    const r = makeRunner(clock, fetch);
    r.setUser(USER);
    const q = await enqueueOp(presenceNewOp('v1'));
    await r.idle();
    expect(getOps()[0]!.state).toBe('retrying');
    clock.advance(1_000);
    await drained(r);
    expect(new Set(calls.map((c) => c.headers.get('Idempotency-Key')))).toEqual(new Set([q.key]));
    r.dispose();
  });

  it('a conflict keeps its problem, blocks the photo, and other lanes continue', async () => {
    const { fetch, calls } = scriptedFetch((c) =>
      c.url.endsWith('/presences')
        ? problem(409, 'allocation_changed', {
            details: { current_resource_id: 'b9', current_resource_code: 'A12' },
          })
        : json(201, { id: 'pay-1' }),
    );
    const r = makeRunner(clock, fetch);
    setOnline(false);
    const parent = await enqueueOp(presenceNewOp('v1'));
    const photo = await enqueueOp(photoNewOp('v1', parent.id));
    const cash = await enqueueOp(cashNewOp('c1'));
    setOnline(true);
    r.setUser(USER);
    await r.idle();
    await drained(r);
    const byId = new Map(getOps().map((o) => [o.id, o]));
    expect(byId.get(parent.id)!.state).toBe('conflict');
    expect(byId.get(parent.id)!.problem).toEqual({
      status: 409,
      type: 'allocation_changed',
      detail: { current_resource_id: 'b9', current_resource_code: 'A12' },
    });
    expect(byId.get(photo.id)!.state).toBe('blocked');
    expect(byId.get(cash.id)!.state).toBe('done');
    expect(calls.filter((c) => c.url.includes('/photo'))).toHaveLength(0);
    r.dispose();
  });

  it('sends the photo after its presence with the bound id and the stored bytes', async () => {
    const { fetch, calls } = scriptedFetch((c) =>
      c.url.endsWith('/presences') ? json(201, presenceResult) : json(200, { file_id: 'f1' }),
    );
    const r = makeRunner(clock, fetch);
    setOnline(false);
    const parent = await enqueueOp(presenceNewOp('v1'));
    await enqueueOp(photoNewOp('v1', parent.id));
    setOnline(true);
    r.setUser(USER);
    await r.idle();
    await drained(r);
    expect(calls.map((c) => c.url)).toEqual([
      expect.stringMatching(/\/presences$/),
      expect.stringMatching(/\/presences\/presence-1\/photo$/),
    ]);
    expect(calls[1]!.headers.get('Content-Type')).toBe('image/jpeg');
    expect(calls[1]!.body).toBeInstanceOf(Blob);
    expect(getOps().every((o) => o.state === 'done')).toBe(true);
    r.dispose();
  });

  it('401 pauses the runner; ops of a different user are never sent', async () => {
    const { fetch, calls } = scriptedFetch(() => problem(401, 'unauthorized'));
    const onAuth = vi.fn();
    const r = makeRunner(clock, fetch);
    r.setEvents({ onAuthRequired: onAuth });
    r.setUser(USER);
    const q = await enqueueOp(presenceNewOp('v1'));
    await r.idle();
    expect(r.isPaused).toBe(true);
    expect(onAuth).toHaveBeenCalledTimes(1);
    expect(getOps().find((o) => o.id === q.id)!.state).toBe('pending');
    // Another user signs in on this device: the first user's op stays frozen.
    const ok = scriptedFetch(() => json(201, presenceResult));
    const r2 = makeRunner(clock, ok.fetch);
    r2.setUser('user-2');
    await r2.idle();
    expect(ok.calls).toHaveLength(0);
    // The same user signs in again: it is sent.
    r2.setUser(USER);
    await r2.idle();
    expect(ok.calls).toHaveLength(1);
    expect(calls).toHaveLength(1);
    r.dispose();
    r2.dispose();
  });

  it('only the lock holder drains: two runners never send the same op', async () => {
    let held = false;
    const locks = {
      request: async (
        name: string,
        _o: { ifAvailable: boolean },
        cb: (lock: unknown) => Promise<void>,
      ) => {
        expect(name).toBe(LOCK_NAME);
        if (held) return cb(null);
        held = true;
        try {
          return await cb({ name });
        } finally {
          held = false;
        }
      },
    };
    let release: () => void = () => {};
    const gate = new Promise<void>((res) => (release = res));
    const { fetch, calls } = scriptedFetch(async () => {
      await gate;
      return json(201, presenceResult);
    });
    setOnline(false);
    await enqueueOp(presenceNewOp('v1'));
    setOnline(true);
    const a = makeRunner(clock, fetch, { locks });
    const b = makeRunner(clock, fetch, { locks });
    a.setUser(USER);
    b.setUser(USER);
    await new Promise((res) => setTimeout(res, 10));
    release();
    await a.idle();
    await b.idle();
    expect(calls).toHaveLength(1);
    a.dispose();
    b.dispose();
  });
});
