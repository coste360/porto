import type { ReactNode } from 'react';
import type { Control, FieldPath, FieldValues } from 'react-hook-form';

import { useT } from '@porto/shared/i18n';
import { Input } from '@porto/shared/ui/input';
import { FormField, LengthInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

type Base<T extends FieldValues, N extends FieldPath<T>> = {
  control: Control<T>;
  name: N;
  label: ReactNode;
  description?: ReactNode;
};

/** Text input bound to a string field. */
export function TextField<T extends FieldValues, N extends FieldPath<T>>({
  type = 'text',
  autoComplete,
  ...props
}: Base<T, N> & { type?: string; autoComplete?: string }) {
  return (
    <FormField
      {...props}
      render={(field) => (
        <Input
          {...field}
          value={(field.value as string | undefined) ?? ''}
          type={type}
          autoComplete={autoComplete ?? 'off'}
        />
      )}
    />
  );
}

export function TextAreaField<T extends FieldValues, N extends FieldPath<T>>(props: Base<T, N>) {
  return (
    <FormField
      {...props}
      render={(field) => (
        <Textarea {...field} value={(field.value as string | undefined) ?? ''} rows={3} />
      )}
    />
  );
}

/** Length in metres bound to an integer-cm (or null) field. */
export function LengthField<T extends FieldValues, N extends FieldPath<T>>(props: Base<T, N>) {
  const { t, i18n } = useT();
  return (
    <FormField
      {...props}
      render={({ value, onChange, onBlur, name, id, ref, ...rest }) => (
        <LengthInput
          id={id}
          name={name}
          ref={ref}
          aria-invalid={rest['aria-invalid']}
          value={(value as number | null | undefined) ?? null}
          onChange={onChange}
          onBlur={onBlur}
          unit={t('vessels:units.m')}
          locale={i18n.language}
        />
      )}
    />
  );
}

/** Select bound to a string field; `''` is the "none" value when `noneLabel` is given. */
export function SelectField<T extends FieldValues, N extends FieldPath<T>>({
  options,
  noneLabel,
  ...props
}: Base<T, N> & { options: { value: string; label: string }[]; noneLabel?: string }) {
  const NONE = '__none__';
  return (
    <FormField
      {...props}
      render={({ value, onChange, id, ...field }) => (
        <Select
          value={
            value === '' || value === null || value === undefined
              ? noneLabel
                ? NONE
                : ''
              : String(value)
          }
          onValueChange={(v) => onChange(v === NONE ? '' : v)}
        >
          <SelectTrigger id={id} className="w-full" aria-invalid={field['aria-invalid']}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {noneLabel ? <SelectItem value={NONE}>{noneLabel}</SelectItem> : null}
            {options.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    />
  );
}
