// Registry hooks (m1-plan §4.4, §5.6): customers, consents, vessels and parties, documents and files,
// deadlines and their policies. Every key sits under ['marina', id, 'registry'] so `registry.changed` (SSE)
// and every mutation here invalidate the whole area at once.
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import {
  ifMatch,
  newIdempotencyKey,
  unwrap,
  uploadBinary,
  type ApiClient,
  type UploadBinaryOptions,
} from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const PAGE_SIZE = 50;

/** Filters are stored in the query key as strings only (stable keys, no undefined values). */
function compact<T extends Record<string, string | boolean | undefined | null>>(
  filters: T,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) {
    if (v === undefined || v === null || v === '' || v === false) continue;
    out[k] = String(v);
  }
  return out;
}

function invalidateRegistry(queryClient: QueryClient, marinaId: string) {
  return queryClient.invalidateQueries({ queryKey: queryKeys.registry(marinaId) });
}

/** Mutation hook with registry invalidation on success. */
function useRegistryMutation<V, R>(marinaId: string, fn: (api: ApiClient, vars: V) => Promise<R>) {
  const api = useApi();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => fn(api, vars),
    onSuccess: () => invalidateRegistry(queryClient, marinaId),
  });
}

// ---------------------------------------------------------------------------------------------------------
// Customers

export type CustomerFilters = {
  q?: string;
  category?: S['CustomerCategory'];
  tag?: string;
  archived?: boolean;
};

export function useCustomers(marinaId: string, filters: CustomerFilters = {}) {
  const api = useApi();
  const key = compact(filters);
  return useInfiniteQuery({
    queryKey: queryKeys.customers(marinaId, key),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/customers', {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...(filters.q ? { q: filters.q } : {}),
              ...(filters.category ? { category: filters.category } : {}),
              ...(filters.tag ? { tag: filters.tag } : {}),
              ...(filters.archived ? { archived: true } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCustomerOverview(marinaId: string, customerId: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.customer(marinaId, customerId), 'overview'],
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/overview', {
          params: { path: { marinaId, customerId } },
        }),
      ),
  });
}

export function useCreateCustomer(marinaId: string) {
  return useRegistryMutation(marinaId, (api, body: S['CustomerInput']) =>
    unwrap(
      api.POST('/api/admin/v1/marinas/{marinaId}/customers', {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useUpdateCustomer(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { customerId: string; version: number; patch: S['CustomerPatch'] }) =>
      unwrap(
        api.PATCH('/api/admin/v1/marinas/{marinaId}/customers/{customerId}', {
          params: {
            path: { marinaId, customerId: vars.customerId },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useArchiveCustomer(marinaId: string) {
  return useRegistryMutation(marinaId, (api, vars: { customerId: string; archive: boolean }) =>
    vars.archive
      ? unwrap(
          api.POST('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/archive', {
            params: { path: { marinaId, customerId: vars.customerId } },
          }),
        )
      : unwrap(
          api.POST('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/unarchive', {
            params: { path: { marinaId, customerId: vars.customerId } },
          }),
        ),
  );
}

export function useMergeCustomers(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { targetId: string; version: number; sourceIds: string[] }) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/merge', {
          params: {
            path: { marinaId, customerId: vars.targetId },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: { source_ids: vars.sourceIds },
        }),
      ),
  );
}

export function useAnonymizeCustomer(marinaId: string) {
  return useRegistryMutation(marinaId, (api, customerId: string) =>
    unwrap(
      api.POST('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/anonymize', {
        params: { path: { marinaId, customerId } },
        body: { confirm: true },
      }),
    ),
  );
}

/** GDPR export: fetched through the client (session + CSRF rules), saved by the caller. */
export function useExportCustomer(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (customerId: string) =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/export', {
          params: { path: { marinaId, customerId } },
        }),
      ),
  });
}

export function useConsents(marinaId: string, customerId: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.customer(marinaId, customerId), 'consents'],
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/consents', {
          params: { path: { marinaId, customerId } },
        }),
      ),
  });
}

export function useRecordConsent(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { customerId: string; consent: S['ConsentInput'] }) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/customers/{customerId}/consents', {
          params: {
            path: { marinaId, customerId: vars.customerId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: vars.consent,
        }),
      ),
  );
}

