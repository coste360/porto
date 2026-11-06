import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { Link, useRouterState } from '@tanstack/react-router';
import { CircleCheckIcon } from 'lucide-react';
import { useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { unwrap, useApi } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';

import { AuthCard } from '../components/AuthCard';
import { readHashParams } from '../lib/hash';
import { newPasswordField } from '../lib/schemas';

const schema = z
  .object({ password: newPasswordField(), confirm: z.string() })
  .refine((v) => v.password === v.confirm, {
    path: ['confirm'],
    message: 'common:validation.passwordMismatch',
  });

/** Password reset and invitation acceptance: the token travels in the URL fragment. */
export function ResetPasswordPage() {
  const { t } = useT();
  const api = useApi();
  const hash = useRouterState({ select: (s) => s.location.hash });
  const { token, invite } = useMemo(() => {
    const params = readHashParams(
      hash || (typeof window !== 'undefined' ? window.location.hash : ''),
    );
    return { token: params.get('token') ?? '', invite: params.get('invite') === '1' };
  }, [hash]);

  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { password: '', confirm: '' },
  });
  const confirm = useMutation({
    mutationFn: (newPassword: string) =>
      unwrap(
        api.POST('/api/admin/v1/auth/password-reset/confirm', {
          body: { token, new_password: newPassword },
        }),
      ),
  });

  const title = invite ? t('auth:reset.inviteTitle') : t('auth:reset.title');
  const description = invite ? t('auth:reset.inviteDescription') : t('auth:reset.description');

  return (
    <AuthCard title={title} description={description}>
      {!token ? (
        <Alert variant="destructive">
          <AlertDescription>{t('auth:reset.missingToken')}</AlertDescription>
        </Alert>
      ) : confirm.isSuccess ? (
        <div className="flex flex-col gap-4">
          <Alert>
            <CircleCheckIcon />
            <AlertDescription>{t('auth:reset.done')}</AlertDescription>
          </Alert>
          <Button asChild>
            <Link to="/login">{t('auth:reset.goToLogin')}</Link>
          </Button>
        </div>
      ) : (
        <form onSubmit={form.handleSubmit((v) => confirm.mutate(v.password))} noValidate>
          <FieldGroup>
            {confirm.error ? <ProblemAlert error={confirm.error} /> : null}
            <FormField
              control={form.control}
              name="password"
              label={t('auth:reset.password')}
              render={(field) => <Input type="password" autoComplete="new-password" {...field} />}
            />
            <FormField
              control={form.control}
              name="confirm"
              label={t('auth:reset.confirm')}
              render={(field) => <Input type="password" autoComplete="new-password" {...field} />}
            />
            <Button type="submit" disabled={confirm.isPending}>
              {t('auth:reset.submit')}
            </Button>
          </FieldGroup>
        </form>
      )}
    </AuthCard>
  );
}
