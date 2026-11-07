import { LocateIcon } from 'lucide-react';
import { useState } from 'react';

import { useMovements, type Movement } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { PresenceDialog } from '../dialogs/PresenceDialog';
import { MOVEMENT_SECTIONS, type LocateRef } from '../lib/movements';

/** Movements of the day (9.5): arrivals and departures expected, vessels out, arrivals without booking, done ones. */
export function TodayMovementsPanel({
  marinaId,
  date,
  mapId,
  onLocate,
  compact = false,
}: {
  marinaId: string;
  date: string;
  mapId?: string;
  onLocate?: (ref: LocateRef) => void;
  /** Phone layout: larger touch targets, done sections collapsed into counts. */
  compact?: boolean;
}) {
  const { t, i18n } = useT();
  const { can } = useCurrentMarina();
  const movements = useMovements(marinaId, date, mapId);
  const [presence, setPresence] = useState<{ m: Movement; event: 'arrived' | 'departed' } | null>(
    null,
  );
  if (movements.isPending) return <LoadingState rows={4} />;
  if (movements.isError)
    return <QueryErrorState error={movements.error} onRetry={() => void movements.refetch()} />;
  const data = movements.data;
  return (
    <section
      className="flex flex-col gap-4"
      aria-label={t('desk:movements.title')}
      data-testid="desk-movements"
    >
      {MOVEMENT_SECTIONS.map(({ key, action }) => {
        const items = data[key];
        if (compact && action === null && key !== 'arrived_without_booking')
          return (
            <p key={key} className="text-muted-foreground text-sm">
              {t('desk:movements.summary', {
                label: t(`desk:movements.${key}`),
                count: items.length,
              })}
            </p>
          );
        return (
          <div key={key} className="flex flex-col gap-2" data-testid={`desk-movements-${key}`}>
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              {t(`desk:movements.${key}`)}
              <Badge variant="secondary">{items.length}</Badge>
            </h3>
            {items.length === 0 ? (
              <p className="text-muted-foreground text-sm">{t('desk:movements.none')}</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {items.map((m) => (
                  <li
                    key={`${key}-${m.vessel.id}-${m.allocation_id ?? m.presence_id ?? ''}`}
                    className="border-border flex flex-wrap items-center justify-between gap-2 rounded-md border p-2"
                  >
                    <div className="flex flex-col">
                      <span className="font-medium">{m.vessel.name}</span>
                      <span className="text-muted-foreground text-xs">
                        {[
                          m.resource?.code,
                          `${formatCm(m.vessel.loa_cm, i18n.language)} × ${formatCm(m.vessel.beam_cm, i18n.language)}`,
                          m.expected_time,
                          m.contract?.code,
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </div>
                    <div className="flex gap-2">
                      {onLocate && m.resource ? (
                        <Button
                          size={compact ? 'default' : 'sm'}
                          variant="ghost"
                          className={compact ? 'min-h-11 min-w-11' : undefined}
                          aria-label={t('desk:movements.locate', { name: m.vessel.name })}
                          onClick={() =>
                            onLocate({
                              resource_type: m.resource!.type,
                              resource_id: m.resource!.id,
                              ...(m.resource!.map_id ? { map_id: m.resource!.map_id } : {}),
                            })
                          }
                        >
                          <LocateIcon />
                        </Button>
                      ) : null}
                      {action && can('presence.write') ? (
                        <Button
                          size={compact ? 'default' : 'sm'}
                          className={compact ? 'min-h-11' : undefined}
                          onClick={() => setPresence({ m, event: action })}
                        >
                          {action === 'arrived'
                            ? t('desk:movements.arrivedAction')
                            : t('desk:movements.departedAction')}
                        </Button>
                      ) : null}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        );
      })}
      {presence ? (
        <PresenceDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setPresence(null)}
          vesselId={presence.m.vessel.id}
          vesselName={presence.m.vessel.name}
          event={presence.event}
          {...(presence.m.allocation_id ? { allocationId: presence.m.allocation_id } : {})}
        />
      ) : null}
    </section>
  );
}
