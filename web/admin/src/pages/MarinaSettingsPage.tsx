import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import {
  ifMatch,
  isProblem,
  queryKeys,
  unwrap,
  useApi,
  type Marina,
  type Schemas,
} from '@porto/shared/api';
import { SUPPORTED_LOCALES, useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../components/PageStates';
import { EMPTY_ADDRESS } from '../lib/address';
import { requiredString } from '../lib/schemas';

const KINDS = ['marina', 'yard', 'both'] as const;

// The server may omit empty fields of the JSON columns: start from blanks.
const EMPTY_CONTACTS = { email: '', phone: '', website: '', vhf_channel: '' };

const schema = z.object({
  name: requiredString(),
  kind: z.enum(KINDS),
  timezone: requiredString(),
  currency: z
    .string()
    .trim()
    .regex(/^[A-Z]{3}$/, 'common:validation.required'),
  default_locale: z.enum(SUPPORTED_LOCALES),
  locales: z.array(z.enum(SUPPORTED_LOCALES)).min(1, 'common:validation.atLeastOne'),
  contacts: z.object({
    email: z.union([z.literal(''), z.email('common:validation.email')]),
    phone: z.string(),
    website: z.string(),
    vhf_channel: z.string(),
  }),
  address: z.object({
    line1: z.string(),
    line2: z.string(),
    postal_code: z.string(),
    city: z.string(),
    province: z.string(),
    country: z.string(),
  }),
  concession_ref: z.string(),
  concession_expires_on: z.string(),
});
type Values = z.infer<typeof schema>;

function toValues(m: Marina): Values {
  const locales = m.locales.filter((l): l is Values['locales'][number] =>
    (SUPPORTED_LOCALES as readonly string[]).includes(l),
  );
  return {
    name: m.name,
    kind: (KINDS as readonly string[]).includes(m.kind) ? (m.kind as Values['kind']) : 'marina',
    timezone: m.timezone,
    currency: m.currency,
    default_locale: m.default_locale === 'en' ? 'en' : 'it',
    locales: locales.length > 0 ? locales : ['it'],
    contacts: { ...EMPTY_CONTACTS, ...m.contacts },
    address: { ...EMPTY_ADDRESS, ...m.address },
    concession_ref: m.concession_ref ?? '',
    concession_expires_on: m.concession_expires_on ?? '',
  };
}

function toPatch(v: Values): Schemas['MarinaPatch'] {
  return {
    ...v,
    concession_expires_on: v.concession_expires_on === '' ? null : v.concession_expires_on,
  };
}

export function MarinaSettingsPage() {
  const { t } = useT();
  const api = useApi();
  const { marinaId, can } = useCurrentMarina();
  const query = useQuery({
    queryKey: queryKeys.marina(marinaId),
    queryFn: () =>
      unwrap(api.GET('/api/admin/v1/marinas/{marinaId}', { params: { path: { marinaId } } })),
  });

  return (
    <RequirePermission allowed={can('marina.configure')}>
      <PageHeader
        title={t('marina:settings.title')}
        description={t('marina:settings.description')}
      />
      {query.isPending ? (
        <LoadingState rows={6} />
      ) : query.isError ? (
        <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <SettingsForm marina={query.data} onReload={() => void query.refetch()} />
      )}
    </RequirePermission>
  );
}

function SettingsForm({ marina, onReload }: { marina: Marina; onReload: () => void }) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: toValues(marina) });

  // A newer version (after a reload or an SSE refresh) resets the untouched form.
  useEffect(() => {
    if (!form.formState.isDirty) form.reset(toValues(marina));
  }, [marina, form]);

  const save = useMutation({
    mutationFn: (values: Values) =>
      unwrap(
        api.PATCH('/api/admin/v1/marinas/{marinaId}', {
          params: {
            path: { marinaId: marina.id },
            header: { 'If-Match': ifMatch(marina.version) },
          },
          body: toPatch(values),
        }),
      ),
    onSuccess: (updated) => {
      queryClient.setQueryData(queryKeys.marina(marina.id), updated);
      void queryClient.invalidateQueries({ queryKey: queryKeys.me });
      form.reset(toValues(updated));
      toast.success(t('common:status.saved'));
    },
  });

  const conflict = isProblem(save.error, 'version_conflict');
  const selectedLocales = useWatch({ control: form.control, name: 'locales' });

  const text = (name: Parameters<typeof form.register>[0], label: string, type = 'text') => (
    <FormField
      control={form.control}
      name={name}
      label={label}
      render={(field) => <Input type={type} {...field} value={String(field.value ?? '')} />}
    />
  );

  return (
    <form
      onSubmit={form.handleSubmit((v) => save.mutate(v))}
      noValidate
      className="flex flex-col gap-6"
    >
      {conflict ? (
        <Alert variant="destructive">
          <AlertTitle>{t('marina:settings.conflictTitle')}</AlertTitle>
          <AlertDescription>
            <p>{t('marina:settings.conflictDescription')}</p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => {
                save.reset();
                form.reset(toValues(marina));
                onReload();
              }}
            >
              {t('common:actions.reload')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : save.error ? (
        <ProblemAlert error={save.error} />
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{t('marina:settings.general')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            {text('name', t('marina:settings.name'))}
            <Field>
              <FieldLabel htmlFor="field-slug">{t('marina:settings.slug')}</FieldLabel>
              <Input id="field-slug" value={marina.slug} readOnly disabled />
            </Field>
            <FormField
              control={form.control}
              name="kind"
              label={t('marina:settings.kind')}
              render={({ id, value, onChange }) => (
                <Select value={value} onValueChange={onChange}>
                  <SelectTrigger id={id} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {KINDS.map((k) => (
                      <SelectItem key={k} value={k}>
                        {t(`marina:kinds.${k}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
            {text('timezone', t('marina:settings.timezone'))}
            {text('currency', t('marina:settings.currency'))}
            <FormField
              control={form.control}
              name="default_locale"
              label={t('marina:settings.defaultLocale')}
              render={({ id, value, onChange }) => (
                <Select value={value} onValueChange={onChange}>
                  <SelectTrigger id={id} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SUPPORTED_LOCALES.map((l) => (
                      <SelectItem key={l} value={l}>
                        {t(`common:locales.${l}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
            <FieldSet>
              <FieldLegend variant="label">{t('common:user.language')}</FieldLegend>
              <div className="flex gap-4">
                {SUPPORTED_LOCALES.map((l) => {
                  return (
                    <Field key={l} orientation="horizontal">
                      <Checkbox
                        id={`locale-${l}`}
                        checked={selectedLocales.includes(l)}
                        onCheckedChange={(checked) =>
                          form.setValue(
                            'locales',
                            checked
                              ? [...selectedLocales, l]
                              : selectedLocales.filter((x) => x !== l),
                            { shouldDirty: true, shouldValidate: true },
                          )
                        }
                      />
                      <FieldLabel htmlFor={`locale-${l}`}>{t(`common:locales.${l}`)}</FieldLabel>
                    </Field>
                  );
                })}
              </div>
              {form.formState.errors.locales?.message ? (
                <FieldError>{t(form.formState.errors.locales.message)}</FieldError>
              ) : null}
            </FieldSet>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('marina:settings.contacts')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            {text('contacts.email', t('marina:settings.email'), 'email')}
            {text('contacts.phone', t('marina:settings.phone'), 'tel')}
            {text('contacts.website', t('marina:settings.website'), 'url')}
            {text('contacts.vhf_channel', t('marina:settings.vhf'))}
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('marina:settings.address')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            {text('address.line1', t('marina:settings.line1'))}
            {text('address.line2', t('marina:settings.line2'))}
            {text('address.postal_code', t('marina:settings.postalCode'))}
            {text('address.city', t('marina:settings.city'))}
            {text('address.province', t('marina:settings.province'))}
            {text('address.country', t('marina:settings.country'))}
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('marina:settings.concession')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            {text('concession_ref', t('marina:settings.concessionRef'))}
            {text('concession_expires_on', t('marina:settings.concessionExpires'), 'date')}
          </FieldGroup>
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
          {save.isPending ? t('common:actions.saving') : t('common:actions.save')}
        </Button>
      </div>
    </form>
  );
}
