import { QueryClient } from '@tanstack/react-query';
import { describe, expect, it, vi } from 'vitest';

import { queryKeys } from './queryKeys';
import { subscribeMarinaStream } from './sse';

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

    stop();
    expect(source?.closed).toBe(true);
  });
});
