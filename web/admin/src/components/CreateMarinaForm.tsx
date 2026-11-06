import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import { newIdempotencyKey, queryKeys, unwrap, useApi, type Marina } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';

import { requiredString, slugField } from '../lib/schemas';
import { suggestSlug } from '../lib/text';

const schema = z.object({ name: requiredString(), slug: slugField() });
type Values = z.infer<typeof schema>;

export function CreateMarinaForm({
  organizationId,
  onCreated,
}: {
  organizationId: string;
  onCreated: (marina: Marina) => void;
}) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const [idempotencyKey] = useState(newIdempotencyKey);
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', slug: '' },
  });
  const create = useMutation({
    mutationFn: (body: Values) =>
      unwrap(
        api.POST('/api/admin/v1/organizations/{organizationId}/marinas', {
          params: { path: { organizationId }, header: { 'Idempotency-Key': idempotencyKey } },
          body,
        }),
      ),
    onSuccess: async (marina) => {
      toast.success(t('marina:create.created'));
      await queryClient.invalidateQueries({ queryKey: queryKeys.me });
      await queryClient.invalidateQueries({ queryKey: queryKeys.orgMarinas(organizationId) });
      onCreated(marina);
    },
  });

  return (
    <form onSubmit={form.handleSubmit((v) => create.mutate(v))} noValidate>
      <FieldGroup>
        {create.error ? <ProblemAlert error={create.error} /> : null}
        <FormField
          control={form.control}
          name="name"
          label={t('marina:settings.name')}
          render={(field) => (
            <Input
              {...field}
              onChange={(e) => {
                field.onChange(e);
                if (!form.getFieldState('slug').isDirty) {
                  form.setValue('slug', suggestSlug(e.target.value));
                }
              }}
            />
          )}
        />
        <FormField
          control={form.control}
          name="slug"
          label={t('marina:settings.slug')}
          render={(field) => <Input {...field} />}
        />
        <Button type="submit" disabled={create.isPending}>
          {t('marina:create.submit')}
        </Button>
      </FieldGroup>
    </form>
  );
}
