import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState, type ComponentProps, type ReactNode } from 'react';
import { Controller, useFieldArray, useForm, useWatch, type Control } from 'react-hook-form';

import {
  useBerthGroups,
  useCustomers,
  usePricingToolBerthCategories,
  usePricingToolBerths,
  usePricingToolItems,
  usePricingToolPriceLists,
  useVessels,
  type PricingSimulateRequest,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm, formatQtyMilli, parseQtyToMilli } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { InputGroup, InputGroupInput } from '@porto/shared/ui/input-group';
import { Label } from '@porto/shared/ui/label';
import {
  DatePicker,
  ElementCombobox,
  FormField,
  LengthInput,
  MoneyInput,
  PercentInput,
} from '@porto/shared/ui/patterns';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../../auth/hooks';
import { localName } from '../../maps/lib';
import { nightsBetween } from '../lib/dates';
import {
  CHANNELS,
  CONTRACT_TYPES,
  CUSTOMER_CATEGORIES,
  HULL_TYPES,
  MULTIHULLS,
} from '../lib/enums';
import { numberLocale } from '../lib/format';
import { simulatorSchema, toSimulateRequest, type SimulatorValues } from './simulatorValues';

const NONE = '__none__';

export type SimulatorFormProps = {
  defaultValues: SimulatorValues;
  pending: boolean;
  onSubmit: (request: PricingSimulateRequest) => void;
};

