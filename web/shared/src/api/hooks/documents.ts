// Query and mutation hooks of the M4 documents area (m4-plan §8.2, §7.3.3–§7.3.5): print templates and their
// versions, PDF previews, generated documents and their signed download links, map print, signature requests and
// settings, communications, the per-customer message registry, deadline sync and CSV imports. Keys live under
// ['marina', id, 'docs'] (documents.changed); deadline sync also invalidates the registry root (M1 deadlines).
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, uploadBinary, type ApiClient } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';
import { actionHeaders, compactFilters, fetchExport } from './contractsM4';

type S = components['schemas'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const PAGE_SIZE = 50;

export type TemplateKind = S['TemplateKind'];
export type TemplateKindInfo = S['TemplateKindInfo'];
export type FieldDef = S['FieldDef'];
export type Template = S['Template'];
export type TemplateInput = S['TemplateInput'];
export type TemplateVersion = S['TemplateVersion'];
export type TemplateVersionInput = S['TemplateVersionInput'];
export type TemplatePreviewInput = S['TemplatePreviewInput'];
export type GeneratedDocument = S['GeneratedDocument'];
export type DocumentSubjectType = S['DocumentSubjectType'];
export type SignatureRequest = S['SignatureRequest'];
export type SignatureRequestStatus = S['SignatureRequestStatus'];
export type Signer = S['Signer'];
export type SignatureSettings = S['SignatureSettings'];
export type Communication = S['Communication'];
export type CommunicationInput = S['CommunicationInput'];
export type CommAudience = S['CommAudience'];
export type CommunicationPreview = S['CommunicationPreview'];
export type CommRecipient = S['CommRecipient'];
export type CustomerMessage = S['CustomerMessage'];
export type ImportEntity = S['ImportEntity'];
export type ImportInput = S['ImportInput'];
export type ImportReport = S['ImportReport'];
export type ImportRun = S['ImportRun'];
export type DeadlineSyncResult = S['DeadlineSyncResult'];

type Versioned = { id: string; version: number };

function invalidateDocs(qc: QueryClient, marinaId: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.docsArea(marinaId) }),
    // A signature or an import changes contracts, rights and registry rows too.
    qc.invalidateQueries({ queryKey: queryKeys.desk(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.rights(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.registry(marinaId) }),
  ]);
}

function useDocsMutation<V, R>(
  marinaId: string,
  fn: (api: ApiClient, vars: V, idempotencyKey: string) => Promise<R>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => fn(api, vars, newIdempotencyKey()),
    onSuccess: () => invalidateDocs(qc, marinaId),
  });
}

// ---- templates (15.1–15.3) ----

export function useTemplateKinds(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.templateKinds(marinaId),
    staleTime: Infinity,
    queryFn: async () =>
      (await unwrap(api.GET(`${M}/template-kinds`, { params: { path: { marinaId } } }))).items,
  });
}

export function useTemplates(marinaId: string, kind?: TemplateKind) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.templates(marinaId, compactFilters({ kind })),
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/templates`, {
            params: { path: { marinaId }, query: kind ? { kind } : {} },
          }),
        )
      ).items,
  });
}

export function useTemplate(marinaId: string, templateId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.template(marinaId, templateId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/templates/{templateId}`, { params: { path: { marinaId, templateId } } }),
      ),
  });
}

