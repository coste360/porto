import type { YardQuoteStatus } from '@porto/shared/api';

import type { BadgeVariant } from '../../contracts-m4/components/kit';

/** Badge variant of a quote status. */
export function statusVariant(s: YardQuoteStatus): BadgeVariant {
  switch (s) {
    case 'accepted':
      return 'default';
    case 'declined':
    case 'expired':
      return 'destructive';
    case 'sent':
      return 'secondary';
    default:
      return 'outline';
  }
}
