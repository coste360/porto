import type { QueryClient } from '@tanstack/react-query';

import {
  keysForMessage,
  keysForTopic,
  NARROWED_TOPICS,
  type QueryKey,
  type StreamMessage,
} from './queryKeys';
import { sendQueueNow } from '../offline/runner';

export const SSE_TOPICS = [
  'marina.changed',
  'team.changed',
  'map.changed',
  'registry.changed',
  'pricing.changed',
  'occupancy.changed',
  'presence.changed',
  'contract.changed',
  // M4 (m4-plan §7.5, §15 D21)
  'rights.changed',
  'documents.changed',
  // M5 (m5-plan §7.5)
  'billing.changed',
  // M9 (m9-plan D28)
  'storage.changed',
  'maintenance.changed',
  'utilities.changed',
  // M10 (m10-plan §13)
  'yard.changed',
  // M11 (m11-plan §13.2)
  'rental.changed',
  // M12 (m12-plan §9.1): sensor.state_changed, sensor.anomaly and the sensor configuration.
  'sensor.changed',
] as const;

/** Debounce of the narrowed (M3) invalidations, per query key. */
export const SSE_DEBOUNCE_MS = 250;

function parseMessage(topic: string, event: Event): StreamMessage {
  const data = (event as MessageEvent).data;
  if (typeof data === 'string') {
    try {
      const msg = JSON.parse(data) as Partial<StreamMessage>;
      if (msg && typeof msg === 'object') {
        return { type: '', marina_id: '', entity_ids: [], ...msg, topic };
      }
    } catch {
      // fall through: invalidate the whole topic
    }
  }
  return { topic, type: '', marina_id: '', entity_ids: [] };
}

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
  const pending = new Map<string, { key: QueryKey; timer: ReturnType<typeof setTimeout> }>();
  const debounced = (queryKey: QueryKey) => {
    const id = JSON.stringify(queryKey);
    const prev = pending.get(id);
    if (prev) clearTimeout(prev.timer);
    const timer = setTimeout(() => {
      pending.delete(id);
      void queryClient.invalidateQueries({ queryKey });
    }, SSE_DEBOUNCE_MS);
    pending.set(id, { key: queryKey, timer });
  };
  const narrowed: readonly string[] = NARROWED_TOPICS;
  const listeners = SSE_TOPICS.map((topic) => {
    const listener = (event: Event) => {
      if (narrowed.includes(topic)) {
        for (const queryKey of keysForMessage(parseMessage(topic, event), marinaId))
          debounced(queryKey);
        return;
      }
      for (const queryKey of keysForTopic(topic, marinaId)) {
        void queryClient.invalidateQueries({ queryKey });
      }
    };
    source.addEventListener(topic, listener);
    return [topic, listener] as const;
  });
  // A reconnected stream means the server is reachable again: send the offline queue now (m6b-plan §4.3).
  let opened = false;
  const onOpen = () => {
    if (opened) sendQueueNow();
    opened = true;
  };
  source.addEventListener('open', onOpen);
  return () => {
    source.removeEventListener('open', onOpen);
    for (const [topic, listener] of listeners) source.removeEventListener(topic, listener);
    for (const { timer } of pending.values()) clearTimeout(timer);
    pending.clear();
    source.close();
  };
}
