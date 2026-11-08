// Query and mutation hooks of the M5 admin pages (m5-plan §9.2): charges, exports, payments, credits, receivables,
// cash, fiscal, payment requests, reminders, providers, settings, customer dashboard and statement, payment lens
// and worklist. Keys live under queryKeys.billing(marinaId), invalidated by billing.changed; mutations also
// invalidate the instalment lists, the desk (transit confirmation with payment) and the rights area (mandate
// settlement, holder instalments).
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import { ifMatch, newIdempotencyKey, unwrap, type ApiClient } from '../client';
import { useApi } from '../context';
import { assertOnline, enqueueAndWait, type OfflineReasonCode } from '../../offline/index';
import { amountEqualsMatches } from '../../offline/queue';
import { queryKeys } from '../queryKeys';
import type { components } from '../schema';
import { actionHeaders, compactFilters, fetchExport } from './contractsM4';

type S = components['schemas'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const PAGE_SIZE = 50;

export type Charge = S['Charge'];
export type ChargeLine = S['ChargeLine'];
export type ChargeLineInput = S['ChargeLineInput'];
export type ChargePage = S['ChargePage'];
export type ChargeCreate = S['ChargeCreate'];
export type ChargePatch = S['ChargePatch'];
export type ChargeAdjust = S['ChargeAdjust'];
export type AdjustLineInput = S['AdjustLineInput'];
export type ChargeOriginType = S['ChargeOriginType'];
export type ChargePaymentStatus = S['ChargePaymentStatus'];
export type ChargeInvoicingStatus = S['ChargeInvoicingStatus'];
export type ChargeExport = S['ChargeExport'];
export type ChargeExportDetail = S['ChargeExportDetail'];
export type ExportFilter = S['ExportFilter'];
export type ExportCreate = S['ExportCreate'];
export type ExportPreview = S['ExportPreview'];
export type InvoiceReconciliation = S['InvoiceReconciliation'];
export type InvoiceReconciliationInput = S['InvoiceReconciliationInput'];
export type BillingSettings = S['BillingSettings'];
export type BillingSettingsInput = S['BillingSettingsInput'];
export type ChargeImport = S['ChargeImport'];
export type OpeningBalanceImportInput = S['OpeningBalanceImportInput'];
export type Payment = S['Payment'];
export type PaymentPage = S['PaymentPage'];
export type PaymentKind = S['PaymentKind'];
export type PaymentMethod = S['PaymentMethod'];
export type RecordPayment = S['RecordPayment'];
export type MatchInput = S['MatchInput'];
export type Refund = S['Refund'];
export type RefundInput = S['RefundInput'];
export type CustomerCredit = S['CustomerCredit'];
export type CreditGrantInput = S['CreditGrantInput'];
export type Statement = S['Statement'];
export type CustomerDashboard = S['CustomerDashboard'];
export type Receivable = S['Receivable'];
export type ReceivablePage = S['ReceivablePage'];
export type AgeingBucket = S['AgeingBucket'];
export type AgeingAmounts = S['AgeingAmounts'];
export type AgeingReport = S['AgeingReport'];
export type LensState = S['LensState'];
export type MapPaymentStatus = S['MapPaymentStatus'];
export type MapPaymentResource = S['MapPaymentResource'];
export type WorkItem = S['WorkItem'];
export type WorkItemKind = S['WorkItemKind'];
export type WorkItemStatus = S['WorkItemStatus'];
export type WorkItemPatch = S['WorkItemPatch'];
export type CashRegister = S['CashRegister'];
export type CashRegisterInput = S['CashRegisterInput'];
export type CashRegisterPatch = S['CashRegisterPatch'];
export type CashSession = S['CashSession'];
export type CashSummary = S['CashSummary'];
export type CashMovementInput = S['CashMovementInput'];
export type CloseCashSessionInput = S['CloseCashSessionInput'];
export type CounterSaleInput = S['CounterSaleInput'];
export type CounterSaleResult = S['CounterSaleResult'];
export type FiscalDocument = S['FiscalDocument'];
export type FiscalDailyTotals = S['FiscalDailyTotals'];
export type PaymentRequest = S['PaymentRequest'];
export type PaymentRequestCreate = S['PaymentRequestCreate'];
export type PaymentRequestCreated = S['PaymentRequestCreated'];
export type ReminderRun = S['ReminderRun'];
export type ReminderItem = S['ReminderItem'];
export type ReminderLevel = S['ReminderLevel'];
export type DunningExclusion = S['DunningExclusion'];
export type DunningExclusionInput = S['DunningExclusionInput'];
export type PaymentProviderInfo = S['PaymentProviderInfo'];
export type PaymentProviderSettings = S['PaymentProviderSettings'];
export type PaymentProviderSettingsInput = S['PaymentProviderSettingsInput'];
export type SepaMandate = S['SepaMandate'];
export type VatBucket = S['VatBucket'];
export type SettleMandateInput = S['SettleMandateInput'];
export type TransitPaymentLinkInput = S['TransitPaymentLinkInput'];

type Filters = Record<string, string | boolean | undefined | null>;
type Versioned = { id: string; version: number };

/** Every M5 mutation can move instalments, transits, holder instalments and the map lens. */
function invalidateBilling(qc: QueryClient, marinaId: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.billing(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.installments(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.contractInstallments(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.holderInstallments(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.desk(marinaId) }),
  ]);
}

/** Mutation with billing invalidation; one Idempotency-Key per mutation call (retries reuse it). */
function useBillingMutation<V, R>(
  marinaId: string,
  fn: (api: ApiClient, vars: V, idempotencyKey: string) => Promise<R>,
  offlineGuard?: [operationId: string, reason: OfflineReasonCode],
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    ...(offlineGuard ? { onMutate: () => assertOnline(...offlineGuard) } : {}),
    mutationFn: (vars: V) => fn(api, vars, newIdempotencyKey()),
    onSuccess: () => invalidateBilling(qc, marinaId),
  });
}

