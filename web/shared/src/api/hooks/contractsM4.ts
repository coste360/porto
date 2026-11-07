// Query and mutation hooks of the M4 contracts area (m4-plan §8.2, §7.3.1): contract settings, signature requests,
// suspension, renewal, instalments and the scadenzario, renewal campaigns and proposals, charter fleets and
// charters, addenda, waiting list and the M4 quote send. Keys live under the desk root (contract.changed); every
// mutation invalidates the desk and the ops roots (an instalment or a renewal hold recolours the map) and the docs
// root (a send or a signature request generates a document).
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, type ApiClient } from '../client';
import { useApi } from '../context';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';

type S = components['schemas'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const PAGE_SIZE = 50;

export type ContractSettings = S['ContractSettings'];
export type ContractSettingsUpdate = S['ContractSettingsUpdate'];
export type ContractPolicy = S['ContractPolicy'];
export type PaymentPolicy = S['PaymentPolicy'];
export type PaymentSchedule = S['PaymentSchedule'];
export type Installment = S['Installment'];
export type InstallmentPlan = S['InstallmentPlan'];
export type PlannedInstallment = S['PlannedInstallment'];
export type InstallmentStatus = S['InstallmentStatus'];
export type InstallmentFilterStatus = S['InstallmentFilterStatus'];
export type SettlementMethod = S['SettlementMethod'];
export type SettleInput = S['SettleInput'];
export type BulkSettleInput = S['BulkSettleInput'];
export type PlanChoice = S['PlanChoice'];
export type SignatureRequestInput = S['SignatureRequestInput'];
export type SignerInput = S['SignerInput'];
export type SignerRole = S['SignerRole'];
export type RenewalCampaign = S['RenewalCampaign'];
export type RenewalCampaignInput = S['RenewalCampaignInput'];
export type RenewalCampaignPatch = S['RenewalCampaignPatch'];
export type RenewalCampaignPreview = S['RenewalCampaignPreview'];
export type RenewalCampaignStatus = S['RenewalCampaignStatus'];
export type RenewalFilter = S['RenewalFilter'];
export type RenewalProposal = S['RenewalProposal'];
export type RenewalProposalStatus = S['RenewalProposalStatus'];
export type RenewalExclusionReason = S['RenewalExclusionReason'];
export type RenewInput = S['RenewInput'];
export type CharterFleet = S['CharterFleet'];
export type CharterFleetInput = S['CharterFleetInput'];
export type CharterCreate = S['CharterCreate'];
export type CharterRotate = S['CharterRotate'];
export type Addendum = S['Addendum'];
export type AddendumInput = S['AddendumInput'];
export type AddendumKind = S['AddendumKind'];
export type WaitlistEntry = S['WaitlistEntry'];
export type WaitlistInput = S['WaitlistInput'];
export type WaitlistPatch = S['WaitlistPatch'];
export type QuoteSendResult = S['QuoteSendResult'];
export type WriteWarning = S['WriteWarning'];

type Versioned = { id: string; version: number };

/** Filters are stored in the query key as strings only (stable keys, no undefined values). */
export function compactFilters(filters: Record<string, string | boolean | undefined | null>) {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(filters)) {
    if (v === undefined || v === null || v === '' || v === false) continue;
    out[k] = String(v);
  }
  return out;
}

function invalidateContracts(qc: QueryClient, marinaId: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.desk(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.ops(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.docsArea(marinaId) }),
  ]);
}

function useContractsMutation<V, R>(
  marinaId: string,
  fn: (api: ApiClient, vars: V, idempotencyKey: string) => Promise<R>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => fn(api, vars, newIdempotencyKey()),
    onSuccess: () => invalidateContracts(qc, marinaId),
  });
}

/** Headers of a versioned action (`Idempotency-Key` + `If-Match`). */
export function actionHeaders(key: string, version: number) {
  return { 'Idempotency-Key': key, 'If-Match': ifMatch(version) };
}

/**
 * Fetches a binary or text export of an admin operation (`format=csv|pdf`, PDF previews) through the generated
 * client, so CSRF and problem handling stay the same as JSON calls.
 */
export async function fetchExport(
  promise: Promise<{ data?: unknown; error?: unknown; response: Response }>,
): Promise<Blob> {
  const data = (await unwrap(promise)) as unknown;
  if (data instanceof Blob) return data;
  return new Blob([typeof data === 'string' ? data : JSON.stringify(data)], {
    type: 'text/csv;charset=utf-8',
  });
}

