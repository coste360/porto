import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { CalendarClockIcon, LayoutDashboardIcon, MapIcon } from 'lucide-react';
import { useState } from 'react';

import { useDeadlines } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Tabs, TabsContent } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { useMarinaToday } from '../registry/lib/today';
import { addDays } from '../desk/lib/dates';
import { DayMap, type MapSelection } from './components/DayMap';
import { DirectionDashboardTab } from './components/DirectionDashboardTab';
import { ElementSheet } from './components/ElementSheet';
import { PhoneTabBar } from './components/PhoneTabBar';
import { StaleSince } from './components/StaleSince';
import { canUseDirection } from '../../lib/landing';
import { groupByWeek } from './lib/direction';
import { DIRECTION_TABS, type DirectionTab } from './lib/tabs';

const route = getRouteApi('/auth/m/$marinaId/direction');

/** Direction phone screen (spec 31.3 Direzione, m6b-plan §6.3, D20, m7-plan §10.1): dashboard tiles, map, deadlines. */
export function DirectionPage() {
  const { t } = useT();
  const { marinaId, marina, can } = useCurrentMarina();
  const search = route.useSearch();
  const navigate = useNavigate();
  const today = useMarinaToday();
  const tab: DirectionTab = search.tab ?? 'dashboard';
  const [selection, setSelection] = useState<MapSelection | null>(null);
  const setTab = (next: DirectionTab) =>
    void navigate({
      to: '/m/$marinaId/direction',
      params: { marinaId },
      search: { tab: next },
      replace: true,
    });
  const icons = {
    dashboard: <LayoutDashboardIcon />,
    map: <MapIcon />,
    deadlines: <CalendarClockIcon />,
  } as const;
  return (
    <RequirePermission allowed={canUseDirection(marina?.permissions ?? [])}>
      <Tabs
        value={tab}
        onValueChange={(v) => {
          if ((DIRECTION_TABS as readonly string[]).includes(v)) setTab(v as DirectionTab);
        }}
        data-testid="direction-page"
        data-tab={tab}
      >
        <h1 className="sr-only">{t('phone:direction.title')}</h1>
        <TabsContent value="dashboard">
          <DirectionDashboardTab date={today} />
        </TabsContent>
        <TabsContent value="map">
          {can('map.view') ? (
            <DayMap date={today} onSelect={setSelection} />
          ) : (
            <p className="text-muted-foreground text-sm">{t('dock:notAllowed')}</p>
          )}
        </TabsContent>
        <TabsContent value="deadlines">
          <DeadlinesTab today={today} />
        </TabsContent>
        <PhoneTabBar
          tabs={DIRECTION_TABS.map((value) => ({
            value,
            label: t(`phone:direction.tabs.${value}`),
            icon: icons[value],
          }))}
        />
      </Tabs>
      <ElementSheet selection={selection} onClose={() => setSelection(null)} />
    </RequirePermission>
  );
}

function DeadlinesTab({ today }: { today: string }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const allowed = can('customers.read');
  if (!allowed)
    return <p className="text-muted-foreground text-sm">{t('phone:direction.notAllowed')}</p>;
  return <DeadlinesList today={today} marinaId={marinaId} />;
}

function DeadlinesList({ today, marinaId }: { today: string; marinaId: string }) {
  const { t } = useT();
  const deadlines = useDeadlines(marinaId, { status: 'open', due_before: addDays(today, 31) });
  if (deadlines.isPending) return <LoadingState rows={3} />;
  if (deadlines.isError)
    return <QueryErrorState error={deadlines.error} onRetry={() => void deadlines.refetch()} />;
  const items = deadlines.data.pages.flatMap((p) => p.items);
  const weeks = groupByWeek(items, today);
  return (
    <div className="flex flex-col gap-4" data-testid="direction-deadlines">
      <StaleSince updatedAt={deadlines.dataUpdatedAt} />
      {weeks.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('phone:direction.deadlinesNone')}</p>
      ) : (
        weeks.map((w) => (
          <section key={w.weekStart} className="flex flex-col gap-2" data-testid="direction-week">
            <h2 className="text-sm font-semibold">
              {t('phone:direction.week', { date: formatIsoDate(w.weekStart) })}
            </h2>
            <ul className="flex flex-col gap-2">
              {w.items.map((d) => (
                <li
                  key={d.id}
                  className="border-border flex flex-col rounded-md border p-3 text-sm"
                >
                  <span className="font-medium">
                    {t(`deadlines:kinds.${d.kind}`)} · {formatIsoDate(d.due_on)}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {[d.vessel?.name, d.customer?.display_name].filter(Boolean).join(' · ')}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))
      )}
      <Button variant="outline" className="min-h-11" asChild>
        <Link to="/m/$marinaId/deadlines" params={{ marinaId }}>
          {t('phone:direction.openFull')}
        </Link>
      </Button>
    </div>
  );
}
