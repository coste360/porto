import type { OnlineBookingStatus } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';

const variants: Record<OnlineBookingStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  held: 'outline',
  pending_payment: 'outline',
  awaiting_approval: 'secondary',
  confirmed: 'default',
  arrived: 'default',
  departed: 'secondary',
  cancelled: 'destructive',
  expired: 'destructive',
  no_show: 'destructive',
};

/** Status badge of a booking in the customer area. */
export function StatusBadge({ status }: { status: OnlineBookingStatus }) {
  const { t } = useT();
  return (
    <Badge variant={variants[status]} data-testid="account-status">
      {t(`account:status.${status}`)}
    </Badge>
  );
}
