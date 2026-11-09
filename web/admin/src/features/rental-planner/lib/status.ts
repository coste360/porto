import type { RentalBookingStatus } from '@porto/shared/api';

export const STATUSES: readonly RentalBookingStatus[] = [
  'held',
  'confirmed',
  'checked_out',
  'returned',
  'closed',
  'cancelled',
  'no_show',
];

/** Badge variant of a booking status. */
export function statusVariant(
  s: RentalBookingStatus,
): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (s) {
    case 'confirmed':
    case 'checked_out':
      return 'default';
    case 'held':
    case 'returned':
      return 'secondary';
    case 'no_show':
    case 'cancelled':
      return 'destructive';
    default:
      return 'outline';
  }
}
