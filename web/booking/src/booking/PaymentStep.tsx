import { CreditCardIcon, SendIcon } from 'lucide-react';

import {
  isProblem,
  useStartBookingPayment,
  type Booking,
  type BookingConfig,
  type BookingRef,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';

import { useEmbed } from '../common/useEmbed';
import { Money } from '../common/Money';
import { PriceBreakdown } from '../common/PriceBreakdown';
import { goToCheckout } from './navigation';

/** Step 5 "Pagamento": amount due now and "Paga", or "Invia richiesta" when the marina approves first. */
export default function PaymentStep({
  config,
  booking,
  bookingRef,
  payKey,
  onPayKey,
  onSubmitted,
  onHoldExpired,
}: {
  config: BookingConfig;
  booking: Booking;
  bookingRef: BookingRef;
  payKey: string;
  onPayKey: () => void;
  onSubmitted: () => void;
  onHoldExpired: () => void;
}) {
  const { t } = useT();
  const { embedded, returnTo } = useEmbed();
  const pay = useStartBookingPayment(bookingRef);
  const approval = booking.approval_required;
  const notReady = !booking.details_complete;
  const docsMissing =
    booking.missing_documents.length > 0 && !config.required_fields.documents_later;

  const start = () =>
    pay.mutate(
      { body: returnTo ? { return_to: returnTo } : {}, idempotencyKey: payKey },
      {
        onSuccess: (r) => {
          if (r.redirect_url) goToCheckout(r.redirect_url, embedded);
          else onSubmitted();
        },
        onError: (e) => {
          onPayKey();
          if (isProblem(e, 'hold_expired')) onHoldExpired();
        },
      },
    );

  return (
    <div className="flex flex-col gap-4" data-testid="booking-payment">
      <Card>
        <CardHeader>
          <CardTitle>{t('checkout:payment.title', { reference: booking.reference })}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <p className="text-muted-foreground text-sm">
            {t('checkout:payment.stay', {
              arrival: formatIsoDate(booking.arrival),
              departure: formatIsoDate(booking.departure),
            })}
          </p>
          <PriceBreakdown quote={booking.quote} showDueNow={false} />
          <div className="flex items-baseline justify-between gap-3">
            <span className="font-medium">
              {approval ? t('checkout:payment.dueAfterApproval') : t('checkout:payment.dueNow')}
            </span>
            <span className="text-2xl font-semibold" data-testid="booking-due-now">
              <Money cents={booking.amount_due_now_cents} currency={booking.quote.currency} />
            </span>
          </div>
        </CardContent>
      </Card>
      {approval ? (
        <Alert>
          <AlertDescription>{t('checkout:payment.approvalNotice')}</AlertDescription>
        </Alert>
      ) : null}
      {notReady ? (
        <Alert variant="destructive">
          <AlertDescription>{t('checkout:payment.detailsMissing')}</AlertDescription>
        </Alert>
      ) : null}
      {docsMissing ? (
        <Alert variant="destructive">
          <AlertDescription>{t('checkout:documents.missing')}</AlertDescription>
        </Alert>
      ) : null}
      {pay.error ? <ProblemAlert error={pay.error} /> : null}
      <Button
        size="lg"
        onClick={start}
        disabled={pay.isPending || notReady || docsMissing}
        data-testid="booking-pay"
      >
        {approval ? <SendIcon /> : <CreditCardIcon />}
        {pay.isPending
          ? t('checkout:payment.starting')
          : approval
            ? t('checkout:payment.sendRequest')
            : t('checkout:payment.pay')}
      </Button>
      {!approval ? (
        <p className="text-muted-foreground text-center text-xs">
          {t('checkout:payment.redirectNotice')}
        </p>
      ) : null}
    </div>
  );
}
