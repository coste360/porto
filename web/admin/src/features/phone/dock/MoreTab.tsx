import { Link } from '@tanstack/react-router';
import {
  ArrowLeftIcon,
  ListIcon,
  LogOutIcon,
  PresentationIcon,
  RulerIcon,
  ShipIcon,
  UnlockIcon,
} from 'lucide-react';
import { useDeferredValue, useMemo, useState, type ReactNode } from 'react';
import { toast } from 'sonner';

import {
  useContract,
  useContracts,
  useMovements,
  useRecordVesselMeasurement,
  type PlacementInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { purgeOfflineData, useOnlineStatus, useQueue } from '@porto/shared/offline';
import { Button } from '@porto/shared/ui/button';
import { Drawer, DrawerContent } from '@porto/shared/ui/drawer';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ConfirmDialog, LengthInput, MadeByCoste360 } from '@porto/shared/ui/patterns';
import { OfflineReason } from '@porto/shared/ui/patterns/OfflineReason';
import { QueueSheet } from '@porto/shared/ui/patterns/QueueSheet';

import { useCurrentMarina, useLogout } from '../../../auth/hooks';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { ResourceField } from '../../desk/components/ResourceField';
import { SheetKindProvider } from '../../desk/components/SheetChrome';
import { BlockForm } from '../../desk/dialogs/BlockDialog';
import { ReleaseForm } from '../../desk/dialogs/ReleaseDialog';
import { UnexpectedArrivalForm } from '../../desk/dialogs/UnexpectedArrivalDialog';
import { canUseDirection } from '../../../lib/landing';
import { ONLINE_ONLY } from '../lib/ops';

// Plural key (description_one / description_other): i18n-check matches literal keys only.
const PLURAL_DESCRIPTION = 'description';

type View = 'menu' | 'unexpected' | 'measure' | 'releases';

/**
 * Altro (m6b-plan §6.2): unexpected arrival, vessel measurement, releases and blocks (all online-only, with the
 * reason inline when offline, D7), the queue, the device and user, the direction screen when allowed, sign-out.
 */
export function MoreTab({ date }: { date: string }) {
  const { t } = useT();
  const { marinaId, marina, me, can } = useCurrentMarina();
  const { online } = useOnlineStatus();
  const [view, setView] = useState<View>('menu');
  const [queueOpen, setQueueOpen] = useState(false);

  if (view !== 'menu')
    return (
      <div className="flex flex-col gap-4">
        <Button variant="ghost" className="min-h-11 self-start" onClick={() => setView('menu')}>
          <ArrowLeftIcon />
          {t('phone:dock.more.back')}
        </Button>
        {!online ? (
          <OfflineReason
            reason={
              ONLINE_ONLY[
                view === 'measure'
                  ? 'measure'
                  : view === 'releases'
                    ? 'release'
                    : 'unexpectedArrival'
              ]!
            }
          />
        ) : view === 'unexpected' ? (
          <SheetKindProvider kind="plain">
            <UnexpectedArrivalForm marinaId={marinaId} open onOpenChange={() => setView('menu')} />
          </SheetKindProvider>
        ) : view === 'measure' ? (
          <MeasureVessel date={date} onDone={() => setView('menu')} />
        ) : (
          <ReleasesAndBlocks today={date} />
        )}
      </div>
    );

  const entry = (
    key: View,
    icon: ReactNode,
    label: string,
    allowed: boolean,
    reason: keyof typeof ONLINE_ONLY,
  ) =>
    allowed ? (
      <li key={key} className="flex flex-col gap-1">
        <Button
          variant="outline"
          className="min-h-11 justify-start"
          disabled={!online}
          onClick={() => setView(key)}
          data-testid={`dock-more-${key}`}
        >
          {icon}
          {label}
        </Button>
        {!online ? <OfflineReason reason={ONLINE_ONLY[reason]!} /> : null}
      </li>
    ) : null;

  return (
    <div className="flex flex-col gap-6" data-testid="dock-more">
      <ul className="flex flex-col gap-2">
        {entry(
          'unexpected',
          <ShipIcon />,
          t('phone:dock.unexpected'),
          can('presence.write') && can('transits.desk'),
          'unexpectedArrival',
        )}
        {entry(
          'measure',
          <RulerIcon />,
          t('phone:dock.measure'),
          can('presence.write') || can('customers.write'),
          'measure',
        )}
        {entry(
          'releases',
          <UnlockIcon />,
          t('phone:dock.more.releasesTitle'),
          can('occupancy.move'),
          'release',
        )}
        <li>
          <Button
            variant="outline"
            className="min-h-11 w-full justify-start"
            onClick={() => setQueueOpen(true)}
          >
            <ListIcon />
            {t('phone:dock.more.queue')}
          </Button>
        </li>
        {canUseDirection(marina?.permissions ?? []) ? (
          <li>
            <Button variant="outline" className="min-h-11 w-full justify-start" asChild>
              <Link
                to="/m/$marinaId/direction"
                params={{ marinaId }}
                data-testid="dock-more-direction"
              >
                <PresentationIcon />
                {t('phone:dock.more.direction')}
              </Link>
            </Button>
          </li>
        ) : null}
      </ul>
      <section className="flex flex-col gap-1 text-sm" data-testid="dock-more-device">
        <h2 className="font-semibold">{t('phone:dock.deviceInfo')}</h2>
        <p>
          <span className="text-muted-foreground">{t('phone:dock.more.marina')}: </span>
          {marina?.name}
        </p>
        <p>
          <span className="text-muted-foreground">{t('phone:dock.more.user')}: </span>
          {me?.user.name}
        </p>
        <p>
          <span className="text-muted-foreground">{t('phone:dock.more.device')}: </span>
          {me?.auth_method === 'device'
            ? t('phone:dock.more.deviceSession')
            : t('phone:dock.more.browserSession')}
        </p>
      </section>
      <LogoutButton marinaId={marinaId} />
      <MadeByCoste360 className="mx-auto" />
      <QueueSheet open={queueOpen} onOpenChange={setQueueOpen} marinaId={marinaId} />
    </div>
  );
}

