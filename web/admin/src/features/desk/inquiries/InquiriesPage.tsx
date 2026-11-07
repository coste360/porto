import type { ColumnDef } from '@tanstack/react-table';
import { useState } from 'react';

import { useInquiries, useUpdateInquiry, type Inquiry, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm, formatIsoDate } from '@porto/shared/lib/format';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DataTable, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Button } from '@porto/shared/ui/button';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { INQUIRY_OUTCOMES } from '../lib/enums';

const ALL = '__all__';
export function InquiriesPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [outcome, setOutcome] = useState<Schemas['InquiryOutcome'] | ''>('');
  const inquiries = useInquiries(marinaId, outcome ? { outcome } : {});
  const update = useUpdateInquiry(marinaId);
  const editable = can('transits.desk');

  const columns: ColumnDef<Inquiry, unknown>[] = [
    {
      header: t('desk:columns.created'),
      cell: ({ row }) => formatIsoDate(row.original.created_at.slice(0, 10)),
    },
    {
      header: t('desk:columns.purpose'),
      cell: ({ row }) =>
        row.original.term
          ? t(`occupancyEnums:contractTerm.${row.original.term}`)
          : t(`desk:search.purpose${row.original.purpose === 'transit' ? 'Transit' : 'Contract'}`),
    },
    {
      header: t('desk:columns.vessel'),
      cell: ({ row }) =>
        row.original.vessel?.name ??
        `${formatCm(row.original.loa_cm, i18n.language)} × ${formatCm(row.original.beam_cm, i18n.language)}`,
    },
    {
      header: t('desk:columns.period'),
      cell: ({ row }) =>
        `${formatIsoDate(row.original.arrival)} – ${formatIsoDate(row.original.departure)}`,
    },
    {
      header: t('desk:columns.source'),
      cell: ({ row }) => t(`occupancyEnums:inquirySource.${row.original.source}`),
    },
    {
      header: t('desk:columns.outcome'),
      cell: ({ row }) =>
        editable ? (
          <Select
            value={row.original.outcome}
            onValueChange={(v) =>
              update.mutate({
                inquiryId: row.original.id,
                version: row.original.version,
                patch: { outcome: v as Schemas['InquiryOutcome'] },
              })
            }
          >
            <SelectTrigger className="w-full" aria-label={t('desk:columns.outcome')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {INQUIRY_OUTCOMES.map((o) => (
                <SelectItem key={o} value={o}>
                  {t(`occupancyEnums:inquiryOutcome.${o}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          t(`occupancyEnums:inquiryOutcome.${row.original.outcome}`)
        ),
    },
  ];

  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageHeader title={t('desk:inquiries.title')} description={t('desk:inquiries.description')} />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <Field className="sm:max-w-64">
          <FieldLabel htmlFor="inquiries-outcome">{t('desk:columns.outcome')}</FieldLabel>
          <Select
            value={outcome || ALL}
            onValueChange={(v) => setOutcome(v === ALL ? '' : (v as typeof outcome))}
          >
            <SelectTrigger id="inquiries-outcome" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('desk:filters.all')}</SelectItem>
              {INQUIRY_OUTCOMES.map((o) => (
                <SelectItem key={o} value={o}>
                  {t(`occupancyEnums:inquiryOutcome.${o}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      <DeskProblemAlert error={update.error} />
      {inquiries.isPending ? (
        <LoadingState rows={6} />
      ) : inquiries.isError ? (
        <QueryErrorState error={inquiries.error} onRetry={() => void inquiries.refetch()} />
      ) : (
        <div className="flex flex-col gap-3">
          <DataTable
            columns={columns}
            data={inquiries.data.pages.flatMap((p) => p.items)}
            getRowId={(r) => r.id}
            empty={t('desk:inquiries.empty')}
          />
          {inquiries.hasNextPage ? (
            <Button
              variant="outline"
              onClick={() => void inquiries.fetchNextPage()}
              disabled={inquiries.isFetchingNextPage}
            >
              {t('common:actions.loadMore')}
            </Button>
          ) : null}
        </div>
      )}
    </RequirePermission>
  );
}
