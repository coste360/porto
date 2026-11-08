import { PlusIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import {
  useCreateUtilityMeter,
  useMap,
  useMaps,
  useUpdateUtilityMeter,
  useUtilityMeters,
  type MeterKind,
  type UtilityMeter,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
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
import { Label } from '@porto/shared/ui/label';
import { ElementCombobox, PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, IsoDate, M4ProblemAlert, PageFrame } from '../../contracts-m4/components/kit';
import { Qty, UtilitiesNav } from '../components/kit';
import { UNIT_OF } from '../lib';

const METER_KINDS: readonly MeterKind[] = ['power', 'water', 'hours', 'cycles'];

/** Utility meters (m9-plan §9.1): list, new meter prefilled from a pedestal socket, deactivation. */
export function UtilitiesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [kind, setKind] = useState<MeterKind | ''>('');
  const [q, setQ] = useState('');
  const [editing, setEditing] = useState<UtilityMeter | 'new' | null>(null);
  const meters = useUtilityMeters(marinaId, {
    ...(kind ? { kind } : {}),
    ...(q.trim() ? { q: q.trim() } : {}),
  });
  const items = meters.data?.items ?? [];
  return (
    <RequirePermission allowed={can('marina.configure') || can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('utilities:meters.title')}
          description={t('utilities:meters.description')}
          actions={
            can('marina.configure') ? (
              <Button onClick={() => setEditing('new')}>
                <PlusIcon />
                {t('utilities:meters.new')}
              </Button>
            ) : null
          }
        />
        <UtilitiesNav marinaId={marinaId} current="meters" />
        <div className="flex flex-wrap items-end gap-4">
          <EnumSelect
            id="meter-kind"
            label={t('utilities:fields.kind')}
            values={METER_KINDS}
            value={kind}
            onChange={setKind}
            labelOf={(k) => t(`utilities:enums.kind.${k}`)}
            allLabel={t('utilities:all')}
          />
          <Field className="w-auto min-w-48">
            <FieldLabel htmlFor="meter-q">{t('utilities:meters.search')}</FieldLabel>
            <Input id="meter-q" value={q} onChange={(e) => setQ(e.target.value)} />
          </Field>
        </div>
        {meters.isPending ? (
          <LoadingState rows={5} />
        ) : meters.isError ? (
          <QueryErrorState error={meters.error} onRetry={() => void meters.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('utilities:fields.code'),
              t('utilities:fields.kind'),
              t('utilities:fields.socket'),
              t('utilities:fields.lastReading'),
              t('utilities:fields.status'),
              '',
            ]}
            empty={t('utilities:meters.empty')}
            rows={items.length}
          >
            {items.map((m) => (
              <TableRow key={m.id} data-testid="meter-row">
                <TableCell className="font-medium">{m.code}</TableCell>
                <TableCell>{t(`utilities:enums.kind.${m.kind}`)}</TableCell>
                <TableCell>{m.socket_code ?? ''}</TableCell>
                <TableCell>
                  {m.last_reading ? (
                    <span className="flex gap-2">
                      <Qty milli={m.last_reading.value_milli} unit={m.unit} />
                      <IsoDate date={m.last_reading.read_on} />
                    </span>
                  ) : null}
                </TableCell>
                <TableCell>
                  {m.active ? null : (
                    <Badge variant="outline">{t('utilities:meters.inactive')}</Badge>
                  )}
                  {m.resource_id ? null : (
                    <Badge variant="outline">{t('utilities:meters.notChargeable')}</Badge>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  {can('marina.configure') ? (
                    <Button size="sm" variant="outline" onClick={() => setEditing(m)}>
                      {t('utilities:meters.edit')}
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        {editing ? (
          <MeterDialog
            marinaId={marinaId}
            meter={editing === 'new' ? null : editing}
            onClose={() => setEditing(null)}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

type SocketOption = {
  value: string;
  label: string;
  pedestalId: string;
  socket: string;
  kind: MeterKind;
  berthId?: string;
};

/** Sockets of the pedestals of a map, with the berth each one feeds (prefill of a new meter). */
function useSockets(marinaId: string, mapId: string): SocketOption[] {
  const map = useMap(marinaId, mapId);
  return useMemo(() => {
    const doc = map.data;
    if (!doc) return [];
    const out: SocketOption[] = [];
    for (const p of doc.pedestals) {
      if (!p.id) continue;
      for (const s of p.sockets ?? []) {
        const berth = doc.berths.find(
          (b) => b.pedestal_id === p.id && b.pedestal_socket === s.code,
        );
        out.push({
          value: `${p.id}:${s.code}`,
          label: `${p.code} ${s.code}${berth ? ` → ${berth.code}` : ''}`,
          pedestalId: p.id,
          socket: s.code,
          kind: s.kind === 'water' ? 'water' : 'power',
          ...(berth?.id ? { berthId: berth.id } : {}),
        });
      }
    }
    return out;
  }, [map.data]);
}

function MeterDialog({
  marinaId,
  meter,
  onClose,
}: {
  marinaId: string;
  meter: UtilityMeter | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateUtilityMeter(marinaId);
  const update = useUpdateUtilityMeter(marinaId);
  const maps = useMaps(marinaId);
  const [mapId, setMapId] = useState('');
  const sockets = useSockets(marinaId, mapId || maps.data?.[0]?.id || '');
  const [socket, setSocket] = useState<string | null>(null);
  const [code, setCode] = useState(meter?.code ?? '');
  const [kind, setKind] = useState<MeterKind | ''>(meter?.kind ?? 'power');
  const [serial, setSerial] = useState(meter?.serial ?? '');
  const [multiplier, setMultiplier] = useState(String((meter?.multiplier_milli ?? 1000) / 1000));
  const [active, setActive] = useState(meter?.active ?? true);
  const picked = sockets.find((s) => s.value === socket);
  const save = () => {
    const mult = Math.round(Number(multiplier.replace(',', '.')) * 1000) || 1000;
    if (meter) {
      update.mutate(
        {
          id: meter.id,
          version: meter.version,
          body: {
            code: code.trim(),
            serial: serial.trim() || null,
            multiplier_milli: mult,
            active,
          } as never,
        },
        { onSuccess: onClose },
      );
      return;
    }
    const k = kind || 'power';
    create.mutate(
      {
        code: code.trim(),
        kind: k,
        unit: UNIT_OF[k]!,
        multiplier_milli: mult,
        active,
        ...(serial.trim() ? { serial: serial.trim() } : {}),
        ...(picked ? { pedestal_id: picked.pedestalId, socket_code: picked.socket } : {}),
        ...(picked?.berthId
          ? { resource_type: 'berth' as const, resource_id: picked.berthId }
          : {}),
      },
      { onSuccess: onClose },
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {meter ? t('utilities:meters.edit') : t('utilities:meters.new')}
          </DialogTitle>
          {meter ? null : (
            <DialogDescription>{t('utilities:meters.prefillHint')}</DialogDescription>
          )}
        </DialogHeader>
        <div className="flex flex-col gap-3">
          {meter ? null : (
            <div className="grid gap-3 sm:grid-cols-2">
              <EnumSelect
                id="meter-map"
                label={t('utilities:fields.map')}
                values={(maps.data ?? []).map((m) => m.id)}
                value={mapId || maps.data?.[0]?.id || ''}
                onChange={setMapId}
                labelOf={(id) => maps.data?.find((m) => m.id === id)?.name.it ?? id}
              />
              <Field>
                <FieldLabel htmlFor="meter-socket">{t('utilities:fields.socket')}</FieldLabel>
                <ElementCombobox
                  id="meter-socket"
                  options={sockets}
                  value={socket}
                  onChange={(v) => {
                    setSocket(v);
                    const s = sockets.find((x) => x.value === v);
                    if (s) {
                      setKind(s.kind);
                      if (!code) setCode(s.label.split(' ')[0] + '-' + s.socket);
                    }
                  }}
                  placeholder={t('utilities:meters.chooseSocket')}
                  searchPlaceholder={t('utilities:meters.search')}
                  emptyText={t('utilities:meters.noSocket')}
                />
              </Field>
            </div>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="meter-code">{t('utilities:fields.code')}</FieldLabel>
              <Input
                id="meter-code"
                value={code}
                maxLength={32}
                onChange={(e) => setCode(e.target.value)}
              />
            </Field>
            {meter ? null : (
              <EnumSelect
                id="meter-new-kind"
                label={t('utilities:fields.kind')}
                values={METER_KINDS}
                value={kind}
                onChange={setKind}
                labelOf={(k) => t(`utilities:enums.kind.${k}`)}
              />
            )}
            <Field>
              <FieldLabel htmlFor="meter-serial">{t('utilities:fields.serial')}</FieldLabel>
              <Input
                id="meter-serial"
                value={serial}
                maxLength={64}
                onChange={(e) => setSerial(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="meter-mult">{t('utilities:fields.multiplier')}</FieldLabel>
              <Input
                id="meter-mult"
                inputMode="decimal"
                value={multiplier}
                onChange={(e) => setMultiplier(e.target.value.replace(/[^\d.,]/g, ''))}
              />
            </Field>
            <div className="flex items-center gap-2">
              <Switch id="meter-active" checked={active} onCheckedChange={setActive} />
              <Label htmlFor="meter-active">{t('utilities:fields.active')}</Label>
            </div>
          </div>
          <M4ProblemAlert error={create.error ?? update.error} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button disabled={!code.trim() || create.isPending || update.isPending} onClick={save}>
            {t('utilities:meters.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
