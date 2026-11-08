import type { ColumnDef } from '@tanstack/react-table';
import { useState } from 'react';

import {
  useOnlineBookings,
  type OnlineBookingFilters,
  type OnlineBookingSummary,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatCm, formatIsoDate } from '@porto/shared/lib/format';
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
import { DeadlineLeft, OnlineStatusBadge } from '../components/badges';
import { OnlineBookingSheet } from '../components/OnlineBookingSheet';
import { ONLINE_CHANNELS as CHANNELS, ONLINE_STATUSES } from '../lib/constants';

const ALL = '__all__';

/**
 * Desk list of online and channel bookings (m8-plan §12.3): filters, approval deadline countdown, detail sheet
 * with approve/reject, documents and payment. Live through the contract.changed SSE invalidation.
 */
export function OnlineBookingsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [filters, setFilters] = useState<OnlineBookingFilters>({});
  const [open, setOpen] = useState<string | null>(null);
  const list = useOnlineBookings(marinaId, filters);

  const columns: ColumnDef<OnlineBookingSummary, unknown>[] = [
    {
      header: t('online:columns.reference'),
      cell: ({ row }) => (
        <Button
          variant="link"
          className="h-auto p-0 font-medium"
          onClick={() => setOpen(row.original.contract_id)}
          data-testid="online-booking-open"
        >
          {row.original.reference}
        </Button>
      ),
    },
    {
      header: t('online:columns.dates'),
      cell: ({ row }) =>
        `${formatIsoDate(row.original.arrival)} – ${formatIsoDate(row.original.departure)}`,
    },
    {
      header: t('online:columns.vessel'),
      cell: ({ row }) =>
        `${formatCm(row.original.loa_cm, i18n.language)} × ${formatCm(row.original.beam_cm, i18n.language)}`,
    },
    {
      header: t('online:columns.status'),
      cell: ({ row }) => <OnlineStatusBadge status={row.original.status} />,
    },
    {
      header: t('online:columns.channel'),
      cell: ({ row }) => row.original.channel_code ?? t(`online:channel.${row.original.channel}`),
    },
    {
      header: t('online:columns.deadline'),
      cell: ({ row }) =>
        row.original.status === 'awaiting_approval' && row.original.approval_deadline ? (
          <DeadlineLeft at={row.original.approval_deadline} />
        ) : (
          ''
        ),
    },
    {
      header: t('online:columns.amount'),
      cell: ({ row }) => (
        <span className="tabular-nums">
          {formatCents(row.original.total_cents, 'EUR', i18n.language)}
        </span>
      ),
    },
  ];

  const set = (patch: OnlineBookingFilters) => setFilters((f) => ({ ...f, ...patch }));
  return (
    <RequirePermission allowed={can('transits.desk')}>
      <PageHeader title={t('online:nav.bookings')} description={t('online:bookings.description')} />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-end">
        <Field className="sm:max-w-56">
          <FieldLabel htmlFor="online-status">{t('online:filters.status')}</FieldLabel>
          <Select
            value={filters.status ?? ALL}
            onValueChange={(v) =>
              set({ status: v === ALL ? undefined : (v as Schemas['OnlineBookingStatus']) })
            }
          >
            <SelectTrigger id="online-status" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('online:filters.all')}</SelectItem>
              {ONLINE_STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`online:status.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="sm:max-w-48">
          <FieldLabel htmlFor="online-channel">{t('online:filters.channel')}</FieldLabel>
          <Select
            value={filters.channel ?? ALL}
            onValueChange={(v) =>
              set({ channel: v === ALL ? undefined : (v as Schemas['OnlineChannel']) })
            }
          >
            <SelectTrigger id="online-channel" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('online:filters.all')}</SelectItem>
              {CHANNELS.map((c) => (
                <SelectItem key={c} value={c}>
                  {t(`online:channel.${c}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field className="sm:max-w-44">
          <FieldLabel htmlFor="online-from">{t('online:filters.from')}</FieldLabel>
          <Input
            id="online-from"
            type="date"
            value={filters.from ?? ''}
            onChange={(e) => set({ from: e.target.value || undefined })}
          />
        </Field>
        <Field className="sm:max-w-44">
          <FieldLabel htmlFor="online-to">{t('online:filters.to')}</FieldLabel>
          <Input
            id="online-to"
            type="date"
            value={filters.to ?? ''}
            onChange={(e) => set({ to: e.target.value || undefined })}
          />
        </Field>
      </div>
      {list.isPending ? (
        <LoadingState rows={6} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <DataTable
          columns={columns}
          data={list.data.items}
          getRowId={(r) => r.contract_id}
          empty={t('online:bookings.empty')}
        />
      )}
      <OnlineBookingSheet marinaId={marinaId} contractId={open} onClose={() => setOpen(null)} />
    </RequirePermission>
  );
}
