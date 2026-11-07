// Query and mutation hooks of the M4 rights area (m4-plan §8.2, §7.3.2): berth rights of use with holders and
// vessels, the map rights lens, transfers, management mandates, management fees, millesimal tables, common-charge
// budgets and runs, holder instalments and the concession. Keys live under ['marina', id, 'rights']
// (rights.changed); mutations also invalidate the ops root (owner allocations recolour the map) and the docs root.
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
import { actionHeaders, compactFilters, fetchExport } from './contractsM4';

type S = components['schemas'];

const M = '/api/admin/v1/marinas/{marinaId}' as const;
const PAGE_SIZE = 50;

export type Right = S['Right'];
export type RightInput = S['RightInput'];
export type RightKind = S['RightKind'];
export type RightStatus = S['RightStatus'];
export type HolderShare = S['HolderShare'];
export type RightVesselInput = S['RightVesselInput'];
export type MapRights = S['MapRights'];
export type MapRightItem = S['MapRights']['items'][number];
export type RightTransfer = S['RightTransfer'];
export type RightTransferInput = S['RightTransferInput'];
export type Mandate = S['Mandate'];
export type MandateInput = S['MandateInput'];
export type MandateStatement = S['MandateStatement'];
export type FeeTable = S['FeeTable'];
export type FeeTableInput = S['FeeTableInput'];
export type FeeTableRow = S['FeeTableRow'];
export type FeeRun = S['FeeRun'];
export type FeeRunSimulation = S['FeeRunSimulation'];
export type MillesimalTable = S['MillesimalTable'];
export type MillesimalEntry = S['MillesimalEntry'];
export type MillesimalEntries = S['MillesimalEntries'];
export type Budget = S['Budget'];
export type BudgetItem = S['BudgetItem'];
export type CommonChargeRun = S['CommonChargeRun'];
export type CommonAllocation = S['CommonAllocation'];
export type CommonRunInput = S['CommonRunInput'];
export type HolderInstallment = S['HolderInstallment'];
export type Concession = S['Concession'];
export type ConcessionInput = S['ConcessionInput'];
export type ConcessionObligation = S['ConcessionObligation'];
export type ConcessionObligationInput = S['ConcessionObligationInput'];

type Versioned = { id: string; version: number };

function invalidateRights(qc: QueryClient, marinaId: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.rights(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.ops(marinaId) }),
    qc.invalidateQueries({ queryKey: queryKeys.docsArea(marinaId) }),
  ]);
}

function useRightsMutation<V, R>(
  marinaId: string,
  fn: (api: ApiClient, vars: V, idempotencyKey: string) => Promise<R>,
) {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: V) => fn(api, vars, newIdempotencyKey()),
    onSuccess: () => invalidateRights(qc, marinaId),
  });
}

// ---- rights (14.1) ----

export type RightFilters = {
  berth_id?: string;
  customer_id?: string;
  status?: RightStatus;
  active_on?: string;
};

