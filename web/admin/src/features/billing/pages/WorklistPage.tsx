import { useMemo, useState } from 'react';

import {
  useUpdateWorkItem,
  useWorklist,
  type WorkItem,
  type WorkItemKind,
  type WorkItemStatus,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  IsoDate,
  LoadMore,
  Money,
  PageFrame,
  Section,
  SimpleTable,
} from '../components/kit';
import { WORK_KINDS, groupByKind } from '../lib/catalog';

const STATUSES: readonly WorkItemStatus[] = ['open', 'in_progress', 'done', 'dismissed'];

/** Office worklist (spec 28.3, m5-plan §4.5): items grouped by kind, take in charge, status. */
export function WorklistPage() {
  const { t } = useT();
  const { marinaId, can, me } = useCurrentMarina();
  const [status, setStatus] = useState<WorkItemStatus | ''>('open');
  const [kind, setKind] = useState<WorkItemKind | ''>('');
  const [mine, setMine] = useState(false);
  const userId = me?.user.id;
  const list = useWorklist(marinaId, {
    ...(status ? { status } : {}),
    ...(kind ? { kind } : {}),
    ...(mine && userId ? { assignee_user_id: userId } : {}),
  });
  const update = useUpdateWorkItem(marinaId);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const set = (i: WorkItem, patch: { status: WorkItemStatus; assignee_user_id?: string | null }) =>
    update.mutate({ sourceType: i.source_type, sourceId: i.source_id, patch });
  return (
    <RequirePermission allowed={can('contracts.read') || can('charges.manage')}>
      <PageFrame>
        <PageHeader title={t('worklist:title')} description={t('worklist:description')} />
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="work-status"
            label={t('worklist:status')}
            values={STATUSES}
            labelOf={(s) => t(`billingEnums:workItemStatus.${s}`)}
            value={status}
            onChange={setStatus}
            allLabel={t('worklist:all')}
          />
          <EnumSelect
            id="work-kind"
            label={t('worklist:kind')}
            values={WORK_KINDS}
            labelOf={(k) => t(`billingEnums:workItemKind.${k}`)}
            value={kind}
            onChange={setKind}
            allLabel={t('worklist:all')}
          />
          <Button
            variant={mine ? 'default' : 'outline'}
            aria-pressed={mine}
            onClick={() => setMine((v) => !v)}
          >
            {t('worklist:mine')}
          </Button>
        </div>
        <BillingProblemAlert error={update.error} />
        {list.isPending ? (
          <LoadingState rows={5} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : items.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('worklist:empty')}</p>
        ) : (
          groupByKind(items).map(([k, group]) => (
            <Section key={k} title={`${t(`billingEnums:workItemKind.${k}`)} (${group.length})`}>
              <SimpleTable
                head={[
                  t('worklist:customer'),
                  t('worklist:amount'),
                  t('worklist:dueOn'),
                  t('worklist:status'),
                  t('worklist:assignee'),
                  '',
                ]}
                empty=""
                rows={group.length}
              >
                {group.map((i) => (
                  <TableRow key={`${i.source_type}-${i.source_id}`} data-testid="work-item">
                    <TableCell>{i.customer?.display_name ?? ''}</TableCell>
                    <TableCell>
                      <Money cents={i.amount_cents} />
                    </TableCell>
                    <TableCell>
                      <IsoDate date={i.due_on} />
                    </TableCell>
                    <TableCell>
                      <BillingBadge group="workItemStatus" value={i.status} />
                    </TableCell>
                    <TableCell>
                      {i.assignee_user_id
                        ? i.assignee_user_id === userId
                          ? t('worklist:me')
                          : t('worklist:someone')
                        : ''}
                    </TableCell>
                    <TableCell className="flex justify-end gap-2">
                      {i.status === 'open' && userId ? (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={update.isPending}
                          onClick={() =>
                            set(i, { status: 'in_progress', assignee_user_id: userId })
                          }
                        >
                          {t('worklist:take')}
                        </Button>
                      ) : null}
                      {i.status === 'open' || i.status === 'in_progress' ? (
                        <>
                          <Button
                            size="sm"
                            disabled={update.isPending}
                            onClick={() => set(i, { status: 'done' })}
                          >
                            {t('worklist:done')}
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={update.isPending}
                            onClick={() => set(i, { status: 'dismissed' })}
                          >
                            {t('worklist:dismiss')}
                          </Button>
                        </>
                      ) : (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={update.isPending}
                          onClick={() => set(i, { status: 'open', assignee_user_id: null })}
                        >
                          {t('worklist:reopen')}
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </SimpleTable>
            </Section>
          ))
        )}
        <LoadMore query={list} />
      </PageFrame>
    </RequirePermission>
  );
}
