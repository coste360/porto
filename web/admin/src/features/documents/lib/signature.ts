// Signature helpers (spec 15.4, m4-plan §6.5): badge variant, signer timeline, request body. Pure.
import type {
  SignatureRequest,
  SignatureRequestInput,
  Signer,
  SignerInput,
  SignerRole,
} from '@porto/shared/api';

import type { CustomerChoice } from '../../desk/components/CustomerField';

type BadgeVariant = 'default' | 'secondary' | 'outline' | 'destructive';

export const DRIVERS = ['paper', 'otp', 'provider'] as const;
export const ORDER = ['sequential', 'parallel'] as const;
export type SignerRow = { role: SignerRole; customer: CustomerChoice };

export function signatureVariant(status: SignatureRequest['status']): BadgeVariant {
  switch (status) {
    case 'signed':
      return 'default';
    case 'sent':
    case 'partially_signed':
      return 'secondary';
    case 'declined':
    case 'expired':
    case 'cancelled':
      return 'destructive';
    default:
      return 'outline';
  }
}

/** The moments of a signer, in order, for the timeline. */
export function signerTimeline(s: Signer): { key: string; at: string }[] {
  return (
    [
      ['invited', s.invited_at],
      ['opened', s.opened_at],
      ['signed', s.signed_at],
      ['declined', s.declined_at],
    ] as const
  )
    .filter(([, at]) => !!at)
    .map(([key, at]) => ({ key, at: at! }));
}

/** Body of a signature request: driver and order (marina defaults when empty) and the extra signers. */
export function signatureBody(
  driver: (typeof DRIVERS)[number] | '',
  order: (typeof ORDER)[number] | '',
  rows: readonly SignerRow[],
): SignatureRequestInput {
  const signers: SignerInput[] = rows
    .filter((r) => r.role === 'marina' || r.customer)
    .map((r) =>
      r.role === 'marina' ? { role: r.role } : { role: r.role, customer_id: r.customer!.id },
    );
  return {
    ...(driver ? { driver } : {}),
    ...(order ? { order_mode: order } : {}),
    ...(signers.length ? { signers } : {}),
  };
}
