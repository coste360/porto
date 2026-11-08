// Reads of the phone screens that have no shared hook yet (m6b-plan §5.4, §6.4). Query keys come from
// queryKeys.ts so the offline cache allowlist matches them.
import { useQuery } from '@tanstack/react-query';

import { queryKeys, unwrap, useApi } from '@porto/shared/api';

const M = '/api/admin/v1/marinas/{marinaId}' as const;

/** Open charges to collect at the dock on a date (payments.collect or payments.collect_cash). */
export function useCollectibles(marinaId: string, date: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.collectibles(marinaId, date),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/collectibles`, {
          params: { path: { marinaId }, query: { date, limit: 200 } },
        }),
      ),
  });
}

/** Same-origin URL of the SVG transit QR (cookie session; shown with <img>, never cached offline). */
export function transitQrUrl(marinaId: string, contractId: string): string {
  return `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/transits/${encodeURIComponent(contractId)}/qr`;
}

/** A contract or transit (contracts.read), only when enabled (online lookups of a scanned transit). */
export function useContractDetail(marinaId: string, contractId: string | null, enabled: boolean) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.contract(marinaId, contractId ?? ''),
    enabled: enabled && !!contractId,
    retry: false,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/contracts/{contractId}`, {
          params: { path: { marinaId, contractId: contractId! } },
        }),
      ),
  });
}
