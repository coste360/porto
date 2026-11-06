import { LockIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import { isProblem } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { EmptyState, ProblemAlert } from '@porto/shared/ui/patterns';
import { Skeleton } from '@porto/shared/ui/skeleton';

export function LoadingState({ rows = 3 }: { rows?: number }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3" aria-busy="true" aria-label={t('common:status.loading')}>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  );
}

export function ForbiddenState() {
  const { t } = useT();
  return <EmptyState icon={<LockIcon />} title={t('common:page.forbidden')} />;
}

/** Error of a query: 403 shows the forbidden state, anything else the problem and a retry. */
export function QueryErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const { t } = useT();
  if (isProblem(error, 'forbidden')) return <ForbiddenState />;
  return (
    <div className="flex flex-col gap-3">
      <ProblemAlert error={error} />
      {onRetry ? (
        <div>
          <Button variant="outline" onClick={onRetry}>
            {t('common:actions.retry')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

/** Renders children only when the current marina grants the permission. */
export function RequirePermission({
  allowed,
  children,
}: {
  allowed: boolean;
  children: ReactNode;
}) {
  return allowed ? <>{children}</> : <ForbiddenState />;
}