export function useCreateTemplate(marinaId: string) {
  return useDocsMutation(marinaId, (api, body: TemplateInput, key) =>
    unwrap(
      api.POST(`${M}/templates`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateTemplate(marinaId: string) {
  return useDocsMutation(marinaId, (api, v: Versioned & { patch: S['TemplatePatch'] }) =>
    unwrap(
      api.PATCH(`${M}/templates/{templateId}`, {
        params: {
          path: { marinaId, templateId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

export function useTemplateVersions(marinaId: string, templateId: string, locale?: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.templateVersions(marinaId, templateId), locale ?? ''],
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/templates/{templateId}/versions`, {
            params: {
              path: { marinaId, templateId },
              query: locale ? { locale: locale as S['Locale'] } : {},
            },
          }),
        )
      ).items,
  });
}

/** Saves a draft version (a new draft replaces the previous draft of the same locale server side). */
export function useSaveTemplateVersion(marinaId: string) {
  return useDocsMutation(
    marinaId,
    (api, v: { templateId: string; body: TemplateVersionInput }, key) =>
      unwrap(
        api.POST(`${M}/templates/{templateId}/versions`, {
          params: {
            path: { marinaId, templateId: v.templateId },
            header: { 'Idempotency-Key': key },
          },
          body: v.body,
        }),
      ),
  );
}

export function usePublishTemplateVersion(marinaId: string) {
  return useDocsMutation(
    marinaId,
    (api, v: { templateId: string; version: number; locale: S['Locale'] }, key) =>
      unwrap(
        api.POST(`${M}/templates/{templateId}/versions/{templateVersion}/publish`, {
          params: {
            path: { marinaId, templateId: v.templateId, templateVersion: String(v.version) },
            header: { 'Idempotency-Key': key },
          },
          body: { locale: v.locale },
        }),
      ),
  );
}

/**
 * PDF preview of a template body (unsaved text allowed). Returns the PDF blob; a `template_invalid` problem
 * carries the errors by line in its details.
 */
export function usePreviewTemplate(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { templateId: string; body: TemplatePreviewInput }) =>
      fetchExport(
        api.POST(`${M}/templates/{templateId}/preview`, {
          params: { path: { marinaId, templateId: v.templateId } },
          body: v.body,
          parseAs: 'blob',
        }),
      ),
  });
}

// ---- generated documents (15.3) ----

export function useGeneratedDocuments(
  marinaId: string,
  subjectType: DocumentSubjectType | undefined,
  subjectId: string | undefined,
  enabled = true,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.generatedDocs(marinaId, subjectType, subjectId),
    enabled,
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/generated-documents`, {
            params: {
              path: { marinaId },
              query: {
                limit: 200,
                ...(subjectType ? { subject_type: subjectType } : {}),
                ...(subjectId ? { subject_id: subjectId } : {}),
              },
            },
          }),
        )
      ).items,
  });
}

/** A short-lived signed download link (M1 mechanism, 5 minutes). */
export function useGeneratedDocumentLink(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (documentId: string) =>
      unwrap(
        api.GET(`${M}/generated-documents/{generatedDocumentId}/link`, {
          params: { path: { marinaId, generatedDocumentId: documentId } },
        }),
      ),
  });
}

export function useRetryRender(marinaId: string) {
  return useDocsMutation(marinaId, (api, documentId: string, key) =>
    unwrap(
      api.POST(`${M}/generated-documents/{generatedDocumentId}/render`, {
        params: {
          path: { marinaId, generatedDocumentId: documentId },
          header: { 'Idempotency-Key': key },
        },
      }),
    ),
  );
}

/** PDF of a map for a night (M3 D25), with the lens colours when given. */
export function usePrintMap(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { mapId: string; date: string; lens?: string }) =>
      fetchExport(
        api.GET(`${M}/maps/{mapId}/print`, {
          params: {
            path: { marinaId, mapId: v.mapId },
            query: { date: v.date, ...(v.lens ? { lens: v.lens } : {}) },
          },
          parseAs: 'blob',
        }),
      ),
  });
}

// ---- signature requests (15.4) ----

export function useSignatureRequests(
  marinaId: string,
  filters: {
    status?: SignatureRequestStatus;
    subject_type?: S['SignatureSubjectType'];
    subject_id?: string;
  } = {},
  enabled = true,
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.signatures(marinaId, compactFilters(filters)),
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/signature-requests`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compactFilters(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useSignatureRequest(marinaId: string, requestId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.signature(marinaId, requestId),
    enabled: enabled && !!requestId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/signature-requests/{signatureRequestId}`, {
          params: { path: { marinaId, signatureRequestId: requestId } },
        }),
      ),
  });
}

export function useSignatureAction(marinaId: string) {
  return useDocsMutation(marinaId, (api, v: Versioned & { action: 'remind' | 'cancel' }, key) =>
    v.action === 'remind'
      ? unwrap(
          api.POST(`${M}/signature-requests/{signatureRequestId}/remind`, {
            params: {
              path: { marinaId, signatureRequestId: v.id },
              header: actionHeaders(key, v.version),
            },
          }),
        )
      : unwrap(
          api.POST(`${M}/signature-requests/{signatureRequestId}/cancel`, {
            params: {
              path: { marinaId, signatureRequestId: v.id },
              header: actionHeaders(key, v.version),
            },
          }),
        ),
  );
}

export const PAPER_SCAN_TYPES = ['application/pdf', 'image/png', 'image/jpeg'] as const;
export const PAPER_SCAN_MAX_BYTES = 20 * 1024 * 1024;

/** Uploads the scan of the paper-signed document (raw body, as the M1 files). */
export function useUploadPaperScan(marinaId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { requestId: string; file: Blob }) =>
      (
        await uploadBinary<S['PaperScanResult']>(
          `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/signature-requests/${encodeURIComponent(v.requestId)}/paper-scan`,
          v.file,
          {
            method: 'POST',
            contentType: v.file.type,
            headers: { 'Idempotency-Key': newIdempotencyKey() },
          },
        )
      ).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.docsArea(marinaId) }),
  });
}

