import { zodResolver } from '@hookform/resolvers/zod';
import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { newIdempotencyKey, useCreateMap, type MapKind } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { DialogFooter } from '@porto/shared/ui/dialog';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, LengthInput, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { MAP_KINDS } from './lib';

const sizeField = () =>
  z
    .number('maps:validation.size')
    .int()
    .min(100, 'maps:validation.size')
    .max(500_000, 'maps:validation.size')
    .nullable()
    .refine((v) => v !== null, 'maps:validation.size');

const createMapSchema = z.object({
  kind: z.enum(MAP_KINDS as [MapKind, ...MapKind[]]),
  name_it: z.string().trim().min(1, 'common:validation.required').max(200),
  name_en: z.string().trim().max(200),
  width_cm: sizeField(),
  height_cm: sizeField(),
  grid_cm: z
    .number()
    .int()
    .min(1, 'maps:validation.grid')
    .max(10_000, 'maps:validation.grid')
    .nullable()
    .refine((v) => v !== null, 'maps:validation.grid'),
});

type FormIn = z.input<typeof createMapSchema>;
type FormOut = z.output<typeof createMapSchema>;

export function CreateMapDialogBody({
  marinaId,
  onDone,
}: {
  marinaId: string;
  onDone: () => void;
}) {
  const { t, i18n } = useT();
  const navigate = useNavigate();
  const create = useCreateMap(marinaId);
  const [idempotencyKey] = useState(newIdempotencyKey);
  const form = useForm<FormIn, unknown, FormOut>({
    resolver: zodResolver(createMapSchema),
    defaultValues: {
      kind: 'water',
      name_it: '',
      name_en: '',
      width_cm: 60_000,
      height_cm: 40_000,
      grid_cm: 50,
    },
  });
  const submit = form.handleSubmit(async (v) => {
    const doc = await create.mutateAsync({
      idempotencyKey,
      body: {
        kind: v.kind,
        name: v.name_en ? { it: v.name_it, en: v.name_en } : { it: v.name_it },
        width_cm: v.width_cm!,
        height_cm: v.height_cm!,
        grid_cm: v.grid_cm!,
      },
    });
    onDone();
    await navigate({ to: '/m/$marinaId/maps/$mapId/edit', params: { marinaId, mapId: doc.id } });
  });
  const m = t('maps:units.m');
  return (
    <form onSubmit={(e) => void submit(e).catch(() => undefined)} noValidate>
      <FieldGroup>
        <FormField
          control={form.control}
          name="kind"
          label={t('maps:create.kind')}
          render={(field) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger id={field.id} aria-invalid={field['aria-invalid']}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {MAP_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`maps:kind.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
        <FormField
          control={form.control}
          name="name_it"
          label={t('maps:create.nameIt')}
          render={(field) => <Input {...field} />}
        />
        <FormField
          control={form.control}
          name="name_en"
          label={t('maps:create.nameEn')}
          render={(field) => <Input {...field} />}
        />
        <div className="grid grid-cols-2 gap-4">
          <FormField
            control={form.control}
            name="width_cm"
            label={t('maps:create.width')}
            render={({ value, onChange, onBlur, id, ...rest }) => (
              <LengthInput
                id={id}
                value={value}
                onChange={onChange}
                onBlur={onBlur}
                unit={m}
                locale={i18n.language}
                aria-invalid={rest['aria-invalid']}
              />
            )}
          />
          <FormField
            control={form.control}
            name="height_cm"
            label={t('maps:create.height')}
            render={({ value, onChange, onBlur, id, ...rest }) => (
              <LengthInput
                id={id}
                value={value}
                onChange={onChange}
                onBlur={onBlur}
                unit={m}
                locale={i18n.language}
                aria-invalid={rest['aria-invalid']}
              />
            )}
          />
        </div>
        <FormField
          control={form.control}
          name="grid_cm"
          label={t('maps:create.grid')}
          description={t('maps:create.gridHint')}
          render={({ value, onChange, onBlur, id, ...rest }) => (
            <LengthInput
              id={id}
              value={value}
              onChange={onChange}
              onBlur={onBlur}
              unit={m}
              locale={i18n.language}
              aria-invalid={rest['aria-invalid']}
            />
          )}
        />
        {create.error ? <ProblemAlert error={create.error} /> : null}
      </FieldGroup>
      <DialogFooter className="mt-6">
        <Button type="button" variant="outline" onClick={onDone}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={create.isPending}>
          {t('common:actions.create')}
        </Button>
      </DialogFooter>
    </form>
  );
}
