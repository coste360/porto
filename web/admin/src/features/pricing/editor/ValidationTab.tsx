import { CircleAlertIcon, TriangleAlertIcon } from 'lucide-react';

import type { PriceListIssue } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Spinner } from '@porto/shared/ui/spinner';

import { issueCounts } from './model';

export type ValidationTabProps = {
  issues: PriceListIssue[];
  /** True when the issues come from the dry run of unsaved changes. */
  live: boolean;
  pending: boolean;
  /** Human name of the row an issue refers to (period code, band code, rule name). */
  describe: (issue: PriceListIssue) => string;
  onGoto: (issue: PriceListIssue) => void;
};

/** Tab Validazione: issues of pricing.Validate; a click opens the field the issue refers to. */
export function ValidationTab({ issues, live, pending, describe, onGoto }: ValidationTabProps) {
  const { t } = useT();
  const counts = issueCounts(issues);
  const sorted = [...issues].sort((a, b) =>
    a.severity === b.severity ? 0 : a.severity === 'error' ? -1 : 1,
  );
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3 text-sm" role="status">
        <Badge variant={counts.errors > 0 ? 'destructive' : 'secondary'}>
          {t('pricing:validation.errors', { count: counts.errors })}
        </Badge>
        <Badge variant="outline">
          {t('pricing:validation.warnings', { count: counts.warnings })}
        </Badge>
        <span className="text-muted-foreground">
          {t(live ? 'pricing:validation.live' : 'pricing:validation.saved')}
        </span>
        {pending ? <Spinner aria-label={t('pricing:validation.checking')} /> : null}
      </div>
      {issues.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('pricing:validation.none')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {sorted.map((issue, i) => {
            const where = describe(issue);
            return (
              <li key={`${issue.code}-${issue.ref.kind}-${issue.ref.id ?? ''}-${i}`}>
                <button
                  type="button"
                  className="hover:bg-muted flex w-full items-start gap-3 rounded-md border p-3 text-left"
                  onClick={() => onGoto(issue)}
                >
                  {issue.severity === 'error' ? (
                    <CircleAlertIcon
                      className="text-destructive mt-0.5 size-4 shrink-0"
                      aria-hidden
                    />
                  ) : (
                    <TriangleAlertIcon
                      className="text-muted-foreground mt-0.5 size-4 shrink-0"
                      aria-hidden
                    />
                  )}
                  <span className="flex flex-col gap-0.5">
                    <span className="text-sm font-medium">
                      {t(`pricingEnums:issueSeverities.${issue.severity}`)}:{' '}
                      {t(`pricingEnums:issues.${issue.code}`, { defaultValue: issue.code })}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      {t(`pricing:validation.refKinds.${issue.ref.kind}`)}
                      {where ? ` · ${where}` : ''}
                      {issue.ref.field ? ` · ${issue.ref.field}` : ''}
                    </span>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
