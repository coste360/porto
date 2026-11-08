import { BanknoteIcon, CircleAlertIcon } from 'lucide-react';
import { useMemo } from 'react';

import { useMapPaymentStatus, useMaps, useMovements, type Movement } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { hasPaymentWarning } from '@porto/shared/map';
import { useQueue } from '@porto/shared/offline';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { useCollectibles } from '../api';
import type { PresenceTarget } from '../components/PresenceSheet';
import { StaleSince } from '../components/StaleSince';
import { canQueuePresence, nextAction, queuedPresences, type PresenceEvent } from '../lib/ops';

type SectionKey =
  'arrivals_expected' | 'departures_expected' | 'vessels_out' | 'arrived_without_booking';

const SECTIONS: { key: SectionKey; action: PresenceEvent | null }[] = [
  { key: 'arrivals_expected', action: 'arrived' },
  { key: 'departures_expected', action: 'departed' },
  { key: 'vessels_out', action: 'arrived' },
  { key: 'arrived_without_booking', action: null },
];

/**
 * Oggi (m6b-plan §6.2): expected arrivals and departures, vessels out, arrivals without booking, with one-tap
 * Arrivato / Partito on rows that have an allocation, the "In coda" overlay of queued presences (arrive then
 * depart offline), Incassa when the vessel has something to collect, and the departure payment warning.
 */
export function TodayTab({
  date,
  onPresence,
  onCollect,
}: {
  date: string;
  onPresence: (target: PresenceTarget) => void;
  onCollect: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const movements = useMovements(marinaId, date);
  const queue = useQueue(marinaId);
  const queued = useMemo(() => queuedPresences(queue.ops), [queue.ops]);
  const warned = usePaymentWarnings(marinaId, date, can('map.view'));
  const collect = can('payments.collect') || can('payments.collect_cash');
  const collectibles = useCollectibles(marinaId, date, collect);
  const toCollect = useMemo(
    () => new Set((collectibles.data?.items ?? []).map((c) => c.vessel.id)),
    [collectibles.data],
  );
  if (movements.isPending) return <LoadingState rows={4} />;
  if (movements.isError)
    return <QueryErrorState error={movements.error} onRetry={() => void movements.refetch()} />;
  const data = movements.data;
  const row = (m: Movement, key: SectionKey, action: PresenceEvent | null) => {
    const events = queued.get(m.vessel.id);
    const next = can('presence.write') && canQueuePresence(m) ? nextAction(action, events) : null;
    const warning =
      key === 'departures_expected' &&
      m.resource &&
      warned.has(`${m.resource.type}:${m.resource.id}`);
    return (
      <li
        key={`${key}-${m.vessel.id}-${m.allocation_id ?? m.presence_id ?? ''}`}
        className="border-border flex flex-col gap-2 rounded-md border p-3"
        data-testid={`dock-row-${m.vessel.id}`}
      >
        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 flex-col">
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
          <div className="flex shrink-0 flex-col items-end gap-1">
            {events?.length ? (
              <Badge variant="secondary" data-testid="dock-row-queued">
                {t('offline:queuedBadge')}
              </Badge>
            ) : null}
            {warning ? (
              <Badge variant="outline" data-testid="dock-row-payment-warning">
                <CircleAlertIcon />
                {t('phone:dock.paymentWarning')}
              </Badge>
            ) : null}
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {next && m.allocation_id && m.resource ? (
            <Button
              className="min-h-11 flex-1"
              onClick={() =>
                onPresence({
                  vessel: { id: m.vessel.id, name: m.vessel.name },
                  allocationId: m.allocation_id!,
                  resourceId: m.resource!.id,
                  event: next,
                })
              }
            >
              {next === 'arrived' ? t('phone:dock.arrived') : t('phone:dock.departed')}
            </Button>
          ) : null}
          {collect && toCollect.has(m.vessel.id) ? (
            <Button variant="outline" className="min-h-11 flex-1" onClick={onCollect}>
              <BanknoteIcon />
              {t('phone:dock.collect')}
            </Button>
          ) : null}
        </div>
      </li>
    );
  };
  return (
    <section
      className="flex flex-col gap-4"
      data-testid="dock-today"
      aria-label={t('phone:dock.tabs.today')}
    >
      <StaleSince updatedAt={movements.dataUpdatedAt} />
      {SECTIONS.map(({ key, action }) => {
        const items = data[key];
        return (
          <div key={key} className="flex flex-col gap-2" data-testid={`desk-movements-${key}`}>
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              {t(`phone:dock.sections.${key}`)}
              <Badge variant="secondary">{items.length}</Badge>
            </h2>
            {items.length === 0 ? (
              <p className="text-muted-foreground text-sm">{t('phone:dock.none')}</p>
            ) : (
              <ul className="flex flex-col gap-2">{items.map((m) => row(m, key, action))}</ul>
            )}
          </div>
        );
      })}
      <p className="text-muted-foreground text-sm" data-testid="dock-done-counts">
        {t('desk:movements.summary', {
          label: t('desk:movements.arrived'),
          count: data.arrived.length,
        })}
        {' · '}
        {t('desk:movements.summary', {
          label: t('desk:movements.departed'),
          count: data.departed.length,
        })}
      </p>
    </section>
  );
}

/** Resources whose balance deserves a look at departure (enum states only, never amounts; never a block). */
function usePaymentWarnings(marinaId: string, date: string, enabled: boolean): Set<string> {
  const maps = useMaps(marinaId);
  const mapId = enabled ? (maps.data?.[0]?.id ?? '') : '';
  const status = useMapPaymentStatus(marinaId, mapId, date, !!mapId);
  return useMemo(
    () =>
      new Set(
        (status.data?.resources ?? [])
          .filter((r) => hasPaymentWarning(r))
          .map((r) => `${r.resource_type}:${r.resource_id}`),
      ),
    [status.data],
  );
}
