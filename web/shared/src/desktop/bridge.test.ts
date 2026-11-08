import { describe, expect, it } from 'vitest';

import { createApiClient } from '../api/client';
import { createFetchMock } from '../testing';
import { createBridgeClient } from './bridge';

function client(routes: Parameters<typeof createFetchMock>[0]) {
  const mock = createFetchMock(routes);
  const api = createApiClient({
    fetch: mock.fetch,
    baseUrl: 'http://127.0.0.1:4100',
    readCsrf: () => 'csrf',
  });
  return { bridge: createBridgeClient(api), calls: mock.calls };
}

describe('bridge client', () => {
  it('returns the scan text, or null on 204', async () => {
    const { bridge, calls } = client([
      { path: '/api/bridge/v1/scanner/next', body: { text: 'PT1:abc' } },
    ]);
    expect(await bridge.nextScan(5)).toBe('PT1:abc');
    expect(calls[0]?.url.searchParams.get('timeout')).toBe('5');
    const empty = client([{ path: '/api/bridge/v1/scanner/next', status: 204 }]);
    expect(await empty.bridge.nextScan()).toBeNull();
  });

  it('prints a receipt with the payment id and width', async () => {
    const { bridge, calls } = client([
      { method: 'POST', path: '/api/bridge/v1/receipts', status: 202 },
    ]);
    await bridge.printReceipt('pay-1', 58);
    expect(calls[0]?.body).toEqual({ payment_id: 'pay-1', width: 58 });
    expect(calls[0]?.headers.get('X-CSRF-Token')).toBe('csrf');
  });

  it('surfaces peripheral_unavailable as a problem', async () => {
    const { bridge } = client([
      {
        method: 'POST',
        path: '/api/bridge/v1/receipts',
        status: 503,
        body: { type: 'peripheral_unavailable', title: 'x', status: 503 },
      },
    ]);
    await expect(bridge.printReceipt('pay-1')).rejects.toMatchObject({
      type: 'peripheral_unavailable',
    });
  });
});
