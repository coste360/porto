import { Link } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';

import type { ContractSummary } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';

import { useCurrentMarina } from '../../../auth/hooks';
import { StatusBadge } from '../components/StatusBadge';

/** Columns shared by the contracts and transits lists. */
export function useContractColumns(
  kind: 'mooring' | 'transit',
): ColumnDef<ContractSummary, unknown>[] {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  return [
    {
      header: t('desk:columns.code'),
      cell: ({ row }) =>
        kind === 'transit' ? (
          <Link
            to="/m/$marinaId/transits/$contractId"
            params={{ marinaId, contractId: row.original.id }}
            className="font-medium underline-offset-4 hover:underline"
          >
            {row.original.code}
          </Link>
        ) : (
          <Link
            to="/m/$marinaId/contracts/$contractId"
            params={{ marinaId, contractId: row.original.id }}
            className="font-medium underline-offset-4 hover:underline"
          >
            {row.original.code}
          </Link>
        ),
    },
    { header: t('desk:columns.vessel'), cell: ({ row }) => row.original.vessel.name },
    {
      header: t('desk:columns.customer'),
      cell: ({ row }) => row.original.customer?.display_name ?? '',
    },
    ...(kind === 'mooring'
      ? [
          {
            header: t('desk:columns.term'),
            cell: ({ row }: { row: { original: ContractSummary } }) =>
              row.original.term ? t(`occupancyEnums:contractTerm.${row.original.term}`) : '',
          },
        ]
      : []),
    {
      header: t('desk:columns.period'),
      cell: ({ row }) =>
        `${formatIsoDate(row.original.starts_on)} – ${formatIsoDate(row.original.ends_on)}`,
    },
    {
      header: t('desk:columns.resources'),
      cell: ({ row }) => row.original.resources.map((r) => r.code).join(', '),
    },
    {
      header: t('desk:columns.total'),
      cell: ({ row }) => (
        <span className="tabular-nums">
          {formatCents(row.original.total_cents, 'EUR', i18n.language)}
        </span>
      ),
    },
    {
      header: t('desk:columns.status'),
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          <StatusBadge kind={row.original.kind} status={row.original.status} />
          {row.original.hold_lost ? (
            <Badge variant="destructive">{t('desk:contract.holdLost')}</Badge>
          ) : null}
          {row.original.presence_state ? (
            <Badge variant="outline">
              {t(`occupancyEnums:presenceState.${row.original.presence_state}`)}
            </Badge>
          ) : null}
        </div>
      ),
    },
  ];
}
