import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useFieldArray, useForm, useWatch, type Control } from 'react-hook-form';

import type { Item, PriceBand, PriceRule } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, LengthInput, MoneyInput, PercentInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@porto/shared/ui/sheet';
import { Switch } from '@porto/shared/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { IntegerInput } from '../components/IntegerInput';
import { MultiSelect } from '../components/MultiSelect';
import { QtyInput } from '../components/QtyInput';
import {
  BASES,
  CHANNELS,
  CONTRACT_TYPES,
  CUSTOMER_CATEGORIES,
  DISCOUNT_KINDS,
  HULL_TYPES,
  ITEM_TYPES,
  TIME_UNITS,
  localText,
} from '../lib/enums';
import { rangeLabel } from '../lib/labels';
import { bandRanges } from './model';
import {
  GRID_BASES,
  UNIT_BASES,
  gridKeys,
  ruleDefaults,
  ruleFromForm,
  ruleSchema,
  type RuleFormIn,
  type RuleFormOut,
} from './ruleSchema';

export type RuleSheetContext = {
  items: Item[];
  bands: PriceBand[];
  categories: string[];
  periods: string[];
  groups: { id: string; label: string }[];
};

export type RuleSheetProps = RuleSheetContext & {
  rule: PriceRule | null;
  readOnly: boolean;
  onSubmit: (rule: PriceRule) => void;
  onClose: () => void;
};