/** Filters as a query-key record (strings only). */
function keyOf(filters: Filters): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = {};
  for (const [k, v] of Object.entries(compactFilters(filters))) out[k] = String(v);
  return out;
}

function nextCursor(last: { next_cursor?: string | null }) {
  return last.next_cursor ?? undefined;
}

export type ExportFormat = 'csv' | 'xlsx';

// ---- charges (m5-plan §3, §7.3.1) ----

export type ChargeFilters = {
  payment_status?: ChargePaymentStatus;
  invoicing_status?: ChargeInvoicingStatus;
  origin_type?: ChargeOriginType;
  customer_id?: string;
  contract_id?: string;
  issued_from?: string;
  issued_to?: string;
  due_from?: string;
  due_to?: string;
  overdue?: boolean;
  revenue_class?: string;
  q?: string;
};

export function useCharges(marinaId: string, filters: ChargeFilters = {}, enabled = true) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.charges(marinaId, keyOf(filters)),
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/charges`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compactFilters(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      ) as Promise<ChargePage>,
    getNextPageParam: nextCursor,
  });
}

/** CSV or Excel of the charge list with the same filters. */
export function useChargesFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { filters: ChargeFilters; format: ExportFormat }) =>
      fetchExport(
        api.GET(`${M}/charges`, {
          params: {
            path: { marinaId },
            query: { ...compactFilters(v.filters), format: v.format } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useCharge(marinaId: string, chargeId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.charge(marinaId, chargeId),
    enabled: enabled && !!chargeId,
    queryFn: () =>
      unwrap(api.GET(`${M}/charges/{chargeId}`, { params: { path: { marinaId, chargeId } } })),
  });
}

export function useCreateCharge(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: ChargeCreate, key) =>
    unwrap(
      api.POST(`${M}/charges`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateCharge(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned & { patch: ChargePatch }) =>
    unwrap(
      api.PATCH(`${M}/charges/{chargeId}`, {
        params: { path: { marinaId, chargeId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.patch,
      }),
    ),
  );
}

export function useCancelCharge(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned & { reason: string }) =>
    unwrap(
      api.POST(`${M}/charges/{chargeId}/cancel`, {
        params: { path: { marinaId, chargeId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: { reason: v.reason },
      }),
    ),
  );
}

export function useAdjustCharge(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned & { body: ChargeAdjust }, key) =>
    unwrap(
      api.POST(`${M}/charges/{chargeId}/adjust`, {
        params: { path: { marinaId, chargeId: v.id }, header: actionHeaders(key, v.version) },
        body: v.body,
      }),
    ),
  );
}

export function useSetChargeInvoice(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { id: string; number: string; date: string }) =>
    unwrap(
      api.PUT(`${M}/charges/{chargeId}/invoice`, {
        params: { path: { marinaId, chargeId: v.id } },
        body: { number: v.number, date: v.date },
      }),
    ),
  );
}

export function useCreateTerminationCharge(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/contracts/{contractId}/termination-charge`, {
        params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

// ---- exports and reconciliation (§3.6) ----

export function useChargeExports(marinaId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.chargeExports(marinaId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/charge-exports`, {
          params: {
            path: { marinaId },
            query: { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function useChargeExport(marinaId: string, exportId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.chargeExport(marinaId, exportId),
    enabled: !!exportId,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/charge-exports/{exportId}`, {
          params: {
            path: { marinaId, exportId },
            query: { limit: 200, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function usePreviewChargeExport(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: ExportFilter) =>
      unwrap(api.POST(`${M}/charge-exports/preview`, { params: { path: { marinaId } }, body })),
  });
}

export function useCreateChargeExport(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: ExportCreate, key) =>
    unwrap(
      api.POST(`${M}/charge-exports`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useReexportCharges(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: S['ReexportInput'], key) =>
    unwrap(
      api.POST(`${M}/charge-exports/reexport`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

/** A file of an export (CSV, Excel or the summary PDF). */
export function useChargeExportFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { exportId: string; format: 'csv' | 'xlsx' | 'pdf' }) =>
      fetchExport(
        api.GET(`${M}/charge-exports/{exportId}/file`, {
          params: { path: { marinaId, exportId: v.exportId }, query: { format: v.format } },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useInvoiceReconciliations(marinaId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.reconciliations(marinaId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/invoice-reconciliations`, {
          params: {
            path: { marinaId },
            query: { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function useReconcileInvoices(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: InvoiceReconciliationInput, key) =>
    unwrap(
      api.POST(`${M}/invoice-reconciliations`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

// ---- billing settings and opening balances (§3.8, §16) ----

export function useBillingSettings(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.billingSettings(marinaId),
    enabled,
    queryFn: () => unwrap(api.GET(`${M}/billing-settings`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateBillingSettings(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { version: number; body: BillingSettingsInput }) =>
    unwrap(
      api.PUT(`${M}/billing-settings`, {
        params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.body,
      }),
    ),
  );
}

export function useImportOpeningBalances(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: OpeningBalanceImportInput, key) =>
    unwrap(
      api.POST(`${M}/opening-balances/import`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useOpeningBalanceImports(marinaId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.chargeImports(marinaId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/opening-balances/imports`, {
          params: {
            path: { marinaId },
            query: { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function useUndoOpeningBalanceImport(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned) =>
    unwrap(
      api.POST(`${M}/opening-balances/imports/{importId}/undo`, {
        params: { path: { marinaId, importId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
      }),
    ),
  );
}

// ---- payments, matches, refunds (§4) ----

export type PaymentFilters = {
  method?: PaymentMethod;
  kind?: PaymentKind;
  source?: S['PaymentSource'];
  customer_id?: string;
  from?: string;
  to?: string;
  unallocated?: boolean;
  session_id?: string;
};

export function usePayments(marinaId: string, filters: PaymentFilters = {}, enabled = true) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.payments(marinaId, keyOf(filters)),
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/payments`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compactFilters(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      ) as Promise<PaymentPage>,
    getNextPageParam: nextCursor,
  });
}

export function usePaymentsFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { from?: string; to?: string; method?: PaymentMethod; format: ExportFormat }) =>
      fetchExport(
        api.GET(`${M}/payments/export`, {
          params: { path: { marinaId }, query: compactFilters(v) as never },
          parseAs: 'blob',
        }),
      ),
  });
}

export function usePayment(marinaId: string, paymentId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.payment(marinaId, paymentId),
    enabled: enabled && !!paymentId,
    queryFn: () =>
      unwrap(api.GET(`${M}/payments/{paymentId}`, { params: { path: { marinaId, paymentId } } })),
  });
}

/**
 * A payment the offline queue may carry (29.2, D6): cash, kind payment, matched in full to existing charges (no
 * unmatched credit, INV-87) and none of them in receipt mode (`receiptMode` must be known to be false).
 */
function queueableCash(body: RecordPayment, receiptMode: boolean | undefined): boolean {
  return (
    body.method === 'cash' &&
    body.kind === 'payment' &&
    !body.auto_match &&
    receiptMode === false &&
    Array.isArray(body.matches) &&
    body.matches.length > 0 &&
    amountEqualsMatches(body.amount_cents, body.matches)
  );
}

export type RecordPaymentVars = RecordPayment & {
  amountLabel?: string;
  /**
   * Whether any matched charge has `fiscal_mode: receipt` (the payment numbers a commercial document). Unknown
   * (undefined) is treated as true: the payment goes online only.
   */
  receiptMode?: boolean;
};

/**
 * Records a payment. The cash branch is queue first (m6b-plan §4.3): a cash payment matched in full to charges not in
 * receipt mode goes through the offline queue (online it resolves with the server's answer; offline it rejects with
 * OfflineQueuedError and is sent later). Everything else, receipt-mode cash included, is a direct online POST and is
 * refused offline (OfflineForbiddenError: `document` for cash, `online_only` otherwise).
 */
export function useRecordPayment(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: RecordPaymentVars): Promise<Payment> => {
      const { amountLabel, receiptMode, ...body } = vars;
      const userId = qc.getQueryData<S['Me'] | null>(queryKeys.me)?.user.id;
      const firstCharge = body.matches?.[0]?.charge_id;
      if (userId && firstCharge && queueableCash(body, receiptMode)) {
        return enqueueAndWait<Payment>({
          kind: 'payment.cash',
          userId,
          marinaId,
          lane: `charge:${firstCharge}`,
          request: {
            operationId: 'recordPayment',
            path: `/api/admin/v1/marinas/${encodeURIComponent(marinaId)}/payments`,
            body,
          },
          label: { key: 'offline:queue.label.cash', params: { amount: amountLabel ?? '' } },
          issuesDocument: false,
        });
      }
      assertOnline('recordPayment', body.method === 'cash' ? 'document' : 'online_only');
      return unwrap(
        api.POST(`${M}/payments`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body,
        }),
      );
    },
    onSuccess: () => invalidateBilling(qc, marinaId),
  });
}

export function useMatchPayment(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { id: string; matches: MatchInput[] }, key) =>
    unwrap(
      api.POST(`${M}/payments/{paymentId}/matches`, {
        params: { path: { marinaId, paymentId: v.id }, header: { 'Idempotency-Key': key } },
        body: { matches: v.matches },
      }),
    ),
  );
}

export function useReverseMatch(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { paymentId: string; matchId: string }, key) =>
    unwrap(
      api.POST(`${M}/payments/{paymentId}/matches/{matchId}/reverse`, {
        params: {
          path: { marinaId, paymentId: v.paymentId, matchId: v.matchId },
          header: { 'Idempotency-Key': key },
        },
      }),
    ),
  );
}

export function useRefundPayment(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { id: string; body: RefundInput }, key) =>
      unwrap(
        api.POST(`${M}/payments/{paymentId}/refunds`, {
          params: { path: { marinaId, paymentId: v.id }, header: { 'Idempotency-Key': key } },
          body: v.body,
        }),
      ),
    ['refundPayment', 'online_only'],
  );
}

export function useRetainDeposit(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { id: string; body: S['RetainInput'] }, key) =>
    unwrap(
      api.POST(`${M}/payments/{paymentId}/retain`, {
        params: { path: { marinaId, paymentId: v.id }, header: { 'Idempotency-Key': key } },
        body: v.body,
      }),
    ),
  );
}

/** Receipt of a payment: HTML for the 80/58 mm print view, PDF for A4. */
export function usePaymentReceipt(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { paymentId: string; format: '80' | '58' | 'a4' }) =>
      fetchExport(
        api.GET(`${M}/payments/{paymentId}/receipt`, {
          params: { path: { marinaId, paymentId: v.paymentId }, query: { format: v.format } },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useRefunds(marinaId: string, filters: Filters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.refunds(marinaId, keyOf(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/refunds`, {
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
    getNextPageParam: nextCursor,
  });
}

// ---- credits (§4.4) ----

export function useCredits(marinaId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.credits(marinaId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/credits`, {
          params: {
            path: { marinaId },
            query: { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function useCustomerCredit(marinaId: string, customerId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.customerCredits(marinaId, customerId),
    enabled: enabled && !!customerId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/customers/{customerId}/credits`, {
          params: { path: { marinaId, customerId } },
        }),
      ),
  });
}

export function useGrantCredit(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { customerId: string; body: CreditGrantInput }, key) =>
      unwrap(
        api.POST(`${M}/customers/{customerId}/credits`, {
          params: {
            path: { marinaId, customerId: v.customerId },
            header: { 'Idempotency-Key': key },
          },
          body: v.body,
        }),
      ),
  );
}

export function useApplyCredit(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { customerId: string; chargeIds?: string[] }, key) =>
    unwrap(
      api.POST(`${M}/customers/{customerId}/credits/apply`, {
        params: {
          path: { marinaId, customerId: v.customerId },
          header: { 'Idempotency-Key': key },
        },
        body: v.chargeIds?.length ? { charge_ids: v.chargeIds } : {},
      }),
    ),
  );
}

// ---- statement, dashboard, receivables, ageing (§4.5) ----

export type DateRange = { from?: string; to?: string };

export function useCustomerStatement(
  marinaId: string,
  customerId: string,
  range: DateRange,
  enabled = true,
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.customerStatement(marinaId, customerId, keyOf(range)),
    enabled: enabled && !!customerId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/customers/{customerId}/statement`, {
          params: { path: { marinaId, customerId }, query: compactFilters(range) as never },
        }),
      ) as Promise<Statement>,
  });
}

export function useCustomerStatementFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { customerId: string; range: DateRange; format: 'pdf' | 'csv' }) =>
      fetchExport(
        api.GET(`${M}/customers/{customerId}/statement`, {
          params: {
            path: { marinaId, customerId: v.customerId },
            query: { ...compactFilters(v.range), format: v.format } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useCustomerDashboard(marinaId: string, customerId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.customerDashboard(marinaId, customerId),
    enabled: enabled && !!customerId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/customers/{customerId}/dashboard`, {
          params: { path: { marinaId, customerId } },
        }),
      ),
  });
}

export type ReceivableFilters = {
  due_from?: string;
  due_to?: string;
  bucket?: AgeingBucket;
  status?: 'open' | 'overdue' | 'scheduled';
  customer_id?: string;
  contract_id?: string;
  group_id?: string;
  origin_type?: ChargeOriginType;
};

export function useReceivables(marinaId: string, filters: ReceivableFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.receivables(marinaId, keyOf(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/receivables`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compactFilters(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      ) as Promise<ReceivablePage>,
    getNextPageParam: nextCursor,
  });
}

export function useReceivablesFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { filters: ReceivableFilters; format: ExportFormat; ageing?: boolean }) =>
      fetchExport(
        v.ageing
          ? api.GET(`${M}/receivables/ageing`, {
              params: { path: { marinaId }, query: { format: v.format } },
              parseAs: 'blob',
            })
          : api.GET(`${M}/receivables`, {
              params: {
                path: { marinaId },
                query: { ...compactFilters(v.filters), format: v.format } as never,
              },
              parseAs: 'blob',
            }),
      ),
  });
}

export function useReceivablesAgeing(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.ageing(marinaId),
    enabled,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/receivables/ageing`, { params: { path: { marinaId } } }),
      ) as Promise<AgeingReport>,
  });
}

