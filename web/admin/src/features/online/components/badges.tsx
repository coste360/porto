import { useEffect, useState } from 'react';

import type { Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';

import { remaining } from '../lib/constants';

const variants: Record<
  Schemas['OnlineBookingStatus'],
  'default' | 'secondary' | 'destructive' | 'outline'
> = {
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

export function OnlineStatusBadge({ status }: { status: Schemas['OnlineBookingStatus'] }) {
  const { t } = useT();
  return <Badge variant={variants[status]}>{t(`online:status.${status}`)}</Badge>;
}

/** Countdown to an approval deadline, refreshed every 30 seconds. */
export function DeadlineLeft({ at }: { at: string }) {
  const { t } = useT();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(id);
  }, []);
  const left = remaining(at, now);
  return (
    <span className="tabular-nums" data-testid="online-deadline">
      {left ? t('online:deadline.left', { time: left }) : t('online:deadline.past')}
    </span>
  );
}
