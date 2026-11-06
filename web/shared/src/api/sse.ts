import type { QueryClient } from '@tanstack/react-query';

import { keysForTopic } from './queryKeys';

export const SSE_TOPICS = ['marina.changed', 'team.changed'] as const;

export type EventSourceFactory = (url: string) => EventSource;

/**
 * Subscribes to the admin stream of one marina and invalidates the query keys of each
 * topic. Payloads carry ids only (no PII): the UI refetches instead of reading them.
 * EventSource reconnects by itself and resends Last-Event-ID. Returns an unsubscribe.
 */
export function subscribeMarinaStream(
  queryClient: QueryClient,
  marinaId: string,
  factory?: EventSourceFactory,
): () => void {
  const create: EventSourceFactory =
    factory ??
    ((url) => {
      if (typeof EventSource === 'undefined') throw new Error('EventSource unavailable');
      return new EventSource(url, { withCredentials: true });
    });
  let source: EventSource;
  try {
    source = create(`/api/admin/v1/stream?marina_id=${encodeURIComponent(marinaId)}`);
  } catch {
    return () => undefined;
  }
  const listeners = SSE_TOPICS.map((topic) => {
    const listener = () => {
      for (const queryKey of keysForTopic(topic, marinaId)) {
        void queryClient.invalidateQueries({ queryKey });
      }
    };
    source.addEventListener(topic, listener);
    return [topic, listener] as const;
  });
  return () => {
    for (const [topic, listener] of listeners) source.removeEventListener(topic, listener);
    source.close();
  };
}
