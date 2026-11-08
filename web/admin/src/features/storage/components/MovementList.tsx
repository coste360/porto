import { CheckIcon, LockIcon } from 'lucide-react';

import { useCompleteStorageMovement, type StorageMovementList } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { ProblemAlert } from '@porto/shared/ui/patterns';

import { positionText } from '../lib/plan';

// Movement worklist of a day (spec 17.5, m9-plan §7.4): order, from → to, equipment hint, temporary positions and
// the accessibility warnings of the day. Marking done is online only (D18). Used by the office page and the
// yard_mgr phone screen (one card per movement, readable at 360 px).

export function MovementList({
  marinaId,
  list,
  canComplete,
}: {
  marinaId: string;
  list: StorageMovementList;
  canComplete: boolean;
}) {
  const { t, i18n } = useT();
  const done = useCompleteStorageMovement(marinaId);
  const name = (m: StorageMovementList['items'][number]) =>
    m.vessel?.name ?? m.allocation_id.slice(0, 8);
  return (
    <div className="flex flex-col gap-3" data-testid="storage-movements">
      {list.warnings.length > 0 ? (
        <Card className="border-destructive gap-2 py-3" data-testid="storage-movement-warnings">
          <CardHeader className="px-4">
            <CardTitle className="text-base">
              {t('storage:movements.warnings', { count: list.warnings.length })}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-1 px-4 text-sm">
            {list.warnings.map((w) => (
              <p key={`${w.allocation_id}-${w.event}`} className="flex items-center gap-2">
                <LockIcon className="text-destructive size-4" aria-hidden />
                {t(
                  w.event === 'exit'
                    ? 'storage:warning.exitBlockedShort'
                    : 'storage:warning.arrivalBlockedShort',
                  {
                    count: w.blocking.length,
                  },
                )}
              </p>
            ))}
          </CardContent>
        </Card>
      ) : null}
      <ProblemAlert error={done.error} />
      {list.items.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('storage:movements.empty')}</p>
      ) : (
        <ol className="flex flex-col gap-2">
          {list.items.map((m) => (
            <li key={m.id}>
              <Card className="gap-2 py-3" data-movement={m.id} data-status={m.status}>
                <CardHeader className="px-4">
                  <CardTitle className="flex flex-wrap items-center gap-2 text-base">
                    <span className="tabular-nums">{m.seq}.</span>
                    {name(m)}
                    <Badge variant="outline">{t(`storage:equipment.${m.equipment_hint}`)}</Badge>
                    {m.temporary ? (
                      <Badge variant="secondary">{t('storage:movements.temporary')}</Badge>
                    ) : null}
                    <Badge variant={m.status === 'done' ? 'secondary' : 'outline'}>
                      {t(`storage:movementStatus.${m.status}`)}
                    </Badge>
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex flex-wrap items-center justify-between gap-2 px-4 text-sm">
                  <span className="tabular-nums">
                    {t('storage:movements.fromTo', {
                      from: m.from
                        ? positionText(m.from, i18n.language)
                        : t('storage:movements.outside'),
                      to: positionText(m.to, i18n.language),
                    })}
                  </span>
                  {canComplete && m.status === 'pending' ? (
                    <Button
                      size="sm"
                      disabled={done.isPending}
                      onClick={() => done.mutate({ movementId: m.id, version: m.version })}
                    >
                      <CheckIcon />
                      {t('storage:movements.done')}
                    </Button>
                  ) : null}
                </CardContent>
              </Card>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
