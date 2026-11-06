import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import type { ColumnDef } from '@tanstack/react-table';
import { PlusIcon, UserPlusIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import {
  ifMatch,
  isProblem,
  newIdempotencyKey,
  queryKeys,
  unwrap,
  useApi,
  type Marina,
  type Member,
  type Organization,
} from '@porto/shared/api';
import { isLocale, SOURCE_LOCALE, useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  ConfirmDialog,
  DataTable,
  FormField,
  PageHeader,
  ProblemAlert,
} from '@porto/shared/ui/patterns';

import { CreateMarinaForm } from '../components/CreateMarinaForm';
import { LoadingState, QueryErrorState } from '../components/PageStates';
import { EMPTY_ADDRESS } from '../lib/address';
import { emailField, requiredString } from '../lib/schemas';
import { toastProblem } from '../lib/toast';

const routeApi = getRouteApi('/auth/plain/org/$organizationId');

export function OrganizationPage() {
  const { t } = useT();
  const api = useApi();
  const { organizationId } = routeApi.useParams();
  const query = useQuery({
    queryKey: queryKeys.organizations,
    queryFn: () => unwrap(api.GET('/api/admin/v1/organizations')),
  });

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const org = query.data.items.find((o) => o.id === organizationId);
  if (!org) return <QueryErrorState error={null} />;

  return (
    <>
      <PageHeader title={org.name} description={t('marina:org.description')} />
      <div className="flex flex-col gap-6">
        <OrganizationForm org={org} onReload={() => void query.refetch()} />
        <MarinasCard organizationId={org.id} />
        <OwnersCard organizationId={org.id} />
      </div>
    </>
  );
}

const orgSchema = z.object({
  name: requiredString(),
  vat_number: z.string(),
  fiscal_code: z.string(),
  sdi_code: z.string(),
  pec: z.union([z.literal(''), z.email('common:validation.email')]),
  billing_address: z.object({
    line1: z.string(),
    line2: z.string(),
    postal_code: z.string(),
    city: z.string(),
    province: z.string(),
    country: z.string(),
  }),
});
type OrgValues = z.infer<typeof orgSchema>;

function toOrgValues(o: Organization): OrgValues {
  return {
    name: o.name,
    vat_number: o.vat_number ?? '',
    fiscal_code: o.fiscal_code ?? '',
    sdi_code: o.sdi_code ?? '',
    pec: o.pec ?? '',
    billing_address: { ...EMPTY_ADDRESS, ...o.billing_address },
  };
}

function OrganizationForm({ org, onReload }: { org: Organization; onReload: () => void }) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const form = useForm<OrgValues>({
    resolver: zodResolver(orgSchema),
    defaultValues: toOrgValues(org),
  });
  const save = useMutation({
    mutationFn: (body: OrgValues) =>
      unwrap(
        api.PATCH('/api/admin/v1/organizations/{organizationId}', {
          params: {
            path: { organizationId: org.id },
            header: { 'If-Match': ifMatch(org.version) },
          },
          body,
        }),
      ),
    onSuccess: (updated) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.organizations });
      form.reset(toOrgValues(updated));
      toast.success(t('common:status.saved'));
    },
  });
  const text = (name: Parameters<typeof form.register>[0], label: string) => (
    <FormField
      control={form.control}
      name={name}
      label={label}
      render={(field) => <Input {...field} value={String(field.value ?? '')} />}
    />
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('marina:org.title')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            {save.error ? (
              <div className="md:col-span-2 flex flex-col gap-2">
                <ProblemAlert error={save.error} />
                {isProblem(save.error, 'version_conflict') ? (
                  <div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        save.reset();
                        onReload();
                      }}
                    >
                      {t('common:actions.reload')}
                    </Button>
                  </div>
                ) : null}
              </div>
            ) : null}
            {text('name', t('marina:org.name'))}
            {text('vat_number', t('marina:org.vat'))}
            {text('fiscal_code', t('marina:org.fiscalCode'))}
            {text('sdi_code', t('marina:org.sdi'))}
            {text('pec', t('marina:org.pec'))}
            {text('billing_address.line1', t('marina:settings.line1'))}
            {text('billing_address.postal_code', t('marina:settings.postalCode'))}
            {text('billing_address.city', t('marina:settings.city'))}
            {text('billing_address.province', t('marina:settings.province'))}
            {text('billing_address.country', t('marina:settings.country'))}
            <div className="md:col-span-2 flex justify-end">
              <Button type="submit" disabled={save.isPending || !form.formState.isDirty}>
                {t('common:actions.save')}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  );
}

