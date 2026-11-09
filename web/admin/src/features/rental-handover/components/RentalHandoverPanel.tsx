import { FileTextIcon, WalletIcon } from 'lucide-react';
import { useState } from 'react';

import {
  rentalPdfUrl,
  useRentalDeposit,
  useRentalHandover,
  type RentalBooking,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';

import { phaseOf } from '../lib/wizard';
import { QuickCashDialog } from './QuickCashDialog';

/**
 * Handover summary of a booking for the desktop booking page (m11-plan §14.3): steps still missing, deposit state,
 * damages, PDFs (signed links behind a redirect) and the quick-cash dialog.
 */
export function RentalHandoverPanel({
  marinaId,
  booking,
}: {
  marinaId: string;
  booking: RentalBooking;
}) {
  const { t, i18n } = useT();
  const phase = phaseOf(booking.status);
  const handover = useRentalHandover(marinaId, booking.id, phase);
  const deposit = useRentalDeposit(marinaId, booking.id);
  const [cash, setCash] = useState(false);
  const missing = handover.data?.missing ?? [];
  const unpaid = booking.charges.reduce(
    (s, c) => s + Math.max(0, c.total_cents - c.paid_amount_cents),
    0,
  );
  const done = booking.status === 'closed' || booking.status === 'returned';
  return (
    <Card data-testid="rental-handover-panel">
      <CardHeader>
        <CardTitle>{t('rentalHandover:panel.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {!done ? (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted-foreground">{t(`rentalHandover:phase.${phase}`)}</span>
            {missing.length === 0 ? (
              <Badge variant="secondary">{t('rentalHandover:panel.ready')}</Badge>
            ) : (
              missing.map((m) => (
                <Badge key={m} variant="outline">
                  {t(`rentalHandover:missing.${m}`)}
                </Badge>
              ))
            )}
          </div>
        ) : null}
        {deposit.data ? (
          <div className="flex flex-wrap items-center gap-2" data-testid="rental-deposit-state">
            <WalletIcon className="size-4" />
            <span>{t('rentalHandover:deposit.title')}</span>
            <Badge variant="outline">
              {t(`rentalHandover:deposit.status.${deposit.data.status}`)}
            </Badge>
            <span>{formatCents(deposit.data.amount_cents, 'EUR', i18n.language)}</span>
            {deposit.data.retained_cents > 0 ? (
              <span className="text-muted-foreground">
                {t('rentalHandover:deposit.retained', {
                  amount: formatCents(deposit.data.retained_cents, 'EUR', i18n.language),
                })}
              </span>
            ) : null}
          </div>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" asChild>
            <a
              href={rentalPdfUrl(marinaId, booking.id, 'contract')}
              target="_blank"
              rel="noreferrer"
            >
              <FileTextIcon />
              {t('rentalHandover:pdf.contract')}
            </a>
          </Button>
          {booking.checked_out_at ? (
            <Button variant="outline" asChild>
              <a
                href={rentalPdfUrl(marinaId, booking.id, 'checkout')}
                target="_blank"
                rel="noreferrer"
              >
                <FileTextIcon />
                {t('rentalHandover:pdf.checkout')}
              </a>
            </Button>
          ) : null}
          {booking.returned_at ? (
            <Button variant="outline" asChild>
              <a
                href={rentalPdfUrl(marinaId, booking.id, 'checkin')}
                target="_blank"
                rel="noreferrer"
              >
                <FileTextIcon />
                {t('rentalHandover:pdf.checkin')}
              </a>
            </Button>
          ) : null}
          {booking.status !== 'held' && booking.status !== 'cancelled' ? (
            <Button onClick={() => setCash(true)} data-testid="rental-quick-cash">
              {t('rentalHandover:cash.button', {
                amount: formatCents(unpaid || booking.total_cents, 'EUR', i18n.language),
              })}
            </Button>
          ) : null}
        </div>
      </CardContent>
      <QuickCashDialog
        marinaId={marinaId}
        bookingId={booking.id}
        open={cash}
        onOpenChange={setCash}
      />
    </Card>
  );
}
