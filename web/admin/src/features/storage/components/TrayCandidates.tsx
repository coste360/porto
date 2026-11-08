import { PlusIcon } from 'lucide-react';

import { useStorageTrayCandidates, type StorageTrayCandidate } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';

// "Da collocare" candidates of a draft scenario (spec 17.2, m9-plan §7.2): boats on another surface area or land
// resource in the window. Adding one brings it into the tray of the working copy; the real state is untouched.

function candidateName(c: StorageTrayCandidate): string {
  return c.vessel?.name ?? c.allocation_id.slice(0, 8);
}

export function TrayCandidates({
  marinaId,
  scenarioId,
  disabled,
  pending,
  onAdd,
}: {
  marinaId: string;
  scenarioId: string;
  /** A dirty draft: adding would reload the copy and lose the unsaved changes. */
  disabled: boolean;
  pending: boolean;
  onAdd: (allocationId: string) => void;
}) {
  const { t } = useT();
  const q = useStorageTrayCandidates(marinaId, scenarioId);
  const items = q.data?.items ?? [];
  return (
    <Card className="gap-2 py-4" data-testid="storage-tray-candidates">
      <CardHeader className="px-4">
        <CardTitle className="text-sm">{t('storage:scenario.trayCandidates')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 px-4 text-sm">
        {disabled ? (
          <p className="text-muted-foreground text-xs">{t('storage:scenario.trayDirtyHint')}</p>
        ) : null}
        {q.isSuccess && items.length === 0 ? (
          <p className="text-muted-foreground">{t('storage:scenario.trayCandidatesEmpty')}</p>
        ) : null}
        <ul className="flex flex-col gap-1">
          {items.map((c) => (
            <li key={c.allocation_id} className="flex items-center justify-between gap-2">
              <span className="min-w-0">
                <span className="font-medium">{candidateName(c)}</span>{' '}
                <span className="text-muted-foreground text-xs">
                  {t('storage:scenario.trayFrom', {
                    resource: c.resource_code,
                    from: formatIsoDate(c.first_night),
                    to: formatIsoDate(c.last_night),
                  })}
                </span>
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={disabled || pending}
                aria-label={t('storage:scenario.trayAddLabel', { name: candidateName(c) })}
                onClick={() => onAdd(c.allocation_id)}
              >
                <PlusIcon />
                {t('storage:scenario.trayAdd')}
              </Button>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