export function useRights(marinaId: string, filters: RightFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.rightsList(marinaId, compactFilters(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/rights`, {
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

export function useRight(marinaId: string, rightId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.right(marinaId, rightId),
    enabled,
    queryFn: () =>
      unwrap(api.GET(`${M}/rights/{rightId}`, { params: { path: { marinaId, rightId } } })),
  });
}

export function useCreateRight(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: RightInput, key) =>
    unwrap(
      api.POST(`${M}/rights`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateRight(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { patch: S['RightPatch'] }) =>
    unwrap(
      api.PATCH(`${M}/rights/{rightId}`, {
        params: { path: { marinaId, rightId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.patch,
      }),
    ),
  );
}

export function useEndRight(marinaId: string) {
  return useRightsMutation(
    marinaId,
    (api, v: Versioned & { body: S['RightEndInput']; cancel?: boolean }, key) =>
      v.cancel
        ? unwrap(
            api.POST(`${M}/rights/{rightId}/cancel`, {
              params: { path: { marinaId, rightId: v.id }, header: actionHeaders(key, v.version) },
              body: v.body,
            }),
          )
        : unwrap(
            api.POST(`${M}/rights/{rightId}/end`, {
              params: { path: { marinaId, rightId: v.id }, header: actionHeaders(key, v.version) },
              body: v.body,
            }),
          ),
  );
}

export function useSetRightVessels(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { body: S['RightVesselsInput'] }) =>
    unwrap(
      api.PUT(`${M}/rights/{rightId}/vessels`, {
        params: { path: { marinaId, rightId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.body,
      }),
    ),
  );
}

export function useRightHistory(marinaId: string, berthId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.rightHistory(marinaId, berthId),
    enabled,
    queryFn: async () =>
      (await unwrap(
        api.GET(`${M}/berths/{berthId}/right-history`, {
          params: { path: { marinaId, berthId } },
        }),
      )) as S['RightHistory'],
  });
}

/** Holder chain PDF of a berth (spec 14.1). */
export function useRightHistoryPdf(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (berthId: string) =>
      fetchExport(
        api.GET(`${M}/berths/{berthId}/right-history`, {
          params: { path: { marinaId, berthId }, query: { format: 'pdf' } as never },
          parseAs: 'blob',
        }),
      ),
  });
}

/** The Diritti d'uso lens data of a map for one night (composed with the day view). */
export function useMapRights(marinaId: string, mapId: string, date: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.mapRights(marinaId, mapId, date),
    enabled: enabled && !!mapId && !!date,
    queryFn: () =>
      unwrap(
        api.GET(`${M}/maps/{mapId}/rights`, {
          params: { path: { marinaId, mapId }, query: { date } },
        }),
      ),
  });
}

// ---- transfers (14.2) ----

export function useRightTransfers(
  marinaId: string,
  filters: { right_id?: string; status?: S['TransferStatus'] } = {},
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.transfers(marinaId, compactFilters(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/right-transfers`, {
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

export function useRightTransfer(marinaId: string, transferId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.transfer(marinaId, transferId),
    queryFn: () =>
      unwrap(
        api.GET(`${M}/right-transfers/{transferId}`, {
          params: { path: { marinaId, transferId } },
        }),
      ),
  });
}

export function useCreateRightTransfer(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: RightTransferInput, key) =>
    unwrap(
      api.POST(`${M}/right-transfers`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useRequestTransferSignature(marinaId: string) {
  return useRightsMutation(
    marinaId,
    (api, v: Versioned & { body: S['SignatureRequestInput'] }, key) =>
      unwrap(
        api.POST(`${M}/right-transfers/{transferId}/request-signature`, {
          params: { path: { marinaId, transferId: v.id }, header: actionHeaders(key, v.version) },
          body: v.body,
        }),
      ),
  );
}

export function useCompleteRightTransfer(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { body: S['PaperConfirmInput'] }, key) =>
    unwrap(
      api.POST(`${M}/right-transfers/{transferId}/complete`, {
        params: { path: { marinaId, transferId: v.id }, header: actionHeaders(key, v.version) },
        body: v.body,
      }),
    ),
  );
}

export function useCancelRightTransfer(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { reason?: string }, key) =>
    unwrap(
      api.POST(`${M}/right-transfers/{transferId}/cancel`, {
        params: { path: { marinaId, transferId: v.id }, header: actionHeaders(key, v.version) },
        body: v.reason ? { reason: v.reason } : {},
      }),
    ),
  );
}

// ---- mandates (14.3) ----

export function useMandates(
  marinaId: string,
  filters: { right_id?: string; status?: S['MandateStatus'] } = {},
) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.mandates(marinaId, compactFilters(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET(`${M}/mandates`, {
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

export function useMandate(marinaId: string, mandateId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.mandate(marinaId, mandateId),
    queryFn: () =>
      unwrap(api.GET(`${M}/mandates/{mandateId}`, { params: { path: { marinaId, mandateId } } })),
  });
}

export function useMandateStatement(
  marinaId: string,
  mandateId: string,
  range: { from?: string; to?: string } = {},
) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.mandate(marinaId, mandateId), 'statement', compactFilters(range)],
    queryFn: async () =>
      (await unwrap(
        api.GET(`${M}/mandates/{mandateId}/statement`, {
          params: { path: { marinaId, mandateId }, query: compactFilters(range) },
        }),
      )) as MandateStatement,
  });
}

