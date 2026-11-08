import { CloudOffIcon, CloudIcon } from 'lucide-react';
import { useState } from 'react';

import { cn } from '@porto/shared/lib/utils';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';

import { useT } from '../../i18n/useT';
import { useOfflineSession, useOnlineStatus, useQueue } from '../../offline/index';
import { QueueSheet } from './QueueSheet';

export type OfflineIndicatorProps = { marinaId: string; className?: string };

/**
 * Online/offline status of the shell header (m6b-plan §4.3): a dot, the number of queued operations and the
 * conflicts (destructive colour); tapping it opens the queue sheet. Always visible on phone layouts.
 */
export function OfflineIndicator({ marinaId, className }: OfflineIndicatorProps) {
  const { t } = useT();
  const { online, swActive } = useOnlineStatus();
  const queue = useQueue(marinaId);
  const session = useOfflineSession();
  const [open, setOpen] = useState(false);
  const status = online ? t('offline:status.online') : t('offline:status.offline');
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        data-testid="offline-indicator"
        data-online={online ? 'true' : 'false'}
        data-pending={queue.pending}
        data-conflicts={queue.conflicts}
        data-sw={swActive ? 'active' : 'none'}
        aria-label={`${status} · ${t('offline:queue.title')}`}
        className={cn('min-h-11 gap-2', className)}
        onClick={() => setOpen(true)}
      >
        <span
          aria-hidden
          className={cn('size-2.5 rounded-full', online ? 'bg-primary' : 'bg-muted-foreground')}
        />
        {online ? <CloudIcon aria-hidden /> : <CloudOffIcon aria-hidden />}
        <span className="sr-only sm:not-sr-only">{status}</span>
        {queue.pending > 0 ? (
          <Badge variant="secondary" data-testid="offline-pending">
            {queue.pending === 1
              ? t('offline:status.pending_one', { count: 1 })
              : t('offline:status.pending_other', { count: queue.pending })}
          </Badge>
        ) : null}
        {queue.conflicts > 0 ? (
          <Badge variant="destructive" data-testid="offline-conflicts">
            {queue.conflicts === 1
              ? t('offline:status.conflicts_one', { count: 1 })
              : t('offline:status.conflicts_other', { count: queue.conflicts })}
          </Badge>
        ) : null}
      </Button>
      {session.authRequired ? (
        <p role="status" className="text-sm text-destructive">
          {t('offline:signInAgain')}
        </p>
      ) : null}
      <QueueSheet marinaId={marinaId} open={open} onOpenChange={setOpen} />
    </>
  );
}
