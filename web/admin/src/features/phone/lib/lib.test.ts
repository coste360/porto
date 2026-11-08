import { describe, expect, it } from 'vitest';

import type { QueuedOp } from '@porto/shared/offline';

import { groupByWeek, weekStart } from './direction';
import { applyContext, movementStyle } from './mapContext';
import {
  cashAmountError,
  cashOp,
  collectReason,
  nextAction,
  presenceOp,
  presencePhotoOp,
  queuedCharges,
  queuedPresences,
} from './ops';
import { scaledSize } from './photo';
import { resolveFromDay } from './scan';

const S = { userId: 'u1', marinaId: 'm-1' };

const op = (o: Partial<QueuedOp>): QueuedOp => ({
  id: 'op',
  kind: 'presence.record',
  key: 'k',
  userId: 'u1',
  marinaId: 'm-1',
  createdAt: '2026-07-10T08:00:00Z',
  lane: 'vessel:v1',
  request: {
    operationId: 'recordPresence',
    path: '/x',
    body: { vessel_id: 'v1', event: 'arrived' },
  },
  state: 'pending',
  attempts: 0,
  label: { key: 'l', params: {} },
  ...o,
});

describe('queued operation builders', () => {
  it('builds an arrival on an existing allocation with the berth the worker saw', () => {
    const o = presenceOp(S, {
      vesselId: 'v1',
      vesselName: 'Libeccio',
      event: 'arrived',
      allocationId: 'a1',
      resourceId: 'b1',
      notes: '  di poppa  ',
      at: '2026-07-10T08:00:00Z',
    });
    expect(o).toEqual({
      kind: 'presence.record',
      userId: 'u1',
      marinaId: 'm-1',
      lane: 'vessel:v1',
      request: {
        operationId: 'recordPresence',
        path: '/api/admin/v1/marinas/m-1/presences',
        body: {
          vessel_id: 'v1',
          event: 'arrived',
          at: '2026-07-10T08:00:00Z',
          allocation_id: 'a1',
          expected_resource_id: 'b1',
          notes: 'di poppa',
        },
      },
      label: { key: 'offline:queue.label.arrived', params: { vessel: 'Libeccio' } },
    });
    const empty = presenceOp(S, {
      vesselId: 'v1',
      vesselName: 'L',
      event: 'departed',
      allocationId: 'a1',
      resourceId: 'b1',
      notes: '  ',
      at: 'x',
    });
    expect(empty.request.body).not.toHaveProperty('notes');
    expect(empty.label.key).toBe('offline:queue.label.departed');
  });

  it('binds the photo to the presence queued before it', () => {
    const blob = new Blob(['x'], { type: 'image/jpeg' });
    const o = presencePhotoOp(S, { parentOpId: 'p1', vesselId: 'v1', vesselName: 'L', blob });
    expect(o.dependsOn).toEqual({ opId: 'p1', bind: { presenceId: 'presence.id' } });
    expect(o.request).toEqual({
      operationId: 'uploadPresencePhoto',
      path: '/api/admin/v1/marinas/m-1/presences/{presenceId}/photo',
      contentType: 'image/jpeg',
    });
    expect(o.lane).toBe('vessel:v1');
    expect(o.blob).toBe(blob);
  });

  it('builds a cash collection matched to one charge', () => {
    const o = cashOp(S, {
      charge: { charge_id: 'c1', customer_id: 'cu1', residual_cents: 5000, fiscal_mode: 'invoice' },
      amountCents: 3000,
      registerId: 'r1',
      receivedOn: '2026-07-10',
      amountLabel: '30,00 €',
    });
    expect(o.kind).toBe('payment.cash');
    expect(o.lane).toBe('charge:c1');
    expect(o.request.body).toEqual({
      kind: 'payment',
      method: 'cash',
      amount_cents: 3000,
      received_on: '2026-07-10',
      customer_id: 'cu1',
      cash_register_id: 'r1',
      matches: [{ charge_id: 'c1', amount_cents: 3000 }],
    });
    expect(o.request.body).not.toHaveProperty('auto_match');
  });

  it('checks the collected amount', () => {
    expect(cashAmountError(5000, 5000)).toBeNull();
    expect(cashAmountError(1, 5000)).toBeNull();
    expect(cashAmountError(5001, 5000)).not.toBeNull();
    expect(cashAmountError(0, 5000)).not.toBeNull();
    expect(cashAmountError(null, 5000)).not.toBeNull();
  });

  it('refuses receipt-mode collections offline and every collection without a session', () => {
    expect(collectReason({ fiscal_mode: 'invoice' }, false, true)).toBeNull();
    expect(collectReason({ fiscal_mode: 'receipt' }, true, true)).toBeNull();
    expect(collectReason({ fiscal_mode: 'receipt' }, false, true)).toBe('document');
    expect(collectReason({ fiscal_mode: 'invoice' }, true, false)).toBe('session');
  });
});

