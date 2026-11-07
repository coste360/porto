// What the public pay page shows for a request status (m5-plan §5.5): the open page, or a final state. On the
// return page an open request means the provider webhook has not confirmed yet ("Pagamento in verifica"): the
// browser never confirms a payment (INV-09).
import type { PayStatus } from '@porto/shared/api';

export type PayView = 'open' | 'verifying' | 'paid' | 'cancelled' | 'expired';

export function payView(
  status: PayStatus['status'],
  verifying: boolean,
  returnPage = false,
): PayView {
  if (status === 'paid') return 'paid';
  if (status === 'cancelled') return 'cancelled';
  if (status === 'expired') return 'expired';
  return verifying || returnPage ? 'verifying' : 'open';
}
