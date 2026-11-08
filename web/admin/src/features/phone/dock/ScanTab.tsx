import { BanknoteIcon, CameraIcon, SearchIcon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';
import { toast } from 'sonner';

import {
  PRESENCE_PHOTO_MAX_BYTES,
  PRESENCE_PHOTO_TYPES,
  useDayView,
  useMapPaymentStatus,
  useMaps,
  useMovements,
  useTransits,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { enqueue, useOnlineStatus } from '@porto/shared/offline';
import { CameraScanner, parseTransitQr, useKeyboardWedge } from '@porto/shared/scan';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@porto/shared/ui/drawer';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { OfflineReason } from '@porto/shared/ui/patterns/OfflineReason';

import { useCurrentMarina } from '../../../auth/hooks';
import { useContractDetail } from '../api';
import type { PresenceTarget } from '../components/PresenceSheet';
import { reencodePhoto } from '../lib/photo';
import { resolveFromDay } from '../lib/scan';

const API = '/api/admin/v1/marinas';

/**
 * Scansiona (m6b-plan §6.2, §6.4): camera QR (and USB keyboard-wedge readers), resolved from today's data on the
 * phone (cached offline), then online from the contract; manual search by transit code online. The card shows
 * vessel, berth, nights, payment state and the actions Arrivato / Partito / Incassa / Foto.
 */
export function ScanTab({
  date,
  onPresence,
  onCollect,
}: {
  date: string;
  onPresence: (target: PresenceTarget) => void;
  onCollect: () => void;
}) {
  const { t } = useT();
  const [scanned, setScanned] = useState<string | null>(null);
  const [invalid, setInvalid] = useState(false);
  const accept = (text: string) => {
    const qr = parseTransitQr(text);
    if (!qr) {
      setInvalid(true);
      return;
    }
    setInvalid(false);
    setScanned(qr.contractId);
  };
  useKeyboardWedge(accept);
  return (
    <div className="flex flex-col gap-4" data-testid="dock-scan">
      {scanned ? (
        <TransitCard
          contractId={scanned}
          date={date}
          onClose={() => setScanned(null)}
          onPresence={onPresence}
          onCollect={onCollect}
        />
      ) : (
        <CameraScanner onScan={accept} />
      )}
      {invalid ? (
        <p className="text-destructive text-sm" role="alert">
          {t('phone:scan.invalid')}
        </p>
      ) : null}
      {scanned ? null : <ManualSearch onPick={setScanned} />}
    </div>
  );
}

function ManualSearch({ onPick }: { onPick: (contractId: string) => void }) {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const { online } = useOnlineStatus();
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const enabled = online && can('contracts.read') && deferred.length >= 3;
  return (
    <section className="flex flex-col gap-2" data-testid="dock-scan-manual">
      <Field>
        <FieldLabel htmlFor="dock-scan-q">{t('phone:scan.manualLabel')}</FieldLabel>
        <Input
          id="dock-scan-q"
          type="search"
          className="min-h-11"
          value={q}
          maxLength={64}
          disabled={!online}
          placeholder={t('phone:scan.manualPlaceholder')}
          onChange={(e) => setQ(e.target.value)}
        />
      </Field>
      {!online ? <OfflineReason reason="online_only" /> : null}
      {enabled ? <ManualResults q={deferred} onPick={onPick} /> : null}
    </section>
  );
}

function ManualResults({ q, onPick }: { q: string; onPick: (contractId: string) => void }) {
  const { marinaId } = useCurrentMarina();
  const transits = useTransits(marinaId, { q });
  const items = (transits.data?.pages.flatMap((p) => p.items) ?? []).slice(0, 5);
  return (
    <ul className="flex flex-col gap-2">
      {items.map((c) => (
        <li key={c.id}>
          <Button
            variant="outline"
            className="min-h-11 w-full justify-between"
            onClick={() => onPick(c.id)}
          >
            <span className="flex items-center gap-2">
              <SearchIcon />
              {c.code}
            </span>
            <span className="text-muted-foreground">{c.vessel.name}</span>
          </Button>
        </li>
      ))}
    </ul>
  );
}

function TransitCard({
  contractId,
  date,
  onClose,
  onPresence,
  onCollect,
}: {
  contractId: string;
  date: string;
  onClose: () => void;
  onPresence: (target: PresenceTarget) => void;
  onCollect: () => void;
}) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { online } = useOnlineStatus();
  const movements = useMovements(marinaId, date);
  const maps = useMaps(marinaId);
  const mapId = maps.data?.[0]?.id;
  const day = useDayView(marinaId, mapId, date);
  const hit = resolveFromDay(contractId, movements.data, day.data?.elements);
  const detail = useContractDetail(marinaId, contractId, online && can('contracts.read'));
  const payments = useMapPaymentStatus(marinaId, mapId ?? '', date, !!mapId);
  const [photoFor, setPhotoFor] = useState<string | null>(null);

  const c = detail.data;
  if (c && c.kind !== 'transit')
    return <NotFound text={t('phone:dock.scanCard.notTransit')} onClose={onClose} />;
  if (!hit && !c) {
    if (online && detail.isPending)
      return <p className="text-sm">{t('phone:dock.scanCard.searching')}</p>;
    return (
      <NotFound
        text={online ? t('phone:scan.notFound') : t('phone:scan.notFoundOffline')}
        onClose={onClose}
      />
    );
  }
  const active = c?.allocations.find((a) => a.status === 'active');
  const vessel = hit?.movement?.vessel ?? hit?.element?.vessel ?? c?.vessel;
  const resource = hit?.movement?.resource
    ? {
        id: hit.movement.resource.id,
        type: hit.movement.resource.type,
        code: hit.movement.resource.code,
      }
    : hit?.element
      ? { id: hit.element.resource_id, type: hit.element.resource_type, code: hit.element.code }
      : active
        ? { id: active.resource_id, type: active.resource_type, code: active.resource_code ?? '' }
        : undefined;
  const allocationId = hit?.movement?.allocation_id ?? hit?.element?.allocation_id ?? active?.id;
  const action =
    hit?.action ??
    (active ? (c?.presences.at(-1)?.event === 'arrived' ? 'departed' : 'arrived') : null);
  const pay = resource
    ? payments.data?.resources.find(
        (r) => r.resource_id === resource.id && r.resource_type === resource.type,
      )
    : undefined;
  const nights = active
    ? Math.round(
        (Date.parse(`${active.last_night}T00:00:00Z`) -
          Date.parse(`${active.first_night}T00:00:00Z`)) /
          86_400_000,
      ) + 1
    : null;
  const lastPresence = c?.presences.at(-1);
  const code = hit?.code ?? c?.code ?? '';
  return (
    <Card data-testid="dock-scan-card" data-contract={contractId}>
      <CardHeader>
        <CardTitle>{t('phone:dock.scanCard.title', { code })}</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-3 gap-x-3 gap-y-2 text-sm">
          <dt className="text-muted-foreground">{t('phone:dock.map.contract')}</dt>
          <dd className="col-span-2 font-medium">{vessel?.name}</dd>
          {resource ? (
            <>
              <dt className="text-muted-foreground">{t('phone:dock.scanCard.berth')}</dt>
              <dd className="col-span-2">{resource.code}</dd>
            </>
          ) : null}
          {active && nights !== null ? (
            <>
              <dt className="text-muted-foreground">{t('phone:dock.scanCard.nights')}</dt>
              <dd className="col-span-2">
                {t('phone:dock.scanCard.nightsValue', {
                  first: formatIsoDate(active.first_night),
                  last: formatIsoDate(active.last_night),
                  count: nights,
                })}
              </dd>
            </>
          ) : null}
          {c ? (
            <>
              <dt className="text-muted-foreground">{t('phone:dock.scanCard.status')}</dt>
              <dd className="col-span-2">
                {t(`occupancyEnums:transitStatus.${c.status}`, { defaultValue: c.status })}
              </dd>
            </>
          ) : null}
          {pay ? (
            <>
              <dt className="text-muted-foreground">{t('phone:dock.scanCard.payment')}</dt>
              <dd className="col-span-2" data-testid="dock-scan-payment">
                {pay.suspended
                  ? t('billingEnums:lensState.suspended')
                  : t(`billingEnums:lensState.${pay.state}`)}
              </dd>
            </>
          ) : null}
        </dl>
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        {action && can('presence.write') && allocationId && resource && vessel ? (
          <Button
            className="min-h-11 flex-1"
            onClick={() =>
              onPresence({
                vessel: { id: vessel.id, name: vessel.name },
                allocationId,
                resourceId: resource.id,
                event: action,
              })
            }
          >
            {action === 'arrived' ? t('phone:dock.arrived') : t('phone:dock.departed')}
          </Button>
        ) : null}
        {can('payments.collect') || can('payments.collect_cash') ? (
          <Button variant="outline" className="min-h-11 flex-1" onClick={onCollect}>
            <BanknoteIcon />
            {t('phone:dock.collect')}
          </Button>
        ) : null}
        {lastPresence && can('presence.write') && vessel ? (
          <Button
            variant="outline"
            className="min-h-11 flex-1"
            onClick={() => setPhotoFor(lastPresence.id)}
          >
            <CameraIcon />
            {t('phone:dock.photo')}
          </Button>
        ) : null}
        <Button variant="ghost" className="min-h-11" onClick={onClose}>
          {t('phone:dock.scanCard.close')}
        </Button>
      </CardFooter>
      {vessel ? (
        <PhotoSheet
          presenceId={photoFor}
          vessel={{ id: vessel.id, name: vessel.name }}
          onClose={() => setPhotoFor(null)}
        />
      ) : null}
    </Card>
  );
}

function NotFound({ text, onClose }: { text: string; onClose: () => void }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-2" data-testid="dock-scan-not-found">
      <p className="text-sm" role="status">
        {text}
      </p>
      <Button variant="outline" className="min-h-11" onClick={onClose}>
        {t('phone:dock.scanCard.close')}
      </Button>
    </div>
  );
}