describe('overlay of queued operations', () => {
  it('lists open presences per vessel, oldest first, ignoring done and expired ones', () => {
    const ops = [
      op({
        id: 'b',
        createdAt: '2026-07-10T09:00:00Z',
        request: {
          operationId: 'recordPresence',
          path: '/x',
          body: { vessel_id: 'v1', event: 'departed' },
        },
      }),
      op({ id: 'a', createdAt: '2026-07-10T08:00:00Z' }),
      op({
        id: 'c',
        state: 'done',
        request: {
          operationId: 'recordPresence',
          path: '/x',
          body: { vessel_id: 'v2', event: 'arrived' },
        },
      }),
      op({
        id: 'd',
        state: 'expired',
        request: {
          operationId: 'recordPresence',
          path: '/x',
          body: { vessel_id: 'v3', event: 'arrived' },
        },
      }),
      op({
        id: 'e',
        kind: 'presence.photo',
        request: { operationId: 'uploadPresencePhoto', path: '/y' },
      }),
    ];
    const q = queuedPresences(ops);
    expect(q.get('v1')).toEqual(['arrived', 'departed']);
    expect(q.has('v2')).toBe(false);
    expect(q.has('v3')).toBe(false);
  });

  it('offers the departure after a queued arrival and nothing after a queued departure', () => {
    expect(nextAction('arrived', undefined)).toBe('arrived');
    expect(nextAction('arrived', ['arrived'])).toBe('departed');
    expect(nextAction('departed', ['departed'])).toBeNull();
    expect(nextAction(null, undefined)).toBeNull();
  });

  it('lists charges with a queued cash collection', () => {
    const ops = [
      op({
        kind: 'payment.cash',
        request: {
          operationId: 'recordPayment',
          path: '/p',
          body: { matches: [{ charge_id: 'c1', amount_cents: 1 }] },
        },
      }),
      op({
        kind: 'payment.cash',
        state: 'done',
        request: {
          operationId: 'recordPayment',
          path: '/p',
          body: { matches: [{ charge_id: 'c2', amount_cents: 1 }] },
        },
      }),
    ];
    expect([...queuedCharges(ops)]).toEqual(['c1']);
  });
});

describe('scan resolution from the day', () => {
  const vessel = {
    id: 'v1',
    name: 'Libeccio',
    loa_cm: 1000,
    beam_cm: 300,
    hull_type: 'monohull_sail' as const,
  };
  const contract = {
    id: 'abc',
    kind: 'transit' as const,
    status: 'confirmed' as const,
    code: 'TRN-2026-0001',
  };
  const empty = {
    date: '2026-07-10',
    arrivals_expected: [],
    departures_expected: [],
    vessels_out: [],
    arrived_without_booking: [],
    arrived: [],
    departed: [],
  };

  it('finds an expected arrival in the movements, case-insensitively', () => {
    const mv = {
      ...empty,
      arrivals_expected: [
        {
          vessel,
          contract,
          allocation_id: 'a1',
          resource: { type: 'berth' as const, id: 'b1', code: 'A01' },
        },
      ],
    };
    const hit = resolveFromDay('ABC', mv, []);
    expect(hit?.action).toBe('arrived');
    expect(hit?.section).toBe('arrivals_expected');
    expect(hit?.code).toBe('TRN-2026-0001');
  });

  it('falls back to the day view element (departure when in port)', () => {
    const el = {
      resource_type: 'berth' as const,
      resource_id: 'b1',
      code: 'A01',
      status: 'transit' as const,
      arrival_today: false,
      departure_today: false,
      anomalies: [],
      allocation_id: 'a1',
      contract,
      vessel,
      presence: { state: 'in_port' as const, since: 'x', away_days: 0 },
    };
    expect(resolveFromDay('abc', empty, [el])?.action).toBe('departed');
    expect(resolveFromDay('zzz', empty, [el])).toBeNull();
    expect(resolveFromDay('abc', undefined, undefined)).toBeNull();
  });
});

