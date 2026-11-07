import { zodResolver } from '@hookform/resolvers/zod';
import { useForm, useWatch } from 'react-hook-form';
import { toast } from 'sonner';

import {
  useCreateItem,
  useUpdateItem,
  type Item,
  type PricingSettings,
  type RevenueClass,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, PercentInput } from '@porto/shared/ui/patterns';
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
import { Textarea } from '@porto/shared/ui/textarea';

import { toastProblem } from '../../../lib/toast';
import { IntegerInput } from '../components/IntegerInput';
import { ITEM_TYPES, ITEM_UNITS, VAT_NATURES, VAT_PRESETS_BP } from '../lib/enums';
import { revenueClassOptions } from '../lib/labels';
import {
  CUSTOM_VAT,
  itemCreateBody,
  itemDefaults,
  itemPatchBody,
  itemSchema,
  vatRateOf,
  type ItemFormIn,
  type ItemFormOut,
} from './itemSchema';

export type ItemSheetProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** null = create. */
  item: Item | null;
  settings: PricingSettings | undefined;
  revenueClasses: RevenueClass[];
  readOnly?: boolean;
};

/** Create/edit Sheet of an item (code, names, type, unit, VAT, revenue class, export code, flags). */
export function ItemSheet(props: ItemSheetProps) {
  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
        {props.open ? <ItemForm key={props.item?.id ?? 'new'} {...props} /> : null}
      </SheetContent>
    </Sheet>
  );
}

