import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { queryKeys } from '../api/queryKeys';
import { OFFLINE_OPERATIONS, OFFLINE_QUERY_ALLOWLIST, isPersistable, localDate } from './allowlist';

const M = 'm-1';

/** The operations flagged `x-porto-offline: true` in api/openapi.yaml (operation-level keys, 6 spaces). */
function flaggedInContract(): string[] {
  const spec = readFileSync(resolve(__dirname, '../../../../api/openapi.yaml'), 'utf8');
  const out: string[] = [];
  let current: string | null = null;
  for (const line of spec.split('\n')) {
    if (/^ {4}(get|put|post|delete|patch):/.test(line)) current = null;
    const op = /^ {6}operationId: (\w+)/.exec(line);
    if (op) current = op[1]!;
    if (/^ {6}x-porto-offline: true/.test(line) && current) out.push(current);
  }
  return out.sort();
}

describe('offline allowlists', () => {
  it('OFFLINE_OPERATIONS equals the x-porto-offline operations of the contract', () => {
    expect([...OFFLINE_OPERATIONS].sort()).toEqual(flaggedInContract());
    expect(flaggedInContract()).toEqual(['recordPayment', 'recordPresence', 'uploadPresencePhoto']);
  });

  it('matches the query keys of queryKeys.ts for today and tomorrow only', () => {
    const today = localDate();
    const tomorrow = localDate(1);
    const yes = [
      queryKeys.me,
      queryKeys.maps(M),
      queryKeys.map(M, 'x'),
      queryKeys.dayView(M, 'x', today),
      queryKeys.dayView(M, 'x', 'today'),
      queryKeys.dayView(M, 'x', tomorrow),
      queryKeys.movements(M, today),
      queryKeys.movements(M, tomorrow, 'x'),
      queryKeys.presences(M, today),
      queryKeys.mapPayments(M, 'x', today),
      queryKeys.collectibles(M, today),
      [...queryKeys.cashSession(M, 'current'), 'r1'],
      queryKeys.deadlines(M, { due_before: '2027-01-01' }),
    ];
    const no = [
      queryKeys.dayView(M, 'x', localDate(2)),
      queryKeys.movements(M, localDate(-3)),
      queryKeys.presences(M, tomorrow),
      queryKeys.berths(M),
      queryKeys.cashSession(M, 's1'),
      queryKeys.customers(M),
      queryKeys.charges(M),
      queryKeys.audit(M),
      queryKeys.webhooks(M),
      queryKeys.devices(M),
      queryKeys.apiKeys(M),
      queryKeys.desktop.status,
      queryKeys.bridge.status,
      queryKeys.transitQr(M, 'c1'),
      queryKeys.chargeExports(M),
    ];
    for (const k of yes) expect(isPersistable(k, M), JSON.stringify(k)).toBe(true);
    for (const k of no) expect(isPersistable(k, M), JSON.stringify(k)).toBe(false);
    expect(isPersistable(queryKeys.maps('other'), M)).toBe(false);
  });

  it('keeps the reserved slot for the assigned activities (M9–M11) empty', () => {
    const slot = OFFLINE_QUERY_ALLOWLIST.find((e) => e.name === 'activities.assigned');
    expect(slot?.operationId).toBeNull();
    expect(slot?.match(['marina', M, 'anything'])).toBe(false);
  });
});
