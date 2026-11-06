import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useFieldArray, useForm, useWatch, type Control } from 'react-hook-form';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField } from '@porto/shared/ui/patterns';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { CUSTOMER_CATEGORIES } from '../lib/enums';
import { applyFieldErrors } from '../lib/fieldErrors';
import { SelectField, TextAreaField, TextField } from '../components/fields';
import { customerSchema, emptyCustomerValues, type CustomerFormValues } from './customerSchema';

export type CustomerFormProps = {
  defaultValues?: CustomerFormValues;
  /** Masked IBAN currently stored (edit only). */
  ibanMasked?: string | null;
  submitLabel: string;
  pending: boolean;
  disabled?: boolean;
  onSubmit: (values: CustomerFormValues, opts: { clearIban: boolean }) => void;
  onCancel?: () => void;
  /** Error of the last submit: validation_failed field errors are shown on the fields. */
  error?: unknown;
};

/** Person/company customer form (create and edit). */
export function CustomerForm({
  defaultValues,
  ibanMasked,
  submitLabel,
  pending,
  disabled = false,
  onSubmit,
  onCancel,
  error,
}: CustomerFormProps) {
  const { t } = useT();
  const [clearIban, setClearIban] = useState(false);
  const form = useForm<CustomerFormValues>({
    resolver: zodResolver(customerSchema),
    defaultValues: defaultValues ?? emptyCustomerValues(),
  });
  useEffect(() => {
    if (error) applyFieldErrors(error, form.setError, Object.keys(emptyCustomerValues()));
  }, [error, form.setError]);
  const kind = useWatch({ control: form.control, name: 'kind' });
  const c = form.control;

  return (
    <form
      noValidate
      className="flex flex-col gap-6"
      onSubmit={form.handleSubmit((v) => onSubmit(v, { clearIban }))}
    >
      <fieldset disabled={disabled || pending} className="flex flex-col gap-6">
        <FieldSet>
          <FieldLegend>{t('customers:form.sectionIdentity')}</FieldLegend>
          <FormField
            control={c}
            name="kind"
            label={t('customers:fields.kind')}
            render={({ value, onChange, id }) => (
              <ToggleGroup
                id={id}
                type="single"
                variant="outline"
                value={value}
                onValueChange={(v) => (v ? onChange(v) : undefined)}
                className="justify-start"
              >
                <ToggleGroupItem value="person">{t('customers:kind.person')}</ToggleGroupItem>
                <ToggleGroupItem value="company">{t('customers:kind.company')}</ToggleGroupItem>
              </ToggleGroup>
            )}
          />
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            {kind === 'person' ? (
              <>
                <TextField control={c} name="first_name" label={t('customers:fields.firstName')} />
                <TextField control={c} name="last_name" label={t('customers:fields.lastName')} />
              </>
            ) : (
              <TextField
                control={c}
                name="company_name"
                label={t('customers:fields.companyName')}
              />
            )}
          </FieldGroup>
        </FieldSet>

        <FieldSet>
          <FieldLegend>{t('customers:form.sectionFiscal')}</FieldLegend>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <TextField control={c} name="fiscal_code" label={t('customers:fields.fiscalCode')} />
            <TextField control={c} name="vat_number" label={t('customers:fields.vatNumber')} />
            <TextField control={c} name="sdi_code" label={t('customers:fields.sdiCode')} />
            <TextField control={c} name="pec" label={t('customers:fields.pec')} type="email" />
          </FieldGroup>
        </FieldSet>

        <FieldSet>
          <FieldLegend>{t('customers:form.billingAddress')}</FieldLegend>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <TextField control={c} name="line1" label={t('customers:fields.address.line1')} />
            <TextField control={c} name="line2" label={t('customers:fields.address.line2')} />
            <TextField
              control={c}
              name="postal_code"
              label={t('customers:fields.address.postalCode')}
            />
            <TextField control={c} name="city" label={t('customers:fields.address.city')} />
            <TextField control={c} name="province" label={t('customers:fields.address.province')} />
            <TextField control={c} name="country" label={t('customers:fields.address.country')} />
          </FieldGroup>
        </FieldSet>

        <FieldSet>
          <FieldLegend>{t('customers:form.sectionContacts')}</FieldLegend>
          <ContactList control={c} name="emails" />
          <ContactList control={c} name="phones" />
        </FieldSet>

        <FieldSet>
          <FieldLegend>{t('customers:form.sectionOther')}</FieldLegend>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <TextField control={c} name="nationality" label={t('customers:fields.nationality')} />
            <SelectField
              control={c}
              name="locale"
              label={t('customers:fields.locale')}
              options={[
                { value: 'it', label: t('common:locales.it') },
                { value: 'en', label: t('common:locales.en') },
              ]}
            />
            <SelectField
              control={c}
              name="category"
              label={t('customers:fields.category')}
              noneLabel={t('customers:noCategory')}
              options={CUSTOMER_CATEGORIES.map((v) => ({
                value: v,
                label: t(`customers:categories.${v}`),
              }))}
            />
            <TextField
              control={c}
              name="tags"
              label={t('customers:fields.tags')}
              description={t('customers:fields.tagsHint')}
            />
            <TextField control={c} name="payment_days" label={t('customers:fields.paymentDays')} />
            <FormField
              control={c}
              name="end_of_month"
              label={t('customers:fields.endOfMonth')}
              render={({ value, onChange, id }) => (
                <Checkbox id={id} checked={value} onCheckedChange={(v) => onChange(v === true)} />
              )}
            />
            <TextField
              control={c}
              name="iban"
              label={t('customers:fields.iban')}
              description={
                ibanMasked
                  ? t('customers:fields.ibanCurrent', { iban: ibanMasked })
                  : t('customers:fields.ibanHint')
              }
            />
            {ibanMasked ? (
              <Field orientation="horizontal">
                <Checkbox
                  id="customer-clear-iban"
                  checked={clearIban}
                  onCheckedChange={(v) => setClearIban(v === true)}
                />
                <FieldLabel htmlFor="customer-clear-iban">
                  {t('customers:fields.ibanClear')}
                </FieldLabel>
              </Field>
            ) : null}
          </FieldGroup>
          <TextAreaField control={c} name="notes" label={t('customers:fields.notes')} />
        </FieldSet>
      </fieldset>
      <div className="flex justify-end gap-2">
        {onCancel ? (
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common:actions.cancel')}
          </Button>
        ) : null}
        <Button type="submit" disabled={disabled || pending}>
          {pending ? t('common:actions.saving') : submitLabel}
        </Button>
      </div>
    </form>
  );
}

