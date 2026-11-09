import { Link } from '@tanstack/react-router';
import { useMemo, useState } from 'react';

import {
  useCreateSensorDevice,
  useMap,
  useMaps,
  useRevokeSensorDeviceKey,
  useRotateSensorDeviceKey,
  useSensorDevices,
  useSensorGateways,
  type SensorDevice,
  type SensorDeviceInput,
  type SensorKind,
  type SensorResourceType,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  DEFAULT_LAYERS,
  HarborMap,
  buildScene,
  entriesOf,
  indexScene,
  useMapViewport,
} from '@porto/shared/map';
import { ProblemAlert } from '@porto/shared/ui/patterns';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { EnumSelect } from '../../contracts-m4/components/kit';
import { localName } from '../../maps/lib';
import { RESOURCE_TYPES, SENSOR_KINDS, cmToMetres, metresToCm, segmentIssue } from '../lib';
import { KeyRevealDialog } from './KeyRevealDialog';

/** Devices (m12-plan §16.1): table with health and battery, creation with the map picker, key rotate and revoke. */
export function DevicesTab({
  marinaId,
  canConfigure,
}: {
  marinaId: string;
  canConfigure: boolean;
}) {
  const { t, i18n } = useT();
  const devices = useSensorDevices(marinaId);
  const [creating, setCreating] = useState(false);
  const [revealed, setRevealed] = useState<{ key: string; code: string } | null>(null);
  const [rotating, setRotating] = useState<SensorDevice | null>(null);
  const revoke = useRevokeSensorDeviceKey(marinaId);
  const fmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
  if (devices.isPending) return <LoadingState rows={4} />;
  if (devices.isError)
    return <QueryErrorState error={devices.error} onRetry={() => void devices.refetch()} />;
  return (
    <div className="flex flex-col gap-4">
      {canConfigure ? (
        <div>
          <Button onClick={() => setCreating(true)} data-testid="device-new">
            {t('sensors:devices.new')}
          </Button>
        </div>
      ) : null}
      <ProblemAlert error={revoke.error} />
      {devices.data.items.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('sensors:empty')}</p>
      ) : (
        <Table data-testid="devices-table">
          <TableHeader>
            <TableRow>
              <TableHead>{t('sensors:devices.code')}</TableHead>
              <TableHead>{t('sensors:devices.resource')}</TableHead>
              <TableHead>{t('sensors:devices.kind')}</TableHead>
              <TableHead>{t('sensors:devices.physical')}</TableHead>
              <TableHead>{t('sensors:devices.health')}</TableHead>
              <TableHead className="text-right">{t('sensors:devices.battery')}</TableHead>
              <TableHead>{t('sensors:devices.lastContact')}</TableHead>
              <TableHead>{t('sensors:devices.key')}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {devices.data.items.map((d) => (
              <TableRow key={d.id} data-testid={`device-row-${d.code}`}>
                <TableCell className="font-medium">{d.code}</TableCell>
                <TableCell>
                  {d.resource_code}
                  {d.from_cm !== undefined && d.to_cm !== undefined
                    ? ` (${cmToMetres(d.from_cm)}–${cmToMetres(d.to_cm)} m)`
                    : ''}
                  {d.pedestal_socket ? ` · ${d.pedestal_socket}` : ''}
                </TableCell>
                <TableCell>{t(`sensors:kind.${d.kind}`)}</TableCell>
                <TableCell>{t(`sensors:physical.${d.physical}`)}</TableCell>
                <TableCell>
                  <Badge variant={d.health === 'ok' ? 'secondary' : 'destructive'}>
                    {t(`sensors:health.${d.health}`)}
                  </Badge>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {d.battery_pct !== undefined ? `${d.battery_pct}%` : '–'}
                </TableCell>
                <TableCell>
                  {d.last_contact_at ? fmt.format(new Date(d.last_contact_at)) : '–'}
                </TableCell>
                <TableCell data-testid={`device-key-${d.code}`}>
                  {t(`sensors:keyStatus.${d.key_status}`)}
                </TableCell>
                <TableCell className="flex flex-wrap justify-end gap-2">
                  <Link
                    to="/m/$marinaId/sensors/history"
                    params={{ marinaId }}
                    search={{ resource_type: d.resource_type, resource_id: d.resource_id }}
                    className="text-sm underline-offset-4 hover:underline"
                  >
                    {t('sensors:alerts.history')}
                  </Link>
                  {canConfigure ? (
                    <>
                      <Button
                        size="sm"
                        variant="outline"
                        data-testid={`device-rotate-${d.code}`}
                        onClick={() => setRotating(d)}
                      >
                        {t('sensors:devices.rotate')}
                      </Button>
                      {d.key_status !== 'revoked' ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={revoke.isPending}
                          onClick={() => revoke.mutate({ id: d.id, version: d.version })}
                        >
                          {t('sensors:devices.revoke')}
                        </Button>
                      ) : null}
                    </>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {creating ? (
        <DeviceDialog
          marinaId={marinaId}
          onClose={() => setCreating(false)}
          onCreated={(key, code) => {
            setCreating(false);
            setRevealed({ key, code });
          }}
        />
      ) : null}
      {rotating ? (
        <RotateDialog
          marinaId={marinaId}
          device={rotating}
          onClose={() => setRotating(null)}
          onRotated={(key) => {
            setRevealed({ key, code: rotating.code });
            setRotating(null);
          }}
        />
      ) : null}
      {revealed ? (
        <KeyRevealDialog
          secret={revealed.key}
          code={revealed.code}
          onClose={() => setRevealed(null)}
        />
      ) : null}
    </div>
  );
}

function RotateDialog({
  marinaId,
  device,
  onClose,
  onRotated,
}: {
  marinaId: string;
  device: SensorDevice;
  onClose: () => void;
  onRotated: (key: string) => void;
}) {
  const { t } = useT();
  const rotate = useRotateSensorDeviceKey(marinaId);
  const [grace, setGrace] = useState('24');
  const hours = Number(grace);
  const valid = Number.isInteger(hours) && hours >= 0 && hours <= 168;
  return (
    <Dialog open onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent data-testid="rotate-dialog">
        <DialogHeader>
          <DialogTitle>{t('sensors:devices.rotate')}</DialogTitle>
          <DialogDescription>
            {t('sensors:devices.rotateHint', { code: device.code })}
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="rotate-grace">{t('sensors:devices.grace')}</FieldLabel>
          <Input
            id="rotate-grace"
            inputMode="numeric"
            value={grace}
            onChange={(e) => setGrace(e.target.value)}
            aria-invalid={!valid}
          />
        </Field>
        <ProblemAlert error={rotate.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!valid || rotate.isPending}
            data-testid="rotate-submit"
            onClick={() =>
              rotate.mutate(
                { id: device.id, version: device.version, graceHours: hours },
                { onSuccess: (r) => onRotated(r.key) },
              )
            }
          >
            {t('sensors:devices.rotate')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const CODE_RE = /^[A-Z0-9_-]{1,32}$/;
const EXT_RE = /^[A-Za-z0-9._:-]{1,64}$/;

/** Creation of a device: kind, codes, the resource picked on the map (berth click, segment in metres, pedestal). */
function DeviceDialog({
  marinaId,
  onClose,
  onCreated,
}: {
  marinaId: string;
  onClose: () => void;
  onCreated: (key: string, code: string) => void;
}) {
  const { t, i18n } = useT();
  const create = useCreateSensorDevice(marinaId);
  const gateways = useSensorGateways(marinaId);
  const maps = useMaps(marinaId);
  const [mapId, setMapId] = useState('');
  const current = mapId || maps.data?.[0]?.id || '';
  const doc = useMap(marinaId, current).data;
  const [kind, setKind] = useState<SensorKind>('ultrasonic');
  const [code, setCode] = useState('');
  const [ext, setExt] = useState('');
  const [type, setType] = useState<SensorResourceType>('berth');
  const [resource, setResource] = useState('');
  const [from, setFrom] = useState('0');
  const [to, setTo] = useState('');
  const [socket, setSocket] = useState('');
  const [gateway, setGateway] = useState('');
  const content = useMemo(
    () => ({ minX: 0, minY: 0, maxX: doc?.width_cm ?? 10_000, maxY: doc?.height_cm ?? 10_000 }),
    [doc?.width_cm, doc?.height_cm],
  );
  const { viewport, bindContainer, bindSvg, gestures } = useMapViewport(content);
  const scene = useMemo(() => (doc ? buildScene(entriesOf(doc)) : []), [doc]);
  const index = useMemo(() => indexScene(scene), [scene]);
  const options = useMemo(() => {
    if (!doc) return [];
    if (type === 'berth')
      return doc.berths.filter((b) => b.id).map((b) => ({ id: b.id!, code: b.code }));
    if (type === 'dynamic_area')
      return doc.dynamic_areas
        .filter((a) => a.id && a.kind === 'linear')
        .map((a) => ({ id: a.id!, code: a.code }));
    return doc.pedestals.filter((p) => p.id).map((p) => ({ id: p.id!, code: p.code }));
  }, [doc, type]);
  const area = doc?.dynamic_areas.find((a) => a.id === resource);
  const areaLength =
    area?.from_cm !== undefined && area?.to_cm !== undefined
      ? area.to_cm - area.from_cm
      : undefined;
  const sockets = doc?.pedestals.find((p) => p.id === resource)?.sockets ?? [];
  const fromCm = metresToCm(from);
  const toCm = metresToCm(to);
  const segIssue = type === 'dynamic_area' ? segmentIssue(fromCm, toCm, areaLength) : null;
  const valid =
    CODE_RE.test(code) &&
    EXT_RE.test(ext) &&
    options.some((o) => o.id === resource) &&
    segIssue === null;
  const pick = (ref: string) => {
    if (options.some((o) => o.id === ref)) setResource(ref);
  };
  const submit = () => {
    const body: SensorDeviceInput = {
      external_id: ext,
      code,
      kind,
      resource_type: type,
      resource_id: resource,
    };
    if (type === 'dynamic_area' && fromCm !== null && toCm !== null)
      Object.assign(body, { from_cm: fromCm, to_cm: toCm });
    if (type === 'pedestal' && socket) body.pedestal_socket = socket;
    if (gateway) body.gateway_id = gateway;
    create.mutate(body, { onSuccess: (r) => onCreated(r.key, r.device.code) });
  };
  return (
    <Dialog open onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent className="sm:max-w-3xl" data-testid="device-dialog">
        <DialogHeader>
          <DialogTitle>{t('sensors:devices.new')}</DialogTitle>
          <DialogDescription>{t('sensors:devices.newHint')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-3">
          <Field className="w-40">
            <FieldLabel htmlFor="device-code">{t('sensors:devices.code')}</FieldLabel>
            <Input
              id="device-code"
              value={code}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
            />
          </Field>
          <Field className="w-56">
            <FieldLabel htmlFor="device-ext">{t('sensors:devices.externalId')}</FieldLabel>
            <Input id="device-ext" value={ext} onChange={(e) => setExt(e.target.value)} />
          </Field>
          <EnumSelect
            id="device-kind"
            label={t('sensors:devices.kind')}
            values={SENSOR_KINDS}
            value={kind}
            onChange={(v) => v && setKind(v)}
            labelOf={(v) => t(`sensors:kind.${v}`)}
          />
          <EnumSelect
            id="device-type"
            label={t('sensors:devices.resourceType')}
            values={RESOURCE_TYPES}
            value={type}
            onChange={(v) => {
              if (!v) return;
              setType(v);
              setResource('');
            }}
            labelOf={(v) => t(`sensors:resourceType.${v}`)}
          />
          {(maps.data?.length ?? 0) > 1 ? (
            <EnumSelect
              id="device-map"
              label={t('sensors:devices.map')}
              values={(maps.data ?? []).map((m) => m.id)}
              value={current}
              onChange={(v) => {
                setMapId(v);
                setResource('');
              }}
              labelOf={(id) => localName(maps.data?.find((m) => m.id === id)?.name, i18n.language)}
            />
          ) : null}
          {(gateways.data?.items.length ?? 0) > 0 ? (
            <EnumSelect
              id="device-gateway"
              label={t('sensors:devices.gateway')}
              values={(gateways.data?.items ?? []).map((g) => g.id)}
              value={gateway}
              onChange={setGateway}
              labelOf={(id) => gateways.data?.items.find((g) => g.id === id)?.code ?? id}
              allLabel={t('sensors:devices.ownKey')}
            />
          ) : null}
        </div>
        <div className="grid gap-3 md:grid-cols-3">
          <div
            ref={bindContainer}
            className="bg-muted aspect-video w-full overflow-hidden rounded-md border md:col-span-2"
            data-testid="device-map"
          >
            {doc ? (
              <HarborMap
                svgRef={bindSvg}
                meta={doc}
                scene={scene}
                index={index}
                viewport={viewport}
                layers={DEFAULT_LAYERS}
                selected={resource ? new Set([resource]) : new Set()}
                interactive
                role="group"
                aria-label={t('sensors:devices.mapPicker')}
                onElementPointerDown={(ref) => pick(ref)}
                onElementActivate={(ref) => pick(ref)}
                onBackgroundPointerDown={(e) => gestures.onPointerDown(e, true)}
                onPointerUp={(e) => gestures.onPointerUp(e)}
              />
            ) : null}
          </div>
          <div className="flex flex-col gap-3">
            <EnumSelect
              id="device-resource"
              label={t('sensors:devices.resource')}
              values={options.map((o) => o.id)}
              value={resource}
              onChange={setResource}
              labelOf={(id) => options.find((o) => o.id === id)?.code ?? id}
            />
            {type === 'dynamic_area' ? (
              <>
                <Field>
                  <FieldLabel htmlFor="device-from">{t('sensors:devices.fromM')}</FieldLabel>
                  <Input
                    id="device-from"
                    inputMode="decimal"
                    value={from}
                    onChange={(e) => setFrom(e.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="device-to">{t('sensors:devices.toM')}</FieldLabel>
                  <Input
                    id="device-to"
                    inputMode="decimal"
                    value={to}
                    onChange={(e) => setTo(e.target.value)}
                  />
                </Field>
                {areaLength !== undefined ? (
                  <p className="text-muted-foreground text-xs">
                    {t('sensors:devices.areaLength', { length: cmToMetres(areaLength) })}
                  </p>
                ) : null}
                {segIssue ? (
                  <p className="text-destructive text-sm" role="alert">
                    {t(`sensors:devices.${segIssue}`)}
                  </p>
                ) : null}
              </>
            ) : null}
            {type === 'pedestal' && sockets.length > 0 ? (
              <EnumSelect
                id="device-socket"
                label={t('sensors:devices.socket')}
                values={sockets.map((s) => s.code)}
                value={socket}
                onChange={setSocket}
                labelOf={(c) => c}
                allLabel={t('sensors:devices.noSocket')}
              />
            ) : null}
            <p className="text-sm" data-testid="device-picked">
              {resource
                ? t('sensors:devices.picked', {
                    code: options.find((o) => o.id === resource)?.code ?? '',
                  })
                : t('sensors:devices.pickHint')}
            </p>
          </div>
        </div>
        <ProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!valid || create.isPending}
            onClick={submit}
            data-testid="device-submit"
          >
            {t('sensors:devices.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