describe('direction deadlines by week', () => {
  it('groups by Monday week, overdue items in the current week', () => {
    expect(weekStart('2026-07-10')).toBe('2026-07-06'); // Friday
    expect(weekStart('2026-07-06')).toBe('2026-07-06');
    expect(weekStart('2026-07-12')).toBe('2026-07-06'); // Sunday
    const g = groupByWeek(
      [{ due_on: '2026-07-20' }, { due_on: '2026-07-01' }, { due_on: '2026-07-11' }],
      '2026-07-10',
    );
    expect(g.map((w) => [w.weekStart, w.items.map((i) => i.due_on)])).toEqual([
      ['2026-07-06', ['2026-07-01', '2026-07-11']],
      ['2026-07-20', ['2026-07-20']],
    ]);
  });
});

describe('map contexts', () => {
  it('colours arrivals and departures of the night only', () => {
    const base = {
      status: 'transit' as const,
      arrival_today: false,
      departure_today: false,
      anomalies: [],
    };
    expect(movementStyle(base).legendKey).toBe('free');
    const v = { id: 'v', name: 'n', loa_cm: 1, beam_cm: 1, hull_type: 'rib' as const };
    expect(movementStyle({ ...base, vessel: v, arrival_today: true }).legendKey).toBe('arrival');
    expect(movementStyle({ ...base, vessel: v, departure_today: true }).legendKey).toBe(
      'departure',
    );
    expect(movementStyle({ ...base, vessel: v }).legendKey).toBe('none');
  });

  it('counts the legend of the presence context', () => {
    const v = { id: 'v', name: 'n', loa_cm: 1, beam_cm: 1, hull_type: 'rib' as const };
    const dv = {
      map_id: 'm',
      date: '2026-07-10',
      map_version: 1,
      generated_at: 'x',
      unplaced: [],
      area_placements: [],
      counts: { anomalies: 0 },
      elements: [
        {
          resource_type: 'berth' as const,
          resource_id: 'b1',
          code: 'A01',
          status: 'free' as const,
          arrival_today: false,
          departure_today: false,
          anomalies: [],
        },
        {
          resource_type: 'berth' as const,
          resource_id: 'b2',
          code: 'A02',
          status: 'transit' as const,
          arrival_today: true,
          departure_today: false,
          anomalies: [],
          vessel: v,
          allocation_id: 'a',
        },
      ],
    };
    const subjects = dv.elements.map((e) => ({
      ref: e.resource_id,
      resourceId: e.resource_id,
      resourceType: e.resource_type,
      code: e.code,
      subject: e,
    }));
    const r = applyContext('presence', dv, subjects, undefined);
    expect(Object.fromEntries(r.legend.map((l) => [l.key, l.count]))).toEqual({
      arrival: 1,
      free: 1,
    });
    const p = applyContext('payments', dv, subjects, {
      date: '2026-07-10',
      resources: [
        { resource_type: 'berth', resource_id: 'b2', state: 'overdue', suspended: false },
      ],
    });
    expect(p.legend.find((l) => l.key === 'overdue')).toMatchObject({ count: 1, token: 'bad' });
  });
});

describe('photo scaling', () => {
  it('caps the longest side at 2048 px keeping the ratio', () => {
    expect(scaledSize(4032, 3024)).toEqual({ w: 2048, h: 1536 });
    expect(scaledSize(1000, 500)).toEqual({ w: 1000, h: 500 });
    expect(scaledSize(3000, 6000)).toEqual({ w: 1024, h: 2048 });
  });
});
