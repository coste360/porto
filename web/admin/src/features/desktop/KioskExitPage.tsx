import { Link } from '@tanstack/react-router';
import { useState, type FormEvent } from 'react';

import { useAppWindowStatus, useExitDesktopKiosk } from '@porto/shared/desktop/hooks';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns';

import { AuthCard } from '../../components/AuthCard';
import { LoadingState } from '../../components/PageStates';

/**
 * Leaves kiosk mode until the next launch (m6b-plan §7.6, D14), opened by Cmd/Ctrl+Shift+K: the app PIN in remote
 * mode, the email and password of an owner in local mode (the front desk user is usually not one). Nothing typed
 * here is kept after the call.
 */
export function KioskExitPage() {
  const { t } = useT();
  const status = useAppWindowStatus();
  const exit = useExitDesktopKiosk();
  const [pin, setPin] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const remote = status.remote;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    exit.mutate(remote ? { pin } : { credentials: { email: email.trim(), password } }, {
      onSettled: () => {
        setPin('');
        setPassword('');
      },
    });
  };

  let body;
  if (status.resolving) body = <LoadingState rows={3} />;
  else if (!status.loopback) {
    body = (
      <Alert>
        <AlertDescription>{t('desktop:settings.onlyInApp')}</AlertDescription>
      </Alert>
    );
  } else if (exit.isSuccess) {
    body = (
      <div className="flex flex-col gap-4">
        <Alert data-testid="kiosk-exit-done">
          <AlertDescription>{t('desktop:kiosk.done')}</AlertDescription>
        </Alert>
        <Button asChild>
          <Link to="/">{t('desktop:kiosk.back')}</Link>
        </Button>
      </div>
    );
  } else {
    const ready = remote ? /^[0-9]{6,12}$/.test(pin) : email.trim() !== '' && password !== '';
    body = (
      <form onSubmit={submit} noValidate>
        <FieldGroup>
          {exit.error ? <ProblemAlert error={exit.error} /> : null}
          <p className="text-muted-foreground text-sm">{t('desktop:kiosk.exitHint')}</p>
          {remote ? (
            <Field>
              <FieldLabel htmlFor="kiosk-pin">{t('desktop:appPin')}</FieldLabel>
              <Input
                id="kiosk-pin"
                type="password"
                inputMode="numeric"
                autoComplete="off"
                value={pin}
                onChange={(e) => setPin(e.target.value.replace(/\D/g, '').slice(0, 12))}
              />
            </Field>
          ) : (
            <>
              <Field>
                <FieldLabel htmlFor="kiosk-email">{t('desktop:kiosk.email')}</FieldLabel>
                <Input
                  id="kiosk-email"
                  type="email"
                  autoComplete="off"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="kiosk-password">{t('desktop:kiosk.password')}</FieldLabel>
                <Input
                  id="kiosk-password"
                  type="password"
                  autoComplete="off"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </Field>
            </>
          )}
          <Button type="submit" disabled={!ready || exit.isPending}>
            {t('desktop:kiosk.submit')}
          </Button>
          <Button variant="link" asChild>
            <Link to="/">{t('desktop:kiosk.back')}</Link>
          </Button>
        </FieldGroup>
      </form>
    );
  }
  return (
    <AuthCard title={t('desktop:kiosk.exit')}>
      <div data-testid="kiosk-exit-page">{body}</div>
    </AuthCard>
  );
}
