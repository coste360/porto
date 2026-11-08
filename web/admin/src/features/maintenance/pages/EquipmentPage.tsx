import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateEquipment,
  useEquipmentList,
  useUpdateEquipment,
  type Equipment,
  type EquipmentKind,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert, PageFrame } from '../../contracts-m4/components/kit';
import { MaintenanceNav } from '../components/kit';

const EQUIPMENT_KINDS: readonly EquipmentKind[] = [
  'travel_lift',
  'crane',
  'forklift',
  'trailer',
  'vehicle',
  'plant',
  'other',
];

/** Equipment registry (m9-plan §8.1): lifting machines, vehicles and plants, with the out-of-service flag. */
export function EquipmentPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const list = useEquipmentList(marinaId);
  const [editing, setEditing] = useState<Equipment | 'new' | null>(null);
  const items = list.data?.items ?? [];
  return (
    <RequirePermission allowed={can('maintenance.manage')}>
      <PageFrame>
        <PageHeader
          title={t('maintenance:equipment.title')}
          description={t('maintenance:equipment.description')}
          actions={
            <Button onClick={() => setEditing('new')}>
              <PlusIcon />
              {t('maintenance:equipment.new')}
            </Button>
          }
        />
        <MaintenanceNav marinaId={marinaId} current="equipment" />
        {list.isPending ? (
          <LoadingState rows={4} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('maintenance:fields.code'),
              t('maintenance:fields.name'),
              t('maintenance:fields.kind'),
              t('maintenance:fields.capacity'),
              t('maintenance:fields.status'),
              '',
            ]}
            empty={t('maintenance:equipment.empty')}
            rows={items.length}
          >
            {items.map((e) => (
              <TableRow key={e.id} data-testid="equipment-row">
                <TableCell className="font-medium">{e.code}</TableCell>
                <TableCell>{e.name}</TableCell>
                <TableCell>{t(`maintenance:enums.equipmentKind.${e.kind}`)}</TableCell>
                <TableCell>
                  {e.capacity_kg ? t('maintenance:equipment.kg', { value: e.capacity_kg }) : ''}
                </TableCell>
                <TableCell className="flex gap-2">
                  {e.out_of_service ? (
                    <Badge variant="destructive">{t('maintenance:fields.outOfService')}</Badge>
                  ) : null}
                  {!e.active ? (
                    <Badge variant="outline">{t('maintenance:equipment.inactive')}</Badge>
                  ) : null}
                </TableCell>
                <TableCell className="text-right">
                  <Button size="sm" variant="outline" onClick={() => setEditing(e)}>
                    {t('maintenance:equipment.edit')}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        {editing ? (
          <EquipmentDialog
            marinaId={marinaId}
            equipment={editing === 'new' ? null : editing}
            onClose={() => setEditing(null)}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function EquipmentDialog({
  marinaId,
  equipment,
  onClose,
}: {
  marinaId: string;
  equipment: Equipment | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateEquipment(marinaId);
  const update = useUpdateEquipment(marinaId);
  const [code, setCode] = useState(equipment?.code ?? '');
  const [name, setName] = useState(equipment?.name ?? '');
  const [kind, setKind] = useState<EquipmentKind | ''>(equipment?.kind ?? 'travel_lift');
  const [capacity, setCapacity] = useState(
    equipment?.capacity_kg ? String(equipment.capacity_kg) : '',
  );
  const [active, setActive] = useState(equipment?.active ?? true);
  const save = () => {
    const body = {
      code: code.trim(),
      name: name.trim(),
      kind: kind || 'other',
      active,
      ...(capacity ? { capacity_kg: Number(capacity) } : {}),
    };
    if (equipment)
      update.mutate({ id: equipment.id, version: equipment.version, body }, { onSuccess: onClose });
    else create.mutate(body, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {equipment ? t('maintenance:equipment.edit') : t('maintenance:equipment.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="eq-code">{t('maintenance:fields.code')}</FieldLabel>
            <Input
              id="eq-code"
              value={code}
              maxLength={32}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="eq-name">{t('maintenance:fields.name')}</FieldLabel>
            <Input
              id="eq-name"
              value={name}
              maxLength={100}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="eq-kind"
            label={t('maintenance:fields.kind')}
            values={EQUIPMENT_KINDS}
            value={kind}
            onChange={setKind}
            labelOf={(v) => t(`maintenance:enums.equipmentKind.${v}`)}
          />
          <Field>
            <FieldLabel htmlFor="eq-capacity">{t('maintenance:fields.capacity')}</FieldLabel>
            <Input
              id="eq-capacity"
              inputMode="numeric"
              value={capacity}
              onChange={(e) => setCapacity(e.target.value.replace(/\D/g, ''))}
            />
          </Field>
          <div className="flex items-center gap-2">
            <Switch id="eq-active" checked={active} onCheckedChange={setActive} />
            <Label htmlFor="eq-active">{t('maintenance:fields.active')}</Label>
          </div>
        </div>
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!code.trim() || !name.trim() || create.isPending || update.isPending}
            onClick={save}
          >
            {t('maintenance:equipment.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
