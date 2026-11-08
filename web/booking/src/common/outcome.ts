import type { OnlineBookingStatus } from '@porto/shared/api';

/** What the return page and the flow show for a booking status. */
export type Outcome = 'verifying' | 'confirmed' | 'received' | 'failed' | 'cancelled' | 'timeout';

export function outcomeOf(status: OnlineBookingStatus, verifying: boolean): Outcome {
  if (status === 'confirmed' || status === 'arrived' || status === 'departed') return 'confirmed';
  if (status === 'awaiting_approval') return 'received';
  if (verifying || status === 'pending_payment') return 'verifying';
  if (status === 'held') return 'failed';
  return 'cancelled';
}
