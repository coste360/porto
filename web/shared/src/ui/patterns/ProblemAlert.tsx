import { CircleAlertIcon } from 'lucide-react';

import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';

import { toProblem } from '../../api/problem';
import { useT } from '../../i18n/useT';

export type ProblemAlertProps = { error: unknown; className?: string };

/** Shows any API error through its stable `errors:<slug>` message. */
export function ProblemAlert({ error, className }: ProblemAlertProps) {
  const { t } = useT();
  if (!error) return null;
  const problem = toProblem(error);
  return (
    <Alert variant="destructive" className={className}>
      <CircleAlertIcon />
      <AlertTitle>{t('errors:title')}</AlertTitle>
      <AlertDescription>{t(problem.i18nKey)}</AlertDescription>
    </Alert>
  );
}