// ---- contract settings (13.7, 15.4) ----

export function useContractSettings(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.contractSettings(marinaId),
    enabled,
    queryFn: () => unwrap(api.GET(`${M}/contract-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateContractSettings(marinaId: string) {
  return useContractsMutation(
    marinaId,
    (api, v: { version: number; body: ContractSettingsUpdate }) =>
      unwrap(
        api.PUT(`${M}/contract-settings`, {
          params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
          body: v.body,
        }),
      ),
  );
}

// ---- quote send (13.3) ----

/** Sends the quote by email with its PDF (manual = true only marks it as sent, as in M3). */
export function useSendQuote(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { manual?: boolean }, key) =>
    unwrap(
      api.POST(`${M}/quotes/{quoteId}/send`, {
        params: {
          path: { marinaId, quoteId: v.id },
          header: actionHeaders(key, v.version),
          ...(v.manual ? { query: { manual: true } } : {}),
        },
      }),
    ),
  );
}

// ---- contract M4 actions ----

export function useRequestContractSignature(marinaId: string) {
  return useContractsMutation(
    marinaId,
    (api, v: Versioned & { body: SignatureRequestInput }, key) =>
      unwrap(
        api.POST(`${M}/contracts/{contractId}/request-signature`, {
          params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
          body: v.body,
        }),
      ),
  );
}

export function useSuspendContract(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { reason: string }, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/suspend`, {
        params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
        body: { reason: v.reason },
      }),
    ),
  );
}

export function useResumeContract(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/resume`, {
        params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useRenewContract(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { body: RenewInput }, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/renew`, {
        params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
        body: v.body,
      }),
    ),
  );
}

// ---- instalments (13.7) ----

export function useContractInstallments(marinaId: string, contractId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.contractInstallments(marinaId, contractId),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/contracts/{contractId}/installments`, {
          params: { path: { marinaId, contractId } },
        }),
      ),
  });
}

export function useReplanInstallments(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { body: S['ReplanInput'] }, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/installments/replan`, {
        params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
        body: v.body,
      }),
    ),
  );
}

export type InstallmentFilters = {
  from?: string;
  to?: string;
  status?: InstallmentFilterStatus;
  customer_id?: string;
  contract_id?: string;
  group_id?: string;
};