/** Lens Pagamenti: the payment state of every allocated resource of a map on a night (map.view). */
export function useMapPaymentStatus(marinaId: string, mapId: string, date: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.mapPayments(marinaId, mapId, date),
    enabled: enabled && !!mapId && !!date,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maps/{mapId}/payment-status`, {
          params: { path: { marinaId, mapId }, query: { date } },
        }),
      ),
  });
}

export function useSettleMandate(marinaId: string) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { mandateId: string; body: SettleMandateInput }) =>
      unwrap(
        api.POST(`${M}/mandates/{mandateId}/settle`, {
          params: {
            path: { marinaId, mandateId: v.mandateId },
            header: { 'Idempotency-Key': newIdempotencyKey() },
          },
          body: v.body,
        }),
      ),
    onSuccess: () =>
      Promise.all([
        invalidateBilling(qc, marinaId),
        qc.invalidateQueries({ queryKey: queryKeys.rights(marinaId) }),
      ]),
  });
}

// ---- worklist (28.3) ----

export type WorklistFilters = {
  kind?: WorkItemKind;
  status?: WorkItemStatus;
  assignee_user_id?: string;
};

export function useWorklist(marinaId: string, filters: WorklistFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.worklist(marinaId, keyOf(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/worklist`, {
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
    getNextPageParam: nextCursor,
  });
}

