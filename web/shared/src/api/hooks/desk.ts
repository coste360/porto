// Query and mutation hooks of the desk (M3, m3-plan §8.1, §8.6): search, quotes, contracts, transits, inquiries,
// releases, closures, occupancy settings, presences and movements. Query keys come from queryKeys.ts (desk and
// ops roots). Every desk mutation invalidates both roots: a contract change recolours the map, a move changes the
// contract timeline. SSE (contract.changed, occupancy.changed, presence.changed) does the same for other tabs.
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary, type ApiClient } from '../client';
import { useApi } from '../context';
import { isProblem } from '../problem';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const PAGE_SIZE = 50;

export type OccupancySearchRequest = S['OccupancySearchRequest'];
export type OccupancySolution = S['OccupancySolution'];
export type PlacementInput = S['PlacementInput'];
export type SegmentInput = S['SegmentInput'];
export type ConflictDetail = S['ConflictDetail'];
export type FitResult = S['FitResult'];
export type Quote = S['Quote'];
export type QuoteInput = S['QuoteInput'];
export type QuoteSummary = S['QuoteSummary'];
export type Contract = S['Contract'];
export type ContractSummary = S['ContractSummary'];
export type ContractCreate = S['ContractCreate'];
export type TransitCreate = S['TransitCreate'];
export type Inquiry = S['Inquiry'];
export type InquiryCreate = S['InquiryCreate'];
export type Release = S['Release'];
export type Closure = S['Closure'];
export type OccupancySettings = S['OccupancySettings'];
export type Movement = S['Movement'];
export type Movements = S['Movements'];
export type MapAllocation = S['MapAllocation'];
export type ManualConfirmation = S['ManualConfirmation'];

/** Filters are stored in the query key as strings only (stable keys, no undefined values). */
function compact(filters: Record<string, string | boolean | undefined | null>) {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) {
    if (v === undefined || v === null || v === '' || v === false) continue;
    out[k] = String(v);
  }
  return out;
}

function invalidateDesk(qc: QueryClient, marinaId: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.desk(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.ops(marinaId) }),
  ]);
}

/** Mutation with desk + ops invalidation; one Idempotency-Key per mutation call (retries reuse it). */
function useDeskMutation<V, R>(
  marinaId: string,
  fn: (api: ApiClient, vars: V, idempotencyKey: string) => Promise<R>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => fn(api, vars, newIdempotencyKey()),
    onSuccess: () => invalidateDesk(qc, marinaId),
  });
}

// ---- problems of the occupancy engine (m3-plan §7.4) ----

/** Details of a `resource_taken` problem (conflicting nights, cells and alternatives), or null. */
export function conflictOf(error: unknown): ConflictDetail | null {
  if (!isProblem(error) || error.type !== 'resource_taken') return null;
  const d = error.details as Partial<ConflictDetail>;
  if (typeof d.resource_id !== 'string' || typeof d.from !== 'string') return null;
  return {
    resource_type: d.resource_type ?? 'berth',
    resource_id: d.resource_id,
    from: d.from,
    to: typeof d.to === 'string' ? d.to : d.from,
    allocation_ids: Array.isArray(d.allocation_ids) ? d.allocation_ids : [],
    ...(Array.isArray(d.cells) ? { cells: d.cells } : {}),
    ...(typeof d.vessel_id === 'string' ? { vessel_id: d.vessel_id } : {}),
    alternatives: Array.isArray(d.alternatives) ? d.alternatives : [],
  };
}

/** The fit result of a `fit_blocked` problem, or null. */
export function fitBlockedOf(error: unknown): FitResult | null {
  if (!isProblem(error) || error.type !== 'fit_blocked') return null;
  const d = error.details as Partial<FitResult>;
  return {
    ok: false,
    overridable: d.overridable === true,
    fill_bp: typeof d.fill_bp === 'number' ? d.fill_bp : 0,
    blocking: Array.isArray(d.blocking) ? d.blocking : [],
    warnings: Array.isArray(d.warnings) ? d.warnings : [],
  };
}

// ---- search (10.7) ----

export function useOccupancySearch(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: OccupancySearchRequest) =>
      unwrap(api.POST(`${M}/occupancies/search`, { params: { path: { marinaId } }, body })),
  });
}

// ---- inquiries ----