/** The stay to price; submitting sends the mapped request (integers only) to the server. */
export function SimulatorForm({ defaultValues, pending, onSubmit }: SimulatorFormProps) {
  const { t, i18n } = useT();
  const locale = numberLocale(i18n.language);
  const { marinaId, can } = useCurrentMarina();
  const form = useForm<SimulatorValues>({
    resolver: zodResolver(simulatorSchema),
    defaultValues,
  });
  const { control } = form;
  const [vesselMode, resourceMode, customerMode, depositMode, arrival, departure, hullType] =
    useWatch({
      control,
      name: [
        'vessel_mode',
        'resource_mode',
        'customer_mode',
        'deposit_mode',
        'arrival',
        'departure',
        'hull_type',
      ],
    });
  const nights = nightsBetween(arrival, departure);

  const lists = usePricingToolPriceLists(marinaId);
  const items = usePricingToolItems(marinaId);
  const categories = usePricingToolBerthCategories(marinaId);
  const canRegistry = can('customers.read');
  const canMap = can('map.view');

  const berthFees = (items.data?.items ?? []).filter((i) => i.item_type === 'berth_fee');
  const services = (items.data?.items ?? []).filter((i) => i.item_type !== 'berth_fee');
  const categoryCodes = [
    ...new Set([
      ...(categories.data?.items ?? []).filter((c) => !c.archived).map((c) => c.code),
      ...(categories.data?.unknown_in_use ?? []).map((c) => c.code),
    ]),
  ];

  return (
    <form
      noValidate
      aria-label={t('pricingTools:simulator.formLabel')}
      className="flex flex-col gap-4"
      onSubmit={form.handleSubmit((v) => onSubmit(toSimulateRequest(v)))}
    >
      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:simulator.stay')}</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <FormField
            control={control}
            name="price_list_id"
            label={t('pricingTools:simulator.priceList')}
            render={(field) => (
              <OptionSelect
                id={field.id}
                value={field.value}
                onChange={field.onChange}
                emptyLabel={t('pricingTools:simulator.activeOnArrival')}
                options={(lists.data?.items ?? [])
                  .filter((l) => l.status !== 'archived')
                  .map((l) => ({
                    value: l.id,
                    label: `${l.code} · ${t(`pricingEnums:priceListStatuses.${l.status}`)}`,
                  }))}
              />
            )}
          />
          <FormField
            control={control}
            name="contract_type"
            label={t('pricingTools:simulator.contractType')}
            render={(field) => (
              <OptionSelect
                id={field.id}
                value={field.value}
                onChange={field.onChange}
                options={CONTRACT_TYPES.map((c) => ({
                  value: c,
                  label: t(`pricingEnums:contractTypes.${c}`),
                }))}
              />
            )}
          />
          <FormField
            control={control}
            name="arrival"
            label={t('pricingTools:simulator.arrival')}
            render={(field) => (
              <DatePicker
                id={field.id}
                value={field.value}
                onChange={field.onChange}
                aria-invalid={field['aria-invalid']}
                placeholder={t('pricingTools:simulator.pickDate')}
              />
            )}
          />
          <FormField
            control={control}
            name="departure"
            label={t('pricingTools:simulator.departure')}
            description={
              nights !== null && nights > 0 ? (
                <span data-testid="simulator-nights">
                  {t('pricingTools:simulator.nights', { count: nights })}
                </span>
              ) : undefined
            }
            render={(field) => (
              <DatePicker
                id={field.id}
                value={field.value}
                onChange={field.onChange}
                aria-invalid={field['aria-invalid']}
                placeholder={t('pricingTools:simulator.pickDate')}
              />
            )}
          />
          <FormField
            control={control}
            name="item_id"
            label={t('pricingTools:simulator.item')}
            render={(field) => (
              <OptionSelect
                id={field.id}
                value={field.value}
                onChange={field.onChange}
                emptyLabel={t('pricingTools:simulator.primaryItem')}
                options={berthFees.map((i) => ({
                  value: i.id,
                  label: `${i.code} · ${localName(i.name, i18n.language)}`,
                }))}
              />
            )}
          />
          <FormField
            control={control}
            name="quoted_on"
            label={t('pricingTools:simulator.quotedOn')}
            description={t('pricingTools:simulator.quotedOnHint')}
            render={(field) => (
              <DatePicker
                id={field.id}
                value={field.value}
                onChange={field.onChange}
                placeholder={t('pricingTools:simulator.today')}
              />
            )}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:simulator.vessel')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {canRegistry ? (
            <ModeRadio
              control={control}
              name="vessel_mode"
              legend={t('pricingTools:simulator.vesselMode')}
              options={[
                { value: 'manual', label: t('pricingTools:simulator.vesselManual') },
                { value: 'registry', label: t('pricingTools:simulator.vesselRegistry') },
              ]}
            />
          ) : null}
          {vesselMode === 'registry' ? (
            <FormField
              control={control}
              name="vessel_id"
              label={t('pricingTools:simulator.vesselPick')}
              render={(field) => (
                <VesselCombobox
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  invalid={field['aria-invalid']}
                />
              )}
            />
          ) : (
            <div className="grid gap-4 sm:grid-cols-3">
              <FormField
                control={control}
                name="loa_cm"
                label={t('pricingTools:simulator.loa')}
                render={(field) => (
                  <LengthInput
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    onBlur={field.onBlur}
                    aria-invalid={field['aria-invalid']}
                    locale={locale}
                  />
                )}
              />
              <FormField
                control={control}
                name="beam_cm"
                label={t('pricingTools:simulator.beam')}
                render={(field) => (
                  <LengthInput
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    onBlur={field.onBlur}
                    aria-invalid={field['aria-invalid']}
                    locale={locale}
                  />
                )}
              />
              <FormField
                control={control}
                name="hull_type"
                label={t('pricingTools:simulator.hullType')}
                render={(field) => (
                  <OptionSelect
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    options={HULL_TYPES.map((h) => ({
                      value: h,
                      label: t(`pricingEnums:hullTypes.${h}`),
                    }))}
                  />
                )}
              />
              {MULTIHULLS.includes(hullType) ? (
                <p className="text-sm text-muted-foreground sm:col-span-3">
                  {t('pricingTools:simulator.multihullHint')}
                </p>
              ) : null}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:simulator.resource')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {canMap ? (
            <ModeRadio
              control={control}
              name="resource_mode"
              legend={t('pricingTools:simulator.resourceMode')}
              options={[
                { value: 'category', label: t('pricingTools:simulator.resourceCategory') },
                { value: 'berth', label: t('pricingTools:simulator.resourceBerth') },
              ]}
            />
          ) : null}
          {resourceMode === 'berth' ? (
            <FormField
              control={control}
              name="berth_id"
              label={t('pricingTools:simulator.berthPick')}
              render={(field) => (
                <BerthCombobox
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  invalid={field['aria-invalid']}
                />
              )}
            />
          ) : null}
          <div className="grid gap-4 sm:grid-cols-3">
            <FormField
              control={control}
              name="category"
              label={t('pricingTools:simulator.category')}
              description={
                resourceMode === 'berth' ? t('pricingTools:simulator.overrideHint') : undefined
              }
              render={(field) => (
                <OptionSelect
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  emptyLabel={t('pricingTools:simulator.noCategory')}
                  options={categoryCodes.map((c) => ({ value: c, label: c }))}
                />
              )}
            />
            <FormField
              control={control}
              name="berth_length_cm"
              label={t('pricingTools:simulator.berthLength')}
              render={(field) => (
                <LengthInput
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  aria-invalid={field['aria-invalid']}
                  locale={locale}
                />
              )}
            />
            <FormField
              control={control}
              name="berth_width_cm"
              label={t('pricingTools:simulator.berthWidth')}
              render={(field) => (
                <LengthInput
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  aria-invalid={field['aria-invalid']}
                  locale={locale}
                />
              )}
            />
          </div>
          {canMap ? <GroupsField control={control} /> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:simulator.customer')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {canRegistry ? (
            <ModeRadio
              control={control}
              name="customer_mode"
              legend={t('pricingTools:simulator.customerMode')}
              options={[
                { value: 'category', label: t('pricingTools:simulator.customerCategory') },
                { value: 'registry', label: t('pricingTools:simulator.customerRegistry') },
              ]}
            />
          ) : null}
          <div className="grid gap-4 sm:grid-cols-3">
            {customerMode === 'registry' ? (
              <FormField
                control={control}
                name="customer_id"
                label={t('pricingTools:simulator.customerPick')}
                render={(field) => (
                  <CustomerCombobox
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    invalid={field['aria-invalid']}
                  />
                )}
              />
            ) : (
              <FormField
                control={control}
                name="customer_category"
                label={t('pricingTools:simulator.customerCategory')}
                render={(field) => (
                  <OptionSelect
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    emptyLabel={t('pricingTools:simulator.noCustomerCategory')}
                    options={CUSTOMER_CATEGORIES.map((c) => ({
                      value: c,
                      label: t(`pricingEnums:customerCategories.${c}`),
                    }))}
                  />
                )}
              />
            )}
            <FormField
              control={control}
              name="channel"
              label={t('pricingTools:simulator.channel')}
              render={(field) => (
                <OptionSelect
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  options={CHANNELS.map((c) => ({
                    value: c,
                    label: t(`pricingEnums:channels.${c}`),
                  }))}
                />
              )}
            />
            <FormField
              control={control}
              name="promo_code"
              label={t('pricingTools:simulator.promoCode')}
              render={(field) => (
                <Input
                  id={field.id}
                  value={field.value}
                  onChange={(e) => field.onChange(e.target.value)}
                  onBlur={field.onBlur}
                  autoComplete="off"
                  aria-invalid={field['aria-invalid']}
                />
              )}
            />
          </div>
        </CardContent>
      </Card>

      <ServicesCard control={control} services={services} />

      <Card>
        <CardHeader>
          <CardTitle>{t('pricingTools:simulator.deposit')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <ModeRadio
            control={control}
            name="deposit_mode"
            legend={t('pricingTools:simulator.depositMode')}
            options={[
              { value: 'default', label: t('pricingTools:simulator.depositDefault') },
              { value: 'percent', label: t('pricingTools:simulator.depositPercent') },
              { value: 'amount', label: t('pricingTools:simulator.depositAmount') },
            ]}
          />
          {depositMode === 'percent' ? (
            <FormField
              control={control}
              name="deposit_percent_bp"
              label={t('pricingTools:simulator.depositPercent')}
              render={(field) => (
                <PercentInput
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  aria-invalid={field['aria-invalid']}
                  locale={locale}
                />
              )}
            />
          ) : null}
          {depositMode === 'amount' ? (
            <FormField
              control={control}
              name="deposit_amount_cents"
              label={t('pricingTools:simulator.depositAmount')}
              render={(field) => (
                <MoneyInput
                  id={field.id}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  aria-invalid={field['aria-invalid']}
                  locale={locale}
                />
              )}
            />
          ) : null}
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button type="submit" disabled={pending}>
          {t('pricingTools:simulator.calculate')}
        </Button>
      </div>
    </form>
  );
}