/** A photo of the latest presence of the transit (queued, like every presence photo). */
function PhotoSheet({
  presenceId,
  vessel,
  onClose,
}: {
  presenceId: string | null;
  vessel: { id: string; name: string };
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId, me } = useCurrentMarina();
  const [photo, setPhoto] = useState<File | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!photo || !presenceId || !me) return;
    if (!(PRESENCE_PHOTO_TYPES as readonly string[]).includes(photo.type)) {
      setError('desk:presence.photoType');
      return;
    }
    setBusy(true);
    try {
      const blob = await reencodePhoto(photo);
      if (blob.size > PRESENCE_PHOTO_MAX_BYTES) {
        setError('desk:presence.photoSize');
        return;
      }
      await enqueue({
        kind: 'presence.photo',
        userId: me.user.id,
        marinaId,
        lane: `vessel:${vessel.id}`,
        request: {
          operationId: 'uploadPresencePhoto',
          path: `${API}/${encodeURIComponent(marinaId)}/presences/${encodeURIComponent(presenceId)}/photo`,
          contentType: blob.type || 'image/jpeg',
        },
        blob,
        label: { key: 'offline:queue.label.photo', params: { vessel: vessel.name } },
      });
      toast.success(t('phone:dock.presenceSheet.queuedToast'));
      onClose();
    } catch {
      setError('phone:enqueueFailed');
    } finally {
      setBusy(false);
    }
  };
  return (
    <Drawer open={presenceId !== null} onOpenChange={(o) => !o && onClose()}>
      <DrawerContent className="p-4" data-testid="dock-photo-sheet">
        <DrawerHeader className="px-0">
          <DrawerTitle>{t('phone:dock.photo')}</DrawerTitle>
          <DrawerDescription>{vessel.name}</DrawerDescription>
        </DrawerHeader>
        <Field data-invalid={error ? true : undefined}>
          <FieldLabel htmlFor="dock-photo-file">{t('phone:dock.photo')}</FieldLabel>
          <Input
            id="dock-photo-file"
            type="file"
            accept={PRESENCE_PHOTO_TYPES.join(',')}
            capture="environment"
            className="min-h-11"
            onChange={(e) => {
              setError(null);
              setPhoto(e.target.files?.[0] ?? null);
            }}
          />
          {error ? <FieldError>{t(error)}</FieldError> : null}
        </Field>
        <DrawerFooter className="px-0">
          <Button className="min-h-11" disabled={!photo || busy} onClick={() => void submit()}>
            {t('common:actions.save')}
          </Button>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  );
}
