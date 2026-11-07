import { Link } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import { PlusIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useQuotes, type QuoteSummary, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DataTable, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';

const ALL = '__all__';
const STATUSES = ['draft', 'sent', 'accepted', 'declined', 'expired'] as const;

export function QuotesPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<Schemas['QuoteStatus'] | ''>('');
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const quotes = useQuotes(marinaId, { ...(status ? { status } : {}), q: deferred });

  const columns: ColumnDef<QuoteSummary, unknown>[] = [
    {
      header: t('desk:columns.code'),
      cell: ({ row }) => (
        <Link
          to="/m/$marinaId/quotes/$quoteId"
          params={{ marinaId, quoteId: row.original.id }}
          className="font-medium underline-offset-4 hover:underline"
        >
          {row.original.code}
        </Link>
      ),
    },
    { header: t('desk:columns.vessel'), cell: ({ row }) => row.original.vessel.name },
    {
      header: t('desk:columns.customer'),
      cell: ({ row }) => row.original.customer.display_name ?? '',
    },
    {
      header: t('desk:columns.kind'),
      cell: ({ row }) =>
        row.original.term
          ? t(`occupancyEnums:contractTerm.${row.original.term}`)
          : t(`occupancyEnums:quoteKind.${row.original.kind}`),
    },
    {
      header: t('desk:columns.period'),
      cell: ({ row }) =>
        `${formatIsoDate(row.original.starts_on)} – ${formatIsoDate(row.original.ends_on)}`,
    },
    {
      header: t('desk:columns.validUntil'),
      cell: ({ row }) => formatIsoDate(row.original.valid_until),
    },
    {
      header: t('desk:columns.total'),
      cell: ({ row }) =>
        row.original.min_total_cents !== undefined ? (
          <span className="tabular-nums">
            {formatCents(row.original.min_total_cents, 'EUR', i18n.language)}
          </span>
        ) : null,
    },
    {
      header: t('desk:columns.status'),
      cell: ({ row }) => (
        <Badge variant="secondary">{t(`occupancyEnums:quoteStatus.${row.original.status}`)}</Badge>
      ),
    },
  ];

  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageHeader
        title={t('desk:quotes.title')}
        description={t('desk:quotes.description')}
        actions={
          can('contracts.write') ? (
            <Button asChild>
              <Link to="/m/$marinaId/quotes/new" params={{ marinaId }}>
                <PlusIcon />
                {t('desk:quotes.new')}
              </Link>
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <Field className="sm:max-w-sm">
          <FieldLabel htmlFor="quotes-q">{t('desk:filters.search')}</FieldLabel>
          <Input
            id="quotes-q"
            type="search"
            value={q}
            maxLength={64}
            onChange={(e) => setQ(e.target.value)}
          />
        </Field>
        <Field className="sm:max-w-48">
          <FieldLabel htmlFor="quotes-status">{t('desk:filters.status')}</FieldLabel>
          <Select
            value={status || ALL}
            onValueChange={(v) => setStatus(v === ALL ? '' : (v as typeof status))}
          >
            <SelectTrigger id="quotes-status" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('desk:filters.all')}</SelectItem>
              {STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`occupancyEnums:quoteStatus.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      {quotes.isPending ? (
        <LoadingState rows={6} />
      ) : quotes.isError ? (
        <QueryErrorState error={quotes.error} onRetry={() => void quotes.refetch()} />
      ) : (
        <div className="flex flex-col gap-3">
          <DataTable
            columns={columns}
            data={quotes.data.pages.flatMap((p) => p.items)}
            getRowId={(r) => r.id}
            empty={t('desk:quotes.empty')}
          />
          {quotes.hasNextPage ? (
            <Button
              variant="outline"
              onClick={() => void quotes.fetchNextPage()}
              disabled={quotes.isFetchingNextPage}
            >
              {t('common:actions.loadMore')}
            </Button>
          ) : null}
        </div>
      )}
    </RequirePermission>
  );
}
