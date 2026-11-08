import { overlayOf, residualAfterQueued, stateAfterQueued } from './overlay';
import { M, op } from './testkit';

describe('overlay of pending ops', () => {
  const presence = (id: string, vessel: string, event: string, state = 'pending') =>
    op(id, {
      lane: `vessel:${vessel}`,
      state: state as never,
      request: { operationId: 'recordPresence', path: '/p', body: { vessel_id: vessel, event } },
    });
  const cash = (id: string, charge: string, amount: number, state = 'pending') =>
    op(id, {
      kind: 'payment.cash',
      state: state as never,
      request: {
        operationId: 'recordPayment',
        path: '/pay',
        body: { matches: [{ charge_id: charge, amount_cents: amount }] },
      },
    });

  it('lists queued events per vessel in order and the state after them', () => {
    const o = overlayOf(
      [
        presence('2', 'v1', 'departed'),
        presence('1', 'v1', 'arrived'),
        presence('3', 'v2', 'arrived', 'done'),
      ],
      M,
    );
    expect(o.presences.get('v1')).toEqual(['arrived', 'departed']);
    expect(o.presences.has('v2')).toBe(false);
    expect(stateAfterQueued(o.presences.get('v1'))).toBe('out');
    expect(stateAfterQueued(['arrived'])).toBe('in_port');
    expect(stateAfterQueued(undefined)).toBeUndefined();
  });

  it('nets queued cash from the residual, never below zero, other marinas ignored', () => {
    const o = overlayOf(
      [
        cash('1', 'c1', 500),
        cash('2', 'c1', 700),
        cash('3', 'c2', 100, 'expired'),
        { ...cash('4', 'c3', 9), marinaId: 'x' },
      ],
      M,
    );
    expect(residualAfterQueued('c1', 1000, o)).toBe(0);
    expect(residualAfterQueued('c1', 2000, o)).toBe(800);
    expect(residualAfterQueued('c2', 300, o)).toBe(300);
    expect(residualAfterQueued('c3', 300, o)).toBe(300);
  });
});