// ---------------------------------------------------------------------------------------------------------
// Vessels

export type VesselFilters = {
  q?: string;
  customer_id?: string;
  hull_type?: S['HullType'];
  archived?: boolean;
};

export function useVessels(marinaId: string, filters: VesselFilters = {}) {
  const api = useApi();
  const key = compact(filters);
  return useInfiniteQuery({
    queryKey: queryKeys.vessels(marinaId, key),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/vessels', {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...(filters.q ? { q: filters.q } : {}),
              ...(filters.customer_id ? { customer_id: filters.customer_id } : {}),
              ...(filters.hull_type ? { hull_type: filters.hull_type } : {}),
              ...(filters.archived ? { archived: true } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useVessel(marinaId: string, vesselId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.vessel(marinaId, vesselId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/vessels/{vesselId}', {
          params: { path: { marinaId, vesselId } },
        }),
      ),
  });
}

export function useCreateVessel(marinaId: string) {
  return useRegistryMutation(marinaId, (api, body: S['VesselInput']) =>
    unwrap(
      api.POST('/api/admin/v1/marinas/{marinaId}/vessels', {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useUpdateVessel(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { vesselId: string; version: number; patch: S['VesselPatch'] }) =>
      unwrap(
        api.PATCH('/api/admin/v1/marinas/{marinaId}/vessels/{vesselId}', {
          params: {
            path: { marinaId, vesselId: vars.vesselId },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useReplaceVesselParties(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { vesselId: string; version: number; parties: S['VesselPartyInput'][] }) =>
      unwrap(
        api.PUT('/api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/parties', {
          params: {
            path: { marinaId, vesselId: vars.vesselId },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: { parties: vars.parties },
        }),
      ),
  );
}

export function useRecordVesselMeasurement(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { vesselId: string; measurement: S['VesselMeasurementInput'] }) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/measurements', {
          params: { path: { marinaId, vesselId: vars.vesselId } },
          body: vars.measurement,
        }),
      ),
  );
}

export function useArchiveVessel(marinaId: string) {
  return useRegistryMutation(marinaId, (api, vars: { vesselId: string; archive: boolean }) =>
    vars.archive
      ? unwrap(
          api.POST('/api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/archive', {
            params: { path: { marinaId, vesselId: vars.vesselId } },
          }),
        )
      : unwrap(
          api.POST('/api/admin/v1/marinas/{marinaId}/vessels/{vesselId}/unarchive', {
            params: { path: { marinaId, vesselId: vars.vesselId } },
          }),
        ),
  );
}

// ---------------------------------------------------------------------------------------------------------
// Documents

export type DocumentFilters = {
  customer_id?: string;
  vessel_id?: string;
  type?: S['DocumentType'];
  expires_before?: string;
};

export function useDocuments(marinaId: string, filters: DocumentFilters = {}) {
  const api = useApi();
  const key = compact(filters);
  return useQuery({
    queryKey: queryKeys.documents(marinaId, key),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/documents', {
          params: { path: { marinaId }, query: { limit: 200, ...key } },
        }),
      ),
  });
}

export function useCreateDocument(marinaId: string) {
  return useRegistryMutation(marinaId, (api, body: S['DocumentInput']) =>
    unwrap(
      api.POST('/api/admin/v1/marinas/{marinaId}/documents', {
        params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
        body,
      }),
    ),
  );
}

