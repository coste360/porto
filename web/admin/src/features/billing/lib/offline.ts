import type { OfflineReasonCode } from '@porto/shared/offline';

/**
 * Why the payment cannot be recorded offline (null when the queue may carry it, 29.2): only cash collection matched
 * in full to existing charges, none in receipt mode.
 */
export function offlineReasonOf(v: {
  kind: string;
  method: string;
  cents: number;
  allocated: number;
  receiptMode: boolean | undefined;
}): OfflineReasonCode | null {
  if (v.method !== 'cash' || v.kind !== 'payment') return 'online_only';
  if (v.receiptMode !== false || v.cents <= 0 || v.allocated !== v.cents) return 'document';
  return null;
}
