import type { FormulaIssue } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';

import { column, issueKey } from '../lib';

/** The formula issues of a field with their position (m10-plan §3.5: code, field, pos). */
export function IssueList({
  issues,
  testId,
}: {
  issues: readonly FormulaIssue[];
  testId?: string;
}) {
  const { t } = useT();
  if (issues.length === 0) return null;
  return (
    <Alert variant="destructive" data-testid={testId ?? 'formula-issues'}>
      <AlertDescription>
        <ul className="flex flex-col gap-1">
          {issues.map((i, n) => {
            const col = column(i.pos);
            return (
              <li key={n}>
                {t(issueKey(i.code), { ref: i.ref ?? '', field: i.field ?? '' })}
                {col !== null ? ` ${t('yardCatalog:issues.at', { column: col })}` : ''}
              </li>
            );
          })}
        </ul>
      </AlertDescription>
    </Alert>
  );
}