/** Sheet of one price rule: fields depend on the stage and, for base rules, on the basis. */
export function RuleSheet(props: RuleSheetProps) {
  return (
    <Sheet open={props.rule !== null} onOpenChange={(o) => (o ? null : props.onClose())}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        {props.rule ? (
          <RuleForm key={props.rule.id ?? props.rule.key} {...props} rule={props.rule} />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function RuleForm({
  rule,
  readOnly,
  onSubmit,
  onClose,
  items,
  bands,
  categories,
  periods,
  groups,
}: RuleSheetProps & { rule: PriceRule }) {
  const { t, i18n } = useT();
  const bandCodes = bands.map((b) => b.code);
  const form = useForm<RuleFormIn, unknown, RuleFormOut>({
    resolver: zodResolver(ruleSchema),
    defaultValues: ruleDefaults(rule, bandCodes, categories),
  });
  const control = form.control;
  const stage = useWatch({ control, name: 'stage' });
  const basis = useWatch({ control, name: 'basis' });
  const effectKind = useWatch({ control, name: 'effect_kind' });
  const discountKind = useWatch({ control, name: 'discount_kind' });
  const amounts = useFieldArray({ control, name: 'amounts' });
  const includes = useFieldArray({ control, name: 'includes' });
  const ranges = new Map(bandRanges(bands).map((r) => [r.code, r]));

  const itemOptions = items
    .filter((i) => i.active || i.id === rule.item_id)
    .map((i) => ({ value: i.id, label: `${i.code} · ${localText(i.name, i18n.language)}` }));
  const submit = form.handleSubmit((v) => onSubmit(ruleFromForm(v, rule)));

  const unitLabel =
    basis === 'per_meter'
      ? t('pricing:rules.fields.unitCentsPerMeter')
      : basis === 'metered'
        ? t('pricing:rules.fields.unitCentsPerUnit')
        : t('pricing:rules.fields.unitCentsPerM2');

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4">
      <SheetHeader>
        <SheetTitle>{t(`pricingEnums:stages.${stage}`)}</SheetTitle>
        <SheetDescription>{t(`pricing:rules.stageHint.${stage}`)}</SheetDescription>
      </SheetHeader>
      <fieldset disabled={readOnly} className="flex flex-col gap-6 px-4">
        <FieldGroup>
          <FormField
            control={control}
            name="name"
            label={t('pricing:rules.fields.name')}
            render={(field) => <Input {...field} autoComplete="off" maxLength={120} />}
          />
          <div className="grid gap-4 sm:grid-cols-3">
            <FormField
              control={control}
              name="priority"
              label={t('pricing:rules.fields.priority')}
              description={t('pricing:rules.fields.priorityHint')}
              render={({ value, onChange, onBlur, id, ...rest }) => (
                <IntegerInput
                  id={id}
                  value={value}
                  onChange={onChange}
                  onBlur={onBlur}
                  allowNegative
                  aria-invalid={rest['aria-invalid']}
                />
              )}
            />
            <FormField
              control={control}
              name="active"
              label={t('pricing:rules.fields.active')}
              render={({ value, onChange, id }) => (
                <Switch id={id} checked={value} onCheckedChange={onChange} disabled={readOnly} />
              )}
            />
            {stage !== 'base' ? (
              <FormField
                control={control}
                name="stackable"
                label={t('pricing:rules.fields.stackable')}
                description={t('pricing:rules.fields.stackableHint')}
                render={({ value, onChange, id }) => (
                  <Switch id={id} checked={value} onCheckedChange={onChange} disabled={readOnly} />
                )}
              />
            ) : null}
          </div>
        </FieldGroup>

        {stage === 'base' ? (
          <FieldSet>
            <FieldLegend>{t('pricing:rules.sections.price')}</FieldLegend>
            <FieldGroup>
              <FormField
                control={control}
                name="item_id"
                label={t('pricing:rules.fields.item')}
                render={({ value, onChange, id, ...rest }) => (
                  <Select value={value} onValueChange={onChange} disabled={readOnly}>
                    <SelectTrigger id={id} className="w-full" aria-invalid={rest['aria-invalid']}>
                      <SelectValue placeholder={t('pricing:rules.fields.itemPlaceholder')} />
                    </SelectTrigger>
                    <SelectContent>
                      {itemOptions.map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                          {o.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField
                  control={control}
                  name="basis"
                  label={t('pricing:rules.fields.basis')}
                  render={({ value, onChange, id }) => (
                    <Select
                      value={value}
                      disabled={readOnly}
                      onValueChange={(b) => {
                        onChange(b);
                        amounts.replace(
                          gridKeys(b as typeof value, rule, bandCodes, categories).map((key) => ({
                            key,
                            cents: rule.basis === b ? (rule.amounts?.[key] ?? null) : null,
                          })),
                        );
                      }}
                    >
                      <SelectTrigger id={id} className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {BASES.map((b) => (
                          <SelectItem key={b} value={b}>
                            {t(`pricingEnums:bases.${b}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                />
                <FormField
                  control={control}
                  name="unit"
                  label={t('pricing:rules.fields.unit')}
                  render={({ value, onChange, id }) => (
                    <Select value={value} onValueChange={onChange} disabled={readOnly}>
                      <SelectTrigger id={id} className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {TIME_UNITS.map((u) => (
                          <SelectItem key={u} value={u}>
                            {t(`pricingEnums:timeUnits.${u}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                />
              </div>
              {basis === 'flat' ? (
                <MoneyField
                  control={control}
                  name="amount_cents"
                  label={t('pricing:rules.fields.amount')}
                />
              ) : null}
              {UNIT_BASES.includes(basis) ? (
                <MoneyField control={control} name="unit_cents" label={unitLabel} />
              ) : null}
              {GRID_BASES.includes(basis) ? (
                <div className="flex flex-col gap-3" data-testid="amounts-grid">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>
                          {t(
                            basis === 'length_band'
                              ? 'pricing:rules.fields.band'
                              : 'pricing:rules.fields.category',
                          )}
                        </TableHead>
                        <TableHead>{t('pricing:rules.fields.amount')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {amounts.fields.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={2} className="text-muted-foreground text-sm">
                            {t(
                              basis === 'length_band'
                                ? 'pricing:rules.noBands'
                                : 'pricing:rules.noCategories',
                            )}
                          </TableCell>
                        </TableRow>
                      ) : (
                        amounts.fields.map((f, i) => (
                          <TableRow key={f.id}>
                            <TableCell>
                              <span className="font-mono text-sm">{f.key}</span>
                              {basis === 'length_band' && ranges.get(f.key) ? (
                                <span className="text-muted-foreground block text-xs">
                                  {rangeLabel(t, ranges.get(f.key)!, i18n.language)}
                                </span>
                              ) : null}
                            </TableCell>
                            <TableCell>
                              <MoneyField
                                control={control}
                                name={`amounts.${i}.cents`}
                                label={t('pricing:rules.fields.amountFor', { key: f.key })}
                                srOnlyLabel
                              />
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                  <MoneyField
                    control={control}
                    name="amount_cents"
                    label={t('pricing:rules.fields.defaultAmount')}
                    description={t('pricing:rules.fields.defaultAmountHint')}
                  />
                </div>
              ) : null}
              <FieldSet>
                <FieldLegend variant="label">{t('pricing:rules.fields.includes')}</FieldLegend>
                {includes.fields.map((f, i) => (
                  <div key={f.id} className="grid grid-cols-6 items-end gap-2">
                    <div className="col-span-4">
                      <FormField
                        control={control}
                        name={`includes.${i}.item_id`}
                        label={t('pricing:rules.fields.includedItem')}
                        render={({ value, onChange, id }) => (
                          <Select value={value} onValueChange={onChange} disabled={readOnly}>
                            <SelectTrigger id={id} className="w-full">
                              <SelectValue
                                placeholder={t('pricing:rules.fields.itemPlaceholder')}
                              />
                            </SelectTrigger>
                            <SelectContent>
                              {itemOptions.map((o) => (
                                <SelectItem key={o.value} value={o.value}>
                                  {o.label}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        )}
                      />
                    </div>
                    <FormField
                      control={control}
                      name={`includes.${i}.qty_milli`}
                      label={t('pricing:rules.fields.qty')}
                      render={({ value, onChange, onBlur, id, ...rest }) => (
                        <QtyInput
                          id={id}
                          value={value}
                          onChange={onChange}
                          onBlur={onBlur}
                          locale={i18n.language}
                          aria-invalid={rest['aria-invalid']}
                        />
                      )}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t('pricing:rules.removeInclude', { n: i + 1 })}
                      onClick={() => includes.remove(i)}
                    >
                      <Trash2Icon />
                    </Button>
                  </div>
                ))}
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => includes.append({ item_id: '', qty_milli: 1000 })}
                  >
                    <PlusIcon />
                    {t('pricing:rules.addInclude')}
                  </Button>
                </div>
              </FieldSet>
            </FieldGroup>
          </FieldSet>
        ) : (
          <>
            <FieldSet>
              <FieldLegend>{t('pricing:rules.sections.effect')}</FieldLegend>
              <FieldGroup>
                {stage === 'discount' ? (
                  <div className="grid gap-4 sm:grid-cols-2">
                    <FormField
                      control={control}
                      name="discount_kind"
                      label={t('pricing:rules.fields.discountKind')}
                      render={({ value, onChange, id }) => (
                        <Select value={value} onValueChange={onChange} disabled={readOnly}>
                          <SelectTrigger id={id} className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {DISCOUNT_KINDS.map((k) => (
                              <SelectItem key={k} value={k}>
                                {t(`pricingEnums:discountKinds.${k}`)}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )}
                    />
                    {discountKind === 'promo' ? (
                      <FormField
                        control={control}
                        name="promo_code"
                        label={t('pricing:rules.fields.promoCode')}
                        render={(field) => (
                          <Input
                            {...field}
                            autoComplete="off"
                            className="font-mono uppercase"
                            maxLength={32}
                          />
                        )}
                      />
                    ) : null}
                  </div>
                ) : null}
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField
                    control={control}
                    name="effect_kind"
                    label={t('pricing:rules.fields.effectKind')}
                    render={({ value, onChange, id }) => (
                      <Select value={value} onValueChange={onChange} disabled={readOnly}>
                        <SelectTrigger id={id} className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="percent">
                            {t('pricing:rules.fields.effectPercent')}
                          </SelectItem>
                          <SelectItem value="amount">
                            {t('pricing:rules.fields.effectAmount')}
                          </SelectItem>
                        </SelectContent>
                      </Select>
                    )}
                  />
                  {effectKind === 'percent' ? (
                    <FormField
                      control={control}
                      name="effect_percent_bp"
                      label={t(
                        stage === 'discount'
                          ? 'pricing:rules.fields.discountPercent'
                          : 'pricing:rules.fields.modifierPercent',
                      )}
                      render={({ value, onChange, onBlur, id, ...rest }) => (
                        <PercentInput
                          id={id}
                          value={value}
                          onChange={onChange}
                          onBlur={onBlur}
                          locale={i18n.language}
                          aria-invalid={rest['aria-invalid']}
                        />
                      )}
                    />
                  ) : (
                    <MoneyField
                      control={control}
                      name="effect_amount_cents"
                      label={t(
                        stage === 'discount'
                          ? 'pricing:rules.fields.discountAmount'
                          : 'pricing:rules.fields.modifierAmount',
                      )}
                    />
                  )}
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField
                    control={control}
                    name="target_item_ids"
                    label={t('pricing:rules.fields.targetItems')}
                    render={({ value, onChange, id }) => (
                      <MultiSelect
                        id={id}
                        options={itemOptions}
                        value={value}
                        onChange={onChange}
                        emptyLabel={t('pricing:rules.fields.targetDefault')}
                        disabled={readOnly}
                      />
                    )}
                  />
                  <FormField
                    control={control}
                    name="target_item_types"
                    label={t('pricing:rules.fields.targetTypes')}
                    render={({ value, onChange, id }) => (
                      <MultiSelect
                        id={id}
                        options={ITEM_TYPES.map((x) => ({
                          value: x,
                          label: t(`pricingEnums:itemTypes.${x}`),
                        }))}
                        value={value}
                        onChange={(v) => onChange(v as typeof value)}
                        emptyLabel={t('pricing:rules.fields.targetDefault')}
                        disabled={readOnly}
                      />
                    )}
                  />
                </div>
              </FieldGroup>
            </FieldSet>
            <FieldSet>
              <FieldLegend>{t('pricing:rules.sections.conditions')}</FieldLegend>
              <FieldGroup>
                <div className="grid gap-4 sm:grid-cols-2">
                  <FormField
                    control={control}
                    name="hull_types"
                    label={t('pricing:rules.fields.hullTypes')}
                    render={({ value, onChange, id }) => (
                      <MultiSelect
                        id={id}
                        options={HULL_TYPES.map((h) => ({
                          value: h,
                          label: t(`pricingEnums:hullTypes.${h}`),
                        }))}
                        value={value}
                        onChange={(v) => onChange(v as typeof value)}
                        emptyLabel={t('pricing:rules.any')}
                        disabled={readOnly}
                      />
                    )}
                  />
                  <FormField
                    control={control}
                    name="overhang_min_cm"
                    label={t('pricing:rules.fields.overhang')}
                    render={({ value, onChange, onBlur, id, ...rest }) => (
                      <LengthInput
                        id={id}
                        value={value}
                        onChange={onChange}
                        onBlur={onBlur}
                        unit={t('pricing:units.m')}
                        locale={i18n.language}
                        aria-invalid={rest['aria-invalid']}
                      />
                    )}
                  />
                </div>
                <div className="grid gap-4 sm:grid-cols-4">
                  <IntField
                    control={control}
                    name="min_nights"
                    label={t('pricing:rules.fields.minNights')}
                  />
                  <IntField
                    control={control}
                    name="max_nights"
                    label={t('pricing:rules.fields.maxNights')}
                  />
                  <IntField
                    control={control}
                    name="lead_min_days"
                    label={t('pricing:rules.fields.leadMin')}
                  />
                  <IntField
                    control={control}
                    name="lead_max_days"
                    label={t('pricing:rules.fields.leadMax')}
                  />
                </div>
              </FieldGroup>
            </FieldSet>
          </>
        )}

        <FieldSet>
          <FieldLegend>{t('pricing:rules.sections.scope')}</FieldLegend>
          <p className="text-muted-foreground text-sm">{t('pricing:rules.scopeHint')}</p>
          <FieldGroup>
            <div className="grid gap-4 sm:grid-cols-2">
              <ScopeField
                control={control}
                name="scope.contract_types"
                label={t('pricing:rules.fields.contractTypes')}
                options={CONTRACT_TYPES.map((c) => ({
                  value: c,
                  label: t(`pricingEnums:contractTypes.${c}`),
                }))}
                readOnly={readOnly}
              />
              <ScopeField
                control={control}
                name="scope.periods"
                label={t('pricing:rules.fields.periods')}
                options={periods.map((p) => ({ value: p, label: p }))}
                readOnly={readOnly}
              />
              <ScopeField
                control={control}
                name="scope.categories"
                label={t('pricing:rules.fields.categories')}
                options={categories.map((c) => ({ value: c, label: c }))}
                readOnly={readOnly}
              />
              <ScopeField
                control={control}
                name="scope.group_ids"
                label={t('pricing:rules.fields.groups')}
                options={groups.map((g) => ({ value: g.id, label: g.label }))}
                readOnly={readOnly}
              />
              <ScopeField
                control={control}
                name="scope.channels"
                label={t('pricing:rules.fields.channels')}
                options={CHANNELS.map((c) => ({
                  value: c,
                  label: t(`pricingEnums:channels.${c}`),
                }))}
                readOnly={readOnly}
              />
              <ScopeField
                control={control}
                name="scope.customer_categories"
                label={t('pricing:rules.fields.customerCategories')}
                options={CUSTOMER_CATEGORIES.map((c) => ({
                  value: c,
                  label: t(`pricingEnums:customerCategories.${c}`),
                }))}
                readOnly={readOnly}
              />
            </div>
          </FieldGroup>
        </FieldSet>
      </fieldset>
      <SheetFooter>
        {readOnly ? null : <Button type="submit">{t('pricing:rules.apply')}</Button>}
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common:actions.close')}
        </Button>
      </SheetFooter>
    </form>
  );
}

type CentsName = 'amount_cents' | 'unit_cents' | 'effect_amount_cents' | `amounts.${number}.cents`;

function MoneyField({
  control,
  name,
  label,
  description,
  srOnlyLabel,
}: {
  control: Control<RuleFormIn>;
  name: CentsName;
  label: string;
  description?: string;
  srOnlyLabel?: boolean;
}) {
  const { i18n } = useT();
  return (
    <FormField
      control={control}
      name={name}
      label={srOnlyLabel ? <span className="sr-only">{label}</span> : label}
      description={description}
      render={({ value, onChange, onBlur, id, ...rest }) => (
        <MoneyInput
          id={id}
          value={value as number | null}
          onChange={onChange}
          onBlur={onBlur}
          locale={i18n.language}
          aria-invalid={rest['aria-invalid']}
        />
      )}
    />
  );
}

function IntField({
  control,
  name,
  label,
}: {
  control: Control<RuleFormIn>;
  name: 'min_nights' | 'max_nights' | 'lead_min_days' | 'lead_max_days';
  label: string;
}) {
  return (
    <FormField
      control={control}
      name={name}
      label={label}
      render={({ value, onChange, onBlur, id, ...rest }) => (
        <IntegerInput
          id={id}
          value={value}
          onChange={onChange}
          onBlur={onBlur}
          aria-invalid={rest['aria-invalid']}
        />
      )}
    />
  );
}

function ScopeField({
  control,
  name,
  label,
  options,
  readOnly,
}: {
  control: Control<RuleFormIn>;
  name:
    | 'scope.contract_types'
    | 'scope.periods'
    | 'scope.categories'
    | 'scope.group_ids'
    | 'scope.channels'
    | 'scope.customer_categories';
  label: string;
  options: { value: string; label: string }[];
  readOnly: boolean;
}) {
  const { t } = useT();
  return (
    <FormField
      control={control}
      name={name}
      label={label}
      render={({ value, onChange, id }) => (
        <MultiSelect
          id={id}
          options={options}
          value={value as string[]}
          onChange={(v) => onChange(v as never)}
          emptyLabel={t('pricing:rules.any')}
          disabled={readOnly}
        />
      )}
    />
  );
}