function OptionSelect({
  id,
  value,
  onChange,
  options,
  emptyLabel,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  /** Label of the empty choice; without it a value is required. */
  emptyLabel?: string;
}) {
  return (
    <Select value={value || NONE} onValueChange={(v) => onChange(v === NONE ? '' : v)}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {emptyLabel ? <SelectItem value={NONE}>{emptyLabel}</SelectItem> : null}
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function ModeRadio<N extends 'vessel_mode' | 'resource_mode' | 'customer_mode' | 'deposit_mode'>({
  control,
  name,
  legend,
  options,
}: {
  control: Control<SimulatorValues>;
  name: N;
  legend: ReactNode;
  options: { value: SimulatorValues[N]; label: string }[];
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <FieldSet>
          <FieldLegend variant="label">{legend}</FieldLegend>
          <RadioGroup
            value={field.value}
            onValueChange={field.onChange}
            className="flex flex-wrap gap-4"
          >
            {options.map((o) => (
              <div key={o.value} className="flex items-center gap-2">
                <RadioGroupItem value={o.value} id={`${name}-${o.value}`} />
                <Label htmlFor={`${name}-${o.value}`}>{o.label}</Label>
              </div>
            ))}
          </RadioGroup>
        </FieldSet>
      )}
    />
  );
}

