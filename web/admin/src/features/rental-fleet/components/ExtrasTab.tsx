import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateRentalExtra,
  useRentalExtras,
  useUpdateRentalExtra,
  type RentalCategory,
  type RentalExtra,
  type RentalExtraChargeMode,
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
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert } from '../../contracts-m4/components/kit';
import { CHARGE_MODES, fromMilli, toMilli } from '../lib';
import { CategoryChecks, ItemSelect } from './kit';
import { useItemOptions } from './useItemOptions';

/** Extras (m11-plan §3.4): catalogue items charged per rental or per day, priced with the price lists. */
export function ExtrasTab({ marinaId, canConfigure }: { marinaId: string; canConfigure: boolean }) {
  const { t } = useT();
  const list = useRentalExtras(marinaId);
  const [editing, setEditing] = useState<RentalExtra | 'new' | null>(null);
  const items = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-3">
      {canConfigure ? (
        <div className="flex justify-end">
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('rentalFleet:extras.new')}
          </Button>
        </div>
      ) : null}
      {list.isPending ? (
        <LoadingState rows={3} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            t('rentalFleet:fields.item'),
            t('rentalFleet:fields.chargeMode'),
            t('rentalFleet:fields.defaultQty'),
            t('rentalFleet:fields.maxQty'),
            t('rentalFleet:fields.categories'),
            '',
          ]}
          empty={t('rentalFleet:extras.empty')}
          rows={items.length}
        >
          {items.map((x) => (
            <TableRow key={x.id} data-testid="extra-row">
              <TableCell className="font-medium">
                {x.item_code} · {x.name.it}{' '}
                {!x.active ? (
                  <Badge variant="outline">{t('rentalFleet:extras.inactive')}</Badge>
                ) : null}
              </TableCell>
              <TableCell>{t(`rentalFleet:enums.chargeMode.${x.charge_mode}`)}</TableCell>
              <TableCell className="tabular-nums">{fromMilli(x.default_qty_milli)}</TableCell>
              <TableCell className="tabular-nums">{fromMilli(x.max_qty_milli)}</TableCell>
              <TableCell>
                {x.categories.length
                  ? x.categories.map((c) => t(`rentalFleet:enums.category.${c}`)).join(', ')
                  : t('rentalFleet:hints.allCategoriesShort')}
              </TableCell>
              <TableCell className="text-right">
                {canConfigure ? (
                  <Button size="sm" variant="outline" onClick={() => setEditing(x)}>
                    {t('common:actions.edit')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      {editing ? (
        <ExtraDialog
          marinaId={marinaId}
          extra={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function ExtraDialog({
  marinaId,
  extra,
  onClose,
}: {
  marinaId: string;
  extra: RentalExtra | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateRentalExtra(marinaId);
  const update = useUpdateRentalExtra(marinaId);
  const { items } = useItemOptions(marinaId);
  const [itemId, setItemId] = useState(extra?.item_id ?? '');
  const [mode, setMode] = useState<RentalExtraChargeMode | ''>(extra?.charge_mode ?? 'per_rental');
  const [def, setDef] = useState(extra ? fromMilli(extra.default_qty_milli) : '1');
  const [maxQ, setMaxQ] = useState(extra ? fromMilli(extra.max_qty_milli) : '10');
  const [categories, setCategories] = useState<RentalCategory[]>(extra?.categories ?? []);
  const [active, setActive] = useState(extra?.active ?? true);
  const defMilli = toMilli(def);
  const maxMilli = toMilli(maxQ);
  const valid =
    itemId !== '' && defMilli !== null && maxMilli !== null && maxMilli > 0 && defMilli <= maxMilli;
  const save = () => {
    const body = {
      charge_mode: mode || 'per_rental',
      default_qty_milli: defMilli ?? 1000,
      max_qty_milli: maxMilli ?? 10000,
      categories,
      active,
    };
    if (extra)
      update.mutate({ id: extra.id, version: extra.version, body }, { onSuccess: onClose });
    else create.mutate({ ...body, item_id: itemId }, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {extra ? t('rentalFleet:extras.edit') : t('rentalFleet:extras.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          {extra ? (
            <p className="text-sm sm:col-span-2">{`${extra.item_code} · ${extra.name.it}`}</p>
          ) : (
            <ItemSelect
              id="x-item"
              label={t('rentalFleet:fields.item')}
              items={items}
              value={itemId}
              onChange={setItemId}
            />
          )}
          <EnumSelect
            id="x-mode"
            label={t('rentalFleet:fields.chargeMode')}
            values={CHARGE_MODES}
            value={mode}
            onChange={setMode}
            labelOf={(v) => t(`rentalFleet:enums.chargeMode.${v}`)}
          />
          <Field>
            <FieldLabel htmlFor="x-def">{t('rentalFleet:fields.defaultQty')}</FieldLabel>
            <Input
              id="x-def"
              inputMode="decimal"
              value={def}
              onChange={(e) => setDef(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="x-max">{t('rentalFleet:fields.maxQty')}</FieldLabel>
            <Input
              id="x-max"
              inputMode="decimal"
              value={maxQ}
              onChange={(e) => setMaxQ(e.target.value)}
            />
          </Field>
          <div className="sm:col-span-2">
            <CategoryChecks
              idPrefix="x-cat"
              label={t('rentalFleet:fields.categories')}
              value={categories}
              onChange={setCategories}
            />
          </div>
          <div className="flex items-center gap-2">
            <Switch id="x-active" checked={active} onCheckedChange={setActive} />
            <Label htmlFor="x-active">{t('rentalFleet:fields.active')}</Label>
          </div>
        </div>
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button disabled={!valid || create.isPending || update.isPending} onClick={save}>
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
