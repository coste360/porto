// Customer area of the booking page (m8-plan §7, §12.2): email form, link exchange (token in the URL fragment,
// wiped with history.replaceState), bookings list and detail. Lazy chunk; the session lives in sessionStorage.
import { LinkIcon, LogOutIcon, MailCheckIcon, UserRoundIcon } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import {
  isProblem,
  useCloseAccountSession,
  useOpenAccountSession,
  useRequestAccountLink,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldError, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Spinner } from '@porto/shared/ui/spinner';

import { zodFormResolver } from '../common/formResolver';
import { BookingDetail } from './BookingDetail';
import { BookingList } from './BookingList';
import {
  accountPath,
  clearSession,
  fragmentToken,
  readSession,
  wipeFragment,
  writeSession,
} from './session';

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-2xl flex-col gap-4 p-4">{children}</main>
  );
}

/** /b/{slug}/account[/{contractId}]: the login form without a session, otherwise the bookings. */
export function AccountPage({ slug, contractId }: { slug: string; contractId: string | null }) {
  const [session, setSession] = useState(() => readSession(slug));
  const [expired, setExpired] = useState(false);
  const [current, setCurrent] = useState<string | null>(contractId);

  useEffect(() => {
    const onPop = () => {
      const m = new RegExp(`^/b/${slug}/account/([A-Za-z0-9-]{8,64})/?$`).exec(
        window.location.pathname,
      );
      setCurrent(m?.[1] ?? null);
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [slug]);

  const open = useCallback(
    (id: string | null) => {
      window.history.pushState(null, '', accountPath(slug, id));
      setCurrent(id);
      window.scrollTo?.(0, 0);
    },
    [slug],
  );

  const onSessionLost = useCallback(() => {
    clearSession(slug);
    setSession(null);
    setExpired(true);
  }, [slug]);

  if (!session) {
    return (
      <Shell>
        <LoginForm slug={slug} expired={expired} />
      </Shell>
    );
  }
  return (
    <Shell>
      <AccountHeader slug={slug} session={session} onLoggedOut={() => setSession(null)} />
      {current ? (
        <BookingDetail
          slug={slug}
          session={session}
          contractId={current}
          onBack={() => open(null)}
          onSessionLost={onSessionLost}
        />
      ) : (
        <BookingList slug={slug} session={session} onOpen={open} onSessionLost={onSessionLost} />
      )}
    </Shell>
  );
}

function AccountHeader({
  slug,
  session,
  onLoggedOut,
}: {
  slug: string;
  session: string;
  onLoggedOut: () => void;
}) {
  const { t } = useT();
  const close = useCloseAccountSession(slug);
  const logout = () => {
    const done = () => {
      clearSession(slug);
      onLoggedOut();
    };
    close.mutate(session, { onSettled: done });
  };
  return (
    <header className="flex items-center justify-between gap-2">
      <h1 className="flex items-center gap-2 text-xl font-semibold">
        <UserRoundIcon className="size-5" aria-hidden />
        {t('account:title')}
      </h1>
      <Button
        variant="ghost"
        size="sm"
        onClick={logout}
        disabled={close.isPending}
        data-testid="account-logout"
      >
        <LogOutIcon />
        {t('account:logout')}
      </Button>
    </header>
  );
}

const loginSchema = z.object({
  email: z.string().trim().email('common:validation.email').max(254),
});
type LoginValues = z.infer<typeof loginSchema>;

/** Email form. The answer never says whether the address is known (no enumeration). */
export function LoginForm({ slug, expired = false }: { slug: string; expired?: boolean }) {
  const { t, i18n } = useT();
  const request = useRequestAccountLink(slug);
  const [sent, setSent] = useState(false);
  const form = useForm<LoginValues>({
    resolver: zodFormResolver(loginSchema),
    defaultValues: { email: '' },
  });
  const submit = form.handleSubmit((v) => {
    const locale = i18n.language.startsWith('en') ? 'en' : 'it';
    request.mutate({ email: v.email, locale }, { onSuccess: () => setSent(true) });
  });
  if (sent) {
    return (
      <Card data-testid="account-link-sent">
        <CardContent className="flex flex-col items-center gap-4 pt-6 text-center">
          <MailCheckIcon className="text-primary size-10" aria-hidden />
          <p role="status">{t('account:login.sent')}</p>
          <Button variant="outline" onClick={() => setSent(false)}>
            {t('account:login.again')}
          </Button>
        </CardContent>
      </Card>
    );
  }
  const emailError = form.formState.errors.email?.message;
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h1 className="text-xl font-semibold">{t('account:login.title')}</h1>
        </CardTitle>
        <CardDescription>{t('account:login.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          {expired ? (
            <Alert>
              <AlertDescription>{t('account:expired')}</AlertDescription>
            </Alert>
          ) : null}
          <FieldGroup>
            <Field data-invalid={Boolean(emailError)}>
              <FieldLabel htmlFor="account-email">{t('account:login.email')}</FieldLabel>
              <Input
                id="account-email"
                type="email"
                inputMode="email"
                autoComplete="email"
                aria-invalid={Boolean(emailError)}
                {...form.register('email')}
              />
              {emailError ? <FieldError>{t(emailError)}</FieldError> : null}
            </Field>
          </FieldGroup>
          <ProblemAlert error={request.error} />
          <Button type="submit" disabled={request.isPending} data-testid="account-login-submit">
            {request.isPending ? <Spinner /> : null}
            {t('account:login.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

/** /b/{slug}/a#t=<token>: exchanges the login token for a session, then shows the bookings list. */
export function AccountLinkPage({ slug }: { slug: string }) {
  const { t } = useT();
  const openSession = useOpenAccountSession(slug);
  const [token] = useState(() => fragmentToken(window.location.hash));
  const [state, setState] = useState<'opening' | 'ready' | 'invalid' | 'error'>(() =>
    token ? 'opening' : readSession(slug) ? 'ready' : 'invalid',
  );
  const [error, setError] = useState<unknown>(null);
  const started = useRef(false);

  useEffect(() => {
    wipeFragment();
    if (!token || started.current) return;
    started.current = true;
    openSession.mutate(token, {
      onSuccess: (s) => {
        writeSession(slug, { token: s.session_token, expiresAt: s.expires_at });
        window.history.replaceState(null, '', accountPath(slug));
        setState('ready');
      },
      onError: (e) => {
        setError(e);
        setState(isProblem(e) && e.status === 404 ? 'invalid' : 'error');
      },
    });
  }, [slug, token, openSession]);

  if (state === 'ready') return <AccountPage slug={slug} contractId={null} />;
  return (
    <Shell>
      {state === 'opening' ? (
        <div className="flex items-center justify-center gap-2 py-16" role="status">
          <Spinner />
          {t('account:link.opening')}
        </div>
      ) : state === 'invalid' ? (
        <EmptyState
          icon={<LinkIcon />}
          title={t('account:link.invalidTitle')}
          description={t('account:link.invalidDescription')}
          action={
            <Button onClick={() => window.location.assign(accountPath(slug))}>
              {t('account:link.requestNew')}
            </Button>
          }
        />
      ) : (
        <ProblemAlert error={error} />
      )}
    </Shell>
  );
}