function ItemForm({
  marinaId,
  item,
  settings,
  revenueClasses,
  onOpenChange,
  readOnly,
}: ItemSheetProps) {
  const { t, i18n } = useT();
  const create = useCreateItem(marinaId);
  const update = useUpdateItem(marinaId);
  const classes = revenueClassOptions(revenueClasses, i18n.language).filter(
    (c) => !c.archived || c.id === item?.revenue_class_id,
  );
  const form = useForm<ItemFormIn, unknown, ItemFormOut>({
    resolver: zodResolver(itemSchema),
    defaultValues: itemDefaults(item, settings, classes[0]?.id ?? ''),
  });
  const preset = useWatch({ control: form.control, name: 'vat_preset' });
  const customBp = useWatch({ control: form.control, name: 'vat_custom_bp' });
  const rate = vatRateOf({ vat_preset: preset, vat_custom_bp: customBp });

  const submit = form.handleSubmit(async (v) => {
    try {
      if (item) {
        const patch = itemPatchBody(v, item);
        if (Object.keys(patch).length > 0) {
          await update.mutateAsync({ id: item.id, version: item.version, patch });
        }
      } else {
        await create.mutateAsync(itemCreateBody(v));
      }
      toast.success(t(item ? 'pricing:items.updated' : 'pricing:items.created'));
      onOpenChange(false);
    } catch (err) {
      toastProblem(t, err);
    }
  });

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-4">
      <SheetHeader>
        <SheetTitle>{t(item ? 'pricing:items.editTitle' : 'pricing:items.newTitle')}</SheetTitle>
        <SheetDescription>{t('pricing:items.sheetHint')}</SheetDescription>
      </SheetHeader>
      <fieldset disabled={readOnly} className="px-4">
        <FieldGroup>
          <FormField
            control={form.control}
            name="code"
            label={t('pricing:items.fields.code')}
            description={t('pricing:items.fields.codeHint')}
            render={(field) => (
              <Input {...field} autoComplete="off" className="font-mono uppercase" maxLength={32} />
            )}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="name_it"
              label={t('pricing:items.fields.nameIt')}
              render={(field) => <Input {...field} autoComplete="off" />}
            />
            <FormField
              control={form.control}
              name="name_en"
              label={t('pricing:items.fields.nameEn')}
              render={(field) => <Input {...field} autoComplete="off" />}
            />
          </div>
          <FormField
            control={form.control}
            name="description_it"
            label={t('pricing:items.fields.descriptionIt')}
            render={(field) => <Textarea {...field} rows={2} />}
          />
          <FormField
            control={form.control}
            name="description_en"
            label={t('pricing:items.fields.descriptionEn')}
            render={(field) => <Textarea {...field} rows={2} />}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="item_type"
              label={t('pricing:items.fields.type')}
              render={({ value, onChange, id }) => (
                <Select value={value} onValueChange={onChange} disabled={readOnly}>
                  <SelectTrigger id={id} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {ITEM_TYPES.map((x) => (
                      <SelectItem key={x} value={x}>
                        {t(`pricingEnums:itemTypes.${x}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
            <FormField
              control={form.control}
              name="unit"
              label={t('pricing:items.fields.unit')}
              render={({ value, onChange, id }) => (
                <Select value={value} onValueChange={onChange} disabled={readOnly}>
                  <SelectTrigger id={id} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {ITEM_UNITS.map((x) => (
                      <SelectItem key={x} value={x}>
                        {t(`pricingEnums:itemUnits.${x}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="vat_preset"
              label={t('pricing:items.fields.vatRate')}
              render={({ value, onChange, id }) => (
                <Select
                  value={value}
                  disabled={readOnly}
                  onValueChange={(v) => {
                    onChange(v);
                    // A non-zero rate excludes the nature (exclusive fields).
                    if (v !== '0' && v !== CUSTOM_VAT) form.setValue('vat_nature', '');
                  }}
                >
                  <SelectTrigger id={id} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {VAT_PRESETS_BP.map((bp) => (
                      <SelectItem key={bp} value={String(bp)}>
                        {formatBp(bp, i18n.language)}
                      </SelectItem>
                    ))}
                    <SelectItem value={CUSTOM_VAT}>
                      {t('pricing:items.fields.vatCustom')}
                    </SelectItem>
                  </SelectContent>
                </Select>
              )}
            />
            {preset === CUSTOM_VAT ? (
              <FormField
                control={form.control}
                name="vat_custom_bp"
                label={t('pricing:items.fields.vatCustomRate')}
                render={({ value, onChange, onBlur, id, ...rest }) => (
                  <PercentInput
                    id={id}
                    value={value}
                    onChange={(bp) => {
                      onChange(bp);
                      if (bp !== 0) form.setValue('vat_nature', '');
                    }}
                    onBlur={onBlur}
                    locale={i18n.language}
                    aria-invalid={rest['aria-invalid']}
                  />
                )}
              />
            ) : null}
          </div>
          {rate === 0 ? (
            <FormField
              control={form.control}
              name="vat_nature"
              label={t('pricing:items.fields.vatNature')}
              description={t('pricing:items.fields.vatNatureHint')}
              render={({ value, onChange, id, ...rest }) => (
                <Select value={value} onValueChange={onChange} disabled={readOnly}>
                  <SelectTrigger id={id} className="w-full" aria-invalid={rest['aria-invalid']}>
                    <SelectValue placeholder={t('pricing:items.fields.vatNaturePlaceholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {VAT_NATURES.map((n) => (
                      <SelectItem key={n} value={n}>
                        {t(`pricingEnums:vatNatures.${n}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          ) : null}
          <FormField
            control={form.control}
            name="price_includes_vat"
            label={t('pricing:items.fields.includesVat')}
            description={t('pricing:items.fields.includesVatHint')}
            render={({ value, onChange, id }) => (
              <Switch id={id} checked={value} onCheckedChange={onChange} disabled={readOnly} />
            )}
          />
          <FormField
            control={form.control}
            name="revenue_class_id"
            label={t('pricing:items.fields.revenueClass')}
            render={({ value, onChange, id, ...rest }) => (
              <Select value={value} onValueChange={onChange} disabled={readOnly}>
                <SelectTrigger id={id} className="w-full" aria-invalid={rest['aria-invalid']}>
                  <SelectValue placeholder={t('pricing:items.fields.revenueClassPlaceholder')} />
                </SelectTrigger>
                <SelectContent>
                  {classes.map((c) => (
                    <SelectItem key={c.id} value={c.id}>
                      {c.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="external_code"
              label={t('pricing:items.fields.externalCode')}
              description={t('pricing:items.fields.externalCodeHint')}
              render={(field) => <Input {...field} autoComplete="off" maxLength={64} />}
            />
            <FormField
              control={form.control}
              name="sort_order"
              label={t('pricing:items.fields.sortOrder')}
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
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="online_visible"
              label={t('pricing:items.fields.onlineVisible')}
              render={({ value, onChange, id }) => (
                <Switch id={id} checked={value} onCheckedChange={onChange} disabled={readOnly} />
              )}
            />
            <FormField
              control={form.control}
              name="active"
              label={t('pricing:items.fields.active')}
              render={({ value, onChange, id }) => (
                <Switch id={id} checked={value} onCheckedChange={onChange} disabled={readOnly} />
              )}
            />
          </div>
        </FieldGroup>
      </fieldset>
      <SheetFooter>
        {readOnly ? null : (
          <Button type="submit" disabled={form.formState.isSubmitting}>
            {t('common:actions.save')}
          </Button>
        )}
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('common:actions.close')}
        </Button>
      </SheetFooter>
    </form>
  );
}
