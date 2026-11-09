import { PlusIcon, TrashIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCashRegisters,
  usePutRentalSettings,
  useRentalSettings,
  type RentalBand,
  type RentalCategory,
  type RentalChecklistTemplateItem,
  type RentalSettings,
  type RentalSettingsInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
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

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { M4ProblemAlert } from '../../contracts-m4/components/kit';
import { DEPOSIT_MODES, kw, slotLabel, stripCells, windowIssues } from '../lib';
import { CategoryChecks, IntField, ItemSelect, SlotSelect } from './kit';
import { useItemOptions } from './useItemOptions';

const NONE = '__none__';

/** The rental settings (m11-plan §3.5): open window, bands, buffer, holds, licence, checklist, photos, deposits. */
export function SettingsTab({
  marinaId,
  canConfigure,
}: {
  marinaId: string;
  canConfigure: boolean;
}) {
  const settings = useRentalSettings(marinaId);
  if (settings.isPending) return <LoadingState rows={6} />;
  if (settings.isError)
    return <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />;
  return (
    <SettingsForm
      key={settings.data.version}
      marinaId={marinaId}
      settings={settings.data}
      canConfigure={canConfigure}
    />
  );
}

/** The 48 half hours of the day: closed, open window, bands (one colour each), DST hours. */
export function BandStrip({
  openFrom,
  openTo,
  bands,
}: {
  openFrom: number;
  openTo: number;
  bands: readonly Pick<RentalBand, 'from_slot' | 'to_slot' | 'code'>[];
}) {
  const { t } = useT();
  const colour = ['bg-chart-1', 'bg-chart-2', 'bg-chart-3', 'bg-chart-4'];
  const cells = stripCells(openFrom, openTo, bands);
  return (
    <div className="flex flex-col gap-1">
      <div
        className="flex h-6 w-full overflow-hidden rounded-md border"
        role="img"
        aria-label={t('rentalFleet:settings.stripLabel', {
          from: slotLabel(openFrom),
          to: slotLabel(openTo),
          bands: bands
            .map((b) => `${b.code} ${slotLabel(b.from_slot)}-${slotLabel(b.to_slot)}`)
            .join(', '),
        })}
        data-testid="band-strip"
      >
        {cells.map((c) => (
          <div
            key={c.slot}
            data-kind={c.kind}
            className={`flex-1 ${
              c.kind === 'band'
                ? (colour[c.band ?? 0] ?? 'bg-chart-1')
                : c.kind === 'open'
                  ? 'bg-muted'
                  : c.kind === 'dst'
                    ? 'bg-destructive/20'
                    : 'bg-background'
            }`}
          />
        ))}
      </div>
      <div className="text-muted-foreground flex justify-between text-xs tabular-nums">
        <span>00:00</span>
        <span>06:00</span>
        <span>12:00</span>
        <span>18:00</span>
        <span>24:00</span>
      </div>
    </div>
  );
}

function SettingsForm({
  marinaId,
  settings,
  canConfigure,
}: {
  marinaId: string;
  settings: RentalSettings;
  canConfigure: boolean;
}) {
  const { t } = useT();
  const put = usePutRentalSettings(marinaId);
  const registers = useCashRegisters(marinaId, canConfigure);
  const { items } = useItemOptions(marinaId);
  const [openFrom, setOpenFrom] = useState(settings.open_from_slot);
  const [openTo, setOpenTo] = useState(settings.open_to_slot);
  const [bands, setBands] = useState<RentalBand[]>(settings.bands);
  const [buffer, setBuffer] = useState<number | null>(settings.buffer_slots);
  const [hold, setHold] = useState<number | null>(settings.desk_hold_minutes);
  const [noShow, setNoShow] = useState<number | null>(settings.no_show_after_minutes);
  const [threshold, setThreshold] = useState<number | null>(
    kw(settings.licence_threshold_kw_milli),
  );
  const [skipperItem, setSkipperItem] = useState(settings.skipper_item_id ?? '');
  const [damageItem, setDamageItem] = useState(settings.damage_item_id ?? '');
  const [checklist, setChecklist] = useState<RentalChecklistTemplateItem[]>(settings.checklist);
  const [outPhotos, setOutPhotos] = useState<number | null>(settings.checkout_min_photos);
  const [inPhotos, setInPhotos] = useState<number | null>(settings.checkin_min_photos);
  const [modes, setModes] = useState<string[]>(settings.deposit_modes);
  const [retention, setRetention] = useState<number | null>(settings.identity_retention_days);
  const [register, setRegister] = useState(settings.default_register_id ?? '');
  const issues = windowIssues(openFrom, openTo, bands, buffer ?? 0);
  const setBand = (i: number, patch: Partial<RentalBand>) =>
    setBands(bands.map((b, j) => (j === i ? { ...b, ...patch } : b)));
  const setItem = (i: number, patch: Partial<RentalChecklistTemplateItem>) =>
    setChecklist(checklist.map((c, j) => (j === i ? { ...c, ...patch } : c)));
  const save = () => {
    const body: RentalSettingsInput = {
      open_from_slot: openFrom,
      open_to_slot: openTo,
      bands,
      buffer_slots: buffer ?? 0,
      desk_hold_minutes: hold ?? 60,
      no_show_after_minutes: noShow ?? 0,
      licence_threshold_kw_milli: (threshold ?? 30) * 1000,
      ...(skipperItem ? { skipper_item_id: skipperItem } : {}),
      ...(damageItem ? { damage_item_id: damageItem } : {}),
      checklist,
      checkout_min_photos: outPhotos ?? 0,
      checkin_min_photos: inPhotos ?? 0,
      deposit_modes: modes as RentalSettingsInput['deposit_modes'],
      identity_retention_days: retention ?? 365,
      ...(register ? { default_register_id: register } : {}),
    };
    put.mutate({ version: settings.version, body });
  };
  const valid =
    issues.length === 0 &&
    modes.length > 0 &&
    checklist.length > 0 &&
    checklist.every((c) => /^[a-z0-9_]{1,32}$/.test(c.key) && c.label.it.trim() !== '');
  return (
    <fieldset
      disabled={!canConfigure}
      className="flex flex-col gap-4"
      data-testid="rental-settings"
    >
      <Card>
        <CardHeader>
          <CardTitle>{t('rentalFleet:settings.window')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <BandStrip openFrom={openFrom} openTo={openTo} bands={bands} />
          <div className="flex flex-wrap gap-3">
            <SlotSelect
              id="s-open-from"
              label={t('rentalFleet:fields.openFrom')}
              value={openFrom}
              onChange={setOpenFrom}
              max={47}
            />
            <SlotSelect
              id="s-open-to"
              label={t('rentalFleet:fields.openTo')}
              value={openTo}
              onChange={setOpenTo}
              min={1}
            />
            <IntField
              id="s-buffer"
              label={t('rentalFleet:fields.buffer')}
              value={buffer}
              onChange={setBuffer}
            />
          </div>
          {bands.map((b, i) => (
            <div key={i} className="flex flex-wrap items-end gap-2" data-testid="band-row">
              <Field className="w-auto max-w-36">
                <FieldLabel htmlFor={`b-${i}-code`}>{t('rentalFleet:fields.bandCode')}</FieldLabel>
                <Input
                  id={`b-${i}-code`}
                  value={b.code}
                  maxLength={16}
                  onChange={(e) => setBand(i, { code: e.target.value })}
                />
              </Field>
              <Field className="w-auto max-w-48">
                <FieldLabel htmlFor={`b-${i}-label`}>{t('rentalFleet:fields.label')}</FieldLabel>
                <Input
                  id={`b-${i}-label`}
                  value={b.label.it}
                  maxLength={40}
                  onChange={(e) => setBand(i, { label: { ...b.label, it: e.target.value } })}
                />
              </Field>
              <SlotSelect
                id={`b-${i}-from`}
                label={t('rentalFleet:fields.from')}
                value={b.from_slot}
                onChange={(v) => setBand(i, { from_slot: v })}
                max={47}
              />
              <SlotSelect
                id={`b-${i}-to`}
                label={t('rentalFleet:fields.to')}
                value={b.to_slot}
                onChange={(v) => setBand(i, { to_slot: v })}
                min={1}
              />
              <Button
                size="icon"
                variant="ghost"
                aria-label={t('common:actions.remove')}
                onClick={() => setBands(bands.filter((_, j) => j !== i))}
              >
                <TrashIcon />
              </Button>
            </div>
          ))}
          <div>
            <Button
              variant="outline"
              size="sm"
              disabled={bands.length >= 4}
              onClick={() =>
                setBands([
                  ...bands,
                  {
                    code: `band_${bands.length + 1}`,
                    from_slot: openFrom,
                    to_slot: openTo,
                    label: { it: '' },
                  },
                ])
              }
            >
              <PlusIcon />
              {t('rentalFleet:settings.addBand')}
            </Button>
          </div>
          {issues.length > 0 ? (
            <ul className="text-destructive list-disc pl-5 text-sm" data-testid="window-issues">
              {issues.map((i) => (
                <li key={i}>{t(`rentalFleet:issues.${i}`)}</li>
              ))}
            </ul>
          ) : null}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('rentalFleet:settings.rules')}</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-3">
          <IntField
            id="s-hold"
            label={t('rentalFleet:fields.deskHold')}
            value={hold}
            onChange={setHold}
          />
          <IntField
            id="s-noshow"
            label={t('rentalFleet:fields.noShowAfter')}
            value={noShow}
            onChange={setNoShow}
          />
          <IntField
            id="s-threshold"
            label={t('rentalFleet:fields.threshold')}
            value={threshold}
            onChange={setThreshold}
          />
          <ItemSelect
            id="s-skipper-item"
            label={t('rentalFleet:fields.skipperItem')}
            items={items}
            value={skipperItem}
            onChange={setSkipperItem}
            rentalOnly
            optional
          />
          <ItemSelect
            id="s-damage-item"
            label={t('rentalFleet:fields.damageItem')}
            items={items}
            value={damageItem}
            onChange={setDamageItem}
            optional
          />
          <Field>
            <FieldLabel htmlFor="s-register">{t('rentalFleet:fields.register')}</FieldLabel>
            <Select
              value={register || NONE}
              onValueChange={(v) => setRegister(v === NONE ? '' : v)}
            >
              <SelectTrigger id="s-register">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t('rentalFleet:hints.none')}</SelectItem>
                {(registers.data?.items ?? []).map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.code} · {r.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <IntField
            id="s-out-photos"
            label={t('rentalFleet:fields.checkoutPhotos')}
            value={outPhotos}
            onChange={setOutPhotos}
          />
          <IntField
            id="s-in-photos"
            label={t('rentalFleet:fields.checkinPhotos')}
            value={inPhotos}
            onChange={setInPhotos}
          />
          <IntField
            id="s-retention"
            label={t('rentalFleet:fields.retention')}
            value={retention}
            onChange={setRetention}
          />
          <fieldset className="flex flex-col gap-2 sm:col-span-3">
            <legend className="text-sm font-medium">{t('rentalFleet:fields.depositModes')}</legend>
            <div className="flex flex-wrap gap-4">
              {DEPOSIT_MODES.map((m) => (
                <div key={m} className="flex items-center gap-2">
                  <Checkbox
                    id={`s-mode-${m}`}
                    checked={modes.includes(m)}
                    onCheckedChange={(v) =>
                      setModes(v ? [...modes, m] : modes.filter((x) => x !== m))
                    }
                  />
                  <Label htmlFor={`s-mode-${m}`}>{t(`rentalFleet:enums.depositMode.${m}`)}</Label>
                </div>
              ))}
            </div>
            {modes.includes('preauth') && !settings.preauth_available ? (
              <p className="text-muted-foreground text-xs">
                {t('rentalFleet:settings.preauthUnavailable')}
              </p>
            ) : null}
          </fieldset>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('rentalFleet:settings.checklist')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {checklist.map((c, i) => (
            <div
              key={i}
              className="flex flex-col gap-2 rounded-md border p-3"
              data-testid="checklist-row"
            >
              <div className="flex flex-wrap items-end gap-2">
                <Field className="w-auto max-w-40">
                  <FieldLabel htmlFor={`c-${i}-key`}>{t('rentalFleet:fields.key')}</FieldLabel>
                  <Input
                    id={`c-${i}-key`}
                    value={c.key}
                    maxLength={32}
                    onChange={(e) => setItem(i, { key: e.target.value })}
                  />
                </Field>
                <Field className="w-auto min-w-48 flex-1">
                  <FieldLabel htmlFor={`c-${i}-it`}>{t('rentalFleet:fields.label')}</FieldLabel>
                  <Input
                    id={`c-${i}-it`}
                    value={c.label.it}
                    maxLength={80}
                    onChange={(e) => setItem(i, { label: { ...c.label, it: e.target.value } })}
                  />
                </Field>
                <Field className="w-auto min-w-48 flex-1">
                  <FieldLabel htmlFor={`c-${i}-en`}>{t('rentalFleet:fields.labelEn')}</FieldLabel>
                  <Input
                    id={`c-${i}-en`}
                    value={c.label.en ?? ''}
                    maxLength={80}
                    onChange={(e) => setItem(i, { label: { ...c.label, en: e.target.value } })}
                  />
                </Field>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={t('common:actions.remove')}
                  onClick={() => setChecklist(checklist.filter((_, j) => j !== i))}
                >
                  <TrashIcon />
                </Button>
              </div>
              <CategoryChecks
                idPrefix={`c-${i}-cat`}
                label={t('rentalFleet:fields.categories')}
                value={c.categories}
                onChange={(v: RentalCategory[]) => setItem(i, { categories: v })}
              />
            </div>
          ))}
          <div>
            <Button
              variant="outline"
              size="sm"
              disabled={checklist.length >= 30}
              onClick={() =>
                setChecklist([
                  ...checklist,
                  { key: `item_${checklist.length + 1}`, label: { it: '' }, categories: [] },
                ])
              }
            >
              <PlusIcon />
              {t('common:actions.add')}
            </Button>
          </div>
        </CardContent>
      </Card>
      <M4ProblemAlert error={put.error} />
      {canConfigure ? (
        <div className="flex justify-end">
          <Button disabled={!valid || put.isPending} onClick={save} data-testid="settings-save">
            {put.isPending ? t('common:actions.saving') : t('common:actions.save')}
          </Button>
        </div>
      ) : null}
    </fieldset>
  );
}
