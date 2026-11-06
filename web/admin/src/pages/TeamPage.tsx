import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnDef } from '@tanstack/react-table';
import { KeyRoundIcon, MoreHorizontalIcon, UserPlusIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import type { z } from 'zod';

import {
  newIdempotencyKey,
  queryKeys,
  unwrap,
  useApi,
  type Member,
  type Role,
} from '@porto/shared/api';
import { isLocale, SOURCE_LOCALE, useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  ConfirmDialog,
  DataTable,
  FormField,
  PageHeader,
  ProblemAlert,
} from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../components/PageStates';
import { ROLES } from '../lib/roles';
import { addMemberSchema } from '../lib/schemas';
import { toastProblem } from '../lib/toast';

type AddMemberValues = z.infer<typeof addMemberSchema>;

export function TeamPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('users.manage')}>
      <PageHeader
        title={t('team:members.title')}
        description={t('team:members.description')}
        actions={<AddMemberDialog />}
      />
      <MembersTable />
    </RequirePermission>
  );
}

function useMembersMutations(marinaId: string) {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: queryKeys.members(marinaId) });
  const onError = (error: unknown) => toastProblem(t, error);
  return {
    changeRole: useMutation({
      mutationFn: ({ userId, role }: { userId: string; role: Role }) =>
        unwrap(
          api.PATCH('/api/admin/v1/marinas/{marinaId}/members/{userId}', {
            params: { path: { marinaId, userId } },
            body: { role },
          }),
        ),
      onSuccess: () => {
        toast.success(t('team:members.roleChanged'));
        void invalidate();
      },
      onError,
    }),
    remove: useMutation({
      mutationFn: (userId: string) =>
        unwrap(
          api.DELETE('/api/admin/v1/marinas/{marinaId}/members/{userId}', {
            params: { path: { marinaId, userId } },
          }),
        ),
      onSuccess: () => {
        toast.success(t('team:members.removed'));
        void invalidate();
      },
      onError,
    }),
    sendReset: useMutation({
      mutationFn: (userId: string) =>
        unwrap(
          api.POST('/api/admin/v1/marinas/{marinaId}/members/{userId}/password-reset', {
            params: { path: { marinaId, userId } },
          }),
        ),
      onSuccess: () => toast.success(t('team:members.resetSent')),
      onError,
    }),
  };
}

function MembersTable() {
  const { t, i18n } = useT();
  const api = useApi();
  const { marinaId } = useCurrentMarina();
  const query = useQuery({
    queryKey: queryKeys.members(marinaId),
    queryFn: () =>
      unwrap(
        api.GET('/api/admin/v1/marinas/{marinaId}/members', { params: { path: { marinaId } } }),
      ),
  });
  const { changeRole, remove, sendReset } = useMembersMutations(marinaId);

  if (query.isPending) return <LoadingState />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const columns: ColumnDef<Member, unknown>[] = [
    {
      header: t('team:members.name'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-medium">{row.original.name}</span>
          <span className="text-muted-foreground text-xs">{row.original.email}</span>
        </div>
      ),
    },
    {
      header: t('team:members.role'),
      cell: ({ row }) =>
        row.original.scope === 'organization' ? (
          <Badge variant="secondary">
            {t(`common:roles.${row.original.role}`)} · {t('team:members.scopeOrganization')}
          </Badge>
        ) : (
          <Select
            value={row.original.role}
            onValueChange={(role) =>
              changeRole.mutate({ userId: row.original.user_id, role: role as Role })
            }
          >
            <SelectTrigger size="sm" aria-label={t('team:members.changeRole')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ROLES.map((role) => (
                <SelectItem key={role} value={role}>
                  {t(`common:roles.${role}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ),
    },
    {
      header: t('team:members.status'),
      cell: ({ row }) =>
        row.original.invitation_pending ? (
          <Badge variant="outline">{t('team:members.invited')}</Badge>
        ) : row.original.active ? (
          <Badge variant="secondary">{t('common:status.active')}</Badge>
        ) : (
          <Badge variant="outline">{t('common:status.inactive')}</Badge>
        ),
    },
    {
      header: t('team:members.lastLogin'),
      cell: ({ row }) =>
        row.original.last_login_at
          ? formatDateTime(row.original.last_login_at, i18n.language)
          : t('common:status.never'),
    },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) =>
        row.original.scope === 'marina' ? (
          <div className="flex justify-end gap-1">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={t('common:actions.actions')}>
                  <MoreHorizontalIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => sendReset.mutate(row.original.user_id)}>
                  <KeyRoundIcon />
                  {t('team:members.sendReset')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <ConfirmDialog
              trigger={
                <Button variant="ghost" size="sm">
                  {t('common:actions.remove')}
                </Button>
              }
              title={t('team:members.removeTitle')}
              description={t('team:members.removeDescription')}
              confirmLabel={t('common:actions.remove')}
              cancelLabel={t('common:actions.cancel')}
              destructive
              onConfirm={() => remove.mutate(row.original.user_id)}
            />
          </div>
        ) : null,
    },
  ];

  return (
    <DataTable
      columns={columns}
      data={query.data.items}
      getRowId={(m) => m.user_id}
      empty={t('team:members.empty')}
    />
  );
}

function AddMemberDialog() {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <UserPlusIcon />
          {t('team:members.add')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('team:members.addTitle')}</DialogTitle>
          <DialogDescription>{t('team:members.addDescription')}</DialogDescription>
        </DialogHeader>
        {open ? <AddMemberForm onDone={() => setOpen(false)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

export function AddMemberForm({ onDone }: { onDone: () => void }) {
  const { t, i18n } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const { marinaId } = useCurrentMarina();
  // One key per dialog: a double submit or a retry is executed once by the server.
  const [idempotencyKey] = useState(newIdempotencyKey);
  const form = useForm<AddMemberValues>({
    resolver: zodResolver(addMemberSchema),
    defaultValues: { name: '', email: '', role: 'office' },
  });
  const add = useMutation({
    mutationFn: (body: AddMemberValues) =>
      unwrap(
        api.POST('/api/admin/v1/marinas/{marinaId}/members', {
          params: { path: { marinaId }, header: { 'Idempotency-Key': idempotencyKey } },
          body: { ...body, locale: isLocale(i18n.language) ? i18n.language : SOURCE_LOCALE },
        }),
      ),
    onSuccess: async () => {
      toast.success(t('team:members.added'));
      await queryClient.invalidateQueries({ queryKey: queryKeys.members(marinaId) });
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
        <FormField
          control={form.control}
          name="role"
          label={t('team:members.role')}
          render={({ id, value, onChange }) => (
            <Select value={value} onValueChange={onChange}>
              <SelectTrigger id={id} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((role) => (
                  <SelectItem key={role} value={role}>
                    {t(`common:roles.${role}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onDone}>
            {t('common:actions.cancel')}
          </Button>
          <Button type="submit" disabled={add.isPending}>
            {t('common:actions.add')}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}
