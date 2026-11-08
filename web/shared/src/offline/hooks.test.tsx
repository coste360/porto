import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';

import { createApiClient } from '../api/client';
import { ApiProvider } from '../api/context';
import * as billing from '../api/hooks/billing';
import * as desk from '../api/hooks/desk';
import * as opsmap from '../api/hooks/opsmap';
import * as registry from '../api/hooks/registry';
import { queryKeys } from '../api/queryKeys';
import { dbGet, dbGetAll } from './db';
import { saveBundle } from './persist';
import { OfflineForbiddenError, isQueuedOffline, type OfflineQueuedError } from './types';
import { getOps } from './queue';
import { Runner, setRunnerForTests } from './runner';
import {
  M,
  USER,
  fixQueueClock,
  json,
  problem,
  resetOffline,
  scriptedFetch,
  setOnline,
} from './testkit';

type Answer = Parameters<typeof scriptedFetch>[0];

let current: Runner | null = null;

function setup(answer: Answer = () => json(200, {})) {
  const clock = fixQueueClock(Date.now());
  const net = scriptedFetch(answer);
  const runner = new Runner({
    fetch: net.fetch,
    now: clock.now,
    random: () => 0.5,
    locks: undefined,
    readCsrf: () => undefined,
    setTimer: () => 0,
    clearTimer: () => undefined,
  });
  setRunnerForTests(runner);
  current = runner;
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  qc.setQueryData(queryKeys.me, { user: { id: USER }, marinas: [] });
  const api = createApiClient({
    fetch: net.fetch,
    baseUrl: 'http://localhost',
    readCsrf: () => undefined,
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <ApiProvider client={api}>{children}</ApiProvider>
    </QueryClientProvider>
  );
  return { wrapper, net, runner, qc };
}

async function hookOf<T>(fn: () => T, wrapper: (p: { children: ReactNode }) => ReactNode) {
  const r = renderHook(fn, { wrapper });
  await waitFor(() => expect(r.result.current).not.toBeNull());
  return r;
}

/** Online-only hooks and a variable that would be valid online. */
const FORBIDDEN: [string, () => { mutateAsync: (v: never) => Promise<unknown> }][] = [
  ['useRecordUnexpectedArrival', () => desk.useRecordUnexpectedArrival(M)],
  ['useCreateRelease', () => desk.useCreateRelease(M)],
  ['useRevokeRelease', () => desk.useRevokeRelease(M)],
  ['useCreateTransit', () => desk.useCreateTransit(M)],
  ['useConfirmTransit', () => desk.useConfirmTransit(M)],
  ['useCancelTransit', () => desk.useCancelTransit(M)],
  ['useChangeTransitStay', () => desk.useChangeTransitStay(M)],
  ['useCreateContract', () => desk.useCreateContract(M)],
  ['useActivateContract', () => desk.useActivateContract(M)],
  ['useCreateQuote', () => desk.useCreateQuote(M)],
  ['useAcceptQuote', () => desk.useAcceptQuote(M)],
  ['useMoveAllocation', () => opsmap.useMoveAllocation(M)],
  ['useSwapAllocations', () => opsmap.useSwapAllocations(M)],
  ['useCreateBlock', () => opsmap.useCreateBlock(M)],
  ['useDeleteBlock', () => opsmap.useDeleteBlock(M)],
  ['useRecordVesselMeasurement', () => registry.useRecordVesselMeasurement(M)],
  ['useCounterSale', () => billing.useCounterSale(M)],
  ['useRefundPayment', () => billing.useRefundPayment(M)],
  ['useOpenCashSession', () => billing.useOpenCashSession(M)],
  ['useAddCashMovement', () => billing.useAddCashMovement(M)],
  ['useCloseCashSession', () => billing.useCloseCashSession(M)],
  ['useFiscalDocumentAction', () => billing.useFiscalDocumentAction(M)],
];

describe('mutation hooks offline', () => {
  beforeEach(() => resetOffline());
  afterEach(async () => {
    await current?.idle();
    setRunnerForTests(null);
  });

  it.each(FORBIDDEN)(
    '%s throws OfflineForbiddenError offline and sends nothing',
    async (_name, use) => {
      const { wrapper, net } = setup();
      setOnline(false);
      const { result } = await hookOf(use, wrapper);
      let error: unknown;
      await act(async () => {
        error = await result.current.mutateAsync({} as never).catch((e: unknown) => e);
      });
      expect(error).toBeInstanceOf(OfflineForbiddenError);
      expect(net.calls).toHaveLength(0);
    },
  );

  it('useRecordPresence goes through the queue and resolves with the answer online', async () => {
    const { wrapper, net } = setup(() => json(201, { presence: { id: 'p1' } }));
    const { result } = await hookOf(() => desk.useRecordPresence(M), wrapper);
    let res: unknown;
    await act(async () => {
      res = await result.current.mutateAsync({
        vessel_id: 'v1',
        event: 'arrived',
        allocation_id: 'a1',
        expected_resource_id: 'b1',
        vesselName: 'Libeccio',
      });
    });
    expect(res).toEqual({ presence: { id: 'p1' } });
    expect(net.calls).toHaveLength(1);
    expect(net.calls[0]!.body).toEqual({
      vessel_id: 'v1',
      event: 'arrived',
      allocation_id: 'a1',
      expected_resource_id: 'b1',
    });
    expect(net.calls[0]!.headers.get('Idempotency-Key')).toBeTruthy();
    expect(getOps()[0]!.label.params.vessel).toBe('Libeccio');
  });

  it('useRecordPresence offline rejects with OfflineQueuedError and keeps the op', async () => {
    const { wrapper, net } = setup();
    setOnline(false);
    const { result } = await hookOf(() => desk.useRecordPresence(M), wrapper);
    let error: unknown;
    await act(async () => {
      error = await result.current
        .mutateAsync({
          vessel_id: 'v1',
          event: 'departed',
          allocation_id: 'a1',
          expected_resource_id: 'b1',
        })
        .catch((e: unknown) => e);
    });
    expect(isQueuedOffline(error)).toBe(true);
    expect((error as OfflineQueuedError).op.queuedOffline).toBe(true);
    expect(getOps()).toHaveLength(1);
    expect(net.calls).toHaveLength(0);
  });

  it('a presence without allocation is online only (an arrival without booking creates a transit)', async () => {
    const { wrapper } = setup();
    setOnline(false);
    const { result } = await hookOf(() => desk.useRecordPresence(M), wrapper);
    let error: unknown;
    await act(async () => {
      error = await result.current
        .mutateAsync({ vessel_id: 'v1', event: 'arrived' })
        .catch((e: unknown) => e);
    });
    expect(error).toBeInstanceOf(OfflineForbiddenError);
    expect((error as OfflineForbiddenError).reason).toBe('allocation');
    expect(getOps()).toHaveLength(0);
  });

  it('useRecordPayment queues matched cash and surfaces a refusal as a problem (op discarded)', async () => {
    const { wrapper } = setup(() => problem(409, 'match_exceeds'));
    const { result } = await hookOf(() => billing.useRecordPayment(M), wrapper);
    let error: unknown;
    await act(async () => {
      error = await result.current
        .mutateAsync({
          kind: 'payment',
          method: 'cash',
          amount_cents: 1000,
          matches: [{ charge_id: 'c1', amount_cents: 1000 }],
          receiptMode: false,
        })
        .catch((e: unknown) => e);
    });
    expect((error as { type?: string }).type).toBe('match_exceeds');
    await waitFor(() => expect(getOps()).toHaveLength(0));
  });

  const cash = (patch: Partial<billing.RecordPaymentVars> = {}): billing.RecordPaymentVars => ({
    kind: 'payment',
    method: 'cash',
    amount_cents: 1000,
    matches: [{ charge_id: 'c1', amount_cents: 1000 }],
    ...patch,
  });

  it.each([
    ['receipt mode', cash({ receiptMode: true })],
    ['unknown receipt mode', cash()],
    ['an unmatched remainder', cash({ receiptMode: false, amount_cents: 1500 })],
  ])(
    'useRecordPayment refuses cash with %s offline (document) and queues nothing',
    async (_n, v) => {
      const { wrapper, net } = setup();
      setOnline(false);
      const { result } = await hookOf(() => billing.useRecordPayment(M), wrapper);
      let error: unknown;
      await act(async () => {
        error = await result.current.mutateAsync(v).catch((e: unknown) => e);
      });
      expect(error).toBeInstanceOf(OfflineForbiddenError);
      expect((error as OfflineForbiddenError).reason).toBe('document');
      expect(getOps()).toHaveLength(0);
      expect(net.calls).toHaveLength(0);
    },
  );

  it('useRecordPayment sends receipt-mode cash directly online, never through the queue', async () => {
    // A lost first answer (502) must not turn the payment into a queued offline replay (refused by the server).
    const { wrapper, net } = setup(() => json(502, {}));
    const { result } = await hookOf(() => billing.useRecordPayment(M), wrapper);
    let error: unknown;
    await act(async () => {
      error = await result.current
        .mutateAsync(cash({ receiptMode: true }))
        .catch((e: unknown) => e);
    });
    expect(error).toBeDefined();
    expect(isQueuedOffline(error)).toBe(false);
    expect(net.calls).toHaveLength(1);
    expect(getOps()).toHaveLength(0);
  });

  it('useRecordPayment sends a card payment directly online and refuses it offline', async () => {
    const { wrapper, net } = setup(() => json(201, { id: 'pay-1' }));
    const { result } = await hookOf(() => billing.useRecordPayment(M), wrapper);
    await act(async () => {
      await result.current.mutateAsync({ kind: 'payment', method: 'card_pos', amount_cents: 1000 });
    });
    expect(net.calls).toHaveLength(1);
    expect(getOps()).toHaveLength(0);
    setOnline(false);
    let error: unknown;
    await act(async () => {
      error = await result.current
        .mutateAsync({ kind: 'payment', method: 'card_pos', amount_cents: 1000 })
        .catch((e: unknown) => e);
    });
    expect(error).toBeInstanceOf(OfflineForbiddenError);
  });
});

describe('offline cache and the session', () => {
  beforeEach(() => resetOffline());
  afterEach(async () => {
    await current?.idle();
    setRunnerForTests(null);
  });

  it('a getMe answer of 401 (me = null) purges the cached bundle even with an empty queue', async () => {
    const { wrapper, qc } = setup();
    const now = Date.now();
    await saveBundle(qc, { userId: USER, marinaId: M }, 'v1', now);
    expect(await dbGetAll('cache')).toHaveLength(1);
    expect(await dbGet('meta', 'last')).toBeDefined();
    await hookOf(() => desk.useRecordPresence(M), wrapper);
    act(() => {
      qc.setQueryData(queryKeys.me, null);
    });
    await waitFor(async () => {
      expect(await dbGetAll('cache')).toHaveLength(0);
      expect(await dbGet('meta', 'last')).toBeUndefined();
    });
    expect(getOps()).toHaveLength(0);
  });

  it('signing out keeps the queued ops (frozen until the same user signs in again)', async () => {
    const { wrapper, qc } = setup();
    setOnline(false);
    const { result } = await hookOf(() => desk.useRecordPresence(M), wrapper);
    await act(async () => {
      await result.current
        .mutateAsync({
          vessel_id: 'v1',
          event: 'arrived',
          allocation_id: 'a1',
          expected_resource_id: 'b1',
        })
        .catch(() => undefined);
    });
    act(() => {
      qc.setQueryData(queryKeys.me, null);
    });
    await waitFor(async () => expect(await dbGet('meta', 'last')).toBeUndefined());
    expect(getOps()).toHaveLength(1);
  });
});
