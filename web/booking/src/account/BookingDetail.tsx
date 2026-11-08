import { ArrowLeftIcon, CreditCardIcon, QrCodeIcon, ReceiptIcon } from 'lucide-react';
import { useEffect, useState } from 'react';

import {
  isProblem,
  newIdempotencyKey,
  useAccountBooking,
  usePayAccountBalance,
  type AccountBooking,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Separator } from '@porto/shared/ui/separator';
import { Skeleton } from '@porto/shared/ui/skeleton';
import { Spinner } from '@porto/shared/ui/spinner';

import { ArrivalInfo } from '../common/Confirmation';
import { localized } from '../common/i18nText';
import { Money } from '../common/Money';
import { CancelDialog } from './CancelDialog';
import { ChangeDatesDialog } from './ChangeDatesDialog';
import { DocumentsCard } from './DocumentsCard';
import { StatusBadge } from './status';

/** One booking of the customer: QR, arrival info, documents, receipts and the actions the server allows. */
export function BookingDetail({
  slug,
  session,
  contractId,
  onBack,
  onSessionLost,
}: {
  slug: string;
  session: string;
  contractId: string;
  onBack: () => void;
  onSessionLost: () => void;
}) {
  const { t } = useT();
  const q = useAccountBooking(slug, session, contractId);
  // A 404 is either an expired session or a booking that is not the customer's: the list tells them apart.
  const notFound = q.isError && isProblem(q.error) && q.error.status === 404;
  useEffect(() => {
    if (notFound) onBack();
  }, [notFound, onBack]);
  return (
    <section className="flex flex-col gap-4">
      <div>
        <Button variant="ghost" size="sm" onClick={onBack}>
          <ArrowLeftIcon />
          {t('account:back')}
        </Button>
      </div>
      {q.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : q.isError ? (
        <ProblemAlert error={q.error} />
      ) : (
        <Detail slug={slug} session={session} booking={q.data} onSessionLost={onSessionLost} />
      )}
    </section>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="text-right">{children}</dd>
    </div>
  );
}

function Detail({
  slug,
  session,
  booking: b,
}: {
  slug: string;
  session: string;
  booking: AccountBooking;
  onSessionLost: () => void;
}) {
  const { t, i18n } = useT();
  return (
    <div className="flex flex-col gap-4" data-testid="account-booking">
      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <h2 className="text-lg font-semibold">{b.reference ?? b.code}</h2>
            <StatusBadge status={b.status} />
          </CardTitle>
          <CardDescription>
            {t('account:dates', {
              arrival: formatIsoDate(b.arrival),
              departure: formatIsoDate(b.departure),
            })}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {b.approval_pending ? (
            <Alert>
              <AlertDescription>{t('account:approval')}</AlertDescription>
            </Alert>
          ) : null}
          <dl className="flex flex-col gap-2">
            <Row label={t('account:fields.vessel')}>{b.vessel_name}</Row>
            {b.category ? (
              <Row label={t('account:fields.category')}>{localized(b.category, i18n.language)}</Row>
            ) : null}
            {b.berth_code ? <Row label={t('account:fields.berth')}>{b.berth_code}</Row> : null}
            <Separator />
            <Row label={t('account:fields.total')}>
              <Money cents={b.total_cents} />
            </Row>
            <Row label={t('account:fields.paid')}>
              <Money cents={b.paid_cents} />
            </Row>
            <Row label={t('account:fields.residual')}>
              <Money cents={b.residual_cents} />
            </Row>
          </dl>
          <Actions slug={slug} session={session} booking={b} />
        </CardContent>
      </Card>

      {b.qr_payload ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <QrCodeIcon className="size-5" aria-hidden />
              {t('account:qr.title')}
            </CardTitle>
            <CardDescription>{t('account:qr.description')}</CardDescription>
          </CardHeader>
          <CardContent>
            <p
              className="bg-muted rounded-md p-3 text-center font-mono text-sm break-all"
              data-testid="account-qr"
            >
              {b.qr_payload}
            </p>
          </CardContent>
        </Card>
      ) : null}

      {b.arrival_info ? (
        <Card>
          <CardContent className="pt-6">
            <ArrivalInfo info={b.arrival_info} />
          </CardContent>
        </Card>
      ) : null}

      <DocumentsCard slug={slug} session={session} booking={b} />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ReceiptIcon className="size-5" aria-hidden />
            {t('account:receipts.title')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {b.receipts.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('account:receipts.empty')}</p>
          ) : (
            <ul className="flex flex-col gap-2 text-sm" data-testid="account-receipts">
              {b.receipts.map((r) => (
                <li key={r.payment_id} className="flex justify-between gap-4">
                  <span>
                    {formatIsoDate(r.received_on)} · {t(`billingEnums:paymentMethod.${r.method}`)}
                  </span>
                  <Money cents={r.amount_cents} />
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Actions({
  slug,
  session,
  booking: b,
}: {
  slug: string;
  session: string;
  booking: AccountBooking;
}) {
  const { t } = useT();
  const pay = usePayAccountBalance(slug, session, b.contract_id);
  const [payKey] = useState(newIdempotencyKey);
  if (!b.can_pay_balance && !b.can_change_dates && !b.can_cancel) return null;
  const payBalance = () =>
    pay.mutate(payKey, {
      // Hosted checkouts refuse framing: navigate the top window on the user's click.
      onSuccess: (r) => window.open(r.checkout_url, '_top'),
    });
  return (
    <div className="flex flex-col gap-3">
      <ProblemAlert error={pay.error} />
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
        {b.can_pay_balance ? (
          <Button
            onClick={payBalance}
            disabled={pay.isPending || pay.isSuccess}
            data-testid="account-pay-balance"
          >
            {pay.isPending || pay.isSuccess ? <Spinner /> : <CreditCardIcon />}
            {pay.isSuccess ? t('account:payBalance.redirecting') : t('account:payBalance.button')}
          </Button>
        ) : null}
        {b.can_change_dates ? (
          <ChangeDatesDialog slug={slug} session={session} booking={b} />
        ) : null}
        {b.can_cancel ? <CancelDialog slug={slug} session={session} booking={b} /> : null}
      </div>
    </div>
  );
}