function MarinasCard({ organizationId }: { organizationId: string }) {
  const { t } = useT();
  const api = useApi();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: queryKeys.orgMarinas(organizationId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/organizations/{organizationId}/marinas', {
          params: { path: { organizationId } },
        }),
      ),
  });
  const columns: ColumnDef<Marina, unknown>[] = [
    { header: t('marina:settings.name'), accessorKey: 'name' },
    { header: t('marina:settings.slug'), accessorKey: 'slug' },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) =>
        row.original.archived_at ? (
          <Badge variant="outline">{t('marina:org.archived')}</Badge>
        ) : (
          <div className="flex justify-end">
            <Button asChild variant="ghost" size="sm">
              <Link to="/m/$marinaId" params={{ marinaId: row.original.id }}>
                {t('marina:org.open')}
              </Link>
            </Button>
          </div>
        ),
    },
  ];
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>{t('marina:org.marinas')}</CardTitle>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <PlusIcon />
              {t('marina:create.titleMore')}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('marina:create.titleMore')}</DialogTitle>
              <DialogDescription>{t('marina:create.description')}</DialogDescription>
            </DialogHeader>
            {open ? (
              <CreateMarinaForm
                organizationId={organizationId}
                onCreated={(marina) => {
                  setOpen(false);
                  void navigate({ to: '/m/$marinaId', params: { marinaId: marina.id } });
                }}
              />
            ) : null}
          </DialogContent>
        </Dialog>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <LoadingState />
        ) : query.isError ? (
          <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : (
          <DataTable
            columns={columns}
            data={query.data.items}
            getRowId={(m) => m.id}
            empty={t('marina:org.noMarinas')}
          />
        )}
      </CardContent>
    </Card>
  );
}

const ownerSchema = z.object({ name: requiredString(), email: emailField() });

function OwnersCard({ organizationId }: { organizationId: string }) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: queryKeys.orgOwners(organizationId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/organizations/{organizationId}/owners', {
          params: { path: { organizationId } },
        }),
      ),
  });
  const remove = useMutation({
    mutationFn: (userId: string) =>
      unwrap(
        api.DELETE('/api/admin/v1/organizations/{organizationId}/owners/{userId}', {
          params: { path: { organizationId, userId } },
        }),
      ),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: queryKeys.orgOwners(organizationId) }),
    onError: (error) => toastProblem(t, error),
  });
  const columns: ColumnDef<Member, unknown>[] = [
    { header: t('team:members.name'), accessorKey: 'name' },
    { header: t('team:members.email'), accessorKey: 'email' },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="sm">
                {t('common:actions.remove')}
              </Button>
            }
            title={t('marina:org.removeOwnerTitle')}
            confirmLabel={t('common:actions.remove')}
            cancelLabel={t('common:actions.cancel')}
            destructive
            onConfirm={() => remove.mutate(row.original.user_id)}
          />
        </div>
      ),
    },
  ];
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <CardTitle>{t('marina:org.owners')}</CardTitle>
          <CardDescription>{t('marina:org.ownersDescription')}</CardDescription>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <UserPlusIcon />
              {t('marina:org.addOwner')}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('marina:org.addOwner')}</DialogTitle>
            </DialogHeader>
            {open ? (
              <AddOwnerForm organizationId={organizationId} onDone={() => setOpen(false)} />
            ) : null}
          </DialogContent>
        </Dialog>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <LoadingState />
        ) : query.isError ? (
          <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : (
          <DataTable
            columns={columns}
            data={query.data.items}
            getRowId={(m) => m.user_id}
            empty={t('team:members.empty')}
          />
        )}
      </CardContent>
    </Card>
  );
}

function AddOwnerForm({ organizationId, onDone }: { organizationId: string; onDone: () => void }) {
  const { t, i18n } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const [idempotencyKey] = useState(newIdempotencyKey);
  const form = useForm<z.infer<typeof ownerSchema>>({
    resolver: zodResolver(ownerSchema),
    defaultValues: { name: '', email: '' },
  });
  const add = useMutation({
    mutationFn: (v: z.infer<typeof ownerSchema>) =>
      unwrap(
        api.POST('/api/admin/v1/organizations/{organizationId}/owners', {
          params: { path: { organizationId }, header: { 'Idempotency-Key': idempotencyKey } },
          body: {
            ...v,
            role: 'owner',
            locale: isLocale(i18n.language) ? i18n.language : SOURCE_LOCALE,
          },
        }),
      ),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.orgOwners(organizationId) });
      onDone();
    },
  });
  return (
    <form onSubmit={form.handleSubmit((v) => add.mutate(v))} noValidate>
      <FieldGroup>
        {add.error ? <ProblemAlert error={add.error} /> : null}
        <FormField
          control={form.control}
          name="name"
          label={t('team:members.name')}
          render={(field) => <Input autoComplete="off" {...field} />}
        />
        <FormField
          control={form.control}
          name="email"
          label={t('team:members.email')}
          render={(field) => <Input type="email" autoComplete="off" {...field} />}
        />
        <Button type="submit" disabled={add.isPending}>
          {t('common:actions.add')}
        </Button>
      </FieldGroup>
    </form>
  );
}
