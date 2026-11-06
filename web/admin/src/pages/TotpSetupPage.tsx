import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { isProblem, queryKeys, unwrap, useApi, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@porto/shared/ui/input-otp';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';

import { useMe } from '../auth/hooks';
import { AuthCard } from '../components/AuthCard';
import { RecoveryCodes } from '../components/RecoveryCodes';
import { svgDataUrl } from '../lib/svg';
import { totpCodeField } from '../lib/schemas';

const schema = z.object({ code: totpCodeField() });

export function TotpSetupPage() {
  const { t } = useT();
  const api = useApi();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const [enrollment, setEnrollment] = useState<Schemas['TotpEnrollment'] | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { code: '' },
  });

  const enroll = useMutation({
    mutationFn: () => unwrap(api.POST('/api/admin/v1/auth/totp/enroll')),
    onSuccess: setEnrollment,
  });
  const confirm = useMutation({
    mutationFn: (code: string) =>
      unwrap(api.POST('/api/admin/v1/auth/totp/confirm', { body: { code } })),
    onSuccess: (result) => setCodes(result.codes),
  });

  const finish = async () => {
    await queryClient.invalidateQueries({ queryKey: queryKeys.me });
    await navigate({ to: '/' });
  };

  return (
    <AuthCard title={t('auth:totpSetup.title')} description={t('auth:totpSetup.description')}>
      {codes ? (
        <RecoveryCodes codes={codes} onDone={() => void finish()} />
      ) : (
        <div className="flex flex-col gap-4">
          {me?.totp_enrolment_required ? (
            <Alert>
              <AlertDescription>{t('auth:totpSetup.required')}</AlertDescription>
            </Alert>
          ) : null}
          {enroll.error && !isProblem(enroll.error, 'totp_already_enabled') ? (
            <ProblemAlert error={enroll.error} />
          ) : null}
          {!enrollment ? (
            <Button onClick={() => enroll.mutate()} disabled={enroll.isPending}>
              {t('auth:totpSetup.start')}
            </Button>
          ) : (
            <form onSubmit={form.handleSubmit((v) => confirm.mutate(v.code))} noValidate>
              <FieldGroup>
                <img
                  src={svgDataUrl(enrollment.qr_svg)}
                  alt={t('auth:totpSetup.qrAlt')}
                  className="mx-auto size-48 rounded-md bg-brand-white p-2"
                />
                <div className="flex flex-col gap-1 text-sm">
                  <span className="text-muted-foreground">{t('auth:totpSetup.manual')}</span>
                  <code className="bg-muted rounded px-2 py-1 font-mono break-all">
                    {enrollment.secret}
                  </code>
                </div>
                {confirm.error ? <ProblemAlert error={confirm.error} /> : null}
                <FormField
                  control={form.control}
                  name="code"
                  label={t('auth:totpSetup.code')}
                  render={({ ref: _ref, ...field }) => (
                    <InputOTP
                      maxLength={6}
                      inputMode="numeric"
                      autoComplete="one-time-code"
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
                <Button type="submit" disabled={confirm.isPending}>
                  {t('auth:totpSetup.submit')}
                </Button>
              </FieldGroup>
            </form>
          )}
        </div>
      )}
    </AuthCard>
  );
}
