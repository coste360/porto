import { CloudOffIcon } from 'lucide-react';

import { cn } from '@porto/shared/lib/utils';

import { useT } from '../../i18n/useT';
import type { OfflineReasonCode } from '../../offline/types';

export type OfflineReasonProps = { reason: OfflineReasonCode; className?: string };

/**
 * Why a control is unavailable offline (m6b-plan §4.2): shown inline next to the disabled control, not only as a
 * tooltip (phones have no hover).
 */
export function OfflineReason({ reason, className }: OfflineReasonProps) {
  const { t } = useT();
  return (
    <p
      data-testid="offline-reason"
      data-reason={reason}
      className={cn('flex items-start gap-1.5 text-sm text-muted-foreground', className)}
    >
      <CloudOffIcon className="mt-0.5 size-4 shrink-0" aria-hidden />
      <span>{t(`offline:forbidden.${reason}`)}</span>
    </p>
  );
}
