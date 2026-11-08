import { TimerIcon } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';

import { useT } from '@porto/shared/i18n';
import { cn } from '@porto/shared/lib/utils';

import { formatMinutes, secondsLeft } from './holdTime';

/**
 * Countdown of a berth hold (m8-plan §12.1): minutes and seconds until hold_expires_at, from the server value. It
 * calls onExpire once when it reaches zero. Screen readers hear it only at the last minute (aria-live off before).
 */
export function Countdown({
  expiresAt,
  onExpire,
  className,
}: {
  expiresAt: string;
  onExpire?: () => void;
  className?: string;
}) {
  const { t } = useT();
  const [now, setNow] = useState(() => Date.now());
  const fired = useRef(false);
  const left = secondsLeft(expiresAt, now);

  useEffect(() => {
    fired.current = false;
  }, [expiresAt]);

  useEffect(() => {
    if (left <= 0) {
      if (!fired.current) {
        fired.current = true;
        onExpire?.();
      }
      return;
    }
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [left, onExpire]);

  return (
    <p
      role="timer"
      aria-live={left <= 60 ? 'polite' : 'off'}
      className={cn(
        'flex items-center gap-2 text-sm',
        left <= 60 ? 'text-destructive' : 'text-muted-foreground',
        className,
      )}
      data-testid="hold-countdown"
    >
      <TimerIcon className="size-4" aria-hidden />
      {t('checkout:hold.countdown', { time: formatMinutes(left) })}
    </p>
  );
}
