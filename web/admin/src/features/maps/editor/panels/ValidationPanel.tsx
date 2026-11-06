import { CircleAlertIcon, CircleCheckIcon, TriangleAlertIcon } from 'lucide-react';

import type { MapValidationReport } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Spinner } from '@porto/shared/ui/spinner';

import { groupIssues, type ValidationStatus } from '../model/validation';

/** Errors and warnings grouped by code; a click selects the elements and zooms on them. */
export function ValidationPanel({
  report,
  status,
  codeOf,
  onPick,
}: {
  report: MapValidationReport | null;
  status: ValidationStatus;
  codeOf: (ref: string) => string;
  onPick: (refs: string[]) => void;
}) {
  const { t } = useT();
  const groups = groupIssues(report);
  const errors = report?.errors.length ?? 0;
  const warnings = report?.warnings.length ?? 0;
  return (
    <section
      className="flex flex-col gap-3"
      aria-label={t('editor:validation.title')}
      aria-live="polite"
    >
      <div className="flex items-center gap-2 text-sm" data-testid="validation-summary">
        {status === 'pending' ? (
          <Spinner />
        ) : errors === 0 ? (
          <CircleCheckIcon className="text-chart-2 size-4" />
        ) : (
          <CircleAlertIcon className="text-destructive size-4" />
        )}
        <span>{t('editor:validation.errors', { count: errors })}</span>
        <span className="text-muted-foreground">
          {t('editor:validation.warnings', { count: warnings })}
        </span>
      </div>
      {status === 'error' ? (
        <p className="text-destructive text-sm">{t('editor:validation.failed')}</p>
      ) : null}
      <ul className="flex flex-col gap-3">
        {groups.map((g) => (
          <li key={`${g.severity}:${g.code}`} className="flex flex-col gap-1">
            <div className="flex items-center gap-2 text-sm font-medium">
              {g.severity === 'error' ? (
                <CircleAlertIcon className="text-destructive size-4" aria-hidden="true" />
              ) : (
                <TriangleAlertIcon className="text-chart-4 size-4" aria-hidden="true" />
              )}
              <span data-issue-code={g.code}>
                {t(`editor:issues.${g.code}`, { defaultValue: g.code })}
              </span>
              <Badge variant={g.severity === 'error' ? 'destructive' : 'secondary'}>
                {g.issues.length}
              </Badge>
            </div>
            <ul className="flex flex-wrap gap-1">
              {g.issues.slice(0, 50).map((issue, i) => (
                <li key={i}>
                  <Button variant="outline" size="sm" onClick={() => onPick(issue.refs)}>
                    {issue.refs.map(codeOf).join(' / ') || t('editor:validation.map')}
                  </Button>
                </li>
              ))}
              {g.issues.length > 50 ? (
                <li className="text-muted-foreground text-sm">
                  {t('editor:validation.more', { count: g.issues.length - 50 })}
                </li>
              ) : null}
            </ul>
          </li>
        ))}
      </ul>
    </section>
  );
}