export function useMandateStatementExport(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: { mandateId: string; format: 'csv' | 'pdf'; from?: string; to?: string }) =>
      fetchExport(
        api.GET(`${M}/mandates/{mandateId}/statement`, {
          params: {
            path: { marinaId, mandateId: v.mandateId },
            query: { ...compactFilters({ from: v.from, to: v.to }), format: v.format } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useCreateMandate(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: MandateInput, key) =>
    unwrap(
      api.POST(`${M}/mandates`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useEndMandate(marinaId: string) {
  return useRightsMutation(
    marinaId,
    (api, v: Versioned & { on: string; reason?: string; cancel?: boolean }, key) =>
      v.cancel
        ? unwrap(
            api.POST(`${M}/mandates/{mandateId}/cancel`, {
              params: {
                path: { marinaId, mandateId: v.id },
                header: actionHeaders(key, v.version),
              },
              body: v.reason ? { reason: v.reason } : {},
            }),
          )
        : unwrap(
            api.POST(`${M}/mandates/{mandateId}/end`, {
              params: {
                path: { marinaId, mandateId: v.id },
                header: actionHeaders(key, v.version),
              },
              body: { on: v.on, ...(v.reason ? { reason: v.reason } : {}) },
            }),
          ),
  );
}

// ---- management fees (14.4) ----

export function useFeeTables(marinaId: string, year?: number) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.feeTables(marinaId, year ? String(year) : undefined),
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/fee-tables`, {
            params: { path: { marinaId }, query: year ? { year } : {} },
          }),
        )
      ).items,
  });
}

export function useCreateFeeTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: FeeTableInput, key) =>
    unwrap(
      api.POST(`${M}/fee-tables`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useUpdateFeeTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { patch: S['FeeTablePatch'] }) =>
    unwrap(
      api.PATCH(`${M}/fee-tables/{feeTableId}`, {
        params: {
          path: { marinaId, feeTableId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.patch,
      }),
    ),
  );
}

export function useDeleteFeeTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned) =>
    unwrap(
      api.DELETE(`${M}/fee-tables/{feeTableId}`, {
        params: {
          path: { marinaId, feeTableId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useActivateFeeTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/fee-tables/{feeTableId}/activate`, {
        params: { path: { marinaId, feeTableId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useFeeRuns(marinaId: string, year?: number) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.feeRuns(marinaId, year ? String(year) : undefined),
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/fee-runs`, { params: { path: { marinaId }, query: year ? { year } : {} } }),
        )
      ).items,
  });
}

export function useCreateFeeRun(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: S['FeeRunInput'], key) =>
    unwrap(
      api.POST(`${M}/fee-runs`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useSimulateFeeRun(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/fee-runs/{feeRunId}/simulate`, {
        params: { path: { marinaId, feeRunId: v.id }, header: actionHeaders(key, v.version) },
      }),
    ),
  );
}

export function useIssueFeeRun(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { discard?: boolean }, key) =>
    v.discard
      ? unwrap(
          api.POST(`${M}/fee-runs/{feeRunId}/discard`, {
            params: { path: { marinaId, feeRunId: v.id }, header: actionHeaders(key, v.version) },
          }),
        )
      : unwrap(
          api.POST(`${M}/fee-runs/{feeRunId}/issue`, {
            params: { path: { marinaId, feeRunId: v.id }, header: actionHeaders(key, v.version) },
          }),
        ),
  );
}

export function useFeeRunLines(marinaId: string, feeRunId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.feeRun(marinaId, feeRunId), 'lines'],
    enabled,
    queryFn: async () =>
      (await unwrap(
        api.GET(`${M}/fee-runs/{feeRunId}/lines`, {
          params: { path: { marinaId, feeRunId }, query: { limit: 200 } },
        }),
      )) as S['FeeLinePage'],
  });
}

// ---- millesimal tables (14.5, INV-41) ----

export function useMillesimalTables(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.millesimalTables(marinaId),
    queryFn: async () =>
      (await unwrap(api.GET(`${M}/millesimal-tables`, { params: { path: { marinaId } } }))).items,
  });
}

export function useCreateMillesimalTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: S['MillesimalTableInput'], key) =>
    unwrap(
      api.POST(`${M}/millesimal-tables`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useArchiveMillesimalTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned) =>
    unwrap(
      api.PATCH(`${M}/millesimal-tables/{millesimalTableId}`, {
        params: {
          path: { marinaId, millesimalTableId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: { status: 'archived' },
      }),
    ),
  );
}

export function useMillesimalEntries(marinaId: string, tableId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.millesimalTable(marinaId, tableId), 'entries'],
    enabled: enabled && !!tableId,
    queryFn: async () =>
      (await unwrap(
        api.GET(`${M}/millesimal-tables/{millesimalTableId}/entries`, {
          params: { path: { marinaId, millesimalTableId: tableId } },
        }),
      )) as MillesimalEntries,
  });
}

export function useReplaceMillesimalEntries(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { body: S['MillesimalEntriesInput'] }) =>
    unwrap(
      api.PUT(`${M}/millesimal-tables/{millesimalTableId}/entries`, {
        params: {
          path: { marinaId, millesimalTableId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
        body: v.body,
      }),
    ),
  );
}

