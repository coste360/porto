import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import { queryKeys, unwrap, useApi, type Me } from '@porto/shared/api';
import { SUPPORTED_LOCALES, useT } from '@porto/shared/i18n';
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
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useMe } from '../auth/hooks';
import { LoadingState } from '../components/PageStates';
import { RecoveryCodes } from '../components/RecoveryCodes';
import { newPasswordField, requiredString, totpCodeField } from '../lib/schemas';

export function AccountPage() {
  const { t } = useT();
  const { data: me, isPending } = useMe();
  if (isPending || !me) return <LoadingState />;
  return (
    <>
      <PageHeader title={t('auth:account.title')} description={t('auth:account.description')} />
      <div className="flex flex-col gap-6">
        <ProfileCard me={me} />
        <PasswordCard />
        <SecurityCard me={me} />
      </div>
    </>
  );
}

const profileSchema = z.object({ name: requiredString(), locale: z.enum(SUPPORTED_LOCALES) });

function ProfileCard({ me }: { me: Me }) {
  const { t, i18n } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const form = useForm<z.infer<typeof profileSchema>>({
    resolver: zodResolver(profileSchema),
    defaultValues: { name: me.user.name, locale: me.user.locale === 'en' ? 'en' : 'it' },
  });
  const save = useMutation({
    mutationFn: (body: z.infer<typeof profileSchema>) =>
      unwrap(api.PATCH('/api/admin/v1/auth/me', { body })),
    onSuccess: (updated) => {
      queryClient.setQueryData(queryKeys.me, updated);
      void i18n.changeLanguage(updated.user.locale);
      form.reset({ name: updated.user.name, locale: updated.user.locale === 'en' ? 'en' : 'it' });
      toast.success(t('common:status.saved'));
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('auth:account.profile')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
          <FieldGroup className="max-w-md">
            {save.error ? <ProblemAlert error={save.error} /> : null}
            <Field>
              <FieldLabel htmlFor="account-email">{t('auth:account.email')}</FieldLabel>
              <Input id="account-email" value={me.user.email} readOnly disabled />
            </Field>
            <FormField
              control={form.control}
              name="name"
              label={t('auth:account.name')}
              render={(field) => <Input autoComplete="name" {...field} />}
            />
            <FormField
              control={form.control}
              name="locale"
              label={t('auth:account.locale')}
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
            <div>
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

const passwordSchema = z
  .object({
    current_password: requiredString(),
    new_password: newPasswordField(),
    confirm: z.string(),
  })
  .refine((v) => v.new_password === v.confirm, {
    path: ['confirm'],
    message: 'common:validation.passwordMismatch',
  });

function PasswordCard() {
  const { t } = useT();
  const api = useApi();
  const form = useForm<z.infer<typeof passwordSchema>>({
    resolver: zodResolver(passwordSchema),
    defaultValues: { current_password: '', new_password: '', confirm: '' },
  });
  const change = useMutation({
    mutationFn: (v: z.infer<typeof passwordSchema>) =>
      unwrap(
        api.POST('/api/admin/v1/auth/password', {
          body: { current_password: v.current_password, new_password: v.new_password },
        }),
      ),
    onSuccess: () => {
      form.reset();
      toast.success(t('auth:account.passwordChanged'));
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('auth:account.password')}</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={form.handleSubmit((v) => change.mutate(v))} noValidate>
          <FieldGroup className="max-w-md">
            {change.error ? <ProblemAlert error={change.error} /> : null}
            <FormField
              control={form.control}
              name="current_password"
              label={t('auth:account.currentPassword')}
              render={(field) => (
                <Input type="password" autoComplete="current-password" {...field} />
              )}
            />
            <FormField
              control={form.control}
              name="new_password"
              label={t('auth:account.newPassword')}
              render={(field) => <Input type="password" autoComplete="new-password" {...field} />}
            />
            <FormField
              control={form.control}
              name="confirm"
              label={t('auth:account.confirmPassword')}
              render={(field) => <Input type="password" autoComplete="new-password" {...field} />}
            />
            <div>
              <Button type="submit" disabled={change.isPending}>
                {t('auth:account.changePassword')}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  );
}

function SecurityCard({ me }: { me: Me }) {
  const { t } = useT();
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {t('auth:account.security')}
          <Badge variant={me.user.totp_enabled ? 'secondary' : 'outline'}>
            {me.user.totp_enabled ? t('auth:account.totpEnabled') : t('auth:account.totpDisabled')}
          </Badge>
        </CardTitle>
        {me.user.totp_enabled ? (
          <CardDescription>{t('auth:account.regenerateDescription')}</CardDescription>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-wrap gap-2">
        {me.user.totp_enabled ? (
          <>
            <RegenerateCodesDialog />
            <DisableTotpDialog />
          </>
        ) : (
          <Button asChild>
            <Link to="/totp-setup">{t('auth:account.enableTotp')}</Link>
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

const codeSchema = z.object({ code: totpCodeField() });

function RegenerateCodesDialog() {
  const { t } = useT();
  const api = useApi();
  const [open, setOpen] = useState(false);
  const [codes, setCodes] = useState<string[] | null>(null);
  const form = useForm<z.infer<typeof codeSchema>>({
    resolver: zodResolver(codeSchema),
    defaultValues: { code: '' },
  });
  const regenerate = useMutation({
    mutationFn: (code: string) =>
      unwrap(api.POST('/api/admin/v1/auth/recovery-codes', { body: { code } })),
    onSuccess: (result) => setCodes(result.codes),
  });
  const close = () => {
    setOpen(false);
    setCodes(null);
    regenerate.reset();
    form.reset();
  };
  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
      <DialogTrigger asChild>
        <Button variant="outline">{t('auth:account.regenerate')}</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('auth:account.regenerate')}</DialogTitle>
          <DialogDescription>{t('auth:account.regenerateDescription')}</DialogDescription>
        </DialogHeader>
        {codes ? (
          <RecoveryCodes codes={codes} onDone={close} />
        ) : (
          <form onSubmit={form.handleSubmit((v) => regenerate.mutate(v.code))} noValidate>
            <FieldGroup>
              {regenerate.error ? <ProblemAlert error={regenerate.error} /> : null}
              <FormField
                control={form.control}
                name="code"
                label={t('auth:account.totpCode')}
                render={(field) => (
                  <Input inputMode="numeric" autoComplete="one-time-code" {...field} />
                )}
              />
              <Button type="submit" disabled={regenerate.isPending}>
                {t('common:actions.confirm')}
              </Button>
            </FieldGroup>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

const disableSchema = z.object({ password: requiredString(), code: totpCodeField() });

function DisableTotpDialog() {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const form = useForm<z.infer<typeof disableSchema>>({
    resolver: zodResolver(disableSchema),
    defaultValues: { password: '', code: '' },
  });
  const disable = useMutation({
    mutationFn: (body: z.infer<typeof disableSchema>) =>
      unwrap(api.POST('/api/admin/v1/auth/totp/disable', { body })),
    onSuccess: () => {
      setOpen(false);
      toast.success(t('auth:account.totpDisabledToast'));
      void queryClient.invalidateQueries({ queryKey: queryKeys.me });
    },
  });
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">{t('auth:account.disableTotp')}</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('auth:account.disableTitle')}</DialogTitle>
          <DialogDescription>{t('auth:account.disableDescription')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((v) => disable.mutate(v))} noValidate>
          <FieldGroup>
            {disable.error ? <ProblemAlert error={disable.error} /> : null}
            <FormField
              control={form.control}
              name="password"
              label={t('auth:account.currentPassword')}
              render={(field) => (
                <Input type="password" autoComplete="current-password" {...field} />
              )}
            />
            <FormField
              control={form.control}
              name="code"
              label={t('auth:account.totpCode')}
              render={(field) => (
                <Input inputMode="numeric" autoComplete="one-time-code" {...field} />
              )}
            />
            <Button type="submit" variant="destructive" disabled={disable.isPending}>
              {t('auth:account.disableTotp')}
            </Button>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}
