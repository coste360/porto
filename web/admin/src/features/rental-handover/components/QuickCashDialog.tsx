import { BanknoteIcon, CreditCardIcon, LinkIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useQuickCashRental,
  useRentalBooking,
  type RentalQuickCashInput,
  type RentalQuickCashResult,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';

import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { RenterQr } from './RenterQr';

type Method = RentalQuickCashInput['method'];
type Target = NonNullable<RentalQuickCashInput['target']>;

/**
 * Quick cash of a rental (m11-plan §9.3, D19): one tap per method (cash, card POS, payment link). Receipt-mode
 * charges get their commercial document from the server; a link is shown as a QR for the renter's own phone (no
 * email). A payment link belongs to a registered customer (ADR 0016): for a walk-in renter the method is disabled.
 */
export function QuickCashDialog({
  marinaId,
  bookingId,
  target = 'open',
  open,
  onOpenChange,
  onDone,
}: {
  marinaId: string;
  bookingId: string;
  target?: Target;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onDone?: (r: RentalQuickCashResult) => void;
}) {
  const { t } = useT();
  const cash = useQuickCashRental(marinaId);
  const booking = useRentalBooking(marinaId, bookingId);
  const walkIn = booking.data !== undefined && !booking.data.customer_id;
  const [link, setLink] = useState<{ url: string; qr?: string } | null>(null);
  const pay = (method: Method) =>
    cash.mutate(
      { bookingId, body: { method, target } },
      {
        onSuccess: (r) => {
          if (method === 'link' && r.link_url) {
            setLink({ url: r.link_url, qr: r.link_qr_svg });
          } else {
            toast.success(t('rentalHandover:cash.done'));
            onOpenChange(false);
          }
          onDone?.(r);
        },
      },
    );
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="quick-cash-dialog">
        <DialogHeader>
          <DialogTitle>{t('rentalHandover:cash.title')}</DialogTitle>
          <DialogDescription>{t(`rentalHandover:cash.target.${target}`)}</DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-1 gap-2">
          <Button className="min-h-11" disabled={cash.isPending} onClick={() => pay('cash')}>
            <BanknoteIcon />
            {t('rentalHandover:cash.method.cash')}
          </Button>
          <Button
            className="min-h-11"
            variant="secondary"
            disabled={cash.isPending}
            onClick={() => pay('card_pos')}
          >
            <CreditCardIcon />
            {t('rentalHandover:cash.method.card_pos')}
          </Button>
          <Button
            className="min-h-11"
            variant="outline"
            disabled={cash.isPending || walkIn}
            aria-describedby={walkIn ? 'quick-cash-link-hint' : undefined}
            onClick={() => pay('link')}
          >
            <LinkIcon />
            {t('rentalHandover:cash.method.link')}
          </Button>
          {walkIn ? (
            <p id="quick-cash-link-hint" className="text-muted-foreground text-sm">
              {t('rentalHandover:cash.linkNeedsCustomer')}
            </p>
          ) : null}
        </div>
        {link?.qr ? (
          <RenterQr
            svg={link.qr}
            alt={t('rentalHandover:cash.qrAlt')}
            hint={t('rentalHandover:cash.qrHint')}
            testId="quick-cash-qr"
          />
        ) : null}
        {link ? (
          <a
            className="text-primary break-all underline"
            href={link.url}
            target="_blank"
            rel="noreferrer"
            data-testid="quick-cash-link"
          >
            {t('rentalHandover:cash.openLink')}
          </a>
        ) : null}
        {cash.error ? <DeskProblemAlert error={cash.error} /> : null}
      </DialogContent>
    </Dialog>
  );
}
