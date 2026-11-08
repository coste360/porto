import { zodResolver } from '@hookform/resolvers/zod';
import { Link, useNavigate, useRouterState } from '@tanstack/react-router';
import { LinkIcon, UploadIcon } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';

import { useDesktopFirstRun, useDesktopFirstRunRestore, useDesktopStatus } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';
import { Separator } from '@porto/shared/ui/separator';

import { AuthCard } from '../../components/AuthCard';
import { LoadingState } from '../../components/PageStates';
import { readHashParams } from '../../lib/hash';
import { RestartingNotice } from './RestartingNotice';
import { firstRunSchema, type FirstRunForm } from './schemas';

/**
 * First run of the desktop edition (m6a-plan §2.6, §4.4): the launcher opens `/admin/desktop/first-run#t=<token>`;
 * the single-use token is read from the fragment (removed from the address bar at once) and either creates the
 * owner and the organization, or restores a backup. The marina is then created by the usual panel flow.
 */
export function FirstRunPage() {
  const { t } = useT();
  const navigate = useNavigate();
  const hash = useRouterState({ select: (s) => s.location.hash });
  // Only this page strips its own fragment (a link to the sibling page also carries the token in the fragment).
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [token] = useState(() => readHashParams(hash).get('t')?.trim() ?? '');
  // M6b: the remote mode was reset because the online instance revoked this app.
  const [revoked] = useState(() => readHashParams(hash).get('revoked') === '1');
  const status = useDesktopStatus();
  const [restarting, setRestarting] = useState(false);

  useEffect(() => {
    if (hash && pathname.endsWith('/desktop/first-run'))
      void navigate({ to: '/desktop/first-run', hash: '', replace: true });
  }, [hash, pathname, navigate]);

  if (restarting) {
    return (
      <AuthCard title={t('desktop:firstRun.title')}>
        <RestartingNotice />
      </AuthCard>
    );
  }

  let body;
  if (status.resolving) body = <LoadingState rows={4} />;
  else if (status.data && !status.data.first_run_pending) {
    body = (
      <div className="flex flex-col gap-4">
        <Alert>
          <AlertDescription>{t('errors:first_run_done')}</AlertDescription>
        </Alert>
        <Button asChild>
          <Link to="/login">{t('desktop:firstRun.goToLogin')}</Link>
        </Button>
      </div>
    );
  } else if (!token || !status.loopback) {
    body = (
      <Alert variant="destructive" data-testid="first-run-no-token">
        <AlertDescription>{t('errors:first_run_token_invalid')}</AlertDescription>
      </Alert>
    );
  } else {
    body = (
      <div className="flex flex-col gap-6">
        {revoked ? (
          <Alert variant="destructive" data-testid="first-run-revoked">
            <AlertTitle>{t('desktop:remote.revoked')}</AlertTitle>
            <AlertDescription>{t('desktop:remote.revokedDescription')}</AlertDescription>
          </Alert>
        ) : null}
        <Button variant="outline" asChild data-testid="first-run-remote">
          <Link to="/desktop/remote" hash={`t=${token}`}>
            <LinkIcon />
            {t('desktop:remote.fromFirstRun')}
          </Link>
        </Button>
        <Separator />
        <OwnerForm token={token} />
        <Separator />
        <FirstRunRestore token={token} onRestart={() => setRestarting(true)} />
      </div>
    );
  }

  return (
    <AuthCard title={t('desktop:firstRun.title')} description={t('desktop:firstRun.description')}>
      <div data-testid="first-run-page">{body}</div>
    </AuthCard>
  );
}

function OwnerForm({ token }: { token: string }) {
  const { t } = useT();
  const navigate = useNavigate();
  const firstRun = useDesktopFirstRun();
  const form = useForm<FirstRunForm>({
    resolver: zodResolver(firstRunSchema),
    defaultValues: {
      organization: '',
      name: '',
      email: '',
      password: '',
      confirm: '',
      update_check: true,
    },
  });
  const onSubmit = (v: FirstRunForm) =>
    firstRun.mutate(
      {
        token,
        organization: v.organization.trim(),
        name: v.name.trim(),
        email: v.email.trim(),
        password: v.password,
        update_check: v.update_check,
      },
      { onSuccess: () => void navigate({ to: '/', replace: true }) },
    );

  return (
    <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
      <FieldGroup>
        {firstRun.error ? <ProblemAlert error={firstRun.error} /> : null}
        <FormField
          control={form.control}
          name="organization"
          label={t('desktop:firstRun.organization')}
          render={(field) => <Input autoComplete="organization" {...field} />}
        />
        <FormField
          control={form.control}
          name="name"
          label={t('desktop:firstRun.name')}
          render={(field) => <Input autoComplete="name" {...field} />}
        />
        <FormField
          control={form.control}
          name="email"
          label={t('desktop:firstRun.email')}
          render={(field) => <Input type="email" autoComplete="username" {...field} />}
        />
        <FormField
          control={form.control}
          name="password"
          label={t('desktop:firstRun.password')}
          render={(field) => <Input type="password" autoComplete="new-password" {...field} />}
        />
        <FormField
          control={form.control}
          name="confirm"
          label={t('desktop:firstRun.passwordRepeat')}
          render={(field) => <Input type="password" autoComplete="new-password" {...field} />}
        />
        <Controller
          control={form.control}
          name="update_check"
          render={({ field }) => (
            <Field orientation="horizontal">
              <Checkbox
                id="first-run-update-check"
                checked={field.value}
                onCheckedChange={(v) => field.onChange(v === true)}
              />
              <FieldLabel htmlFor="first-run-update-check" className="font-normal">
                {t('desktop:firstRun.updateCheck')}
              </FieldLabel>
            </Field>
          )}
        />
        <Button type="submit" disabled={firstRun.isPending}>
          {t('desktop:firstRun.submit')}
        </Button>
      </FieldGroup>
    </form>
  );
}

function FirstRunRestore({ token, onRestart }: { token: string; onRestart: () => void }) {
  const { t } = useT();
  const restore = useDesktopFirstRunRestore();
  const [file, setFile] = useState<File | null>(null);
  return (
    <div className="flex flex-col gap-3">
      <p className="text-muted-foreground text-sm">{t('desktop:firstRun.restoreDescription')}</p>
      <Field>
        <FieldLabel htmlFor="first-run-restore-file">{t('desktop:backup.file')}</FieldLabel>
        <Input
          id="first-run-restore-file"
          type="file"
          accept=".gz,.tgz,application/gzip"
          onChange={(e) => setFile(e.target.files?.[0] ?? null)}
        />
      </Field>
      {restore.error ? <ProblemAlert error={restore.error} /> : null}
      <Button
        type="button"
        variant="outline"
        disabled={!file || restore.isPending}
        onClick={() => file && restore.mutate({ token, file }, { onSuccess: onRestart })}
      >
        <UploadIcon />
        {t('desktop:firstRun.restore')}
      </Button>
    </div>
  );
}