/** Sign-out: with unsent operations it asks first (they would be lost), then deletes the offline data. */
function LogoutButton({ marinaId }: { marinaId: string }) {
  const { t } = useT();
  const logout = useLogout();
  const queue = useQueue(marinaId);
  const out = () => {
    void purgeOfflineData()
      .catch(() => undefined)
      .finally(() => logout.mutate());
  };
  const button = (
    <Button
      variant="outline"
      className="min-h-11"
      data-testid="dock-logout"
      onClick={queue.pending > 0 ? undefined : out}
    >
      <LogOutIcon />
      {t('phone:dock.logout')}
    </Button>
  );
  if (queue.pending === 0) return button;
  return (
    <ConfirmDialog
      trigger={button}
      title={t('offline:logoutConfirm.title')}
      description={t(`offline:logoutConfirm.${PLURAL_DESCRIPTION}`, { count: queue.pending })}
      confirmLabel={t('offline:logoutConfirm.confirm')}
      cancelLabel={t('common:actions.cancel')}
      destructive
      onConfirm={out}
    />
  );
}

/** Measured dimensions of a vessel of today's movements (online only: they change the fit, D7). */
function MeasureVessel({ date, onDone }: { date: string; onDone: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const movements = useMovements(marinaId, date);
  const record = useRecordVesselMeasurement(marinaId);
  const vessels = useMemo(() => {
    const out = new Map<string, { id: string; name: string; loa: number; beam: number }>();
    const d = movements.data;
    if (d)
      for (const list of [
        d.arrivals_expected,
        d.departures_expected,
        d.arrived,
        d.vessels_out,
        d.arrived_without_booking,
      ])
        for (const m of list)
          out.set(m.vessel.id, {
            id: m.vessel.id,
            name: m.vessel.name,
            loa: m.vessel.loa_cm,
            beam: m.vessel.beam_cm,
          });
    return [...out.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [movements.data]);
  const [vesselId, setVesselId] = useState<string | null>(null);
  const [loa, setLoa] = useState<number | null>(null);
  const [beam, setBeam] = useState<number | null>(null);
  const [draft, setDraft] = useState<number | null>(null);
  const vessel = vessels.find((v) => v.id === vesselId);
  const valid =
    !!vessel &&
    loa !== null &&
    loa > 0 &&
    beam !== null &&
    beam > 0 &&
    (draft === null || draft > 0);
  if (!vessel)
    return (
      <section className="flex flex-col gap-2" data-testid="dock-measure">
        <h2 className="font-semibold">{t('phone:dock.more.measureTitle')}</h2>
        <p className="text-muted-foreground text-sm">{t('phone:dock.more.measureVesselHint')}</p>
        <ul className="flex flex-col gap-2">
          {vessels.map((v) => (
            <li key={v.id}>
              <Button
                variant="outline"
                className="min-h-11 w-full justify-start"
                onClick={() => {
                  setVesselId(v.id);
                  setLoa(v.loa);
                  setBeam(v.beam);
                }}
              >
                {v.name}
              </Button>
            </li>
          ))}
        </ul>
      </section>
    );
  return (
    <form
      className="flex flex-col gap-4"
      data-testid="dock-measure"
      onSubmit={(e) => {
        e.preventDefault();
        if (!valid || loa === null || beam === null) return;
        record.mutate(
          {
            vesselId: vessel.id,
            measurement: {
              loa_cm: loa,
              beam_cm: beam,
              ...(draft !== null ? { draft_cm: draft } : {}),
            },
          },
          {
            onSuccess: () => {
              toast.success(t('phone:dock.more.measureSaved'));
              onDone();
            },
          },
        );
      }}
    >
      <h2 className="font-semibold">
        {t('phone:dock.more.measureTitle')} · {vessel.name}
      </h2>
      <Field>
        <FieldLabel htmlFor="dock-measure-loa">{t('vessels:fields.loa')}</FieldLabel>
        <LengthInput id="dock-measure-loa" value={loa} onChange={setLoa} locale={i18n.language} />
      </Field>
      <Field>
        <FieldLabel htmlFor="dock-measure-beam">{t('vessels:fields.beam')}</FieldLabel>
        <LengthInput
          id="dock-measure-beam"
          value={beam}
          onChange={setBeam}
          locale={i18n.language}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="dock-measure-draft">{t('vessels:fields.draft')}</FieldLabel>
        <LengthInput
          id="dock-measure-draft"
          value={draft}
          onChange={setDraft}
          locale={i18n.language}
        />
      </Field>
      <DeskProblemAlert error={record.error} />
      <Button type="submit" className="min-h-11" disabled={!valid || record.isPending}>
        {t('common:actions.save')}
      </Button>
    </form>
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
    <div className="flex flex-col gap-6" data-testid="dock-releases">
      <section className="flex flex-col gap-3">
        <h2 className="font-semibold">{t('dock:release.title')}</h2>
        <Field>
          <FieldLabel htmlFor="dock-release-q">{t('dock:release.search')}</FieldLabel>
          <Input
            id="dock-release-q"
            type="search"
            className="min-h-11"
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
