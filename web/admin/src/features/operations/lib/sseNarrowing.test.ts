import { describe, expect, it } from 'vitest';

import { keysForMessage, queryKeys } from '@porto/shared/api';

// The operational view relies on the SSE narrowing of m3-plan §7.5: an occupancy change of one map invalidates
// that map's day views only (all maps when the message carries no map).
const M = 'm1';

function invalidates(key: readonly unknown[], prefix: readonly unknown[]): boolean {
  return prefix.every((p, i) => JSON.stringify(key[i]) === JSON.stringify(p));
}

describe('SSE keysForMessage narrowing for the ops map', () => {
  const dayA = queryKeys.dayView(M, 'map-a', '2026-11-07');
  const dayB = queryKeys.dayView(M, 'map-b', '2026-11-07');

  it('narrows occupancy.changed to the message map', () => {
    const keys = keysForMessage(
      {
        topic: 'occupancy.changed',
        type: 'occupancy.moved',
        marina_id: M,
        entity_ids: [],
        map_id: 'map-a',
      },
      M,
    );
    expect(keys.some((k) => invalidates(dayA, k))).toBe(true);
    expect(keys.some((k) => invalidates(dayB, k))).toBe(false);
  });

  it('invalidates every map without a map id, and on contract changes', () => {
    for (const topic of ['occupancy.changed', 'contract.changed']) {
      const keys = keysForMessage({ topic, type: 'x', marina_id: M, entity_ids: [] }, M);
      expect(keys.some((k) => invalidates(dayA, k))).toBe(true);
      expect(keys.some((k) => invalidates(dayB, k))).toBe(true);
    }
  });

  it('presence.changed refreshes the day view of its map', () => {
    const keys = keysForMessage(
      {
        topic: 'presence.changed',
        type: 'presence.arrived',
        marina_id: M,
        entity_ids: [],
        map_id: 'map-b',
      },
      M,
    );
    expect(keys.some((k) => invalidates(dayB, k))).toBe(true);
    expect(keys.some((k) => invalidates(dayA, k))).toBe(false);
  });
});
