import { QueryClient } from '@tanstack/react-query';
import { describe, expect, it, vi } from 'vitest';

import { keysForMessage, queryKeys } from './queryKeys';
import { SSE_DEBOUNCE_MS, SSE_TOPICS, subscribeMarinaStream } from './sse';

class FakeEventSource extends EventTarget {
  url: string;
  closed = false;
  constructor(url: string) {
    super();
    this.url = url;
  }
  close() {
    this.closed = true;
  }
}

describe('subscribeMarinaStream', () => {
  it('invalidates the query keys of each topic and closes on unsubscribe', () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, 'invalidateQueries');
    let source: FakeEventSource | undefined;
    const stop = subscribeMarinaStream(queryClient, 'm1', (url) => {
      source = new FakeEventSource(url);
      return source as unknown as EventSource;
    });
    expect(source?.url).toBe('/api/admin/v1/stream?marina_id=m1');

    source?.dispatchEvent(new Event('team.changed'));
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.team('m1') });

    source?.dispatchEvent(new Event('marina.changed'));
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.marina('m1') });

    source?.dispatchEvent(new Event('pricing.changed'));
    expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.pricing('m1') });

    stop();
    expect(source?.closed).toBe(true);
  });

  it('narrows occupancy invalidation by map and debounces it per key', () => {
    vi.useFakeTimers();
    try {
      const queryClient = new QueryClient();
      const spy = vi.spyOn(queryClient, 'invalidateQueries');
      let source: FakeEventSource | undefined;
      const stop = subscribeMarinaStream(queryClient, 'm1', (url) => {
        source = new FakeEventSource(url);
        return source as unknown as EventSource;
      });
      const data = JSON.stringify({
        topic: 'occupancy.changed',
        type: 'occupancy.allocated',
        marina_id: 'm1',
        entity_ids: ['a1'],
        map_id: 'map1',
        resource_ids: ['b1'],
        nights: { from: '2027-07-01', to: '2027-07-03' },
      });
      source?.dispatchEvent(new MessageEvent('occupancy.changed', { data }));
      source?.dispatchEvent(new MessageEvent('occupancy.changed', { data }));
      expect(spy).not.toHaveBeenCalled();
      vi.advanceTimersByTime(SSE_DEBOUNCE_MS);
      expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.dayView('m1', 'map1') });
      expect(spy).toHaveBeenCalledWith({ queryKey: queryKeys.mapOccupancy('m1', 'map1') });
      const dayCalls = spy.mock.calls.filter(
        ([arg]) =>
          JSON.stringify(arg?.queryKey) === JSON.stringify(queryKeys.dayView('m1', 'map1')),
      );
      expect(dayCalls).toHaveLength(1);
      stop();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('keysForMessage', () => {
  const base = { type: 'x', marina_id: 'm1', entity_ids: [] };

  it('invalidates every map when the message has no map id', () => {
    const keys = keysForMessage({ ...base, topic: 'occupancy.changed' }, 'm1');
    expect(keys).toContainEqual(queryKeys.dayView('m1'));
    expect(keys).toContainEqual(queryKeys.releases('m1'));
  });

  it('maps presence and contract topics to their desk keys', () => {
    expect(
      keysForMessage({ ...base, topic: 'presence.changed', map_id: 'p' }, 'm1'),
    ).toContainEqual(queryKeys.presences('m1'));
    const contract = keysForMessage({ ...base, topic: 'contract.changed' }, 'm1');
    expect(contract).toContainEqual(queryKeys.transits('m1'));
    expect(contract).toContainEqual(queryKeys.dayView('m1'));
  });

  it('keeps prefix keys as prefixes of the full keys', () => {
    const full = queryKeys.dayView('m1', 'map1', '2027-07-01');
    expect(full.slice(0, queryKeys.dayView('m1').length)).toEqual(queryKeys.dayView('m1'));
  });

  it('falls back to the topic keys for older topics', () => {
    expect(keysForMessage({ ...base, topic: 'pricing.changed' }, 'm1')).toEqual([
      queryKeys.pricing('m1'),
    ]);
  });

  it('invalidates the M4 areas', () => {
    const base = { type: 'x', marina_id: 'm1', entity_ids: [] };
    expect(keysForMessage({ ...base, topic: 'rights.changed' }, 'm1')).toEqual([
      queryKeys.rights('m1'),
    ]);
    expect(keysForMessage({ ...base, topic: 'documents.changed' }, 'm1')).toEqual([
      queryKeys.docsArea('m1'),
    ]);
    const contract = keysForMessage({ ...base, topic: 'contract.changed' }, 'm1');
    expect(contract).toContainEqual(queryKeys.installments('m1'));
    expect(contract).toContainEqual(queryKeys.renewals('m1'));
    expect(SSE_TOPICS).toContain('rights.changed');
    expect(SSE_TOPICS).toContain('documents.changed');
  });

  it('invalidates the M5 billing area and the instalment lists', () => {
    const base = { type: 'x', marina_id: 'm1', entity_ids: [] };
    const keys = keysForMessage({ ...base, topic: 'billing.changed' }, 'm1');
    expect(keys).toContainEqual(queryKeys.billing('m1'));
    expect(keys).toContainEqual(queryKeys.installments('m1'));
    expect(keys).toContainEqual(queryKeys.holderInstallments('m1'));
    expect(queryKeys.charge('m1', 'c1').slice(0, 3)).toEqual(queryKeys.billing('m1'));
    expect(SSE_TOPICS).toContain('billing.changed');
  });
});