function VesselCombobox({
  id,
  value,
  onChange,
  invalid,
}: {
  id: string;
  value: string | null;
  onChange: (v: string | null) => void;
  invalid: boolean;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const vessels = useVessels(marinaId, q ? { q } : {});
  const locale = numberLocale(i18n.language);
  const options = (vessels.data?.pages ?? [])
    .flatMap((p) => p.items)
    .map((v) => ({
      value: v.id,
      label: `${v.name} · ${formatCm(v.effective_loa_cm, locale)}`,
      keywords: v.registration ? [v.registration] : [],
    }));
  return (
    <ElementCombobox
      id={id}
      options={options}
      value={value}
      onChange={onChange}
      onSearch={setQ}
      aria-invalid={invalid}
      placeholder={t('pricingTools:simulator.vesselPick')}
      searchPlaceholder={t('pricingTools:simulator.search')}
      emptyText={t('pricingTools:simulator.noResults')}
    />
  );
}

function CustomerCombobox({
  id,
  value,
  onChange,
  invalid,
}: {
  id: string;
  value: string | null;
  onChange: (v: string | null) => void;
  invalid: boolean;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const customers = useCustomers(marinaId, q ? { q } : {});
  const options = (customers.data?.pages ?? [])
    .flatMap((p) => p.items)
    .map((c) => ({
      value: c.id,
      label: c.category
        ? `${c.display_name} · ${t(`pricingEnums:customerCategories.${c.category}`)}`
        : c.display_name,
    }));
  return (
    <ElementCombobox
      id={id}
      options={options}
      value={value}
      onChange={onChange}
      onSearch={setQ}
      aria-invalid={invalid}
      placeholder={t('pricingTools:simulator.customerPick')}
      searchPlaceholder={t('pricingTools:simulator.search')}
      emptyText={t('pricingTools:simulator.noResults')}
    />
  );
}

function BerthCombobox({
  id,
  value,
  onChange,
  invalid,
}: {
  id: string;
  value: string | null;
  onChange: (v: string | null) => void;
  invalid: boolean;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const berths = usePricingToolBerths(marinaId, q);
  const locale = numberLocale(i18n.language);
  const list = berths.data?.items ?? [];
  const options = list.map((b) => ({
    value: b.id,
    label: `${b.code}${b.price_class ? ` · ${b.price_class}` : ''} · ${formatCm(b.length_cm, locale)} × ${formatCm(b.width_cm, locale)}`,
  }));
  return (
    <ElementCombobox
      id={id}
      options={options}
      value={value}
      onChange={onChange}
      onSearch={setQ}
      aria-invalid={invalid}
      placeholder={t('pricingTools:simulator.berthPick')}
      searchPlaceholder={t('pricingTools:simulator.search')}
      emptyText={t('pricingTools:simulator.noResults')}
    />
  );
}

function GroupsField({ control }: { control: Control<SimulatorValues> }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const groups = useBerthGroups(marinaId);
  const selected = useWatch({ control, name: 'group_ids' });
  if (!groups.data || groups.data.length === 0) return null;
  return (
    <FormField
      control={control}
      name="group_ids"
      label={t('pricingTools:simulator.groups')}
      render={(field) => (
        <div className="flex flex-wrap gap-4" id={field.id}>
          {(groups.data ?? []).map((g) => {
            const checked = selected.includes(g.id);
            return (
              <div key={g.id} className="flex items-center gap-2">
                <Checkbox
                  id={`group-${g.id}`}
                  checked={checked}
                  onCheckedChange={(c) =>
                    field.onChange(
                      c === true ? [...selected, g.id] : selected.filter((x) => x !== g.id),
                    )
                  }
                />
                <Label htmlFor={`group-${g.id}`}>
                  {g.code} · {localName(g.name, i18n.language)}
                </Label>
              </div>
            );
          })}
        </div>
      )}
    />
  );
}

function ServicesCard({
  control,
  services,
}: {
  control: Control<SimulatorValues>;
  services: { id: string; code: string; name: { it: string; en?: string } }[];
}) {
  const { t, i18n } = useT();
  const { fields, append, remove } = useFieldArray({ control, name: 'services' });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('pricingTools:simulator.services')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {fields.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('pricingTools:simulator.noServices')}</p>
        ) : null}
        {fields.map((f, i) => (
          <div key={f.id} className="flex flex-col gap-4 sm:flex-row sm:items-end">
            <div className="flex-1">
              <FormField
                control={control}
                name={`services.${i}.item_id`}
                label={t('pricingTools:simulator.serviceItem', { n: i + 1 })}
                render={(field) => (
                  <OptionSelect
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    emptyLabel={t('pricingTools:simulator.pickItem')}
                    options={services.map((s) => ({
                      value: s.id,
                      label: `${s.code} · ${localName(s.name, i18n.language)}`,
                    }))}
                  />
                )}
              />
            </div>
            <div className="sm:w-48">
              <FormField
                control={control}
                name={`services.${i}.qty_milli`}
                label={t('pricingTools:simulator.serviceQty', { n: i + 1 })}
                render={(field) => (
                  <QtyInput
                    id={field.id}
                    value={field.value}
                    onChange={field.onChange}
                    aria-invalid={field['aria-invalid']}
                    locale={numberLocale(i18n.language)}
                  />
                )}
              />
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('pricingTools:simulator.removeService', { n: i + 1 })}
              onClick={() => remove(i)}
            >
              <Trash2Icon />
            </Button>
          </div>
        ))}
        <div>
          <Button
            type="button"
            variant="outline"
            onClick={() => append({ item_id: '', qty_milli: null })}
          >
            <PlusIcon />
            {t('pricingTools:simulator.addService')}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

/** A quantity typed with decimals (at most three) kept as integer thousandths. */
function QtyInput({
  value,
  onChange,
  locale,
  ...props
}: Omit<ComponentProps<'input'>, 'value' | 'onChange' | 'type'> & {
  value: number | null;
  onChange: (milli: number | null) => void;
  locale: string;
}) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? (value === null ? '' : formatQtyMilli(value, locale));
  return (
    <InputGroup>
      <InputGroupInput
        {...props}
        inputMode="decimal"
        value={shown}
        onChange={(e) => {
          setText(e.target.value);
          onChange(e.target.value.trim() === '' ? null : parseQtyToMilli(e.target.value, locale));
        }}
        onBlur={() => {
          if (text !== null && parseQtyToMilli(text, locale) !== null) setText(null);
        }}
      />
    </InputGroup>
  );
}