export function useConfirmPaperSignature(marinaId: string) {
  return useDocsMutation(marinaId, (api, v: Versioned & { body: S['PaperConfirmInput'] }, key) =>
    unwrap(
      api.POST(`${M}/signature-requests/{signatureRequestId}/confirm-paper`, {
        params: {
          path: { marinaId, signatureRequestId: v.id },
          header: actionHeaders(key, v.version),
        },
        body: v.body,
      }),
    ),
  );
}

export function useSignatureSettings(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.signatureSettings(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/signature-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateSignatureSettings(marinaId: string) {
  return useDocsMutation(
    marinaId,
    (api, v: { version: number; body: S['SignatureSettingsInput'] }) =>
      unwrap(
        api.PUT(`${M}/signature-settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
  );
}

// ---- communications (28) ----

export function useCommunications(marinaId: string, status?: S['CommStatus']) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.communications(marinaId, compactFilters({ status })),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/communications`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...(status ? { status } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCommunication(marinaId: string, campaignId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.communication(marinaId, campaignId),
    enabled: enabled && !!campaignId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/communications/{commCampaignId}`, {
          params: { path: { marinaId, commCampaignId: campaignId } },
        }),
      ),
  });
}

export function useCreateCommunication(marinaId: string) {
  return useDocsMutation(marinaId, (api, body: CommunicationInput, key) =>
    unwrap(
      api.POST(`${M}/communications`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateCommunication(marinaId: string) {
  return useDocsMutation(marinaId, (api, v: Versioned & { patch: S['CommunicationPatch'] }) =>
    unwrap(
      api.PATCH(`${M}/communications/{commCampaignId}`, {
        params: {
          path: { marinaId, commCampaignId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

export function usePreviewCommunication(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { id: string; customerId?: string }) =>
      unwrap(
        api.POST(`${M}/communications/{commCampaignId}/preview`, {
          params: { path: { marinaId, commCampaignId: v.id } },
          body: v.customerId ? { customer_id: v.customerId } : {},
        }),
      ),
  });
}

export function useCommunicationAction(marinaId: string) {
  return useDocsMutation(
    marinaId,
    (api, v: Versioned & { action: 'send' | 'cancel' | 'schedule'; scheduledAt?: string }, key) => {
      const params = {
        path: { marinaId, commCampaignId: v.id },
        header: actionHeaders(key, v.version),
      };
      if (v.action === 'schedule') {
        return unwrap(
          api.POST(`${M}/communications/{commCampaignId}/schedule`, {
            params,
            body: { scheduled_at: v.scheduledAt ?? '' },
          }),
        );
      }
      if (v.action === 'cancel') {
        return unwrap(api.POST(`${M}/communications/{commCampaignId}/cancel`, { params }));
      }
      return unwrap(api.POST(`${M}/communications/{commCampaignId}/send`, { params }));
    },
  );
}

export function useCommunicationRecipients(marinaId: string, campaignId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.communicationRecipients(marinaId, campaignId),
    enabled: enabled && !!campaignId,
    queryFn: async () =>
      (await unwrap(
        api.GET(`${M}/communications/{commCampaignId}/recipients`, {
          params: { path: { marinaId, commCampaignId: campaignId }, query: { limit: 200 } },
        }),
      )) as S['CommRecipientPage'],
  });
}

export function useCustomerMessages(marinaId: string, customerId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.customerMessages(marinaId, customerId),
    enabled: enabled && !!customerId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/customers/{customerId}/messages`, {
          params: { path: { marinaId, customerId }, query: { limit: 100 } },
        }),
      ),
  });
}

// ---- deadlines engine (11.5) ----

export function useSyncDeadlines(marinaId: string) {
  return useDocsMutation(marinaId, (api, _v: void, key) =>
    unwrap(
      api.POST(`${M}/deadlines/sync`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
      }),
    ),
  );
}

// ---- imports (11.6) ----

export function useImports(marinaId: string, entity?: ImportEntity) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.imports(marinaId), entity ?? ''],
    queryFn: () =>
      unwrap(
        api.GET(`${M}/imports`, {
          params: { path: { marinaId }, query: { limit: 50, ...(entity ? { entity } : {}) } },
        }),
      ),
  });
}

/** Dry run or apply of a CSV import (the dry run writes nothing). */
export function useRunImport(marinaId: string) {
  return useDocsMutation(marinaId, (api, body: ImportInput, key) =>
    unwrap(
      api.POST(`${M}/imports`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUndoImport(marinaId: string) {
  return useDocsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/imports/{importRunId}/undo`, {
        params: { path: { marinaId, importRunId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}