export function useInstallments(marinaId: string, filters: InstallmentFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.installments(marinaId, compactFilters(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      (await unwrap(
        api.GET(`${M}/installments`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compactFilters(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      )) as S['InstallmentPage'],
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** CSV of the scadenzario with the same filters (spec 13.7). */
export function useInstallmentsCsv(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (filters: InstallmentFilters) =>
      fetchExport(
        api.GET(`${M}/installments`, {
          params: {
            path: { marinaId },
            query: { ...compactFilters(filters), format: 'csv' } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useIssueInstallments(marinaId: string) {
  return useContractsMutation(marinaId, (api, ids: string[], key) =>
    unwrap(
      api.POST(`${M}/installments/issue`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body: { ids },
      }),
    ),
  );
}

export function useRemindInstallments(marinaId: string) {
  return useContractsMutation(marinaId, (api, ids: string[], key) =>
    unwrap(
      api.POST(`${M}/installments/remind`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body: { ids },
      }),
    ),
  );
}

export function useSettleInstallment(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { body: SettleInput }, key) =>
    unwrap(
      api.POST(`${M}/installments/{installmentId}/settle`, {
        params: { path: { marinaId, installmentId: v.id }, header: actionHeaders(key, v.version) },
        body: v.body,
      }),
    ),
  );
}

export function useSettleInstallments(marinaId: string) {
  return useContractsMutation(marinaId, (api, body: BulkSettleInput, key) =>
    unwrap(
      api.POST(`${M}/installments/settle`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

// ---- renewal campaigns and proposals (13.8) ----

export function useRenewalCampaigns(marinaId: string, status?: RenewalCampaignStatus) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.renewals(marinaId, compactFilters({ status })),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/renewal-campaigns`, {
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

export function useRenewalCampaign(marinaId: string, campaignId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.campaign(marinaId, campaignId),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/renewal-campaigns/{campaignId}`, {
          params: { path: { marinaId, campaignId } },
        }),
      ),
  });
}

export function useCreateRenewalCampaign(marinaId: string) {
  return useContractsMutation(marinaId, (api, body: RenewalCampaignInput, key) =>
    unwrap(
      api.POST(`${M}/renewal-campaigns`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateRenewalCampaign(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { patch: RenewalCampaignPatch }) =>
    unwrap(
      api.PATCH(`${M}/renewal-campaigns/{campaignId}`, {
        params: {
          path: { marinaId, campaignId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

/** Computes the proposals (holds the berths, D4) and returns the preview table. */
export function usePreviewRenewalCampaign(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Versioned) =>
      unwrap(
        api.POST(`${M}/renewal-campaigns/{campaignId}/preview`, {
          params: {
            path: { marinaId, campaignId: v.id },
            header: actionHeaders(newIdempotencyKey(), v.version),
          },
        }),
      ),
    onSuccess: () => invalidateContracts(qc, marinaId),
  });
}

export function useLaunchRenewalCampaign(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/renewal-campaigns/{campaignId}/launch`, {
        params: { path: { marinaId, campaignId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useCloseRenewalCampaign(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/renewal-campaigns/{campaignId}/close`, {
        params: { path: { marinaId, campaignId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useDiscardRenewalCampaign(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/renewal-campaigns/{campaignId}/discard`, {
        params: { path: { marinaId, campaignId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useCampaignProposals(
  marinaId: string,
  campaignId: string,
  status?: RenewalProposalStatus,
  enabled = true,
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.campaignProposals(marinaId, campaignId, compactFilters({ status })),
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      (await unwrap(
        api.GET(`${M}/renewal-campaigns/{campaignId}/proposals`, {
          params: {
            path: { marinaId, campaignId },
            query: {
              limit: 200,
              ...(status ? { status } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      )) as S['RenewalProposalPage'],
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCampaignProposalsCsv(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (campaignId: string) =>
      fetchExport(
        api.GET(`${M}/renewal-campaigns/{campaignId}/proposals`, {
          params: { path: { marinaId, campaignId }, query: { format: 'csv' } as never },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useRenewalProposal(marinaId: string, proposalId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.proposal(marinaId, proposalId),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/renewal-proposals/{proposalId}`, {
          params: { path: { marinaId, proposalId } },
        }),
      ),
  });
}

export function useSendRenewalProposal(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/renewal-proposals/{proposalId}/send`, {
        params: { path: { marinaId, proposalId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useAcceptRenewalProposal(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { plan?: PlanChoice }, key) =>
    unwrap(
      api.POST(`${M}/renewal-proposals/{proposalId}/accept`, {
        params: { path: { marinaId, proposalId: v.id }, header: actionHeaders(key, v.version) },
        body: v.plan ? { plan: v.plan } : {},
      }),
    ),
  );
}

export function useDeclineRenewalProposal(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { reason?: string }, key) =>
    unwrap(
      api.POST(`${M}/renewal-proposals/{proposalId}/decline`, {
        params: { path: { marinaId, proposalId: v.id }, header: actionHeaders(key, v.version) },
        body: v.reason ? { reason: v.reason } : {},
      }),
    ),
  );
}

export function useIncludeRenewalProposal(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { reason: string }, key) =>
    unwrap(
      api.POST(`${M}/renewal-proposals/{proposalId}/include`, {
        params: { path: { marinaId, proposalId: v.id }, header: actionHeaders(key, v.version) },
        body: { reason: v.reason },
      }),
    ),
  );
}

// ---- charter (13.6) ----

export function useCharterFleets(marinaId: string, customerId?: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.fleets(marinaId), customerId ?? ''],
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/charter-fleets`, {
            params: { path: { marinaId }, query: customerId ? { customer_id: customerId } : {} },
          }),
        )
      ).items,
  });
}

export function useCharterFleet(marinaId: string, fleetId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.fleet(marinaId, fleetId),
    enabled,
    queryFn: () =>
      unwrap(api.GET(`${M}/charter-fleets/{fleetId}`, { params: { path: { marinaId, fleetId } } })),
  });
}

export function useCreateCharterFleet(marinaId: string) {
  return useContractsMutation(marinaId, (api, body: CharterFleetInput, key) =>
    unwrap(
      api.POST(`${M}/charter-fleets`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateCharterFleet(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { patch: S['CharterFleetPatch'] }) =>
    unwrap(
      api.PATCH(`${M}/charter-fleets/{fleetId}`, {
        params: { path: { marinaId, fleetId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.patch,
      }),
    ),
  );
}

/** Charter contracts are Contract resources with `kind = charter` (listContracts filter). */
export function useCharters(marinaId: string, activeOn?: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.charters(marinaId, compactFilters({ active_on: activeOn })),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/contracts`, {
          params: {
            path: { marinaId },
            query: {
              kind: 'charter',
              limit: 200,
              ...(activeOn ? { active_on: activeOn } : {}),
            } as never,
          },
        }),
      ),
  });
}

export function useCreateCharter(marinaId: string) {
  return useContractsMutation(marinaId, (api, body: CharterCreate, key) =>
    unwrap(
      api.POST(`${M}/charters`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useRotateCharter(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { body: CharterRotate }, key) =>
    unwrap(
      api.POST(`${M}/charters/{contractId}/rotate`, {
        params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
        body: v.body,
      }),
    ),
  );
}

// ---- addenda (13.5) ----

export function useAddenda(marinaId: string, contractId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.addenda(marinaId, contractId),
    enabled,
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/contracts/{contractId}/addenda`, {
            params: { path: { marinaId, contractId } },
          }),
        )
      ).items,
  });
}

export function useCreateAddendum(marinaId: string) {
  return useContractsMutation(
    marinaId,
    (api, v: { contractId: string; body: AddendumInput }, key) =>
      unwrap(
        api.POST(`${M}/contracts/{contractId}/addenda`, {
          params: {
            path: { marinaId, contractId: v.contractId },
            header: { 'Idempotency-Key': key },
          },
          body: v.body,
        }),
      ),
  );
}

type AddendumRef = { contractId: string; addendumId: string; version: number };

export function useRequestAddendumSignature(marinaId: string) {
  return useContractsMutation(
    marinaId,
    (api, v: AddendumRef & { body: SignatureRequestInput }, key) =>
      unwrap(
        api.POST(`${M}/contracts/{contractId}/addenda/{addendumId}/request-signature`, {
          params: {
            path: { marinaId, contractId: v.contractId, addendumId: v.addendumId },
            header: actionHeaders(key, v.version),
          },
          body: v.body,
        }),
      ),
  );
}

export function useActivateAddendum(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: AddendumRef & { paperScanFileId?: string }, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/addenda/{addendumId}/activate`, {
        params: {
          path: { marinaId, contractId: v.contractId, addendumId: v.addendumId },
          header: actionHeaders(key, v.version),
        },
        body: v.paperScanFileId ? { paper_scan_file_id: v.paperScanFileId } : {},
      }),
    ),
  );
}

export function useCancelAddendum(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: AddendumRef, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/addenda/{addendumId}/cancel`, {
        params: {
          path: { marinaId, contractId: v.contractId, addendumId: v.addendumId },
          header: actionHeaders(key, v.version),
        },
      }),
    ),
  );
}

