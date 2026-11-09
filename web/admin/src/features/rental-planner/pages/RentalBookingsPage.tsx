import { useNavigate } from '@tanstack/react-router';
import { CalendarRangeIcon } from 'lucide-react';
import { useState } from 'react';

import { useRentalBookings, type RentalBookingStatus } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { STATUSES, statusVariant } from '../lib/status';

const ALL = '__all__';

/** Rental bookings list (m11-plan §14.2): newest first, filtered by status; a row opens the booking page. */
export function RentalBookingsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const navigate = useNavigate();
  const [status, setStatus] = useState(ALL);
  const list = useRentalBookings(
    marinaId,
    status === ALL ? {} : { status: status as RentalBookingStatus },
  );
  const items = list.data?.items ?? [];
  return (
    <RequirePermission allowed={can('rental.manage')}>
      <div className="flex w-full flex-col gap-4">
        <PageHeader
          title={t('rentalBookings:nav.bookings')}
          actions={
            <Button
              variant="outline"
              onClick={() =>
                void navigate({ to: '/m/$marinaId/rental/planner', params: { marinaId } })
              }
            >
              <CalendarRangeIcon />
              {t('rentalBookings:nav.planner')}
            </Button>
          }
        />
        <Field className="w-56">
          <FieldLabel htmlFor="rl-status">{t('rentalBookings:fields.status')}</FieldLabel>
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger id="rl-status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('rentalBookings:list.allStatuses')}</SelectItem>
              {STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`rentalBookings:status.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        {list.isLoading ? <LoadingState /> : null}
        {list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : null}
        {list.isSuccess && items.length === 0 ? (
          <EmptyState icon={<CalendarRangeIcon />} title={t('rentalBookings:list.empty')} />
        ) : null}
        {items.length > 0 ? (
          <Table data-testid="rental-bookings-table">
            <TableHeader>
              <TableRow>
                <TableHead>{t('rentalBookings:fields.number')}</TableHead>
                <TableHead>{t('rentalBookings:fields.unit')}</TableHead>
                <TableHead>{t('rentalBookings:fields.formula')}</TableHead>
                <TableHead>{t('rentalBookings:fields.start')}</TableHead>
                <TableHead>{t('rentalBookings:fields.end')}</TableHead>
                <TableHead>{t('rentalBookings:fields.status')}</TableHead>
                <TableHead className="text-right">{t('rentalBookings:fields.total')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((b) => (
                <TableRow
                  key={b.id}
                  className="cursor-pointer"
                  onClick={() =>
                    void navigate({
                      to: '/m/$marinaId/rental/bookings/$bookingId',
                      params: { marinaId, bookingId: b.id },
                    })
                  }
                >
                  <TableCell className="font-medium">{b.number}</TableCell>
                  <TableCell>{b.unit_code}</TableCell>
                  <TableCell>{b.formula_code}</TableCell>
                  <TableCell>{formatDateTime(b.starts_at, i18n.language)}</TableCell>
                  <TableCell>{formatDateTime(b.ends_at, i18n.language)}</TableCell>
                  <TableCell>
                    <Badge variant={statusVariant(b.status)}>
                      {t(`rentalBookings:status.${b.status}`)}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    {formatCents(b.total_cents, 'EUR', i18n.language)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : null}
      </div>
    </RequirePermission>
  );
}
