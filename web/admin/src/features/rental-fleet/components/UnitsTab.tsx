import { PlusIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import {
  useCreateRentalUnit,
  useCustomers,
  usePricingToolBerths,
  useRentalSettings,
  useRentalUnits,
  useUpdateRentalUnit,
  useVessels,
  type RentalCategory,
  type RentalFuelType,
  type RentalLicenceMode,
  type RentalOwnership,
  type RentalUnit,
  type RentalUnitStatus,
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
import { ElementCombobox, MoneyInput } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert, Money } from '../../contracts-m4/components/kit';
import {
  CATEGORIES,
  FUEL_TYPES,
  LICENCE_MODES,
  OWNERSHIPS,
  UNIT_STATUSES,
  fromMilli,
  kw,
  licenceRequired,
  toMilli,
} from '../lib';
import { IntField, ItemSelect } from './kit';
import { useItemOptions } from './useItemOptions';

const STATUS_VARIANT: Record<RentalUnitStatus, 'secondary' | 'outline' | 'destructive'> = {
  available: 'secondary',
  maintenance: 'destructive',
  retired: 'outline',
};

/** The units of the fleet (m11-plan §3.1): category, seats, licence, status and home berth. */
export function UnitsTab({ marinaId }: { marinaId: string }) {
  const { t } = useT();
  const list = useRentalUnits(marinaId);
  const [editing, setEditing] = useState<RentalUnit | 'new' | null>(null);
  const items = [...(list.data?.items ?? [])].sort(
    (a, b) => a.sort - b.sort || a.code.localeCompare(b.code),
  );
  return (
    <div className="flex flex-col gap-3">
      <div className="flex justify-end">
        <Button onClick={() => setEditing('new')} data-testid="unit-new">
          <PlusIcon />
          {t('rentalFleet:units.new')}
        </Button>
      </div>
      {list.isPending ? (
        <LoadingState rows={4} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            t('rentalFleet:fields.code'),
            t('rentalFleet:fields.vessel'),
            t('rentalFleet:fields.category'),
            t('rentalFleet:fields.seats'),
            t('rentalFleet:fields.licence'),
            t('rentalFleet:fields.deposit'),
            t('rentalFleet:fields.homeBerth'),
            t('rentalFleet:fields.status'),
            '',
          ]}
          empty={t('rentalFleet:units.empty')}
          rows={items.length}
        >
          {items.map((u) => (
            <TableRow key={u.id} data-testid="unit-row">
              <TableCell className="font-medium">{u.code}</TableCell>
              <TableCell>{u.vessel_name}</TableCell>
              <TableCell>{t(`rentalFleet:enums.category.${u.category}`)}</TableCell>
              <TableCell className="tabular-nums">{u.seats}</TableCell>
              <TableCell>
                {u.licence_required ? (
                  <Badge variant="outline">{t('rentalFleet:units.licenceNeeded')}</Badge>
                ) : (
                  t('rentalFleet:units.licenceNotNeeded')
                )}
              </TableCell>
              <TableCell>
                <Money cents={u.deposit_cents} />
              </TableCell>
              <TableCell>{u.home_berth_code ?? ''}</TableCell>
              <TableCell>
                <Badge variant={STATUS_VARIANT[u.status]}>
                  {t(`rentalFleet:enums.status.${u.status}`)}
                </Badge>
              </TableCell>
              <TableCell className="text-right">
                <Button size="sm" variant="outline" onClick={() => setEditing(u)}>
                  {t('common:actions.edit')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      {editing ? (
        <UnitDialog
          marinaId={marinaId}
          unit={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
    </div>
  );
}

function UnitDialog({
  marinaId,
  unit,
  onClose,
}: {
  marinaId: string;
  unit: RentalUnit | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateRentalUnit(marinaId);
  const update = useUpdateRentalUnit(marinaId);
  const settings = useRentalSettings(marinaId);
  const { items } = useItemOptions(marinaId);
  const [vesselQ, setVesselQ] = useState('');
  const vessels = useVessels(marinaId, vesselQ ? { q: vesselQ } : {});
  const [berthQ, setBerthQ] = useState('');
  const berths = usePricingToolBerths(marinaId, berthQ);
  const [vesselId, setVesselId] = useState(unit?.vessel_id ?? '');
  const [code, setCode] = useState(unit?.code ?? '');
  const [category, setCategory] = useState<RentalCategory | ''>(unit?.category ?? 'rib');
  const [seats, setSeats] = useState<number | null>(unit?.seats ?? 6);
  const [licence, setLicence] = useState<RentalLicenceMode | ''>(unit?.licence ?? 'threshold');
  const [fuelType, setFuelType] = useState<RentalFuelType | ''>(unit?.fuel_type ?? 'petrol');
  const [tank, setTank] = useState(unit ? fromMilli(unit.tank_milli) : '');
  const [deposit, setDeposit] = useState<number | null>(unit?.deposit_cents ?? 0);
  const [status, setStatus] = useState<RentalUnitStatus | ''>(unit?.status ?? 'available');
  const [ownership, setOwnership] = useState<RentalOwnership | ''>(unit?.ownership ?? 'marina');
  const [homeBerth, setHomeBerth] = useState<string | null>(unit?.home_berth_id ?? null);
  const [itemId, setItemId] = useState(unit?.item_id ?? '');
  const [ownerId, setOwnerId] = useState<string | null>(unit?.owner_customer_id ?? null);
  const [ownerQ, setOwnerQ] = useState('');
  const owners = useCustomers(marinaId, ownerQ ? { q: ownerQ } : {});
  const [note, setNote] = useState(unit?.note ?? '');

  const vesselOptions = useMemo(() => {
    const opts = (vessels.data?.pages ?? [])
      .flatMap((p) => p.items)
      .map((v) => ({ value: v.id, label: v.name }));
    if (unit && !opts.some((o) => o.value === unit.vessel_id))
      opts.unshift({ value: unit.vessel_id, label: unit.vessel_name });
    return opts;
  }, [vessels.data, unit]);
  const berthOptions = useMemo(() => {
    const opts = (berths.data?.items ?? []).map((b) => ({ value: b.id, label: b.code }));
    if (unit?.home_berth_id && !opts.some((o) => o.value === unit.home_berth_id))
      opts.unshift({ value: unit.home_berth_id, label: unit.home_berth_code ?? '' });
    return opts;
  }, [berths.data, unit]);

  const ownerOptions = useMemo(() => {
    const opts = (owners.data?.pages ?? [])
      .flatMap((p) => p.items)
      .map((c) => ({ value: c.id, label: c.display_name }));
    if (unit?.owner_customer_id && !opts.some((o) => o.value === unit.owner_customer_id))
      opts.unshift({ value: unit.owner_customer_id, label: unit.owner_customer_id });
    return opts;
  }, [owners.data, unit]);
  const tankMilli = toMilli(tank);
  const threshold = settings.data?.licence_threshold_kw_milli ?? 30000;
  const engineKw = unit?.engine_kw_milli ?? 0;
  const save = () => {
    const common = {
      code: code.trim().toUpperCase(),
      category: category || 'rib',
      seats: seats ?? 1,
      licence: licence || 'threshold',
      fuel_type: fuelType || 'none',
      tank_milli: tankMilli ?? 0,
      deposit_cents: deposit ?? 0,
      status: status || 'available',
      item_id: itemId,
      ownership: ownership || 'marina',
      note,
      ...(ownership === 'third_party' && ownerId ? { owner_customer_id: ownerId } : {}),
    };
    if (unit) {
      update.mutate(
        {
          id: unit.id,
          version: unit.version,
          body: { ...common, home_berth_id: homeBerth ?? undefined },
        },
        { onSuccess: onClose },
      );
    } else {
      create.mutate(
        { ...common, vessel_id: vesselId, ...(homeBerth ? { home_berth_id: homeBerth } : {}) },
        { onSuccess: onClose },
      );
    }
  };
  const valid =
    (ownership !== 'third_party' || ownerId !== null) &&
    code.trim() !== '' &&
    (unit || vesselId) &&
    itemId !== '' &&
    tankMilli !== null &&
    (seats ?? 0) > 0;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {unit ? t('rentalFleet:units.edit') : t('rentalFleet:units.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="unit-vessel">{t('rentalFleet:fields.vessel')}</FieldLabel>
            <ElementCombobox
              id="unit-vessel"
              options={vesselOptions}
              value={vesselId || null}
              onChange={(v) => setVesselId(v ?? '')}
              onSearch={setVesselQ}
              disabled={unit !== null}
              placeholder={t('rentalFleet:hints.chooseVessel')}
              searchPlaceholder={t('rentalFleet:hints.searchVessel')}
              emptyText={t('rentalFleet:hints.noVessel')}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="unit-code">{t('rentalFleet:fields.code')}</FieldLabel>
            <Input
              id="unit-code"
              value={code}
              maxLength={16}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="unit-category"
            label={t('rentalFleet:fields.category')}
            values={CATEGORIES}
            value={category}
            onChange={setCategory}
            labelOf={(v) => t(`rentalFleet:enums.category.${v}`)}
          />
          <IntField
            id="unit-seats"
            label={t('rentalFleet:fields.seats')}
            value={seats}
            onChange={setSeats}
          />
          <EnumSelect
            id="unit-licence"
            label={t('rentalFleet:fields.licence')}
            values={LICENCE_MODES}
            value={licence}
            onChange={setLicence}
            labelOf={(v) => t(`rentalFleet:enums.licence.${v}`)}
          />
          <div
            className="text-muted-foreground flex items-end pb-2 text-sm"
            data-testid="unit-licence-hint"
          >
            {unit
              ? t('rentalFleet:units.kwComputed', {
                  kw: kw(engineKw),
                  threshold: kw(threshold),
                  verdict: licenceRequired(licence || 'threshold', engineKw, threshold)
                    ? t('rentalFleet:units.licenceNeeded')
                    : t('rentalFleet:units.licenceNotNeeded'),
                })
              : t('rentalFleet:units.kwFromVessel', { threshold: kw(threshold) })}
          </div>
          <EnumSelect
            id="unit-fuel"
            label={t('rentalFleet:fields.fuelType')}
            values={FUEL_TYPES}
            value={fuelType}
            onChange={setFuelType}
            labelOf={(v) => t(`rentalFleet:enums.fuelType.${v}`)}
          />
          <Field>
            <FieldLabel htmlFor="unit-tank">{t('rentalFleet:fields.tank')}</FieldLabel>
            <Input
              id="unit-tank"
              inputMode="decimal"
              value={tank}
              aria-invalid={tankMilli === null}
              onChange={(e) => setTank(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="unit-deposit">{t('rentalFleet:fields.deposit')}</FieldLabel>
            <MoneyInput id="unit-deposit" value={deposit} onChange={setDeposit} />
          </Field>
          <ItemSelect
            id="unit-item"
            label={t('rentalFleet:fields.item')}
            items={items}
            value={itemId}
            onChange={setItemId}
            rentalOnly
          />
          <EnumSelect
            id="unit-status"
            label={t('rentalFleet:fields.status')}
            values={UNIT_STATUSES}
            value={status}
            onChange={setStatus}
            labelOf={(v) => t(`rentalFleet:enums.status.${v}`)}
          />
          <EnumSelect
            id="unit-ownership"
            label={t('rentalFleet:fields.ownership')}
            values={OWNERSHIPS}
            value={ownership}
            onChange={setOwnership}
            labelOf={(v) => t(`rentalFleet:enums.ownership.${v}`)}
          />
          <Field>
            <FieldLabel htmlFor="unit-berth">{t('rentalFleet:fields.homeBerth')}</FieldLabel>
            <ElementCombobox
              id="unit-berth"
              options={berthOptions}
              value={homeBerth}
              onChange={setHomeBerth}
              onSearch={setBerthQ}
              placeholder={t('rentalFleet:hints.none')}
              searchPlaceholder={t('rentalFleet:hints.searchBerth')}
              emptyText={t('rentalFleet:hints.noBerth')}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="unit-note">{t('rentalFleet:fields.note')}</FieldLabel>
            <Input
              id="unit-note"
              value={note}
              maxLength={500}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
        </div>
        {ownership === 'third_party' ? (
          <Field>
            <FieldLabel htmlFor="unit-owner">{t('rentalFleet:fields.owner')}</FieldLabel>
            <ElementCombobox
              id="unit-owner"
              options={ownerOptions}
              value={ownerId}
              onChange={setOwnerId}
              onSearch={setOwnerQ}
              placeholder={t('rentalFleet:hints.chooseOwner')}
              searchPlaceholder={t('rentalFleet:hints.searchOwner')}
              emptyText={t('rentalFleet:hints.noOwner')}
            />
            <p className="text-muted-foreground text-xs">{t('rentalFleet:units.thirdPartyHint')}</p>
          </Field>
        ) : null}
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!valid || create.isPending || update.isPending}
            onClick={save}
            data-testid="unit-save"
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
