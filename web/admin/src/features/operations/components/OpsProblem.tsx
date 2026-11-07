import { CircleAlertIcon } from 'lucide-react';

import { toProblem } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';

import { useProblemKey } from '../lib/useProblemKey';

/** Problem alert of the operational view: resource_taken shows the conflicting nights. */
export function OpsProblem({ error }: { error: unknown }) {
  const { t } = useT();
  const keyOf = useProblemKey();
  if (!error) return null;
  const p = toProblem(error);
  const details = p.details as { from?: string; to?: string };
  return (
    <Alert variant="destructive" data-testid="ops-problem" data-type={p.type}>
      <CircleAlertIcon />
      <AlertTitle>
        {p.type === 'resource_taken' ? t('occupancyEnums:conflict.title') : t('errors:title')}
      </AlertTitle>
      <AlertDescription>
        {t(keyOf(error))}
        {p.type === 'resource_taken' && details.from && details.to
          ? ` ${t('occupancyEnums:conflict.nights', { from: details.from, to: details.to })}`
          : null}
      </AlertDescription>
    </Alert>
  );
}
