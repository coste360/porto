import { zodResolver } from '@hookform/resolvers/zod';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { MoreHorizontalIcon, PlusIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import {
  ifMatch,
  newIdempotencyKey,
  queryKeys,
  unwrap,
  useApi,
  type WebhookDelivery,
  type WebhookEndpoint,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@porto/shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
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
import { Switch } from '@porto/shared/ui/switch';

import { useCurrentMarina } from '../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../components/PageStates';
import { httpsUrlField } from '../lib/schemas';
import { toastProblem } from '../lib/toast';

type DialogState =
  | { kind: 'closed' }
  | { kind: 'create' }
  | { kind: 'edit'; endpoint: WebhookEndpoint }
  | { kind: 'secret'; secret: string };

export function WebhooksPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const [dialog, setDialog] = useState<DialogState>({ kind: 'closed' });
  const [selected, setSelected] = useState<string | null>(null);
  const close = () => setDialog({ kind: 'closed' });

  return (
    <RequirePermission allowed={can('users.manage')}>
      <PageHeader
        title={t('webhooks:title')}
        description={t('webhooks:description')}
        actions={
          <Button onClick={() => setDialog({ kind: 'create' })}>
            <PlusIcon />
            {t('webhooks:create')}
          </Button>
        }
      />
      <EndpointsTable
        onEdit={(endpoint) => setDialog({ kind: 'edit', endpoint })}
        onSecret={(secret) => setDialog({ kind: 'secret', secret })}
        onShowDeliveries={setSelected}
      />
      {selected ? <DeliveriesCard endpointId={selected} /> : null}

      <Dialog open={dialog.kind !== 'closed'} onOpenChange={(open) => (open ? null : close())}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>
              {dialog.kind === 'edit'
                ? t('webhooks:editTitle')
                : dialog.kind === 'secret'
                  ? t('webhooks:secretLabel')
                  : t('webhooks:createTitle')}
            </DialogTitle>
          </DialogHeader>
          {dialog.kind === 'secret' ? (
            <div className="flex flex-col gap-2">
              <p className="text-muted-foreground text-sm">{t('webhooks:secretHint')}</p>
              <SecretReveal
                secret={dialog.secret}
                label={t('webhooks:secretLabel')}
                onDone={close}
              />
            </div>
          ) : dialog.kind === 'create' ? (
            <EndpointForm
              onCancel={close}
              onCreated={(secret) => setDialog({ kind: 'secret', secret })}
              onSaved={close}
            />
          ) : dialog.kind === 'edit' ? (
            <EndpointForm endpoint={dialog.endpoint} onCancel={close} onSaved={close} />
          ) : null}
        </DialogContent>
      </Dialog>
    </RequirePermission>
  );
}

function EndpointsTable({
  onEdit,
  onSecret,
  onShowDeliveries,
}: {
  onEdit: (endpoint: WebhookEndpoint) => void;
  onSecret: (secret: string) => void;
  onShowDeliveries: (endpointId: string) => void;
}) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId } = useCurrentMarina();
  const query = useQuery({
    queryKey: queryKeys.webhooks(marinaId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/webhook-endpoints', {
          params: { path: { marinaId } },
        }),
      ),
  });
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: queryKeys.webhooks(marinaId) });
  const onError = (error: unknown) => toastProblem(t, error);
  const path = (endpointId: string) => ({ path: { marinaId, endpointId } });

  const rotate = useMutation({
    mutationFn: (endpointId: string) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/webhook-endpoints/{endpointId}/rotate-secret', {
          params: path(endpointId),
        }),
      ),
    onSuccess: (result) => {
      void invalidate();
      onSecret(result.secret);
    },
    onError,
  });
  const test = useMutation({
    mutationFn: (endpointId: string) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/webhook-endpoints/{endpointId}/test', {
          params: { ...path(endpointId), header: { 'Idempotency-Key': newIdempotencyKey() } },
        }),
      ),
    onSuccess: (_result, endpointId) => {
      toast.success(t('webhooks:testSent'));
      void queryClient.invalidateQueries({
        queryKey: queryKeys.webhookDeliveries(marinaId, endpointId),
      });
    },
    onError,
  });
  const remove = useMutation({
    mutationFn: (endpointId: string) =>
      unwrap(
        api.DELETE('/api/admin/v1/marinas/{marinaId}/webhook-endpoints/{endpointId}', {
          params: path(endpointId),
        }),
      ),
    onSuccess: () => {
      toast.success(t('webhooks:deleted'));
      void invalidate();
    },
    onError,
  });

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const columns: ColumnDef<WebhookEndpoint, unknown>[] = [
    {
      header: t('webhooks:url'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-mono text-xs break-all">{row.original.url}</span>
          {row.original.description ? (
            <span className="text-muted-foreground text-xs">{row.original.description}</span>
          ) : null}
        </div>
      ),
    },
    {
      header: t('webhooks:events'),
      cell: ({ row }) => (
        <div className="flex max-w-xs flex-wrap gap-1">
          {row.original.events.map((e) => (
            <Badge key={e} variant="outline" className="font-mono">
              {e}
            </Badge>
          ))}
        </div>
      ),
    },
    {
      header: t('webhooks:status'),
      cell: ({ row }) =>
        row.original.disabled_at ? (
          <Badge variant="destructive">{t('webhooks:disabled')}</Badge>
        ) : row.original.active ? (
          <Badge variant="secondary">{t('common:status.active')}</Badge>
        ) : (
          <Badge variant="outline">{t('common:status.inactive')}</Badge>
        ),
    },
    { header: t('webhooks:failures'), accessorKey: 'consecutive_failures' },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) => (
        <div className="flex justify-end gap-1">
          <Button variant="ghost" size="sm" onClick={() => onShowDeliveries(row.original.id)}>
            {t('webhooks:deliveries')}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" aria-label={t('common:actions.actions')}>
                <MoreHorizontalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => onEdit(row.original)}>
                {t('common:actions.edit')}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => test.mutate(row.original.id)}>
                {t('webhooks:test')}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => rotate.mutate(row.original.id)}>
                {t('webhooks:rotate')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="sm">
                {t('common:actions.delete')}
              </Button>
            }
            title={t('webhooks:deleteTitle')}
            description={t('webhooks:deleteDescription')}
            confirmLabel={t('common:actions.delete')}
            cancelLabel={t('common:actions.cancel')}
            destructive
            onConfirm={() => remove.mutate(row.original.id)}
          />
        </div>
      ),
    },
  ];

  return (
    <DataTable
      columns={columns}
      data={query.data.items}
      getRowId={(e) => e.id}
      empty={t('webhooks:empty')}
    />
  );
}

