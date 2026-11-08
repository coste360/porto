import { LayoutDashboardIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import { useDashboard, useDayView, useMaps, useMovements } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { TileCard } from '../../dashboard/components/TileCard';
import { useCanSeeDashboard } from '../../dashboard/lib/access';
import { presetPeriod } from '../../dashboard/lib/period';
import { StaleSince } from './StaleSince';

const PHONE_PRESETS = ['mtd', 'ytd'] as const;
type PhonePreset = (typeof PHONE_PRESETS)[number];

/**
 * Dashboard tab of the direction phone screen (m6b-plan §6.3, m7-plan §10.1): the dashboard tiles the role may
 * see in one column (compact cards, month or year to date) above today's arrivals, departures and vessels in
 * port. A tile opens its report page.
 */
export function DirectionDashboardTab({ date }: { date: string }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const movements = useMovements(marinaId, date);
  const maps = useMaps(marinaId);
  const day = useDayView(marinaId, can('map.view') ? maps.data?.[0]?.id : undefined, date);
  const inPort = useMemo(
    () => day.data?.elements.filter((e) => e.vessel && e.presence?.state === 'in_port').length,
    [day.data],
  );
  const counts: [string, number | undefined][] = [
    [t('phone:direction.arrivals'), movements.data?.arrivals_expected.length],
    [t('phone:direction.departures'), movements.data?.departures_expected.length],
    [t('phone:direction.inPort'), inPort],
  ];
  return (
    <div className="flex flex-col gap-4" data-testid="direction-dashboard">
      <StaleSince updatedAt={movements.dataUpdatedAt} />
      <Card>
        <CardHeader>
          <CardTitle>{t('phone:direction.today')}</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-3 gap-2 text-center">
            {counts.map(([label, n]) => (
              <div key={label} className="flex flex-col gap-1" data-testid="direction-count">
                <dt className="text-muted-foreground text-xs">{label}</dt>
                <dd className="text-2xl font-semibold tabular-nums">{n ?? '–'}</dd>
              </div>
            ))}
          </dl>
        </CardContent>
      </Card>
      <DirectionTiles today={date} />
    </div>
  );
}

function DirectionTiles({ today }: { today: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const allowed = useCanSeeDashboard();
  const [preset, setPreset] = useState<PhonePreset>('mtd');
  const period = presetPeriod(preset, today);
  const dashboard = useDashboard(marinaId, period, allowed);
  if (!allowed) return null;
  const tiles = dashboard.data?.tiles.filter((x) => x.available) ?? [];
  const linkPeriod = dashboard.data
    ? { from: dashboard.data.period.from, to: dashboard.data.period.to }
    : period;
  return (
    <section
      className="flex flex-col gap-3"
      aria-labelledby="direction-tiles-title"
      data-testid="direction-tiles"
    >
      <div className="flex items-center justify-between gap-2">
        <h2 id="direction-tiles-title" className="flex items-center gap-2 text-base font-semibold">
          <LayoutDashboardIcon className="size-4" />
          {t('phone:direction.indicators')}
        </h2>
        <ToggleGroup
          type="single"
          variant="outline"
          value={preset}
          onValueChange={(v) => v && setPreset(v as PhonePreset)}
          aria-label={t('dashboard:period')}
        >
          {PHONE_PRESETS.map((p) => (
            <ToggleGroupItem key={p} value={p} className="min-h-11 px-3">
              {t(`dashboard:periods.${p}`)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {dashboard.isPending ? (
        <LoadingState rows={3} />
      ) : dashboard.isError ? (
        <QueryErrorState error={dashboard.error} onRetry={() => void dashboard.refetch()} />
      ) : tiles.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('dashboard:empty')}</p>
      ) : (
        <div className="flex flex-col gap-3">
          {tiles.map((tile) => (
            <TileCard key={tile.key} tile={tile} marinaId={marinaId} period={linkPeriod} compact />
          ))}
        </div>
      )}
    </section>
  );
}
