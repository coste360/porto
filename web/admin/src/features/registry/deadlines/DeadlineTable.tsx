import { Link } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import { CheckIcon, Undo2Icon, XIcon } from 'lucide-react';
import { toast } from 'sonner';

import { useUpdateDeadline, type Deadline, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { DataTable } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { toastProblem } from '../../../lib/toast';
import { customerName } from '../lib/format';

const URGENCY_VARIANT: Record<Schemas['DeadlineUrgency'], 'destructive' | 'default' | 'outline'> = {
  overdue: 'destructive',
  due_soon: 'default',
  later: 'outline',
};

export function UrgencyBadge({ urgency }: { urgency: Schemas['DeadlineUrgency'] }) {
  const { t } = useT();
  return <Badge variant={URGENCY_VARIANT[urgency]}>{t(`deadlines:urgency.${urgency}`)}</Badge>;
}

export type DeadlineTableProps = {
  deadlines: Deadline[];
  canWrite: boolean;
  /** Hide the subject column (customer page: every row is the same customer). */
  hideSubject?: boolean;
};

/** Deadlines with their source and the mark done / dismiss / reopen actions (If-Match on the version). */
export function DeadlineTable({ deadlines, canWrite, hideSubject = false }: DeadlineTableProps) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateDeadline(marinaId);

  const setStatus = (d: Deadline, status: Schemas['DeadlineStatus']) =>
    update.mutate(
      { deadlineId: d.id, version: d.version, patch: { status } },
      {
        onSuccess: () => toast.success(t('deadlines:updated')),
        onError: (error) => toastProblem(t, error),
      },
    );

  const columns: ColumnDef<Deadline, unknown>[] = [
    {
      header: t('deadlines:columns.due'),
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          <span className="tabular-nums">{formatIsoDate(row.original.due_on)}</span>
          {row.original.status === 'open' ? <UrgencyBadge urgency={row.original.urgency} /> : null}
        </div>
      ),
    },
    {
      header: t('deadlines:columns.kind'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span>{t(`deadlines:kinds.${row.original.kind}`)}</span>
          {row.original.document_type ? (
            <span className="text-muted-foreground text-xs">
              {t(`documents:types.${row.original.document_type}`)}
            </span>
          ) : null}
        </div>
      ),
    },
    ...(hideSubject
      ? []
      : [
          {
            id: 'subject',
            header: t('deadlines:columns.subject'),
            cell: ({ row }) => (
              <div className="flex flex-col">
                {row.original.customer ? (
                  <Link
                    to="/m/$marinaId/customers/$customerId"
                    params={{ marinaId, customerId: row.original.customer.id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {customerName(t, { ...row.original.customer, anonymized_at: null })}
                  </Link>
                ) : null}
                {row.original.vessel ? (
                  <Link
                    to="/m/$marinaId/vessels/$vesselId"
                    params={{ marinaId, vesselId: row.original.vessel.id }}
                    className="text-muted-foreground text-xs underline-offset-4 hover:underline"
                  >
                    {row.original.vessel.name}
                  </Link>
                ) : null}
              </div>
            ),
          } satisfies ColumnDef<Deadline, unknown>,
        ]),
    {
      header: t('deadlines:columns.status'),
      cell: ({ row }) => (
        <Badge variant={row.original.status === 'open' ? 'secondary' : 'outline'}>
          {t(`deadlines:statuses.${row.original.status}`)}
        </Badge>
      ),
    },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) =>
        !canWrite ? null : row.original.status === 'open' ? (
          <div className="flex justify-end gap-1">
            <Button
              variant="ghost"
              size="sm"
              disabled={update.isPending}
              onClick={() => setStatus(row.original, 'done')}
            >
              <CheckIcon />
              {t('deadlines:markDone')}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={update.isPending}
              onClick={() => setStatus(row.original, 'dismissed')}
            >
              <XIcon />
              {t('deadlines:dismiss')}
            </Button>
          </div>
        ) : (
          <div className="flex justify-end">
            <Button
              variant="ghost"
              size="sm"
              disabled={update.isPending}
              onClick={() => setStatus(row.original, 'open')}
            >
              <Undo2Icon />
              {t('deadlines:reopen')}
            </Button>
          </div>
        ),
    },
  ];

  return (
    <DataTable
      columns={columns}
      data={deadlines}
      getRowId={(d) => d.id}
      empty={t('deadlines:empty')}
    />
  );
}