export function useUpdateWorkItem(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { sourceType: string; sourceId: string; patch: WorkItemPatch }) =>
      unwrap(
        api.PATCH(`${M}/worklist/{sourceType}/{sourceId}`, {
          params: { path: { marinaId, sourceType: v.sourceType, sourceId: v.sourceId } },
          body: v.patch,
        }),
      ),
  );
}

// ---- cash registers and sessions (§5.1) ----

export function useCashRegisters(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.cashRegisters(marinaId),
    enabled,
    queryFn: () => unwrap(api.GET(`${M}/cash-registers`, { params: { path: { marinaId } } })),
  });
}

export function useCreateCashRegister(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: CashRegisterInput, key) =>
    unwrap(
      api.POST(`${M}/cash-registers`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateCashRegister(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned & { patch: CashRegisterPatch }) =>
    unwrap(
      api.PATCH(`${M}/cash-registers/{registerId}`, {
        params: {
          path: { marinaId, registerId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

export function useCashSessions(
  marinaId: string,
  filters: { register_id?: string; status?: 'open' | 'closed' } = {},
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: [...queryKeys.cashSessions(marinaId, filters.register_id), filters.status ?? ''],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/cash-sessions`, {
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
    getNextPageParam: nextCursor,
  });
}

/** Open session of a register (404 session_not_open when none: data stays undefined, no retry). */
export function useCurrentCashSession(marinaId: string, registerId: string | undefined) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.cashSession(marinaId, 'current'), registerId ?? ''],
    retry: false,
    queryFn: async () => {
      const res = await api.GET(`${M}/cash-sessions/current`, {
        params: {
          path: { marinaId },
          query: registerId ? { register_id: registerId } : {},
        },
      });
      if (res.response.status === 404) return null;
      return unwrap(Promise.resolve(res));
    },
  });
}

export function useCashSession(marinaId: string, sessionId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.cashSession(marinaId, sessionId),
    enabled: enabled && !!sessionId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/cash-sessions/{sessionId}`, { params: { path: { marinaId, sessionId } } }),
      ),
  });
}

export function useOpenCashSession(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, body: S['OpenCashSessionInput'], key) =>
      unwrap(
        api.POST(`${M}/cash-sessions`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
          body,
        }),
      ),
    ['openCashSession', 'online_only'],
  );
}

