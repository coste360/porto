import { PrinterIcon } from 'lucide-react';
import { toast } from 'sonner';

import { usePrintBridgeReceipt, useReceiptPrinterReady } from '@porto/shared/desktop/hooks';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';

import { toastProblem } from '../../lib/toast';

/**
 * "Stampa scontrino" on the local receipt printer of the app window (m6b-plan §8.3): rendered only when the
 * bridge reports the receipt printer ready, so it never shows in a browser or on a phone.
 */
export function BridgeReceiptButton({
  paymentId,
  size = 'default',
}: {
  paymentId: string;
  size?: 'default' | 'sm';
}) {
  const { t } = useT();
  const ready = useReceiptPrinterReady();
  const print = usePrintBridgeReceipt();
  if (!ready) return null;
  return (
    <Button
      type="button"
      variant="outline"
      size={size}
      data-testid="bridge-receipt"
      disabled={print.isPending}
      onClick={() =>
        print.mutate(
          { paymentId },
          {
            onSuccess: () => toast.success(t('desktop:peripherals.printed')),
            onError: (e) => toastProblem(t, e),
          },
        )
      }
    >
      <PrinterIcon />
      {t('desktop:peripherals.printReceipt')}
    </Button>
  );
}
