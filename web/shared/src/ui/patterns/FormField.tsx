import type { ReactNode } from 'react';
import {
  Controller,
  type Control,
  type ControllerRenderProps,
  type FieldPath,
  type FieldValues,
} from 'react-hook-form';

import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';

import { useT } from '../../i18n/useT';

export type FormFieldProps<T extends FieldValues, N extends FieldPath<T>> = {
  control: Control<T>;
  name: N;
  label: ReactNode;
  description?: ReactNode;
  render: (
    field: ControllerRenderProps<T, N> & { id: string; 'aria-invalid': boolean },
  ) => ReactNode;
};

/**
 * react-hook-form field rendered with shadcn Field. Validation messages are i18n keys
 * (zod schemas use keys such as `common:validation.required` as messages).
 */
export function FormField<T extends FieldValues, N extends FieldPath<T>>({
  control,
  name,
  label,
  description,
  render,
}: FormFieldProps<T, N>) {
  const { t } = useT();
  const id = `field-${name.replace(/\./g, '-')}`;
  return (
    <Controller
      control={control}
      name={name}
      render={({ field, fieldState }) => (
        <Field data-invalid={fieldState.invalid || undefined}>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          {render({ ...field, id, 'aria-invalid': fieldState.invalid })}
          {description ? <FieldDescription>{description}</FieldDescription> : null}
          {fieldState.error?.message ? (
            <FieldError>{t(fieldState.error.message)}</FieldError>
          ) : null}
        </Field>
      )}
    />
  );
}