export function useAddCashMovement(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { sessionId: string; body: CashMovementInput }, key) =>
      unwrap(
        api.POST(`${M}/cash-sessions/{sessionId}/movements`, {
          params: {
            path: { marinaId, sessionId: v.sessionId },
            header: { 'Idempotency-Key': key },
          },
          body: v.body,
        }),
      ),
    ['addCashMovement', 'online_only'],
  );
}

export function useCloseCashSession(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: Versioned & { body: CloseCashSessionInput }) =>
      unwrap(
        api.POST(`${M}/cash-sessions/{sessionId}/close`, {
          params: {
            path: { marinaId, sessionId: v.id },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
    ['closeCashSession', 'online_only'],
  );
}

export function useZReport(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { sessionId: string; format: 'html' | 'pdf' }) =>
      fetchExport(
        api.GET(`${M}/cash-sessions/{sessionId}/z-report`, {
          params: { path: { marinaId, sessionId: v.sessionId }, query: { format: v.format } },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useCounterSale(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, body: CounterSaleInput, key) =>
      unwrap(
        api.POST(`${M}/counter-sales`, {
          params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
          body,
        }),
      ),
    ['createCounterSale', 'document'],
  );
}

// ---- fiscal (§5.2) ----

export function useFiscalDailyTotals(
  marinaId: string,
  range: { from?: string; to?: string; group?: 'day' | 'month' },
) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.fiscalTotals(marinaId, keyOf(range)),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/fiscal/daily-totals`, {
          params: { path: { marinaId }, query: compactFilters(range) as never },
        }),
      ) as Promise<FiscalDailyTotals>,
  });
}

export function useFiscalTotalsFile(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: {
      from?: string;
      to?: string;
      group?: 'day' | 'month';
      format: ExportFormat;
    }) =>
      fetchExport(
        api.GET(`${M}/fiscal/daily-totals`, {
          params: { path: { marinaId }, query: compactFilters(v) as never },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useFiscalDocuments(marinaId: string, filters: Filters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.fiscalDocs(marinaId, keyOf(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/fiscal/documents`, {
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
    getNextPageParam: nextCursor,
  });
}

export function useFiscalDocumentAction(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { id: string; action: 'retry' | 'void' }) =>
      v.action === 'retry'
        ? unwrap(
            api.POST(`${M}/fiscal/documents/{fiscalDocumentId}/retry`, {
              params: { path: { marinaId, fiscalDocumentId: v.id } },
            }),
          )
        : unwrap(
            api.POST(`${M}/fiscal/documents/{fiscalDocumentId}/void`, {
              params: { path: { marinaId, fiscalDocumentId: v.id } },
            }),
          ),
    ['fiscalDocumentAction', 'document'],
  );
}

