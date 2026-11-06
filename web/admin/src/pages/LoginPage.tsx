import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { isProblem, queryKeys, unwrap, useApi, type Me } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@porto/shared/ui/input-otp';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';

import { AuthCard } from '../components/AuthCard';
import { safeRedirect } from '../lib/hash';
import { emailField, requiredString, totpCodeField } from '../lib/schemas';

const routeApi = getRouteApi('/login');

const credentialsSchema = z.object({ email: emailField(), password: requiredString() });
type Credentials = z.infer<typeof credentialsSchema>;

const codeSchema = z.object({ code: totpCodeField() });
const recoverySchema = z.object({ code: requiredString() });

export function LoginPage() {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const search = routeApi.useSearch();
  const [credentials, setCredentials] = useState<Credentials | null>(null);

  const login = useMutation({
    mutationFn: (body: Credentials & { totp_code?: string; recovery_code?: string }) =>
      unwrap(api.POST('/api/admin/v1/auth/login', { body })),
    onSuccess: async (me: Me) => {
      queryClient.setQueryData(queryKeys.me, me);
      if (me.totp_enrolment_required) {
        await navigate({ to: '/totp-setup' });
        return;
      }
      await navigate({ href: `/admin${safeRedirect(search.redirect)}`, replace: true });
    },
  });

  const onCredentials = (values: Credentials) => {
    login.mutate(values, {
      onError: (error) => {
        if (isProblem(error, 'totp_required')) {
          login.reset();
          setCredentials(values);
        }
      },
    });
  };

  if (credentials) {
    return (
      <TotpStep
        pending={login.isPending}
        error={login.error}
        onBack={() => {
          login.reset();
          setCredentials(null);
        }}
        onSubmit={(code, recovery) =>
          login.mutate(
            recovery
              ? { ...credentials, recovery_code: code }
              : { ...credentials, totp_code: code },
          )
        }
      />
    );
  }

  return (
    <AuthCard title={t('auth:login.title')} description={t('auth:login.description')}>
      <CredentialsForm pending={login.isPending} error={login.error} onSubmit={onCredentials} />
      <div className="mt-4 text-center text-sm">
        <Link to="/forgot-password" className="text-link underline-offset-4 hover:underline">
          {t('auth:login.forgot')}
        </Link>
      </div>
    </AuthCard>
  );
}

function CredentialsForm({
  pending,
  error,
  onSubmit,
}: {
  pending: boolean;
  error: unknown;
  onSubmit: (values: Credentials) => void;
}) {
  const { t } = useT();
  const form = useForm<Credentials>({
    resolver: zodResolver(credentialsSchema),
    defaultValues: { email: '', password: '' },
  });
  return (
    <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
      <FieldGroup>
        {error && !isProblem(error, 'totp_required') ? <ProblemAlert error={error} /> : null}
        <FormField
          control={form.control}
          name="email"
          label={t('auth:login.email')}
          render={(field) => <Input type="email" autoComplete="username" {...field} />}
        />
        <FormField
          control={form.control}
          name="password"
          label={t('auth:login.password')}
          render={(field) => <Input type="password" autoComplete="current-password" {...field} />}
        />
        <Button type="submit" disabled={pending}>
          {pending ? t('auth:login.submitting') : t('auth:login.submit')}
        </Button>
      </FieldGroup>
    </form>
  );
}

function TotpStep({
  pending,
  error,
  onSubmit,
  onBack,
}: {
  pending: boolean;
  error: unknown;
  onSubmit: (code: string, recovery: boolean) => void;
  onBack: () => void;
}) {
  const { t } = useT();
  const [recovery, setRecovery] = useState(false);
  const form = useForm<{ code: string }>({
    resolver: zodResolver(recovery ? recoverySchema : codeSchema),
    defaultValues: { code: '' },
  });

  return (
    <AuthCard title={t('auth:totpStep.title')} description={t('auth:totpStep.description')}>
      <form onSubmit={form.handleSubmit((v) => onSubmit(v.code.trim(), recovery))} noValidate>
        <FieldGroup>
          {error ? <ProblemAlert error={error} /> : null}
          {recovery ? (
            <FormField
              key="recovery"
              control={form.control}
              name="code"
              label={t('auth:totpStep.recoveryCode')}
              render={(field) => <Input autoComplete="one-time-code" {...field} />}
            />
          ) : (
            <FormField
              key="totp"
              control={form.control}
              name="code"
              label={t('auth:totpStep.code')}
              render={({ ref: _ref, ...field }) => (
                <InputOTP
                  maxLength={6}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  autoFocus
                  {...field}
                >
                  <InputOTPGroup>
                    {Array.from({ length: 6 }, (_, i) => (
                      <InputOTPSlot key={i} index={i} />
                    ))}
                  </InputOTPGroup>
                </InputOTP>
              )}
            />
          )}
          <Button type="submit" disabled={pending}>
            {t('auth:totpStep.submit')}
          </Button>
          <div className="flex flex-col gap-2 text-center text-sm">
            <Button
              type="button"
              variant="link"
              onClick={() => {
                form.reset({ code: '' });
                setRecovery((r) => !r);
              }}
            >
              {recovery ? t('auth:totpStep.useCode') : t('auth:totpStep.useRecovery')}
            </Button>
            <Button type="button" variant="link" onClick={onBack}>
              {t('auth:totpStep.back')}
            </Button>
          </div>
        </FieldGroup>
      </form>
    </AuthCard>
  );
}