export type InquiryFilters = { outcome?: S['InquiryOutcome']; from?: string; to?: string };

export function useInquiries(marinaId: string, filters: InquiryFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.inquiries(marinaId, compact(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/inquiries`, {
          params: {
            path: { marinaId },
            query: { limit: PAGE_SIZE, ...filters, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCreateInquiry(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: InquiryCreate, key) =>
    unwrap(
      api.POST(`${M}/inquiries`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateInquiry(marinaId: string) {
  return useDeskMutation(
    marinaId,
    (api, v: { inquiryId: string; version: number; patch: S['InquiryPatch'] }) =>
      unwrap(
        api.PATCH(`${M}/inquiries/{inquiryId}`, {
          params: {
            path: { marinaId, inquiryId: v.inquiryId },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.patch,
        }),
      ),
  );
}

// ---- quotes ----

export type QuoteFilters = { status?: S['QuoteStatus']; kind?: S['QuoteKind']; q?: string };

export function useQuotes(marinaId: string, filters: QuoteFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.quotes(marinaId, compact(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/quotes`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...(filters.status ? { status: filters.status } : {}),
              ...(filters.kind ? { kind: filters.kind } : {}),
              ...(filters.q ? { q: filters.q } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useQuote(marinaId: string, quoteId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.quote(marinaId, quoteId),
    queryFn: () =>
      unwrap(api.GET(`${M}/quotes/{quoteId}`, { params: { path: { marinaId, quoteId } } })),
  });
}

export function useCreateQuote(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: QuoteInput, key) =>
    unwrap(
      api.POST(`${M}/quotes`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateQuote(marinaId: string) {
  return useDeskMutation(
    marinaId,
    (api, v: { quoteId: string; version: number; body: QuoteInput }) =>
      unwrap(
        api.PATCH(`${M}/quotes/{quoteId}`, {
          params: {
            path: { marinaId, quoteId: v.quoteId },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
  );
}

type Versioned = { id: string; version: number };

export function useMarkQuoteSent(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/quotes/{quoteId}/send`, {
        params: {
          path: { marinaId, quoteId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useAcceptQuote(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { body: S['QuoteAccept'] }, key) =>
    unwrap(
      api.POST(`${M}/quotes/{quoteId}/accept`, {
        params: {
          path: { marinaId, quoteId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.body,
      }),
    ),
  );
}

export function useDeclineQuote(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { reason?: string }, key) =>
    unwrap(
      api.POST(`${M}/quotes/{quoteId}/decline`, {
        params: {
          path: { marinaId, quoteId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.reason ? { reason: v.reason } : {},
      }),
    ),
  );
}

// ---- contracts (mooring) and transits: both are Contract resources ----

export type ContractFilters = {
  status?: S['ContractStatus'];
  term?: S['ContractTerm'];
  q?: string;
  vessel_id?: string;
  customer_id?: string;
  active_on?: string;
};

export function useContracts(marinaId: string, filters: ContractFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.contracts(marinaId, compact(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/contracts`, {
          params: {
            path: { marinaId },
            query: {
              kind: 'mooring',
              limit: PAGE_SIZE,
              ...compact(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** A contract or a transit (getContract serves both). */
export function useContract(marinaId: string, contractId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.contract(marinaId, contractId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/contracts/{contractId}`, { params: { path: { marinaId, contractId } } }),
      ),
  });
}

export function useCreateContract(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: ContractCreate, key) =>
    unwrap(
      api.POST(`${M}/contracts`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateContract(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { patch: S['ContractPatch'] }) =>
    unwrap(
      api.PATCH(`${M}/contracts/{contractId}`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

export function useActivateContract(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/activate`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useCancelContract(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { reason?: string }, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/cancel`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.reason ? { reason: v.reason } : {},
      }),
    ),
  );
}

export function useTerminateContract(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { body: S['TerminateInput'] }, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/terminate`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.body,
      }),
    ),
  );
}

export type TransitFilters = {
  status?: S['ContractStatus'];
  arrival_from?: string;
  arrival_to?: string;
  on?: string;
  q?: string;
};

export function useTransits(marinaId: string, filters: TransitFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.transits(marinaId, compact(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/transits`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compact(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCreateTransit(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: TransitCreate, key) =>
    unwrap(
      api.POST(`${M}/transits`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

/** Manual-driver confirmation (INV-09): the only confirmation path of the desk. */
export function useConfirmTransit(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { body: ManualConfirmation }, key) =>
    unwrap(
      api.POST(`${M}/transits/{contractId}/confirm`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.body,
      }),
    ),
  );
}

export function useCancelTransit(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { reason?: string }, key) =>
    unwrap(
      api.POST(`${M}/transits/{contractId}/cancel`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.reason ? { reason: v.reason } : {},
      }),
    ),
  );
}

export function useMarkTransitNoShow(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/transits/{contractId}/no-show`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useChangeTransitStay(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { body: S['StayChange'] }, key) =>
    unwrap(
      api.POST(`${M}/transits/{contractId}/stay`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.body,
      }),
    ),
  );
}

export function useUpdateTransit(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { patch: S['TransitPatch'] }) =>
    unwrap(
      api.PATCH(`${M}/transits/{contractId}`, {
        params: {
          path: { marinaId, contractId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

// ---- releases ----

export type ReleaseFilters = {
  from?: string;
  to?: string;
  resource_id?: string;
  status?: S['ReleaseStatus'];
};

export function useReleases(marinaId: string, filters: ReleaseFilters = {}, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.releases(marinaId, compact(filters)),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/releases`, {
          params: { path: { marinaId }, query: { limit: 200, ...compact(filters) } as never },
        }),
      ),
  });
}

export function useCreateRelease(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: S['ReleaseCreate'], key) =>
    unwrap(
      api.POST(`${M}/releases`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useRevokeRelease(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned & { body: S['RevokeRequest'] }, key) =>
    unwrap(
      api.POST(`${M}/releases/{releaseId}/revoke`, {
        params: {
          path: { marinaId, releaseId: v.id },
          header: { 'Idempotency-Key': key, 'If-Match': ifMatch(v.version) },
        },
        body: v.body,
      }),
    ),
  );
}

// ---- closures and occupancy settings ----

export function useClosures(marinaId: string, filters: { from?: string; to?: string } = {}) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.closures(marinaId, compact(filters)),
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/closures`, { params: { path: { marinaId }, query: compact(filters) } }),
        )
      ).items,
  });
}

export function useCreateClosure(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: S['ClosureCreate'], key) =>
    unwrap(
      api.POST(`${M}/closures`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useDeleteClosure(marinaId: string) {
  return useDeskMutation(marinaId, (api, v: Versioned) =>
    unwrap(
      api.DELETE(`${M}/closures/{closureId}`, {
        params: { path: { marinaId, closureId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
      }),
    ),
  );
}

export function useOccupancySettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.occupancySettings(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/occupancy-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateOccupancySettings(marinaId: string) {
  return useDeskMutation(
    marinaId,
    (api, v: { version: number; body: S['OccupancySettingsUpdate'] }) =>
      unwrap(
        api.PUT(`${M}/occupancy-settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
  );
}

// ---- presences and movements ----

export function useMovements(marinaId: string, date: string, mapId?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.movements(marinaId, date, mapId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/movements`, {
          params: { path: { marinaId }, query: { date, ...(mapId ? { map_id: mapId } : {}) } },
        }),
      ),
  });
}

export function usePresences(marinaId: string, date: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.presences(marinaId, date),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/presences`, { params: { path: { marinaId }, query: { date, limit: 200 } } }),
      ),
  });
}

export function useRecordPresence(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: S['PresenceCreate'], key) =>
    unwrap(
      api.POST(`${M}/presences`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useRecordUnexpectedArrival(marinaId: string) {
  return useDeskMutation(marinaId, (api, body: S['UnexpectedArrival'], key) =>
    unwrap(
      api.POST(`${M}/presences/unexpected-arrival`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

/** Accepted photo types and size (raw body upload, as the M1 files). */
export const PRESENCE_PHOTO_TYPES = ['image/jpeg', 'image/png', 'image/webp'] as const;
export const PRESENCE_PHOTO_MAX_BYTES = 8 * 1024 * 1024;

export function useUploadPresencePhoto(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { presenceId: string; file: Blob }) =>
      (
        await uploadBinary<S['PresencePhoto']>(
          `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/presences/${encodeURIComponent(v.presenceId)}/photo`,
          v.file,
          { method: 'POST', contentType: v.file.type },
        )
      ).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.presences(marinaId) }),
  });
}
