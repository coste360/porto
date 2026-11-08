import { zodResolver } from '@hookform/resolvers/zod';
import { Link, useNavigate, useRouterState } from '@tanstack/react-router';
import { LinkIcon } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';

import { ProblemError } from '@porto/shared/api';
import { usePairDesktopRemote } from '@porto/shared/desktop/hooks';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField, ProblemAlert } from '@porto/shared/ui/patterns';
import { Separator } from '@porto/shared/ui/separator';
import { Textarea } from '@porto/shared/ui/textarea';

import { AuthCard } from '../../components/AuthCard';
import { readHashParams } from '../../lib/hash';
import { remotePairSchema, toPairRequest, type RemotePairForm } from './remote';

/**
 * First-run branch "Collega a un Porto online" (m6b-plan §7.2): the porto:// link copied from the Devices page of
 * the online instance, or its address and code, plus the app PIN for the local administration of the remote
 * mode. The first-run token comes in the fragment (#t=…), removed from the address bar at once. On success the
 * app restarts in remote mode and the window opens the panel of the online instance by itself.
 */
export function RemotePairPage() {
  const { t } = useT();
  const navigate = useNavigate();
  const hash = useRouterState({ select: (s) => s.location.hash });
  // Only this page strips its own fragment (a link to the sibling page also carries the token in the fragment).
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [token] = useState(() => readHashParams(hash).get('t')?.trim() ?? '');
  const pair = usePairDesktopRemote();
  const [done, setDone] = useState(false);
  const form = useForm<RemotePairForm>({
    resolver: zodResolver(remotePairSchema),
    defaultValues: {
      link: '',
      url: '',
      code: '',
      fp: '',
      pin: '',
      confirm: '',
      name: t('desktop:remote.defaultName'),
    },
  });

  useEffect(() => {
    if (hash && pathname.endsWith('/desktop/remote'))
      void navigate({ to: '/desktop/remote', hash: '', replace: true });
  }, [hash, pathname, navigate]);

  const mismatch =
    pair.error instanceof ProblemError && pair.error.type === 'remote_pin_mismatch'
      ? pair.error.details
      : null;

  let body;
  if (done) {
    body = (
      <Alert data-testid="remote-pair-done">
        <AlertTitle>{t('desktop:restore.title')}</AlertTitle>
        <AlertDescription>{t('desktop:remote.description')}</AlertDescription>
      </Alert>
    );
  } else if (!token) {
    body = (
      <Alert variant="destructive" data-testid="remote-pair-no-token">
        <AlertDescription>{t('errors:first_run_token_invalid')}</AlertDescription>
      </Alert>
    );
  } else {
    body = (
      <form
        noValidate
        onSubmit={form.handleSubmit((v) =>
          pair.mutate(toPairRequest(token, v), { onSuccess: () => setDone(true) }),
        )}
      >
        <FieldGroup>
          {mismatch ? (
            <Alert variant="destructive" data-testid="remote-pin-mismatch">
              <AlertTitle>{t('desktop:remote.pinMismatch')}</AlertTitle>
              <AlertDescription className="break-all">
                {t('desktop:remote.pinMismatchDetail', {
                  expected: String(mismatch.expected ?? ''),
                  presented: String(mismatch.presented ?? ''),
                })}
              </AlertDescription>
            </Alert>
          ) : pair.error ? (
            <ProblemAlert error={pair.error} />
          ) : null}
          <p className="text-muted-foreground text-sm">{t('desktop:remote.linkHint')}</p>
          <FormField
            control={form.control}
            name="link"
            label={t('desktop:remote.link')}
            render={(field) => <Textarea rows={2} className="font-mono text-xs" {...field} />}
          />
          <div className="text-muted-foreground flex items-center gap-2 text-xs">
            <Separator className="flex-1" />
            {t('desktop:remote.or')}
            <Separator className="flex-1" />
          </div>
          <FormField
            control={form.control}
            name="url"
            label={t('desktop:remote.url')}
            render={(field) => <Input type="url" placeholder="https://" {...field} />}
          />
          <FormField
            control={form.control}
            name="code"
            label={t('desktop:remote.code')}
            render={(field) => <Input autoComplete="off" className="font-mono" {...field} />}
          />
          <FormField
            control={form.control}
            name="fp"
            label={t('desktop:remote.fingerprint')}
            render={(field) => (
              <Input autoComplete="off" className="font-mono text-xs" {...field} />
            )}
          />
          <Separator />
          <FormField
            control={form.control}
            name="name"
            label={t('desktop:remote.name')}
            render={(field) => <Input {...field} />}
          />
          <FormField
            control={form.control}
            name="pin"
            label={t('desktop:remote.pin')}
            description={t('desktop:remote.pinHint')}
            render={(field) => (
              <Input type="password" inputMode="numeric" autoComplete="new-password" {...field} />
            )}
          />
          <FormField
            control={form.control}
            name="confirm"
            label={t('desktop:remote.pinRepeat')}
            render={(field) => (
              <Input type="password" inputMode="numeric" autoComplete="new-password" {...field} />
            )}
          />
          <Button type="submit" disabled={pair.isPending}>
            <LinkIcon />
            {t('desktop:remote.submit')}
          </Button>
          <Button variant="link" asChild>
            <Link to="/desktop/first-run" hash={`t=${token}`}>
              {t('desktop:remote.back')}
            </Link>
          </Button>
        </FieldGroup>
      </form>
    );
  }
  return (
    <AuthCard title={t('desktop:remote.connect')} description={t('desktop:remote.description')}>
      <div data-testid="remote-pair-page">{body}</div>
    </AuthCard>
  );
}
