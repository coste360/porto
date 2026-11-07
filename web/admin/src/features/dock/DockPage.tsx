import { Link } from '@tanstack/react-router';
import { AnchorIcon, CalendarCheckIcon, MapIcon, ShipIcon, UnlockIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useContract, useContracts, type PlacementInput } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Drawer, DrawerContent } from '@porto/shared/ui/drawer';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../auth/hooks';
import { RequirePermission } from '../../components/PageStates';
import { opsMapSearch } from '../operations/public';
import { useMarinaToday } from '../registry/lib/today';
import { ResourceField } from '../desk/components/ResourceField';
import { SheetKindProvider } from '../desk/components/SheetChrome';
import { BlockForm } from '../desk/dialogs/BlockDialog';
import { ReleaseForm } from '../desk/dialogs/ReleaseDialog';
import { UnexpectedArrivalForm } from '../desk/dialogs/UnexpectedArrivalDialog';
import { TodayMovementsPanel } from '../desk/movements/TodayMovementsPanel';

export type DockTab = 'today' | 'map' | 'unexpected' | 'releases';

/**
 * Phone panel of the dock staff (31.3, m3-plan §8.6): today's movements with one-tap presences, the operational
 * map, unexpected arrival, releases and blocks. Touch targets ≥ 44 px; works from 360 px wide.
 */
export function DockPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [tab, setTab] = useState<DockTab>('today');
  return (
    <RequirePermission allowed={can('presence.write') || can('map.view')}>
      <Tabs
        value={tab}
        onValueChange={(v) => setTab(v as DockTab)}
        className="pb-20"
        data-testid="dock-page"
      >
        <h1 className="mb-3 flex items-center gap-2 text-xl font-semibold">
          <AnchorIcon />
          {t('dock:title')}
        </h1>
        <TabsContent value="today">
          <TodayMovementsPanel marinaId={marinaId} date={today} compact />
        </TabsContent>
        <TabsContent value="map" className="flex flex-col gap-3">
          <p className="text-muted-foreground text-sm">{t('dock:mapHint')}</p>
          <Button asChild className="min-h-11">
            <Link
              to="/m/$marinaId/ops"
              params={{ marinaId }}
              search={{ ...opsMapSearch({ date: today }), mode: 'schema' }}
            >
              <MapIcon />
              {t('dock:openMap')}
            </Link>
          </Button>
        </TabsContent>
        <TabsContent value="unexpected">
          {can('presence.write') && can('transits.desk') ? (
            <SheetKindProvider kind="plain">
              <UnexpectedArrivalForm
                marinaId={marinaId}
                open
                onOpenChange={() => setTab('today')}
              />
            </SheetKindProvider>
          ) : (
            <p className="text-muted-foreground text-sm">{t('dock:notAllowed')}</p>
          )}
        </TabsContent>
        <TabsContent value="releases">
          <ReleasesAndBlocks today={today} />
        </TabsContent>
        <TabsList className="bg-background border-border fixed inset-x-0 bottom-0 z-40 grid h-16 w-full grid-cols-4 rounded-none border-t">
          <TabsTrigger value="today" className="flex min-h-11 flex-col gap-0 text-xs">
            <CalendarCheckIcon />
            {t('dock:tabs.today')}
          </TabsTrigger>
          <TabsTrigger value="map" className="flex min-h-11 flex-col gap-0 text-xs">
            <MapIcon />
            {t('dock:tabs.map')}
          </TabsTrigger>
          <TabsTrigger value="unexpected" className="flex min-h-11 flex-col gap-0 text-xs">
            <ShipIcon />
            {t('dock:tabs.unexpected')}
          </TabsTrigger>
          <TabsTrigger value="releases" className="flex min-h-11 flex-col gap-0 text-xs">
            <UnlockIcon />
            {t('dock:tabs.releases')}
          </TabsTrigger>
        </TabsList>
      </Tabs>
    </RequirePermission>
  );
}

/** Releases (contract chosen by code or vessel) and blocks (place chosen in the list), as bottom sheets. */
function ReleasesAndBlocks({ today }: { today: string }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const contracts = useContracts(marinaId, { status: 'active', active_on: today, q: deferred });
  const [contractId, setContractId] = useState<string | null>(null);
  const [target, setTarget] = useState<PlacementInput | null>(null);
  const [blocking, setBlocking] = useState(false);
  if (!can('occupancy.move'))
    return <p className="text-muted-foreground text-sm">{t('dock:notAllowed')}</p>;
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <h2 className="font-semibold">{t('dock:release.title')}</h2>
        <Field>
          <FieldLabel htmlFor="dock-release-q">{t('dock:release.search')}</FieldLabel>
          <Input
            id="dock-release-q"
            type="search"
            value={q}
            maxLength={64}
            onChange={(e) => setQ(e.target.value)}
          />
        </Field>
        <ul className="flex flex-col gap-2">
          {(contracts.data?.pages.flatMap((p) => p.items) ?? []).slice(0, 10).map((c) => (
            <li key={c.id}>
              <Button
                variant="outline"
                className="min-h-11 w-full justify-between"
                onClick={() => setContractId(c.id)}
              >
                <span>{c.vessel.name}</span>
                <span className="text-muted-foreground">
                  {c.resources.map((r) => r.code).join(', ')}
                </span>
              </Button>
            </li>
          ))}
        </ul>
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="font-semibold">{t('dock:block.title')}</h2>
        <ResourceField
          marinaId={marinaId}
          idPrefix="dock-block"
          value={target}
          onChange={setTarget}
          date={today}
          from={today}
          to={today}
        />
        <Button className="min-h-11" disabled={!target} onClick={() => setBlocking(true)}>
          {t('dock:block.open')}
        </Button>
      </section>
      <Drawer open={contractId !== null} onOpenChange={(o) => !o && setContractId(null)}>
        <DrawerContent className="p-4">
          <SheetKindProvider kind="drawer">
            {contractId ? (
              <ReleaseSheet
                contractId={contractId}
                today={today}
                onClose={() => setContractId(null)}
              />
            ) : null}
          </SheetKindProvider>
        </DrawerContent>
      </Drawer>
      <Drawer open={blocking} onOpenChange={setBlocking}>
        <DrawerContent className="p-4">
          <SheetKindProvider kind="drawer">
            {blocking && target ? (
              <BlockForm
                marinaId={marinaId}
                open
                onOpenChange={setBlocking}
                target={target}
                defaultFrom={today}
              />
            ) : null}
          </SheetKindProvider>
        </DrawerContent>
      </Drawer>
    </div>
  );
}

function ReleaseSheet({
  contractId,
  today,
  onClose,
}: {
  contractId: string;
  today: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const contract = useContract(marinaId, contractId);
  if (!contract.data) return null;
  const allocation = contract.data.allocations.find(
    (a) => a.status === 'active' && a.kind === 'contract' && a.last_night >= today,
  );
  if (!allocation) return <p className="text-muted-foreground text-sm">{t('dock:release.none')}</p>;
  return (
    <ReleaseForm
      marinaId={marinaId}
      open
      onOpenChange={(o) => !o && onClose()}
      allocationId={allocation.id}
      defaultFrom={allocation.first_night > today ? allocation.first_night : today}
      defaultSource="dock"
    />
  );
}
