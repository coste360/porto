import { useEffect, useMemo, useState } from 'react';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import {
  toProblem,
  useCreateStorageContract,
  useMap,
  useSimulatePrice,
  useStorageAreas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, LengthInput, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import { VesselField } from '../../desk/components/VesselField';
import { NO_VESSEL, type VesselValue } from '../../desk/lib/vessel';
import { useMarinaToday } from '../../registry/lib/today';
import {
  emptyStorageForm,
  storageCreateBody,
  storageFormError,
  storagePreviewBody,
  usesMarinaCradle,
  type StorageForm,
} from '../lib/contract';

const route = getRouteApi('/auth/m/$marinaId/storage/contracts/new');

const TERMS = ['annual', 'seasonal', 'monthly', 'period'] as const;
const SUPPORTS = ['cradle', 'trailer', 'rack', 'none'] as const;
const OWNERS = ['own', 'marina'] as const;
const ROTATIONS = [0, 900, 1800, 2700] as const;

/**
 * New storage contract (spec 17.1, m9-plan §3.2): customer, vessel, term and dates, area with automatic
 * position (the optimizer picks a free one, 409 storage_no_space otherwise) or an explicit one, support and
 * planned haul-out and launch. storage.manage (D3).
 */
export function StorageContractNewPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const search = route.useSearch();
  const navigate = useNavigate();
  const today = useMarinaToday();
  const areas = useStorageAreas(marinaId);
  const create = useCreateStorageContract(marinaId);
  const [form, setForm] = useState<StorageForm>(() => emptyStorageForm(today, search.area_id));
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [vessel, setVessel] = useState<VesselValue>(NO_VESSEL);
  const [submitted, setSubmitted] = useState(false);
  const set = (patch: Partial<StorageForm>) => setForm((f) => ({ ...f, ...patch }));
  const error = storageFormError(form);
  const area = areas.data?.items.find((a) => a.id === form.areaId);
  // Marina cradles: land resources of kind cradle on the map of the chosen area.
  const areaMap = useMap(marinaId, area?.map_id ?? '');
  const cradles = (areaMap.data?.land_resources ?? []).filter(
    (l) => l.kind === 'cradle' && l.active && l.id,
  );
  const canPreview = can('pricing.read');
  const preview = useSimulatePrice(marinaId);
  const previewBody = useMemo(() => storagePreviewBody(form), [form]);
  const previewKey = previewBody ? JSON.stringify(previewBody) : '';
  const runPreview = preview.mutate;
  const resetPreview = preview.reset;
  useEffect(() => {
    if (!canPreview || !previewKey) {
      resetPreview();
      return;
    }
    const h = setTimeout(() => runPreview(JSON.parse(previewKey)), 400);
    return () => clearTimeout(h);
  }, [canPreview, previewKey, runPreview, resetPreview]);
  const noSpace = create.error && toProblem(create.error).type === 'storage_no_space';

  return (
    <RequirePermission allowed={can('storage.manage')}>
      <PageHeader
        title={t('storage:contract.new')}
        description={t('storage:contract.newDescription')}
      />
      <form
        className="flex max-w-3xl flex-col gap-4"
        data-testid="storage-contract-form"
        onSubmit={(e) => {
          e.preventDefault();
          setSubmitted(true);
          if (error) return;
          create.mutate(
            { body: storageCreateBody(form) },
            {
              onSuccess: (r) =>
                void navigate({
                  to: '/m/$marinaId/contracts/$contractId',
                  params: { marinaId, contractId: r.contract.id },
                }),
            },
          );
        }}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="sc-customer">{t('storage:contract.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="sc-customer"
              value={customer}
              onChange={(c) => {
                setCustomer(c);
                set({ customerId: c?.id ?? null });
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-vessel">{t('storage:contract.vessel')}</FieldLabel>
            <VesselField
              marinaId={marinaId}
              idPrefix="sc-vessel"
              value={vessel}
              onChange={(v) => {
                setVessel(v);
                set({ vesselId: v.kind === 'existing' ? v.id : null });
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-term">{t('storage:contract.term')}</FieldLabel>
            <Select
              value={form.term}
              onValueChange={(v) => set({ term: v as StorageForm['term'] })}
            >
              <SelectTrigger id="sc-term">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TERMS.map((x) => (
                  <SelectItem key={x} value={x}>
                    {t(`storage:term.${x}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-area">{t('storage:contract.area')}</FieldLabel>
            <Select value={form.areaId ?? ''} onValueChange={(v) => set({ areaId: v || null })}>
              <SelectTrigger id="sc-area">
                <SelectValue placeholder={t('storage:contract.areaPlaceholder')} />
              </SelectTrigger>
              <SelectContent>
                {(areas.data?.items ?? []).map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name || a.code}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-starts">{t('storage:contract.startsOn')}</FieldLabel>
            <DatePicker
              id="sc-starts"
              value={form.startsOn}
              onChange={(d) => set({ startsOn: d })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-ends">{t('storage:contract.endsOn')}</FieldLabel>
            <DatePicker
              id="sc-ends"
              value={form.endsOn}
              onChange={(d) => set({ endsOn: d })}
              placeholder={t('storage:contract.endsOnDefault')}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-haul">{t('storage:contract.haulOutOn')}</FieldLabel>
            <DatePicker
              id="sc-haul"
              value={form.haulOutOn}
              onChange={(d) => set({ haulOutOn: d })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-launch">{t('storage:contract.launchOn')}</FieldLabel>
            <DatePicker
              id="sc-launch"
              value={form.launchOn}
              onChange={(d) => set({ launchOn: d })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-support">{t('storage:contract.support')}</FieldLabel>
            <Select
              value={form.support}
              onValueChange={(v) => set({ support: v as StorageForm['support'] })}
            >
              <SelectTrigger id="sc-support">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SUPPORTS.map((x) => (
                  <SelectItem key={x} value={x}>
                    {t(`storage:support.${x}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="sc-owner">{t('storage:contract.supportOwner')}</FieldLabel>
            <Select
              value={form.supportOwner}
              onValueChange={(v) => set({ supportOwner: v as StorageForm['supportOwner'] })}
            >
              <SelectTrigger id="sc-owner">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {OWNERS.map((x) => (
                  <SelectItem key={x} value={x}>
                    {t(`storage:supportOwner.${x}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
        {usesMarinaCradle(form) ? (
          <Field className="sm:max-w-sm">
            <FieldLabel htmlFor="sc-cradle">{t('storage:contract.cradle')}</FieldLabel>
            <Select
              value={form.cradleId ?? ''}
              onValueChange={(v) => set({ cradleId: v || null })}
              disabled={cradles.length === 0}
            >
              <SelectTrigger id="sc-cradle" data-testid="storage-cradle-picker">
                <SelectValue
                  placeholder={t(
                    cradles.length === 0
                      ? 'storage:contract.cradleNone'
                      : 'storage:contract.cradlePlaceholder',
                  )}
                />
              </SelectTrigger>
              <SelectContent>
                {cradles.map((l) => (
                  <SelectItem key={l.id} value={l.id!}>
                    {l.code}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        <div className="flex items-center gap-2">
          <Switch
            id="sc-auto"
            checked={form.autoPlace}
            onCheckedChange={(v) => set({ autoPlace: v })}
          />
          <FieldLabel htmlFor="sc-auto">{t('storage:contract.autoPlace')}</FieldLabel>
        </div>
        {!form.autoPlace ? (
          <div className="grid gap-4 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="sc-x">{t('storage:contract.x')}</FieldLabel>
              <LengthInput
                id="sc-x"
                value={form.x}
                locale={i18n.language}
                onChange={(v) => set({ x: v })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="sc-y">{t('storage:contract.y')}</FieldLabel>
              <LengthInput
                id="sc-y"
                value={form.y}
                locale={i18n.language}
                onChange={(v) => set({ y: v })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="sc-rot">{t('storage:contract.rotation')}</FieldLabel>
              <Select
                value={String(form.rotation)}
                onValueChange={(v) => set({ rotation: Number(v) as StorageForm['rotation'] })}
              >
                <SelectTrigger id="sc-rot">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ROTATIONS.map((r) => (
                    <SelectItem key={r} value={String(r)}>
                      {t('storage:contract.degrees', { value: r / 10 })}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>
        ) : null}
        <Field>
          <FieldLabel htmlFor="sc-notes">{t('storage:contract.notesHandling')}</FieldLabel>
          <Textarea
            id="sc-notes"
            maxLength={500}
            value={form.notesHandling}
            onChange={(e) => set({ notesHandling: e.target.value })}
          />
        </Field>
        <div className="flex items-center gap-2">
          <Switch
            id="sc-activate"
            checked={form.activate}
            onCheckedChange={(v) => set({ activate: v })}
          />
          <FieldLabel htmlFor="sc-activate">{t('storage:contract.activate')}</FieldLabel>
        </div>
        {canPreview ? (
          <div
            className="bg-muted/50 rounded-md border p-3 text-sm"
            data-testid="storage-price-preview"
            aria-live="polite"
          >
            <p className="font-medium">{t('storage:contract.pricePreview')}</p>
            {!previewBody ? (
              <p className="text-muted-foreground">{t('storage:contract.pricePreviewHint')}</p>
            ) : preview.isPending ? (
              <p className="text-muted-foreground">{t('storage:contract.pricePreviewLoading')}</p>
            ) : preview.isError ? (
              <p className="text-muted-foreground">
                {t('storage:contract.pricePreviewUnavailable')}
              </p>
            ) : preview.data ? (
              <p>
                {t('storage:contract.pricePreviewTotal', {
                  total: formatCents(preview.data.total_cents, 'EUR', i18n.language),
                  vat: formatCents(preview.data.vat_cents, 'EUR', i18n.language),
                })}
              </p>
            ) : null}
          </div>
        ) : null}
        {submitted && error ? <FieldError>{t(error)}</FieldError> : null}
        <ProblemAlert error={create.error} />
        {noSpace ? (
          <p className="text-muted-foreground text-sm">{t('storage:contract.noSpaceHint')}</p>
        ) : null}
        <div>
          <Button type="submit" disabled={create.isPending}>
            {t('storage:contract.create')}
          </Button>
        </div>
      </form>
    </RequirePermission>
  );
}
