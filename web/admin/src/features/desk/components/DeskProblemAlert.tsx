import { CircleAlertIcon } from 'lucide-react';
import { useState } from 'react';

import { conflictOf, fitBlockedOf, toProblem, type OccupancySolution } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';

import { solutionCodes } from '../lib/search';

export type DeskProblemAlertProps = {
  error: unknown;
  /** One-click alternative of a resource_taken problem (m3-plan §8.6). */
  onAlternative?: (s: OccupancySolution) => void;
  /** Present when the user holds occupancy.override_fit: shows the override form of a fit_blocked problem. */
  onOverride?: (reason: string) => void;
};

/**
 * Problem of a desk operation: the `errors:<slug>` message, the conflicting nights and alternatives of
 * `resource_taken`, and the codes of `fit_blocked` with the override form for who may override.
 */
export function DeskProblemAlert({ error, onAlternative, onOverride }: DeskProblemAlertProps) {
  const { t, i18n } = useT();
  const [reason, setReason] = useState('');
  if (!error) return null;
  const problem = toProblem(error);
  const key = i18n.exists(`errors:${problem.type}`) ? `errors:${problem.type}` : problem.i18nKey;
  const conflict = conflictOf(error);
  const fit = fitBlockedOf(error);
  return (
    <Alert variant="destructive" role="alert">
      <CircleAlertIcon />
      <AlertTitle>{t(key)}</AlertTitle>
      <AlertDescription className="flex flex-col gap-2">
        {conflict ? (
          <>
            <span>
              {t('occupancyEnums:conflict.nights', {
                from: formatIsoDate(conflict.from),
                to: formatIsoDate(conflict.to),
              })}
            </span>
            {conflict.cells?.length ? (
              <span>{t('occupancyEnums:conflict.cells', { count: conflict.cells.length })}</span>
            ) : null}
            {onAlternative ? (
              <div className="flex flex-col gap-1">
                <span className="font-medium">
                  {conflict.alternatives?.length
                    ? t('occupancyEnums:conflict.alternatives')
                    : t('occupancyEnums:conflict.none')}
                </span>
                <div className="flex flex-wrap gap-2">
                  {(conflict.alternatives ?? []).slice(0, 5).map((s) => (
                    <Button
                      key={s.key}
                      size="sm"
                      variant="outline"
                      onClick={() => onAlternative(s)}
                    >
                      {solutionCodes(s)}
                      {s.pricing
                        ? ` · ${formatCents(s.pricing.total_cents, 'EUR', i18n.language)}`
                        : ''}
                    </Button>
                  ))}
                </div>
              </div>
            ) : null}
          </>
        ) : null}
        {fit ? (
          <>
            <ul className="list-disc pl-4">
              {fit.blocking.map((issue) => (
                <li key={issue.code}>{t(`occupancyEnums:fit.${issue.code}`)}</li>
              ))}
            </ul>
            {onOverride && fit.overridable ? (
              <form
                className="flex flex-col gap-2 sm:flex-row sm:items-end"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (reason.trim()) onOverride(reason.trim());
                }}
              >
                <Field>
                  <FieldLabel htmlFor="fit-override-reason">
                    {t('desk:fitOverride.reason')}
                  </FieldLabel>
                  <Input
                    id="fit-override-reason"
                    value={reason}
                    maxLength={200}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </Field>
                <Button type="submit" variant="secondary" disabled={!reason.trim()}>
                  {t('desk:fitOverride.apply')}
                </Button>
              </form>
            ) : null}
          </>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}