function ContactList({
  control,
  name,
}: {
  control: Control<CustomerFormValues>;
  name: 'emails' | 'phones';
}) {
  const { t } = useT();
  const { fields, append, remove } = useFieldArray({ control, name });
  const legend = name === 'emails' ? t('customers:fields.emails') : t('customers:fields.phones');
  return (
    <div className="flex flex-col gap-2">
      <span className="text-sm font-medium">{legend}</span>
      {fields.map((f, i) => (
        <div key={f.id} className="grid grid-cols-3 items-start gap-2">
          <FormField
            control={control}
            name={`${name}.${i}.label`}
            label={<span className="sr-only">{t('customers:fields.contactLabel')}</span>}
            render={(field) => (
              <Input
                {...field}
                placeholder={t('customers:fields.contactLabel')}
                aria-label={`${legend} ${i + 1}: ${t('customers:fields.contactLabel')}`}
              />
            )}
          />
          <div className="col-span-2 flex items-start gap-2">
            <FormField
              control={control}
              name={`${name}.${i}.value`}
              label={<span className="sr-only">{legend}</span>}
              render={(field) => (
                <Input
                  {...field}
                  type={name === 'emails' ? 'email' : 'tel'}
                  autoComplete="off"
                  aria-label={`${legend} ${i + 1}`}
                />
              )}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('customers:form.removeContact')}
              onClick={() => remove(i)}
            >
              <Trash2Icon />
            </Button>
          </div>
        </div>
      ))}
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => append({ label: '', value: '' })}
        >
          <PlusIcon />
          {name === 'emails' ? t('customers:form.addEmail') : t('customers:form.addPhone')}
        </Button>
      </div>
    </div>
  );
}