export function useUpdateDocument(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { documentId: string; version: number; patch: S['DocumentPatch'] }) =>
      unwrap(
        api.PATCH('/api/admin/v1/marinas/{marinaId}/documents/{documentId}', {
          params: {
            path: { marinaId, documentId: vars.documentId },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useDeleteDocument(marinaId: string) {
  return useRegistryMutation(marinaId, (api, vars: { documentId: string; version: number }) =>
    unwrap(
      api.DELETE('/api/admin/v1/marinas/{marinaId}/documents/{documentId}', {
        params: {
          path: { marinaId, documentId: vars.documentId },
          header: { 'If-Match': ifMatch(vars.version) },
        },
      }),
    ),
  );
}

/** Content types accepted for document files (D19); the server sniffs the bytes anyway. */
export const DOCUMENT_FILE_TYPES = ['application/pdf', 'image/png', 'image/jpeg'] as const;
/** 15 MiB (D19). */
export const DOCUMENT_FILE_MAX_BYTES = 15 * 1024 * 1024;

export function documentFilePath(marinaId: string, documentId: string): string {
  return `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/documents/${encodeURIComponent(documentId)}/file`;
}

/**
 * Header values must be printable ASCII to be portable: other characters (accents, control characters) become
 * "_". The server sanitises the name again and keeps at most 120 characters.
 */
export function headerSafeFileName(name: string): string {
  return name.replace(/[^\x20-\x7e]/g, '_').slice(0, 255);
}

/** Uploads (or replaces) a document file as a raw binary PUT (m1-plan §5.6). */
export function useUploadDocumentFile(
  marinaId: string,
  options: Pick<UploadBinaryOptions, 'fetch' | 'readCsrf'> & { baseUrl?: string } = {},
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { documentId: string; file: File }) => {
      const { data } = await uploadBinary<S['Document']>(
        `${options.baseUrl ?? ''}${documentFilePath(marinaId, vars.documentId)}`,
        vars.file,
        {
          method: 'PUT',
          contentType: vars.file.type || 'application/octet-stream',
          headers: { 'X-File-Name': headerSafeFileName(vars.file.name) },
          ...(options.fetch ? { fetch: options.fetch } : {}),
          ...(options.readCsrf ? { readCsrf: options.readCsrf } : {}),
        },
      );
      return data;
    },
    onSuccess: () => invalidateRegistry(queryClient, marinaId),
  });
}

export function useDeleteDocumentFile(marinaId: string) {
  return useRegistryMutation(marinaId, (api, documentId: string) =>
    unwrap(
      api.DELETE('/api/admin/v1/marinas/{marinaId}/documents/{documentId}/file', {
        params: { path: { marinaId, documentId } },
      }),
    ),
  );
}

/** Asks for a short-lived signed link (5 minutes, bound to the user) to view a document file. */
export function useDocumentFileUrl(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (documentId: string) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/documents/{documentId}/file-url', {
          params: { path: { marinaId, documentId } },
        }),
      ),
  });
}

// ---------------------------------------------------------------------------------------------------------
// Deadlines

export type DeadlineFilters = {
  status?: S['DeadlineStatus'];
  kind?: S['DeadlineKind'];
  urgency?: S['DeadlineUrgency'];
  due_before?: string;
  due_after?: string;
  customer_id?: string;
  vessel_id?: string;
};

export function useDeadlines(marinaId: string, filters: DeadlineFilters = {}) {
  const api = useApi();
  const key = compact(filters);
  return useInfiniteQuery({
    queryKey: queryKeys.deadlines(marinaId, key),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/deadlines', {
          params: {
            path: { marinaId },
            query: { limit: 200, ...key, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** Open deadlines of one urgency, first page only (dashboard counts). */
export function useDeadlineCount(marinaId: string, urgency: S['DeadlineUrgency'], enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.deadlines(marinaId, { urgency, status: 'open', count: '1' }),
    enabled,
    queryFn: async () => {
      const page = await unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/deadlines', {
          params: { path: { marinaId }, query: { limit: 200, status: 'open', urgency } },
        }),
      );
      return { count: page.items.length, more: page.next_cursor !== null };
    },
  });
}

export function useUpdateDeadline(marinaId: string) {
  return useRegistryMutation(
    marinaId,
    (api, vars: { deadlineId: string; version: number; patch: S['DeadlinePatch'] }) =>
      unwrap(
        api.PATCH('/api/admin/v1/marinas/{marinaId}/deadlines/{deadlineId}', {
          params: {
            path: { marinaId, deadlineId: vars.deadlineId },
            header: { 'If-Match': ifMatch(vars.version) },
          },
          body: vars.patch,
        }),
      ),
  );
}

export function useDeadlinePolicies(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.deadlinePolicies(marinaId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/deadline-policies', {
          params: { path: { marinaId } },
        }),
      ),
  });
}

export function useReplaceDeadlinePolicies(marinaId: string) {
  return useRegistryMutation(marinaId, (api, items: S['DeadlinePolicy'][]) =>
    unwrap(
      api.PUT('/api/admin/v1/marinas/{marinaId}/deadline-policies', {
        params: { path: { marinaId } },
        body: { items },
      }),
    ),
  );
}
