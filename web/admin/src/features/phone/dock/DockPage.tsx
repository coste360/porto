import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { BanknoteIcon, CalendarCheckIcon, EllipsisIcon, MapIcon, ScanLineIcon } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import { useT } from '@porto/shared/i18n';
import { Tabs, TabsContent } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { DayMap, type MapSelection } from '../components/DayMap';
import { ElementSheet } from '../components/ElementSheet';
import { PhoneTabBar } from '../components/PhoneTabBar';
import { PresenceSheet, type PresenceTarget } from '../components/PresenceSheet';
import { canUseDock } from '../../../lib/landing';
import { DOCK_TABS, type DockTab } from '../lib/tabs';
import { dockSlots } from '../slots';
import { CashTab } from './CashTab';
import { MoreTab } from './MoreTab';
import { ScanTab } from './ScanTab';
import { TodayTab } from './TodayTab';

const route = getRouteApi('/auth/m/$marinaId/dock');

/**
 * Dock phone screen (spec 31.3 Ormeggiatore, m6b-plan §6.2), at /m/$marinaId/dock?tab=: Oggi, Mappa, Scansiona,
 * Cassa, Altro. Arrivals, departures, photos and cash collection go through the offline queue; everything that
 * changes occupancy or numbers a document is online-only. Touch targets ≥ 44 px; works from 360 px wide.
 */
export function DockPage() {
  const { t } = useT();
  const { marinaId, marina, can } = useCurrentMarina();
  const search = route.useSearch();
  const navigate = useNavigate();
  const today = useMarinaToday();
  const tab: string = search.tab ?? 'today';
  const setTab = (next: string) =>
    void navigate({
      to: '/m/$marinaId/dock',
      params: { marinaId },
      search: { tab: next },
      replace: true,
    });
  const [presence, setPresence] = useState<PresenceTarget | null>(null);
  const [selection, setSelection] = useState<MapSelection | null>(null);
  const collect = can('payments.collect') || can('payments.collect_cash');
  const goCash = () => setTab('cash');
  const icons: Record<DockTab, ReactNode> = {
    today: <CalendarCheckIcon />,
    map: <MapIcon />,
    scan: <ScanLineIcon />,
    cash: <BanknoteIcon />,
    more: <EllipsisIcon />,
  };
  // M9: extra tabs registered by other features (DockSlots.extraTabs), shown before "Altro".
  const extraTabs = (dockSlots().extraTabs ?? []).filter((x) => !x.permission || can(x.permission));
  const builtIn = DOCK_TABS.map((value) => ({
    value: value as string,
    label: t(`phone:dock.tabs.${value}`),
    icon: icons[value],
  }));
  const barTabs = [
    ...builtIn.slice(0, -1),
    ...extraTabs.map((x) => ({ value: x.value, label: t(x.labelKey), icon: x.icon })),
    ...builtIn.slice(-1),
  ];
  return (
    <RequirePermission allowed={canUseDock(marina?.permissions ?? [])}>
      <Tabs
        value={tab}
        onValueChange={(v) => {
          if (barTabs.some((x) => x.value === v)) setTab(v);
        }}
        data-testid="dock-page"
        data-tab={tab}
      >
        <h1 className="sr-only">{t('phone:dock.title')}</h1>
        <TabsContent value="today">
          <TodayTab date={today} onPresence={setPresence} onCollect={goCash} />
        </TabsContent>
        <TabsContent value="map">
          {can('map.view') ? (
            <DayMap date={today} onSelect={setSelection} />
          ) : (
            <p className="text-muted-foreground text-sm">{t('dock:notAllowed')}</p>
          )}
        </TabsContent>
        <TabsContent value="scan">
          <ScanTab date={today} onPresence={setPresence} onCollect={goCash} />
        </TabsContent>
        <TabsContent value="cash">
          <CashTab date={today} receiptAction={dockSlots().receiptAction} />
        </TabsContent>
        {extraTabs.map((x) => (
          <TabsContent key={x.value} value={x.value}>
            {x.render(today)}
          </TabsContent>
        ))}
        <TabsContent value="more">
          <MoreTab date={today} />
        </TabsContent>
        <PhoneTabBar tabs={barTabs} />
      </Tabs>
      <ElementSheet
        selection={selection}
        onClose={() => setSelection(null)}
        actions={{
          canPresence: can('presence.write'),
          canCollect: collect,
          onPresence: setPresence,
          onCollect: goCash,
        }}
      />
      <PresenceSheet target={presence} onOpenChange={(o) => !o && setPresence(null)} />
    </RequirePermission>
  );
}