const endpointSchema = z.object({
  url: httpsUrlField(),
  description: z.string(),
  events: z.array(z.string()).min(1, 'common:validation.atLeastOne'),
  active: z.boolean(),
});
type EndpointValues = z.infer<typeof endpointSchema>;

function EndpointForm({
  endpoint,
  onCancel,
  onCreated,
  onSaved,
}: {
  endpoint?: WebhookEndpoint;
  onCancel: () => void;
  onCreated?: (secret: string) => void;
  onSaved: () => void;
}) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId } = useCurrentMarina();
  const [idempotencyKey] = useState(newIdempotencyKey);
  const eventTypes = useQuery({
    queryKey: queryKeys.eventTypes,
    queryFn: () => unwrap(api.GET('/api/admin/v1/event-types')),
    staleTime: 5 * 60_000,
  });
  const form = useForm<EndpointValues>({
    resolver: zodResolver(endpointSchema),
    defaultValues: {
      url: endpoint?.url ?? '',
      description: endpoint?.description ?? '',
      events: endpoint?.events ?? [],
      active: endpoint?.active ?? true,
    },
  });
  const save = useMutation({
    mutationFn: async (v: EndpointValues) => {
      if (endpoint) {
        await unwrap(
          api.PATCH('/api/admin/v1/marinas/{marinaId}/webhook-endpoints/{endpointId}', {
            params: {
              path: { marinaId, endpointId: endpoint.id },
              header: { 'If-Match': ifMatch(endpoint.version) },
            },
            body: v,
          }),
        );
        return null;
      }
      const created = await unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/webhook-endpoints', {
          params: { path: { marinaId }, header: { 'Idempotency-Key': idempotencyKey } },
          body: { url: v.url, description: v.description, events: v.events },
        }),
      );
      return created.secret;
    },
    onSuccess: (secret) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhooks(marinaId) });
      toast.success(t('webhooks:saved'));
      if (secret && onCreated) onCreated(secret);
      else onSaved();
    },
  });
  const selected = useWatch({ control: form.control, name: 'events' });
  const active = useWatch({ control: form.control, name: 'active' });

  return (
    <form onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
      <FieldGroup>
        {save.error ? <ProblemAlert error={save.error} /> : null}
        <FormField
          control={form.control}
          name="url"
          label={t('webhooks:url')}
          render={(field) => <Input type="url" autoComplete="off" {...field} />}
        />
        <FormField
          control={form.control}
          name="description"
          label={t('webhooks:descriptionField')}
          render={(field) => <Input autoComplete="off" {...field} />}
        />
        <FieldSet>
          <FieldLegend variant="label">{t('webhooks:events')}</FieldLegend>
          {eventTypes.isError ? <ProblemAlert error={eventTypes.error} /> : null}
          <ScrollArea className="h-40 rounded-md border p-3">
            <div className="flex flex-col gap-2">
              {(eventTypes.data?.items ?? []).map((et) => (
                <Field key={et.type} orientation="horizontal">
                  <Checkbox
                    id={`event-${et.type}`}
                    checked={selected.includes(et.type)}
                    onCheckedChange={(checked) =>
                      form.setValue(
                        'events',
                        checked ? [...selected, et.type] : selected.filter((e) => e !== et.type),
                        { shouldValidate: true },
                      )
                    }
                  />
                  <FieldLabel htmlFor={`event-${et.type}`} className="font-mono font-normal">
                    {et.type}
                  </FieldLabel>
                </Field>
              ))}
            </div>
          </ScrollArea>
          {form.formState.errors.events?.message ? (
            <FieldError>{t(form.formState.errors.events.message)}</FieldError>
          ) : null}
        </FieldSet>
        {endpoint ? (
          <Field orientation="horizontal">
            <Switch
              id="endpoint-active"
              checked={active}
              onCheckedChange={(v) => form.setValue('active', v)}
            />
            <FieldLabel htmlFor="endpoint-active">{t('webhooks:active')}</FieldLabel>
          </Field>
        ) : (
          <FieldDescription>{t('webhooks:secretHint')}</FieldDescription>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common:actions.cancel')}
          </Button>
          <Button type="submit" disabled={save.isPending}>
            {t('common:actions.save')}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}

const DELIVERY_STATUSES: readonly string[] = [
  'pending',
  'delivered',
  'failed',
] satisfies WebhookDelivery['status'][];

function DeliveriesCard({ endpointId }: { endpointId: string }) {
  const { t, i18n } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId } = useCurrentMarina();
  const query = useInfiniteQuery({
    queryKey: queryKeys.webhookDeliveries(marinaId, endpointId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/webhook-endpoints/{endpointId}/deliveries', {
          params: {
            path: { marinaId, endpointId },
            query: { limit: 50, ...(pageParam ? { cursor: pageParam } : {}) },
          },
        }),
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const redeliver = useMutation({
    mutationFn: (deliveryId: string) =>
      unwrap(
        api.POST(
          '/api/admin/v1/marinas/{marinaId}/webhook-endpoints/{endpointId}/deliveries/{deliveryId}/redeliver',
          {
            params: {
              path: { marinaId, endpointId, deliveryId },
              header: { 'Idempotency-Key': newIdempotencyKey() },
            },
          },
        ),
      ),
    onSuccess: () => {
      toast.success(t('webhooks:redelivered'));
      void queryClient.invalidateQueries({
        queryKey: queryKeys.webhookDeliveries(marinaId, endpointId),
      });
    },
    onError: (error) => toastProblem(t, error),
  });

  const columns: ColumnDef<WebhookDelivery, unknown>[] = [
    {
      header: t('webhooks:createdAt'),
      cell: ({ row }) => formatDateTime(row.original.created_at, i18n.language),
    },
    {
      header: t('webhooks:event'),
      cell: ({ row }) => <code className="font-mono text-xs">{row.original.event_type}</code>,
    },
    {
      header: t('webhooks:status'),
      cell: ({ row }) =>
        DELIVERY_STATUSES.includes(row.original.status)
          ? t(`webhooks:deliveryStatus.${row.original.status}`)
          : row.original.status,
    },
    { header: t('webhooks:attempts'), accessorKey: 'attempts' },
    { header: t('webhooks:statusCode'), cell: ({ row }) => row.original.last_status_code ?? '' },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) => (
        <div className="flex justify-end">
          <Button variant="ghost" size="sm" onClick={() => redeliver.mutate(row.original.id)}>
            {t('webhooks:redeliver')}
          </Button>
        </div>
      ),
    },
  ];

  return (
    <Card className="mt-6">
      <CardHeader>
        <CardTitle>{t('webhooks:deliveries')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {query.isPending ? (
          <LoadingState />
        ) : query.isError ? (
          <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : (
          <>
            <DataTable
              columns={columns}
              data={query.data.pages.flatMap((p) => p.items)}
              getRowId={(d) => d.id}
              empty={t('webhooks:deliveriesEmpty')}
            />
            {query.hasNextPage ? (
              <Button
                variant="outline"
                onClick={() => void query.fetchNextPage()}
                disabled={query.isFetchingNextPage}
              >
                {t('common:actions.loadMore')}
              </Button>
            ) : null}
          </>
        )}
      </CardContent>
    </Card>
  );
}