// ---- waiting list (13.9) ----

export type WaitlistFilters = { status?: S['WaitlistStatus']; term?: S['ContractTerm'] };

export function useWaitlist(marinaId: string, filters: WaitlistFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.waitlist(marinaId, compactFilters(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/waitlist`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...(filters.status ? { status: filters.status } : {}),
              ...(filters.term ? { term: filters.term } : {}),
              ...(pageParam ? { cursor: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useCreateWaitlistEntry(marinaId: string) {
  return useContractsMutation(marinaId, (api, body: WaitlistInput, key) =>
    unwrap(
      api.POST(`${M}/waitlist`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateWaitlistEntry(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: Versioned & { patch: WaitlistPatch }) =>
    unwrap(
      api.PATCH(`${M}/waitlist/{waitlistId}`, {
        params: {
          path: { marinaId, waitlistId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

export function useQuoteFromWaitlist(marinaId: string) {
  return useContractsMutation(marinaId, (api, v: { id: string; body: S['QuoteInput'] }, key) =>
    unwrap(
      api.POST(`${M}/waitlist/{waitlistId}/quote`, {
        params: { path: { marinaId, waitlistId: v.id }, header: { 'Idempotency-Key': key } },
        body: v.body,
      }),
    ),
  );
}

// ---- history tab (audit log of one entity, audit.read) ----

export function useEntityAudit(
  marinaId: string,
  entityType: string,
  entityId: string,
  enabled = true,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.audit(marinaId, { entity_type: entityType, entity_id: entityId }),
    enabled: enabled && !!entityId,
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/audit-log', {
          params: {
            path: { marinaId },
            query: { entity_type: entityType, entity_id: entityId, limit: 100 },
          },
        }),
      ),
  });
}
