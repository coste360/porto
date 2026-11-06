import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import {
  newIdempotencyKey,
  queryKeys,
  unwrap,
  useApi,
  type ApiKey,
  type Permission,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  ConfirmDialog,
  DataTable,
  FormField,
  PageHeader,
  ProblemAlert,
  SecretReveal,
} from '@porto/shared/ui/patterns';
import { ScrollArea } from '@porto/shared/ui/scroll-area';

import { useCurrentMarina } from '../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../components/PageStates';
import { grantableScopes } from '../lib/roles';
import { requiredString } from '../lib/schemas';
import { toastProblem } from '../lib/toast';

export function ApiKeysPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('users.manage')}>
      <PageHeader
        title={t('team:apiKeys.title')}
        description={t('team:apiKeys.description')}
        actions={<CreateApiKeyDialog />}
      />
      <ApiKeysTable />
    </RequirePermission>
  );
}

function ApiKeysTable() {
  const { t, i18n } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId } = useCurrentMarina();
  const query = useQuery({
    queryKey: queryKeys.apiKeys(marinaId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/api-keys', { params: { path: { marinaId } } }),
      ),
  });
  const revoke = useMutation({
    mutationFn: (apiKeyId: string) =>
      unwrap(
        api.DELETE('/api/admin/v1/marinas/{marinaId}/api-keys/{apiKeyId}', {
          params: { path: { marinaId, apiKeyId } },
        }),
      ),
    onSuccess: () => {
      toast.success(t('team:apiKeys.revoked'));
      void queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys(marinaId) });
    },
    onError: (error) => toastProblem(t, error),
  });

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const when = (value: string | null) =>
    value ? formatDateTime(value, i18n.language) : t('common:status.never');

  const columns: ColumnDef<ApiKey, unknown>[] = [
    { header: t('team:apiKeys.name'), accessorKey: 'name' },
    {
      header: t('team:apiKeys.prefix'),
      cell: ({ row }) => <code className="font-mono text-xs">{row.original.prefix}</code>,
    },
    {
      header: t('team:apiKeys.scopes'),
      cell: ({ row }) => (
        <div className="flex max-w-md flex-wrap gap-1">
          {row.original.scopes.map((s) => (
            <Badge key={s} variant="outline">
              {t(`team:permissions.${s}`)}
            </Badge>
          ))}
        </div>
      ),
    },
    { header: t('team:apiKeys.lastUsed'), cell: ({ row }) => when(row.original.last_used_at) },
    {
      header: t('team:apiKeys.expiresAt'),
      cell: ({ row }) => when(row.original.expires_at),
    },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) =>
        row.original.revoked_at ? (
          <Badge variant="outline">{t('common:status.revoked')}</Badge>
        ) : (
          <div className="flex justify-end">
            <ConfirmDialog
              trigger={
                <Button variant="ghost" size="sm">
                  {t('common:actions.revoke')}
                </Button>
              }
              title={t('team:apiKeys.revokeTitle')}
              description={t('team:apiKeys.revokeDescription')}
              confirmLabel={t('common:actions.revoke')}
              cancelLabel={t('common:actions.cancel')}
              destructive
              onConfirm={() => revoke.mutate(row.original.id)}
            />
          </div>
        ),
    },
  ];

  return (
    <DataTable
      columns={columns}
      data={query.data.items}
      getRowId={(k) => k.id}
      empty={t('team:apiKeys.empty')}
    />
  );
}

function CreateApiKeyDialog() {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const close = () => {
    setOpen(false);
    setSecret(null);
  };
  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
      <DialogTrigger asChild>
        <Button>
          <PlusIcon />
          {t('team:apiKeys.create')}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('team:apiKeys.createTitle')}</DialogTitle>
        </DialogHeader>
        {secret ? (
          <SecretReveal secret={secret} label={t('team:apiKeys.secretLabel')} onDone={close} />
        ) : open ? (
          <CreateApiKeyForm onCreated={setSecret} onCancel={close} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

const createSchema = z.object({
  name: requiredString(),
  scopes: z.array(z.string()).min(1, 'common:validation.atLeastOne'),
  expires_on: z.string(),
});
type CreateValues = z.infer<typeof createSchema>;

function CreateApiKeyForm({
  onCreated,
  onCancel,
}: {
  onCreated: (secret: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId, marina } = useCurrentMarina();
  const scopes = grantableScopes(marina?.permissions ?? []);
  const [idempotencyKey] = useState(newIdempotencyKey);
  const form = useForm<CreateValues>({
    resolver: zodResolver(createSchema),
    defaultValues: { name: '', scopes: ['marina.read'], expires_on: '' },
  });
  const create = useMutation({
    mutationFn: (v: CreateValues) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/api-keys', {
          params: { path: { marinaId }, header: { 'Idempotency-Key': idempotencyKey } },
          body: {
            name: v.name,
            scopes: v.scopes as Permission[],
            // End of the chosen day, UTC: the server stores an instant.
            ...(v.expires_on ? { expires_at: `${v.expires_on}T23:59:59Z` } : {}),
          },
        }),
      ),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys(marinaId) });
      onCreated(result.secret);
    },
  });
  const selected = useWatch({ control: form.control, name: 'scopes' });

  return (
    <form onSubmit={form.handleSubmit((v) => create.mutate(v))} noValidate>
      <FieldGroup>
        {create.error ? <ProblemAlert error={create.error} /> : null}
        <FormField
          control={form.control}
          name="name"
          label={t('team:apiKeys.name')}
          render={(field) => <Input autoComplete="off" {...field} />}
        />
        <FieldSet>
          <FieldLegend variant="label">{t('team:apiKeys.scopes')}</FieldLegend>
          <FieldDescription>{t('team:apiKeys.scopesHint')}</FieldDescription>
          <ScrollArea className="h-48 rounded-md border p-3">
            <div className="flex flex-col gap-2">
              {scopes.map((scope) => (
                <Field key={scope} orientation="horizontal">
                  <Checkbox
                    id={`scope-${scope}`}
                    checked={selected.includes(scope)}
                    onCheckedChange={(checked) =>
                      form.setValue(
                        'scopes',
                        checked ? [...selected, scope] : selected.filter((s) => s !== scope),
                        { shouldValidate: true },
                      )
                    }
                  />
                  <FieldLabel htmlFor={`scope-${scope}`} className="font-normal">
                    {t(`team:permissions.${scope}`)}
                  </FieldLabel>
                </Field>
              ))}
            </div>
          </ScrollArea>
          {form.formState.errors.scopes?.message ? (
            <FieldError>{t(form.formState.errors.scopes.message)}</FieldError>
          ) : null}
        </FieldSet>
        <FormField
          control={form.control}
          name="expires_on"
          label={t('team:apiKeys.expiresAt')}
          description={t('team:apiKeys.expiresHint')}
          render={(field) => <Input type="date" {...field} />}
        />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common:actions.cancel')}
          </Button>
          <Button type="submit" disabled={create.isPending}>
            {t('common:actions.create')}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}
