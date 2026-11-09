import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { ArrowLeftIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useRentalBooking,
  useTransitionRentalBooking,
  type RentalBooking,
  type RentalTransitionInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate, formatQtyMilli } from '@porto/shared/lib/format';
import { slotLabel } from '@porto/shared/planner';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
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
import { RentalHandoverPanel } from '../../rental-handover/public';
import { statusVariant } from '../lib/status';

const route = getRouteApi('/auth/m/$marinaId/rental/bookings/$bookingId');

/** "dd/MM/yyyy HH:MM" of a slot point. */
function pointText(p: RentalBooking['start']): string {
  return `${formatIsoDate(p.date)} ${slotLabel(p.slot)}`;
}

/** States with a check-out or check-in: the handover panel shows the steps, deposit, damages and quick cash. */
const HANDOVER_STATUSES = new Set(['confirmed', 'checked_out', 'returned', 'closed']);

/**
 * Rental booking page (m11-plan §14.2): summary, transitions (confirm a held option, extend it, cancel, no-show),
 * price lines, extras and charges. Check-out, check-in, deposit, damages and quick cash are the handover panels.
 */
export function RentalBookingPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { bookingId } = route.useParams();
  const navigate = useNavigate();
  const q = useRentalBooking(marinaId, bookingId);
  const transition = useTransitionRentalBooking(marinaId);
  const [reason, setReason] = useState('');
  const b = q.data;
  const act = (body: RentalTransitionInput) => {
    if (!b) return;
    transition.mutate(
      { id: b.id, version: b.version, body },
      { onSuccess: () => toast.success(t(`rentalBookings:toast.${body.action}`)) },
    );
  };
  const money = (c: number) => formatCents(c, 'EUR', i18n.language);
  return (
    <RequirePermission allowed={can('rental.manage')}>
      <div className="flex w-full flex-col gap-4">
        <PageHeader
          title={
            b
              ? t('rentalBookings:booking.title', { number: b.number })
              : t('rentalBookings:nav.bookings')
          }
          actions={
            <Button
              variant="outline"
              onClick={() =>
                void navigate({ to: '/m/$marinaId/rental/planner', params: { marinaId } })
              }
            >
              <ArrowLeftIcon />
              {t('rentalBookings:nav.planner')}
            </Button>
          }
        />
        {q.isLoading ? <LoadingState /> : null}
        {q.isError ? <QueryErrorState error={q.error} onRetry={() => void q.refetch()} /> : null}
        {b ? (
          <>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  {b.unit_code} · {b.formula_code}
                  <Badge variant={statusVariant(b.status)} data-testid="rental-booking-status">
                    {t(`rentalBookings:status.${b.status}`)}
                  </Badge>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
                  <dt className="text-muted-foreground">{t('rentalBookings:fields.start')}</dt>
                  <dd>{pointText(b.start)}</dd>
                  <dt className="text-muted-foreground">{t('rentalBookings:fields.end')}</dt>
                  <dd>{pointText(b.end)}</dd>
                  {b.renter?.name ? (
                    <>
                      <dt className="text-muted-foreground">
                        {t('rentalBookings:fields.renterName')}
                      </dt>
                      <dd>{b.renter.name}</dd>
                    </>
                  ) : null}
                  {b.renter?.phone ? (
                    <>
                      <dt className="text-muted-foreground">
                        {t('rentalBookings:fields.renterPhone')}
                      </dt>
                      <dd>{b.renter.phone}</dd>
                    </>
                  ) : null}
                  {b.hold_expires_at ? (
                    <>
                      <dt className="text-muted-foreground">
                        {t('rentalBookings:fields.holdExpires')}
                      </dt>
                      <dd>{new Date(b.hold_expires_at).toLocaleString(i18n.language)}</dd>
                    </>
                  ) : null}
                  <dt className="text-muted-foreground">{t('rentalBookings:fields.licence')}</dt>
                  <dd>
                    {b.licence_required
                      ? t('rentalBookings:booking.licenceYes')
                      : t('rentalBookings:booking.licenceNo')}
                  </dd>
                  <dt className="text-muted-foreground">{t('rentalBookings:fields.fiscalMode')}</dt>
                  <dd>{t(`rentalBookings:fiscal.${b.fiscal_mode}`)}</dd>
                  <dt className="text-muted-foreground">{t('rentalBookings:fields.total')}</dt>
                  <dd className="font-medium">{money(b.total_cents)}</dd>
                  {b.slots_lost ? (
                    <dd className="text-destructive sm:col-span-2">
                      {t('rentalBookings:booking.slotsLost')}
                    </dd>
                  ) : null}
                  {b.cancel_reason ? (
                    <>
                      <dt className="text-muted-foreground">
                        {t('rentalBookings:fields.cancelReason')}
                      </dt>
                      <dd>{b.cancel_reason}</dd>
                    </>
                  ) : null}
                </dl>
              </CardContent>
            </Card>
            {HANDOVER_STATUSES.has(b.status) ? (
              <RentalHandoverPanel marinaId={marinaId} booking={b} />
            ) : null}
            {b.status === 'held' || b.status === 'confirmed' ? (
              <Card>
                <CardHeader>
                  <CardTitle>{t('rentalBookings:booking.actions')}</CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                  <div className="flex flex-wrap gap-2">
                    {b.status === 'held' ? (
                      <>
                        <Button
                          disabled={transition.isPending}
                          onClick={() => act({ action: 'confirm' })}
                        >
                          {t('rentalBookings:actions.confirm')}
                        </Button>
                        <Button
                          variant="outline"
                          disabled={transition.isPending}
                          onClick={() => act({ action: 'extend_hold' })}
                        >
                          {t('rentalBookings:actions.extendHold')}
                        </Button>
                      </>
                    ) : (
                      <Button
                        variant="outline"
                        disabled={transition.isPending}
                        onClick={() => act({ action: 'no_show' })}
                      >
                        {t('rentalBookings:actions.noShow')}
                      </Button>
                    )}
                  </div>
                  <div className="flex flex-wrap items-end gap-2">
                    <Field className="w-72">
                      <FieldLabel htmlFor="rb-reason">
                        {t('rentalBookings:fields.cancelReason')}
                      </FieldLabel>
                      <Input
                        id="rb-reason"
                        value={reason}
                        maxLength={200}
                        onChange={(e) => setReason(e.target.value)}
                      />
                    </Field>
                    <Button
                      variant="destructive"
                      disabled={transition.isPending}
                      onClick={() => act({ action: 'cancel', ...(reason ? { reason } : {}) })}
                    >
                      {t('rentalBookings:actions.cancel')}
                    </Button>
                  </div>
                  <ProblemAlert error={transition.error} />
                </CardContent>
              </Card>
            ) : null}
            <Card>
              <CardHeader>
                <CardTitle>{t('rentalBookings:booking.lines')}</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('rentalBookings:fields.item')}</TableHead>
                      <TableHead className="text-right">{t('rentalBookings:fields.qty')}</TableHead>
                      <TableHead className="text-right">
                        {t('rentalBookings:fields.unitPrice')}
                      </TableHead>
                      <TableHead className="text-right">
                        {t('rentalBookings:fields.amount')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {b.lines.map((l) => (
                      <TableRow key={l.seq}>
                        <TableCell>
                          {l.item_code}{' '}
                          <span className="text-muted-foreground">
                            {t(`rentalBookings:lineKind.${l.kind}`)}
                          </span>
                        </TableCell>
                        <TableCell className="text-right">
                          {formatQtyMilli(l.qty_milli, i18n.language)}
                        </TableCell>
                        <TableCell className="text-right">{money(l.unit_cents)}</TableCell>
                        <TableCell className="text-right">{money(l.amount_cents)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
            {b.charges.length > 0 ? (
              <Card>
                <CardHeader>
                  <CardTitle>{t('rentalBookings:booking.charges')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableBody>
                      {b.charges.map((c) => (
                        <TableRow key={c.charge_id}>
                          <TableCell>{t(`rentalBookings:chargeKind.${c.kind}`)}</TableCell>
                          <TableCell>{c.status}</TableCell>
                          <TableCell className="text-right">{money(c.total_cents)}</TableCell>
                          <TableCell className="text-right">{money(c.paid_amount_cents)}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            ) : null}
          </>
        ) : null}
      </div>
    </RequirePermission>
  );
}