// ---- payment requests (§5.5) ----

export function usePaymentRequests(
  marinaId: string,
  filters: { status?: S['PaymentRequestStatus']; customer_id?: string } = {},
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.paymentRequests(marinaId, keyOf(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/payment-requests`, {
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
    getNextPageParam: nextCursor,
  });
}

export function useCreatePaymentRequest(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: PaymentRequestCreate, key) =>
    unwrap(
      api.POST(`${M}/payment-requests`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useBulkPaymentRequests(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: S['PaymentRequestBulk'], key) =>
    unwrap(
      api.POST(`${M}/payment-requests/bulk`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useSendPaymentRequest(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned & { rotate?: boolean }) =>
    unwrap(
      api.POST(`${M}/payment-requests/{requestId}/send`, {
        params: { path: { marinaId, requestId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.rotate ? { rotate: true } : {},
      }),
    ),
  );
}

export function useCancelPaymentRequest(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: Versioned) =>
    unwrap(
      api.POST(`${M}/payment-requests/{requestId}/cancel`, {
        params: { path: { marinaId, requestId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
      }),
    ),
  );
}

export function useTransitPaymentLink(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: Versioned & { body: TransitPaymentLinkInput }, key) =>
      unwrap(
        api.POST(`${M}/transits/{contractId}/payment-link`, {
          params: { path: { marinaId, contractId: v.id }, header: actionHeaders(key, v.version) },
          body: v.body,
        }),
      ),
  );
}

// ---- reminders and exclusions (§5.5) ----

export function useReminderRuns(marinaId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.reminderRuns(marinaId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/reminder-runs`, {
          params: {
            path: { marinaId },
            query: { limit: PAGE_SIZE, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function useReminderRun(marinaId: string, runId: string) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.reminderRun(marinaId, runId),
    enabled: !!runId,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/reminder-runs/{runId}`, {
          params: {
            path: { marinaId, runId },
            query: { limit: 200, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: nextCursor,
  });
}

export function useCreateReminderRun(marinaId: string) {
  return useBillingMutation(marinaId, (api, _v: void, key) =>
    unwrap(
      api.POST(`${M}/reminder-runs`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
      }),
    ),
  );
}

export function useSendReminders(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: S['SendRemindersInput'], key) =>
    unwrap(
      api.POST(`${M}/reminders/send`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useReminderItemAction(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { id: string; action: 'approve' | 'dismiss' }) =>
    v.action === 'approve'
      ? unwrap(
          api.POST(`${M}/reminder-items/{reminderItemId}/approve`, {
            params: { path: { marinaId, reminderItemId: v.id } },
          }),
        )
      : unwrap(
          api.POST(`${M}/reminder-items/{reminderItemId}/dismiss`, {
            params: { path: { marinaId, reminderItemId: v.id } },
          }),
        ),
  );
}

export function useDunningExclusions(
  marinaId: string,
  filters: { customer_id?: string; charge_id?: string; active?: boolean } = {},
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.exclusions(marinaId, keyOf(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/dunning-exclusions`, {
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
    getNextPageParam: nextCursor,
  });
}

export function useCreateDunningExclusion(marinaId: string) {
  return useBillingMutation(marinaId, (api, body: DunningExclusionInput, key) =>
    unwrap(
      api.POST(`${M}/dunning-exclusions`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useDeleteDunningExclusion(marinaId: string) {
  return useBillingMutation(marinaId, (api, id: string) =>
    unwrap(
      api.DELETE(`${M}/dunning-exclusions/{exclusionId}`, {
        params: { path: { marinaId, exclusionId: id } },
      }),
    ),
  );
}

// ---- providers and SEPA (§5.4, §5.7) ----

export function usePaymentProviders(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.paymentProviders(marinaId),
    enabled,
    queryFn: () => unwrap(api.GET(`${M}/payment-providers`, { params: { path: { marinaId } } })),
  });
}

/** True when an online provider (anything but the manual driver) is enabled for the marina. */
export function useOnlinePaymentsEnabled(marinaId: string, enabled = true): boolean {
  const providers = usePaymentProviders(marinaId, enabled);
  return (providers.data?.items ?? []).some((p) => p.code !== 'manual' && p.enabled);
}

export function useProviderSettings(marinaId: string, code: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.providerSettings(marinaId, code),
    enabled: enabled && !!code,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/payment-providers/{providerCode}/settings`, {
          params: { path: { marinaId, providerCode: code } },
        }),
      ),
  });
}

export function useUpdateProviderSettings(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { code: string; version: number; body: PaymentProviderSettingsInput }) =>
      unwrap(
        api.PUT(`${M}/payment-providers/{providerCode}/settings`, {
          params: {
            path: { marinaId, providerCode: v.code },
            header: { 'If-Match': ifMatch(v.version) },
          },
          body: v.body,
        }),
      ),
  );
}

export function useTestProvider(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (code: string) =>
      unwrap(
        api.POST(`${M}/payment-providers/{providerCode}/test`, {
          params: { path: { marinaId, providerCode: code } },
        }),
      ),
  });
}

export function useSepaMandates(marinaId: string, customerId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.sepaMandates(marinaId, customerId),
    enabled: enabled && !!customerId,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/customers/{customerId}/sepa-mandates`, {
          params: { path: { marinaId, customerId } },
        }),
      ),
  });
}

export function useCreateSepaMandate(marinaId: string) {
  return useBillingMutation(
    marinaId,
    (api, v: { customerId: string; body: S['SepaMandateCreate'] }, key) =>
      unwrap(
        api.POST(`${M}/customers/{customerId}/sepa-mandates`, {
          params: {
            path: { marinaId, customerId: v.customerId },
            header: { 'Idempotency-Key': key },
          },
          body: v.body,
        }),
      ),
  );
}

export function useRevokeSepaMandate(marinaId: string) {
  return useBillingMutation(marinaId, (api, v: { customerId: string; mandateId: string }) =>
    unwrap(
      api.POST(`${M}/customers/{customerId}/sepa-mandates/{sepaMandateId}/revoke`, {
        params: { path: { marinaId, customerId: v.customerId, sepaMandateId: v.mandateId } },
      }),
    ),
  );
}
