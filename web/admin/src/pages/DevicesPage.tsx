import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { SmartphoneIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  newIdempotencyKey,
  queryKeys,
  unwrap,
  useApi,
  type Device,
  type PairingCode,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { ConfirmDialog, DataTable, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../components/PageStates';
import { svgDataUrl } from '../lib/svg';
import { toastProblem } from '../lib/toast';
import { useCountdown } from '../lib/useCountdown';

const DEVICE_KINDS = ['mobile', 'desktop'] as const;

export function DevicesPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const allowed = can('devices.manage') || can('devices.pair_self');
  return (
    <RequirePermission allowed={allowed}>
      <PageHeader
        title={t('devices:title')}
        description={t('devices:description')}
        actions={<PairDeviceDialog />}
      />
      {can('devices.manage') ? <DevicesTable /> : null}
    </RequirePermission>
  );
}

function DevicesTable() {
  const { t, i18n } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId } = useCurrentMarina();
  const query = useQuery({
    queryKey: queryKeys.devices(marinaId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/devices', { params: { path: { marinaId } } }),
      ),
  });
  const revoke = useMutation({
    mutationFn: (deviceId: string) =>
      unwrap(
        api.DELETE('/api/admin/v1/marinas/{marinaId}/devices/{deviceId}', {
          params: { path: { marinaId, deviceId } },
        }),
      ),
    onSuccess: () => {
      toast.success(t('devices:revoked'));
      void queryClient.invalidateQueries({ queryKey: queryKeys.devices(marinaId) });
    },
    onError: (error) => toastProblem(t, error),
  });

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const columns: ColumnDef<Device, unknown>[] = [
    { header: t('devices:name'), accessorKey: 'name' },
    { header: t('devices:user'), cell: ({ row }) => row.original.user.name },
    {
      header: t('devices:platform'),
      cell: ({ row }) => `${row.original.platform} · ${t(`devices:kinds.${row.original.kind}`)}`,
    },
    { header: t('devices:appVersion'), cell: ({ row }) => row.original.app_version },
    {
      header: t('devices:lastUsed'),
      cell: ({ row }) =>
        row.original.last_used_at
          ? formatDateTime(row.original.last_used_at, i18n.language)
          : t('common:status.never'),
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
              title={t('devices:revokeTitle')}
              description={t('devices:revokeDescription')}
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
      getRowId={(d) => d.id}
      empty={t('devices:empty')}
    />
  );
}

function PairDeviceDialog() {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <SmartphoneIcon />
          {t('devices:pair')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('devices:pairTitle')}</DialogTitle>
          <DialogDescription>{t('devices:pairDescription')}</DialogDescription>
        </DialogHeader>
        {open ? <PairDeviceForm /> : null}
      </DialogContent>
    </Dialog>
  );
}

const SELF = 'self';

function PairDeviceForm() {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId, can, me } = useCurrentMarina();
  const [userId, setUserId] = useState<string>(SELF);
  const [kind, setKind] = useState<(typeof DEVICE_KINDS)[number]>('mobile');
  const [code, setCode] = useState<PairingCode | null>(null);
  const secondsLeft = useCountdown(code?.expires_at ?? null);

  // Pairing for someone else needs devices.manage; the list of people needs users.manage.
  const members = useQuery({
    queryKey: queryKeys.members(marinaId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/members', { params: { path: { marinaId } } }),
      ),
    enabled: can('devices.manage') && can('users.manage'),
  });

  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/devices/pairing-codes', {
          params: { path: { marinaId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
          body: { kind, ...(userId !== SELF ? { user_id: userId } : {}) },
        }),
      ),
    onSuccess: (result) => {
      setCode(result);
      void queryClient.invalidateQueries({ queryKey: queryKeys.devices(marinaId) });
    },
  });

  const others = (members.data?.items ?? []).filter((m) => m.user_id !== me?.user.id && m.active);

  return (
    <div className="flex flex-col gap-4">
      <FieldGroup>
        {create.error ? <ProblemAlert error={create.error} /> : null}
        {others.length > 0 ? (
          <Field>
            <FieldLabel htmlFor="pair-user">{t('devices:user')}</FieldLabel>
            <Select value={userId} onValueChange={setUserId}>
              <SelectTrigger id="pair-user" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={SELF}>{me?.user.name}</SelectItem>
                {others.map((m) => (
                  <SelectItem key={m.user_id} value={m.user_id}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        <Field>
          <FieldLabel htmlFor="pair-kind">{t('devices:kind')}</FieldLabel>
          <Select value={kind} onValueChange={(v) => setKind(v as (typeof DEVICE_KINDS)[number])}>
            <SelectTrigger id="pair-kind" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {DEVICE_KINDS.map((k) => (
                <SelectItem key={k} value={k}>
                  {t(`devices:kinds.${k}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Button type="button" onClick={() => create.mutate()} disabled={create.isPending}>
          {t('devices:generate')}
        </Button>
      </FieldGroup>

      {code ? (
        secondsLeft > 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-md border p-4">
            <p className="text-muted-foreground text-center text-sm">{t('devices:scan')}</p>
            <img
              src={svgDataUrl(code.qr_svg)}
              alt={t('devices:qrAlt')}
              className="bg-brand-white size-48 rounded-md p-2"
            />
            <div className="flex flex-col items-center gap-1">
              <span className="text-muted-foreground text-xs">{t('devices:code')}</span>
              <code className="font-mono text-2xl tracking-widest" data-testid="pairing-code">
                {code.code}
              </code>
            </div>
            <span className="text-muted-foreground text-sm" aria-live="polite">
              {t('devices:expiresIn', { seconds: secondsLeft })}
            </span>
          </div>
        ) : (
          <Alert>
            <AlertDescription>{t('devices:expired')}</AlertDescription>
          </Alert>
        )
      ) : null}
    </div>
  );
}