export function useActivateMillesimalTable(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned, key) =>
    unwrap(
      api.POST(`${M}/millesimal-tables/{millesimalTableId}/activate`, {
        params: {
          path: { marinaId, millesimalTableId: v.id },
          header: actionHeaders(key, v.version),
        },
      }),
    ),
  );
}

// ---- budgets, items, actuals and runs (14.5) ----

export function useBudgets(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.budgets(marinaId),
    queryFn: async () =>
      (await unwrap(api.GET(`${M}/common-charge-budgets`, { params: { path: { marinaId } } })))
        .items,
  });
}

export function useCreateBudget(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: S['BudgetInput'], key) =>
    unwrap(
      api.POST(`${M}/common-charge-budgets`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useBudgetAction(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { action: 'approve' | 'close' }, key) =>
    v.action === 'approve'
      ? unwrap(
          api.POST(`${M}/common-charge-budgets/{budgetId}/approve`, {
            params: { path: { marinaId, budgetId: v.id }, header: actionHeaders(key, v.version) },
          }),
        )
      : unwrap(
          api.POST(`${M}/common-charge-budgets/{budgetId}/close`, {
            params: { path: { marinaId, budgetId: v.id }, header: actionHeaders(key, v.version) },
          }),
        ),
  );
}

export function useBudgetItems(marinaId: string, budgetId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.budget(marinaId, budgetId), 'items'],
    enabled: enabled && !!budgetId,
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/common-charge-budgets/{budgetId}/items`, {
            params: { path: { marinaId, budgetId } },
          }),
        )
      ).items,
  });
}

export function useCreateBudgetItem(marinaId: string) {
  return useRightsMutation(
    marinaId,
    (api, v: { budgetId: string; body: S['BudgetItemInput'] }, key) =>
      unwrap(
        api.POST(`${M}/common-charge-budgets/{budgetId}/items`, {
          params: {
            path: { marinaId, budgetId: v.budgetId },
            header: { 'Idempotency-Key': key },
          },
          body: v.body,
        }),
      ),
  );
}

export function useDeleteBudgetItem(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { budgetId: string }) =>
    unwrap(
      api.DELETE(`${M}/common-charge-budgets/{budgetId}/items/{budgetItemId}`, {
        params: {
          path: { marinaId, budgetId: v.budgetId, budgetItemId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

export function useSetBudgetActuals(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { body: S['ActualsInput'] }) =>
    unwrap(
      api.PUT(`${M}/common-charge-budgets/{budgetId}/actuals`, {
        params: { path: { marinaId, budgetId: v.id }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.body,
      }),
    ),
  );
}

export function useCommonChargeRuns(marinaId: string, budgetId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.commonRuns(marinaId, budgetId),
    enabled: enabled && !!budgetId,
    queryFn: async () =>
      (
        await unwrap(
          api.GET(`${M}/common-charge-budgets/{budgetId}/runs`, {
            params: { path: { marinaId, budgetId } },
          }),
        )
      ).items,
  });
}

export function useCreateCommonChargeRun(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: { budgetId: string; body: CommonRunInput }, key) =>
    unwrap(
      api.POST(`${M}/common-charge-budgets/{budgetId}/runs`, {
        params: {
          path: { marinaId, budgetId: v.budgetId },
          header: { 'Idempotency-Key': key },
        },
        body: v.body,
      }),
    ),
  );
}

export function useCommonChargeRunAction(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { action: 'issue' | 'discard' }, key) =>
    v.action === 'issue'
      ? unwrap(
          api.POST(`${M}/common-charge-runs/{commonRunId}/issue`, {
            params: {
              path: { marinaId, commonRunId: v.id },
              header: actionHeaders(key, v.version),
            },
          }),
        )
      : unwrap(
          api.POST(`${M}/common-charge-runs/{commonRunId}/discard`, {
            params: {
              path: { marinaId, commonRunId: v.id },
              header: actionHeaders(key, v.version),
            },
          }),
        ),
  );
}

export function useCommonChargeAllocations(marinaId: string, runId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.commonRun(marinaId, runId), 'allocations'],
    enabled: enabled && !!runId,
    queryFn: async () =>
      (await unwrap(
        api.GET(`${M}/common-charge-runs/{commonRunId}/allocations`, {
          params: { path: { marinaId, commonRunId: runId }, query: { limit: 200 } },
        }),
      )) as S['CommonAllocationPage'],
  });
}

/** Statements of a run: one PDF (by holder or by item) or the CSV. */
export function useCommonChargeStatements(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (v: {
      runId: string;
      format: 'pdf' | 'csv';
      by?: 'holder' | 'item';
      customerId?: string;
    }) =>
      fetchExport(
        api.GET(`${M}/common-charge-runs/{commonRunId}/statements`, {
          params: {
            path: { marinaId, commonRunId: v.runId },
            query: {
              format: v.format,
              ...(v.by ? { by: v.by } : {}),
              ...(v.customerId ? { customer_id: v.customerId } : {}),
            } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}

// ---- holder instalments (14.6) ----

export type HolderInstallmentFilters = {
  from?: string;
  to?: string;
  status?: S['InstallmentFilterStatus'];
  customer_id?: string;
  right_id?: string;
  source_type?: S['HolderInstallmentSource'];
};

export function useHolderInstallments(marinaId: string, filters: HolderInstallmentFilters = {}) {
  const api = useApi();
  return useInfiniteQuery({
    queryKey: queryKeys.holderInstallments(marinaId, compactFilters(filters)),
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      (await unwrap(
        api.GET(`${M}/holder-installments`, {
          params: {
            path: { marinaId },
            query: {
              limit: PAGE_SIZE,
              ...compactFilters(filters),
              ...(pageParam ? { cursor: pageParam } : {}),
            } as never,
          },
        }),
      )) as S['HolderInstallmentPage'],
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useHolderInstallmentsCsv(marinaId: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (filters: HolderInstallmentFilters) =>
      fetchExport(
        api.GET(`${M}/holder-installments`, {
          params: {
            path: { marinaId },
            query: { ...compactFilters(filters), format: 'csv' } as never,
          },
          parseAs: 'blob',
        }),
      ),
  });
}

export function useSettleHolderInstallments(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: S['BulkSettleInput'], key) =>
    unwrap(
      api.POST(`${M}/holder-installments/settle`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

// ---- concession (14.7) ----

export function useConcession(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: queryKeys.concession(marinaId),
    queryFn: () => unwrap(api.GET(`${M}/concession`, { params: { path: { marinaId } } })),
  });
}

export function useUpdateConcession(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: { version: number; body: ConcessionInput }) =>
    unwrap(
      api.PUT(`${M}/concession`, {
        params: { path: { marinaId }, header: { 'If-Match': ifMatch(v.version) } },
        body: v.body,
      }),
    ),
  );
}

export function useConcessionObligations(marinaId: string) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.concession(marinaId), 'obligations'],
    queryFn: async () =>
      (await unwrap(api.GET(`${M}/concession/obligations`, { params: { path: { marinaId } } })))
        .items,
  });
}

export function useCreateConcessionObligation(marinaId: string) {
  return useRightsMutation(marinaId, (api, body: ConcessionObligationInput, key) =>
    unwrap(
      api.POST(`${M}/concession/obligations`, {
        params: { path: { marinaId }, header: { 'Idempotency-Key': key } },
        body,
      }),
    ),
  );
}

export function useMarkObligationDone(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned & { doneOn?: string }, key) =>
    unwrap(
      api.POST(`${M}/concession/obligations/{obligationId}/done`, {
        params: { path: { marinaId, obligationId: v.id }, header: actionHeaders(key, v.version) },
        body: v.doneOn ? { done_on: v.doneOn } : {},
      }),
    ),
  );
}

export function useDeleteConcessionObligation(marinaId: string) {
  return useRightsMutation(marinaId, (api, v: Versioned) =>
    unwrap(
      api.DELETE(`${M}/concession/obligations/{obligationId}`, {
        params: {
          path: { marinaId, obligationId: v.id },
          header: { 'If-Match': ifMatch(v.version) },
        },
      }),
    ),
  );
}

// ---- berths of the marina for the millesimal grid (all pages, code order from the server) ----

export function useAllBerths(marinaId: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: [...queryKeys.berths(marinaId), 'all'],
    enabled,
    queryFn: async () => {
      const out: { id: string; code: string }[] = [];
      let cursor: string | undefined;
      for (let i = 0; i < 50; i++) {
        const page = await unwrap(
          api.GET(`${M}/berths`, {
            params: { path: { marinaId }, query: { limit: 200, ...(cursor ? { cursor } : {}) } },
          }),
        );
        for (const b of page.items) out.push({ id: b.id, code: b.code });
        if (!page.next_cursor) break;
        cursor = page.next_cursor;
      }
      return out;
    },
  });
}
