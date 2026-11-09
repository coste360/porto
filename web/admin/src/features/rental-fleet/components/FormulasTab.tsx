import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateRentalFormula,
  useRentalFormulas,
  useRentalSettings,
  useUpdateRentalFormula,
  type RentalCategory,
  type RentalFormula,
  type RentalFormulaKind,
  type RentalFuelMode,
  type RentalSkipperMode,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, M4ProblemAlert } from '../../contracts-m4/components/kit';
import { FORMULA_KINDS, FUEL_MODES, LATE_STEPS, SKIPPER_MODES, formulaFields } from '../lib';
import { CoverageDialog } from './CoverageDialog';
import { CategoryChecks, IntField, ItemSelect } from './kit';
import { useItemOptions } from './useItemOptions';

/** Rental formulas (m11-plan §3.3): kind, band, limits, skipper, fuel and late fee. Writes need marina.configure. */
export function FormulasTab({
  marinaId,
  canConfigure,
}: {
  marinaId: string;
  canConfigure: boolean;
}) {
  const { t } = useT();
  const list = useRentalFormulas(marinaId);
  const [editing, setEditing] = useState<RentalFormula | 'new' | null>(null);
  const [coverage, setCoverage] = useState(false);
  const items = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-3">
      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={() => setCoverage(true)} disabled={items.length === 0}>
          {t('rentalFleet:coverage.open')}
        </Button>
        {canConfigure ? (
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('rentalFleet:formulas.new')}
          </Button>
        ) : null}
      </div>
      {list.isPending ? (
        <LoadingState rows={4} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            t('rentalFleet:fields.code'),
            t('rentalFleet:fields.name'),
            t('rentalFleet:fields.kind'),
            t('rentalFleet:fields.skipper'),
            t('rentalFleet:fields.fuel'),
            t('rentalFleet:fields.categories'),
            '',
          ]}
          empty={t('rentalFleet:formulas.empty')}
          rows={items.length}
        >
          {items.map((f) => (
            <TableRow key={f.id} data-testid="formula-row">
              <TableCell className="font-medium">
                {f.code}{' '}
                {!f.active ? (
                  <Badge variant="outline">{t('rentalFleet:formulas.inactive')}</Badge>
                ) : null}
              </TableCell>
              <TableCell>{f.name.it}</TableCell>
              <TableCell>
                {t(`rentalFleet:enums.kind.${f.kind}`)}
                {f.band_code ? ` · ${f.band_code}` : ''}
              </TableCell>
              <TableCell>{t(`rentalFleet:enums.skipper.${f.skipper}`)}</TableCell>
              <TableCell>{t(`rentalFleet:enums.fuel.${f.fuel}`)}</TableCell>
              <TableCell>
                {f.categories.length
                  ? f.categories.map((c) => t(`rentalFleet:enums.category.${c}`)).join(', ')
                  : t('rentalFleet:hints.allCategoriesShort')}
              </TableCell>
              <TableCell className="text-right">
                {canConfigure ? (
                  <Button size="sm" variant="outline" onClick={() => setEditing(f)}>
                    {t('common:actions.edit')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      {editing ? (
        <FormulaDialog
          marinaId={marinaId}
          formula={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
      {coverage ? (
        <CoverageDialog marinaId={marinaId} formulas={items} onClose={() => setCoverage(false)} />
      ) : null}
    </div>
  );
}

const NONE = '__none__';

function FormulaDialog({
  marinaId,
  formula,
  onClose,
}: {
  marinaId: string;
  formula: RentalFormula | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateRentalFormula(marinaId);
  const update = useUpdateRentalFormula(marinaId);
  const settings = useRentalSettings(marinaId);
  const { items } = useItemOptions(marinaId);
  const [code, setCode] = useState(formula?.code ?? '');
  const [nameIt, setNameIt] = useState(formula?.name.it ?? '');
  const [nameEn, setNameEn] = useState(formula?.name.en ?? '');
  const [kind, setKind] = useState<RentalFormulaKind | ''>(formula?.kind ?? 'hourly');
  const [band, setBand] = useState(formula?.band_code ?? '');
  const [minSlots, setMinSlots] = useState<number | null>(formula?.min_slots ?? 2);
  const [maxSlots, setMaxSlots] = useState<number | null>(formula?.max_slots ?? null);
  const fields = formulaFields(kind || 'hourly');
  const [minDays, setMinDays] = useState<number | null>(formula?.min_days ?? null);
  const [maxDays, setMaxDays] = useState<number | null>(formula?.max_days ?? 30);
  const [skipper, setSkipper] = useState<RentalSkipperMode | ''>(formula?.skipper ?? 'none');
  const [fuel, setFuel] = useState<RentalFuelMode | ''>(formula?.fuel ?? 'included');
  const [fuelItem, setFuelItem] = useState(formula?.fuel_item_id ?? '');
  const [lateItem, setLateItem] = useState(formula?.late_item_id ?? '');
  const [grace, setGrace] = useState<number | null>(formula?.grace_minutes ?? 15);
  const [step, setStep] = useState<number>(formula?.late_step_minutes ?? 30);
  const [categories, setCategories] = useState<RentalCategory[]>(formula?.categories ?? []);
  const [active, setActive] = useState(formula?.active ?? true);
  const bands = settings.data?.bands ?? [];
  const save = () => {
    const common = {
      code: code.trim().toUpperCase(),
      name: { it: nameIt.trim(), ...(nameEn.trim() ? { en: nameEn.trim() } : {}) },
      ...(fields.band ? { band_code: band } : {}),
      ...(fields.slots
        ? { min_slots: minSlots ?? 2, ...(maxSlots ? { max_slots: maxSlots } : {}) }
        : {}),
      ...(fields.days ? { min_days: minDays ?? fields.minDays, max_days: maxDays ?? 30 } : {}),
      skipper: skipper || 'none',
      fuel: fuel || 'included',
      ...(fuel === 'metered' ? { fuel_item_id: fuelItem } : {}),
      ...(lateItem ? { late_item_id: lateItem } : {}),
      grace_minutes: grace ?? 0,
      late_step_minutes: step as 15 | 30 | 60,
      categories,
      active,
    };
    if (formula)
      update.mutate(
        { id: formula.id, version: formula.version, body: common },
        { onSuccess: onClose },
      );
    else create.mutate({ ...common, kind: kind || 'hourly' }, { onSuccess: onClose });
  };
  const valid =
    code.trim() !== '' &&
    nameIt.trim() !== '' &&
    (!fields.band || band !== '') &&
    (fuel !== 'metered' || fuelItem !== '');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {formula ? t('rentalFleet:formulas.edit') : t('rentalFleet:formulas.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="f-code">{t('rentalFleet:fields.code')}</FieldLabel>
            <Input
              id="f-code"
              value={code}
              maxLength={24}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <EnumSelect
            id="f-kind"
            label={t('rentalFleet:fields.kind')}
            values={formula ? [formula.kind] : FORMULA_KINDS}
            value={kind}
            onChange={setKind}
            labelOf={(v) => t(`rentalFleet:enums.kind.${v}`)}
          />
          <Field>
            <FieldLabel htmlFor="f-name-it">{t('rentalFleet:fields.nameIt')}</FieldLabel>
            <Input
              id="f-name-it"
              value={nameIt}
              maxLength={80}
              onChange={(e) => setNameIt(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="f-name-en">{t('rentalFleet:fields.nameEn')}</FieldLabel>
            <Input
              id="f-name-en"
              value={nameEn}
              maxLength={80}
              onChange={(e) => setNameEn(e.target.value)}
            />
          </Field>
          {fields.band ? (
            <Field>
              <FieldLabel htmlFor="f-band">{t('rentalFleet:fields.band')}</FieldLabel>
              <Select value={band || NONE} onValueChange={(v) => setBand(v === NONE ? '' : v)}>
                <SelectTrigger id="f-band">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t('rentalFleet:hints.none')}</SelectItem>
                  {bands.map((b) => (
                    <SelectItem key={b.code} value={b.code}>
                      {b.label.it}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : null}
          {fields.slots ? (
            <>
              <IntField
                id="f-min-slots"
                label={t('rentalFleet:fields.minSlots')}
                value={minSlots}
                onChange={setMinSlots}
              />
              <IntField
                id="f-max-slots"
                label={t('rentalFleet:fields.maxSlots')}
                value={maxSlots}
                onChange={setMaxSlots}
              />
            </>
          ) : null}
          {fields.days ? (
            <>
              <IntField
                id="f-min-days"
                label={t('rentalFleet:fields.minDays', { min: fields.minDays })}
                value={minDays}
                onChange={setMinDays}
              />
              <IntField
                id="f-max-days"
                label={t('rentalFleet:fields.maxDays')}
                value={maxDays}
                onChange={setMaxDays}
              />
            </>
          ) : null}
          <EnumSelect
            id="f-skipper"
            label={t('rentalFleet:fields.skipper')}
            values={SKIPPER_MODES}
            value={skipper}
            onChange={setSkipper}
            labelOf={(v) => t(`rentalFleet:enums.skipper.${v}`)}
          />
          <EnumSelect
            id="f-fuel"
            label={t('rentalFleet:fields.fuel')}
            values={FUEL_MODES}
            value={fuel}
            onChange={setFuel}
            labelOf={(v) => t(`rentalFleet:enums.fuel.${v}`)}
          />
          {fuel === 'metered' ? (
            <ItemSelect
              id="f-fuel-item"
              label={t('rentalFleet:fields.fuelItem')}
              items={items}
              value={fuelItem}
              onChange={setFuelItem}
            />
          ) : null}
          <ItemSelect
            id="f-late-item"
            label={t('rentalFleet:fields.lateItem')}
            items={items}
            value={lateItem}
            onChange={setLateItem}
            optional
          />
          <IntField
            id="f-grace"
            label={t('rentalFleet:fields.grace')}
            value={grace}
            onChange={setGrace}
          />
          <Field>
            <FieldLabel htmlFor="f-step">{t('rentalFleet:fields.lateStep')}</FieldLabel>
            <Select value={String(step)} onValueChange={(v) => setStep(Number(v))}>
              <SelectTrigger id="f-step">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LATE_STEPS.map((s) => (
                  <SelectItem key={s} value={String(s)}>
                    {t('rentalFleet:hints.minutes', { count: s })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <div className="sm:col-span-2">
            <CategoryChecks
              idPrefix="f-cat"
              label={t('rentalFleet:fields.categories')}
              value={categories}
              onChange={setCategories}
            />
          </div>
          <div className="flex items-center gap-2">
            <Switch id="f-active" checked={active} onCheckedChange={setActive} />
            <Label htmlFor="f-active">{t('rentalFleet:fields.active')}</Label>
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
