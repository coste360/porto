import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { MailCheckIcon } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { isProblem, unwrap, useApi } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';

import { AuthCard } from '../components/AuthCard';
import { emailField } from '../lib/schemas';

const schema = z.object({ email: emailField() });

export function ForgotPasswordPage() {
  const { t } = useT();
  const api = useApi();
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { email: '' },
  });
  const request = useMutation({
    mutationFn: (email: string) =>
      unwrap(api.POST('/api/admin/v1/auth/password-reset/request', { body: { email } })),
  });

  // The answer is neutral whether or not the address exists (no user enumeration).
  const sent = request.isSuccess;
  const blockingError =
    request.error && isProblem(request.error, 'rate_limited') ? request.error : null;

  return (
    <AuthCard title={t('auth:forgot.title')} description={t('auth:forgot.description')}>
      {sent ? (
        <Alert>
          <MailCheckIcon />
          <AlertDescription>{t('auth:forgot.sent')}</AlertDescription>
        </Alert>
      ) : (
        <form onSubmit={form.handleSubmit((v) => request.mutate(v.email))} noValidate>
          <FieldGroup>
            {blockingError ? <ProblemAlert error={blockingError} /> : null}
            {request.error && !blockingError ? <ProblemAlert error={request.error} /> : null}
            <FormField
              control={form.control}
              name="email"
              label={t('auth:forgot.email')}
              render={(field) => <Input type="email" autoComplete="email" {...field} />}
            />
            <Button type="submit" disabled={request.isPending}>
              {t('auth:forgot.submit')}
            </Button>
          </FieldGroup>
        </form>
      )}
      <div className="mt-4 text-center text-sm">
        <Link to="/login" className="text-link underline-offset-4 hover:underline">
          {t('auth:forgot.backToLogin')}
        </Link>
      </div>
    </AuthCard>
  );
}
