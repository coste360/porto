import { Link } from '@tanstack/react-router';
import { useState } from 'react';

import {
  useApproveTimeEntries,
  useEmployees,
  useRejectTimeEntry,
  useTimesheets,
  type Timesheet,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import {
  IsoDate,
  M4ProblemAlert,
  PageFrame,
  ReasonDialog,
} from '../../contracts-m4/components/kit';
import { hoursText, selectedApprovable } from '../lib/orders';

/** "Approvazione ore" (m10-plan §7, §14.2): submitted entries approved in batch (cost frozen) or rejected with a
 * reason (yard.manage). */
export function TimesheetsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const list = useTimesheets(marinaId, { status: 'submitted' }, can('yard.manage'));
  const employees = useEmployees(marinaId, {});
  const approve = useApproveTimeEntries(marinaId);
  const reject = useRejectTimeEntry(marinaId);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [rejecting, setRejecting] = useState<Timesheet | null>(null);
  const items = list.data?.items ?? [];
  const toApprove = selectedApprovable(items, selected);
  const name = (id: string) => {
    const e = employees.data?.items.find((x) => x.id === id);
    return e ? `${e.code} ${e.first_name} ${e.last_name}` : id;
  };
  const toggle = (id: string, on: boolean) =>
    setSelected((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });
  return (
    <RequirePermission allowed={can('yard.manage')}>
      <PageFrame>
        <div data-testid="timesheets-page" className="flex flex-col gap-4">
          <PageHeader
            title={t('yardOrders:timesheetsTitle')}
            description={t('yardOrders:timesheetsDescription')}
            actions={
              <Button
                disabled={toApprove.length === 0 || approve.isPending}
                onClick={() =>
                  approve.mutate(toApprove, { onSuccess: () => setSelected(new Set()) })
                }
              >
                {t('yardOrders:hours.approveSelected', { count: toApprove.length })}
              </Button>
            }
          />
          <M4ProblemAlert error={approve.error} />
          {list.isPending ? (
            <LoadingState rows={4} />
          ) : list.isError ? (
            <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
          ) : (
            <SimpleTable
              head={[
                <Checkbox
                  key="all"
                  aria-label={t('yardOrders:hours.selectAll')}
                  checked={items.length > 0 && toApprove.length === items.length}
                  onCheckedChange={(v) =>
                    setSelected(v === true ? new Set(items.map((e) => e.id)) : new Set())
                  }
                />,
                t('yardOrders:fields.date'),
                t('yardOrders:fields.employee'),
                t('yardOrders:fields.number'),
                t('yardOrders:fields.hours'),
                t('yardOrders:fields.notes'),
                '',
              ]}
              empty={t('yardOrders:empty.submitted')}
              rows={items.length}
            >
              {items.map((e) => (
                <TableRow key={e.id} data-testid={`entry-${e.id}`}>
                  <TableCell>
                    <Checkbox
                      aria-label={t('yardOrders:hours.select')}
                      checked={selected.has(e.id)}
                      onCheckedChange={(v) => toggle(e.id, v === true)}
                    />
                  </TableCell>
                  <TableCell>
                    <IsoDate date={e.work_date} />
                  </TableCell>
                  <TableCell>{name(e.employee_id)}</TableCell>
                  <TableCell>
                    <Link
                      to="/m/$marinaId/yard/orders/$orderId"
                      params={{ marinaId, orderId: e.work_order_id }}
                      className="underline-offset-4 hover:underline"
                    >
                      {e.work_order_number ?? ''}
                    </Link>
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {hoursText(e.hours_milli, i18n.language)}
                  </TableCell>
                  <TableCell>{e.note}</TableCell>
                  <TableCell>
                    <Button size="sm" variant="ghost" onClick={() => setRejecting(e)}>
                      {t('yardOrders:hours.reject')}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          )}
          <ReasonDialog
            open={rejecting !== null}
            title={t('yardOrders:hours.rejectTitle')}
            confirmLabel={t('yardOrders:hours.reject')}
            required
            destructive
            pending={reject.isPending}
            error={reject.error}
            onClose={() => setRejecting(null)}
            onConfirm={(reason) =>
              rejecting &&
              reject.mutate(
                { id: rejecting.id, version: rejecting.version, reason },
                { onSuccess: () => setRejecting(null) },
              )
            }
          />
        </div>
      </PageFrame>
    </RequirePermission>
  );
}
