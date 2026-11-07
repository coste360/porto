import { useNavigate, useRouterState } from '@tanstack/react-router';
import { LinkIcon } from 'lucide-react';
import { useEffect, useState, type FormEvent } from 'react';

import {
  ProblemError,
  isProblem,
  usePairDeviceSession,
  useVerifyDeviceSessionTotp,
  type PairSessionResult,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns';

import { AuthCard } from '../components/AuthCard';
import { detectDevice, normalizeCode, parsePairFragment } from '../features/desktop/pair';
import { landingFor } from '../lib/landing';

const CODE_RE = /^[A-Z0-9]{4}-[A-Z0-9]{4}$/;

/**
 * Browser pairing by QR (m6a-plan §4.1), public and phone-first: reads `#c=<code>&fp=<fingerprint>`, removes the
 * fragment from the address bar at once, and exchanges the code for a device session (`pairDeviceSession`), then
 * lands on the dock panel or the marina overview (`landingFor`).
 */
export function PairPage() {
  const { t } = useT();
  const navigate = useNavigate();
  const hash = useRouterState({ select: (s) => s.location.hash });
  // Read once: the fragment is cleared right after the first render.
  const [initial] = useState(() => parsePairFragment(hash));
  const [device] = useState(() =>
    detectDevice(typeof navigator !== 'undefined' ? navigator.userAgent : ''),
  );
  const [code, setCode] = useState(initial.code);
  const [name, setName] = useState(
    () => `${t('desktop:pair.defaultName')} ${t(`desktop:pair.platforms.${device.platform}`)}`,
  );
  const [touched, setTouched] = useState(false);
  const pair = usePairDeviceSession();
  // Set when the user has a second factor: the session works only after the code is verified on this device.
  const [pending, setPending] = useState<PairSessionResult | null>(null);

  useEffect(() => {
    if (hash) void navigate({ to: '/pair', hash: '', replace: true });
  }, [hash, navigate]);

  const codeValid = CODE_RE.test(code);
  const nameValid = name.trim().length > 0;

  const land = (result: PairSessionResult) => {
    const to = landingFor(result.permissions, result.device.kind, result.marina.id);
    void navigate({ href: `/admin${to}`, replace: true });
  };

  if (pending) return <PairTotpStep onVerified={() => land(pending)} />;

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (!codeValid || !nameValid) return;
    pair.mutate(
      { code, name: name.trim(), platform: device.platform, kind: device.kind },
      {
        onSuccess: (result: PairSessionResult) => {
          if (result.totp_required) {
            setPending(result);
            return;
          }
          land(result);
        },
      },
    );
  };

  return (
    <AuthCard title={t('desktop:pair.title')} description={t('desktop:pair.description')}>
      <form className="flex flex-col gap-4" data-testid="pair-page" onSubmit={onSubmit} noValidate>
        <FieldGroup>
          <PairError error={pair.error} />
          {initial.code ? null : (
            <Field data-invalid={touched && !codeValid ? true : undefined}>
              <FieldLabel htmlFor="pair-code">{t('desktop:pair.code')}</FieldLabel>
              <Input
                id="pair-code"
                value={code}
                autoComplete="one-time-code"
                autoCapitalize="characters"
                inputMode="text"
                placeholder={t('desktop:pair.codePlaceholder')}
                className="font-mono tracking-widest"
                aria-invalid={touched && !codeValid}
                onChange={(e) => setCode(normalizeCode(e.target.value))}
              />
              {touched && !codeValid ? (
                <p className="text-destructive text-sm">{t('desktop:pair.codeInvalid')}</p>
              ) : null}
            </Field>
          )}
          <Field data-invalid={touched && !nameValid ? true : undefined}>
            <FieldLabel htmlFor="pair-name">{t('desktop:pair.deviceName')}</FieldLabel>
            <Input
              id="pair-name"
              value={name}
              maxLength={100}
              aria-invalid={touched && !nameValid}
              onChange={(e) => setName(e.target.value)}
            />
            {touched && !nameValid ? (
              <p className="text-destructive text-sm">{t('common:validation.required')}</p>
            ) : null}
          </Field>
          {initial.fingerprint ? (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground text-xs">{t('desktop:pair.fingerprint')}</span>
              <code className="font-mono text-xs break-all" data-testid="pair-fingerprint">
                {initial.fingerprint}
              </code>
              <span className="text-muted-foreground text-xs">
                {t('desktop:pair.fingerprintHint')}
              </span>
            </div>
          ) : null}
          <Button type="submit" size="lg" className="min-h-11" disabled={pair.isPending}>
            <LinkIcon />
            {t('desktop:pair.connect')}
          </Button>
        </FieldGroup>
      </form>
    </AuthCard>
  );
}

const TOTP_RE = /^\d{6}$/;

/** Second factor of a device session (`verifyDeviceSessionTotp`): an authenticator code or a recovery code. */
function PairTotpStep({ onVerified }: { onVerified: () => void }) {
  const { t } = useT();
  const verify = useVerifyDeviceSessionTotp();
  const [recovery, setRecovery] = useState(false);
  const [value, setValue] = useState('');
  const [touched, setTouched] = useState(false);
  const valid = recovery ? value.trim().length > 0 : TOTP_RE.test(value);

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (!valid) return;
    const body = recovery ? { recovery_code: value.trim() } : { totp_code: value };
    verify.mutate(body, { onSuccess: onVerified });
  };

  return (
    <AuthCard title={t('desktop:pair.totpTitle')} description={t('desktop:pair.totpHint')}>
      <form className="flex flex-col gap-4" data-testid="pair-totp" onSubmit={onSubmit} noValidate>
        <FieldGroup>
          {verify.error ? <ProblemAlert error={verify.error} /> : null}
          <Field data-invalid={touched && !valid ? true : undefined}>
            <FieldLabel htmlFor="pair-totp-code">{t('desktop:pair.totpCode')}</FieldLabel>
            <Input
              id="pair-totp-code"
              value={value}
              autoComplete="one-time-code"
              inputMode={recovery ? 'text' : 'numeric'}
              className="font-mono tracking-widest"
              aria-invalid={touched && !valid}
              onChange={(e) =>
                setValue(recovery ? e.target.value : e.target.value.replace(/\D/g, '').slice(0, 6))
              }
            />
            {touched && !valid ? (
              <p className="text-destructive text-sm">{t('common:validation.required')}</p>
            ) : null}
          </Field>
          <Button type="submit" size="lg" className="min-h-11" disabled={verify.isPending}>
            {t('desktop:pair.verify')}
          </Button>
          <Button
            type="button"
            variant="link"
            onClick={() => {
              setRecovery((r) => !r);
              setValue('');
              setTouched(false);
            }}
          >
            {recovery ? t('desktop:pair.useTotp') : t('desktop:pair.useRecovery')}
          </Button>
        </FieldGroup>
      </form>
    </AuthCard>
  );
}

function PairError({ error }: { error: unknown }) {
  const { t } = useT();
  if (!error) return null;
  if (isProblem(error, 'pairing_code_invalid')) {
    return (
      <Alert variant="destructive" data-testid="pair-invalid">
        <AlertDescription>{t('desktop:pair.invalid')}</AlertDescription>
      </Alert>
    );
  }
  if (error instanceof ProblemError && error.status === 0) {
    return (
      <Alert variant="destructive" data-testid="pair-network">
        <AlertDescription>{t('desktop:pair.network')}</AlertDescription>
      </Alert>
    );
  }
  return <ProblemAlert error={error} />;
}
