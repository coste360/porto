import { usePaymentReceipt } from '@porto/shared/api';

import { openBlob } from './files';

/**
 * Opens the receipt print view (HTML 80/58 mm) or the A4 PDF in a new tab. The tab opens from the promise, not
 * from a mutate callback, so it still opens when the caller closes (unmounts) right after starting it, as the
 * counter-sale dialog does.
 */
export function useReceiptOpener(marinaId: string) {
  const receipt = usePaymentReceipt(marinaId);
  const open = (paymentId: string, format: '80' | '58' | 'a4') => {
    receipt
      .mutateAsync({ paymentId, format })
      .then((b) =>
        openBlob(format === 'a4' ? b : new Blob([b], { type: 'text/html;charset=utf-8' })),
      )
      .catch(() => undefined); // the error stays on receipt.error while the caller is mounted
  };
  return { open, pending: receipt.isPending, error: receipt.error };
}
