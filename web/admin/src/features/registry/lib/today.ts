import { useQuery } from '@tanstack/react-query';

import { queryKeys, unwrap, useApi } from '@porto/shared/api';

import { useCurrentMarina } from '../../../auth/hooks';

const DEFAULT_TIMEZONE = 'Europe/Rome';

/** Calendar date `YYYY-MM-DD` of an instant in an IANA timezone (ADR 0002: dates in the marina timezone). */
export function todayIn(timeZone: string, now: Date = new Date()): string {
  try {
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).formatToParts(now);
    const get = (type: string) => parts.find((p) => p.type === type)?.value ?? '';
    return `${get('year')}-${get('month')}-${get('day')}`;
  } catch {
    return todayIn(DEFAULT_TIMEZONE, now);
  }
}

/** Today in the current marina's timezone (display only: urgency itself comes from the server). */
export function useMarinaToday(): string {
  const api = useApi();
  const { marinaId } = useCurrentMarina();
  const marina = useQuery({
    queryKey: queryKeys.marina(marinaId),
    queryFn: () =>
      unwrap(api.GET('/api/admin/v1/marinas/{marinaId}', { params: { path: { marinaId } } })),
    staleTime: 5 * 60_000,
  });
  return todayIn(marina.data?.timezone ?? DEFAULT_TIMEZONE);
}
