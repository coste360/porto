import { useState } from 'react';

import {
  useDeclineSignature,
  useRequestSignatureOtp,
  useVerifySignature,
  type PublicSignature,
  type PublicSignatureResult,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@porto/shared/ui/input-otp';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Textarea } from '@porto/shared/ui/textarea';

import { DepositCard, Done, PdfViewer, type LinkRef } from './common';
import { OTP_LENGTH, codeComplete, signingView } from './signingState';

/**
 * Signing with an email code (spec 15.4, D13): read the document, confirm the identity data, request the code,
 * type it. Called "firma elettronica semplice" everywhere (D6 of spec 33 stays open).
 */
export function SigningPage({
  linkRef,
  signature,
}: {
  linkRef: LinkRef;
  signature: PublicSignature;
}) {
  const { t } = useT();
  const otp = useRequestSignatureOtp(linkRef);
  const verify = useVerifySignature(linkRef);
  const decline = useDeclineSignature(linkRef);
  const [confirmed, setConfirmed] = useState(false);
  const [codeSent, setCodeSent] = useState(false);
  const [code, setCode] = useState('');
  const [declining, setDeclining] = useState(false);
  const [reason, setReason] = useState('');
  const [result, setResult] = useState<PublicSignatureResult | null>(null);
  const lastError = verify.error ?? otp.error;
  const view = signingView({ link: signature, codeSent, lastResult: result, lastError });

  const requestCode = () =>
    otp.mutate(undefined, {
      onSuccess: (r) => {
        setResult(r);
        setCodeSent(true);
        setCode('');
        verify.reset();
      },
    });

  switch (view.kind) {
    case 'signed':
      return (
        <div className="flex flex-col gap-4">
          <Done
            title={t('customerLinks:signing.signed')}
            description={t('customerLinks:signing.signedHint')}
          />
          {view.deposit ? <DepositCard deposit={view.deposit} /> : null}
        </div>
      );
    case 'declined':
      return <Done title={t('customerLinks:signing.declined')} />;
    case 'closed':
      return (
        <Done
          title={t(`customerLinks:signing.closed.${view.status}`, {
            defaultValue: t('customerLinks:signing.closedOther'),
          })}
        />
      );
    case 'waiting':
      return (
        <Done
          title={t('customerLinks:signing.waiting')}
          description={t('customerLinks:signing.waitingHint')}
        />
      );
    default:
      break;
  }
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm">
        {t('customerLinks:signing.intro', { title: signature.document_title })}
      </p>
      <PdfViewer linkRef={linkRef} title={signature.document_title} />
      <Card>
        <CardHeader>
          <CardTitle>{t('customerLinks:signing.identity')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-2 text-sm">
          <p data-testid="signer-name">{signature.identity.name}</p>
          {signature.identity.fiscal_code ? (
            <p>{t('customerLinks:signing.fiscalCode', { code: signature.identity.fiscal_code })}</p>
          ) : null}
          <p className="text-muted-foreground">{t(`docsEnums:signerRole.${signature.role}`)}</p>
          <Field orientation="horizontal">
            <Checkbox
              id="sign-confirm"
              checked={confirmed}
              disabled={codeSent}
              onCheckedChange={(c) => setConfirmed(c === true)}
            />
            <FieldLabel htmlFor="sign-confirm">{t('customerLinks:signing.confirm')}</FieldLabel>
          </Field>
        </CardContent>
      </Card>
      <p className="text-muted-foreground text-xs">{t('customerLinks:signing.legal')}</p>
      {view.kind === 'confirm' ? (
        <>
          <ProblemAlert error={otp.error} />
          <Button size="lg" disabled={!confirmed || otp.isPending} onClick={requestCode}>
            {t('customerLinks:signing.requestCode')}
          </Button>
        </>
      ) : null}
      {view.kind === 'rateLimited' ? (
        <Done
          title={t('customerLinks:signing.rateLimited')}
          description={
            view.retryAfterSeconds
              ? t('customerLinks:signing.retryIn', {
                  minutes: Math.ceil(view.retryAfterSeconds / 60),
                })
              : t('customerLinks:signing.retryLater')
          }
        />
      ) : null}
      {view.kind === 'locked' ? (
        <div className="flex flex-col gap-2">
          <Done
            title={t('customerLinks:signing.locked')}
            description={t('customerLinks:signing.lockedHint')}
          />
          <Button size="lg" variant="outline" disabled={otp.isPending} onClick={requestCode}>
            {t('customerLinks:signing.newCode')}
          </Button>
        </div>
      ) : null}
      {view.kind === 'code' ? (
        <div className="flex flex-col gap-3">
          <Field data-invalid={view.invalid || undefined}>
            <FieldLabel htmlFor="sign-code">{t('customerLinks:signing.code')}</FieldLabel>
            <InputOTP
              id="sign-code"
              maxLength={OTP_LENGTH}
              inputMode="numeric"
              autoComplete="one-time-code"
              value={code}
              onChange={setCode}
            >
              <InputOTPGroup>
                {Array.from({ length: OTP_LENGTH }, (_, i) => (
                  <InputOTPSlot key={i} index={i} />
                ))}
              </InputOTPGroup>
            </InputOTP>
            <FieldDescription>{t('customerLinks:signing.codeHint')}</FieldDescription>
            {view.invalid ? <FieldError>{t('customerLinks:signing.invalid')}</FieldError> : null}
            {view.attemptsLeft !== null ? (
              <p className="text-sm" data-testid="attempts-left" aria-live="polite">
                {t('customerLinks:signing.attemptsLeft', { count: view.attemptsLeft })}
              </p>
            ) : null}
          </Field>
          <Button
            size="lg"
            disabled={!codeComplete(code) || verify.isPending}
            onClick={() => verify.mutate(code, { onSuccess: setResult })}
          >
            {t('customerLinks:signing.sign')}
          </Button>
          <Button size="lg" variant="ghost" disabled={otp.isPending} onClick={requestCode}>
            {t('customerLinks:signing.resend')}
          </Button>
        </div>
      ) : null}
      {declining ? (
        <div className="flex flex-col gap-2">
          <Field>
            <FieldLabel htmlFor="sign-reason">{t('customerLinks:signing.reason')}</FieldLabel>
            <Textarea
              id="sign-reason"
              value={reason}
              maxLength={500}
              onChange={(e) => setReason(e.target.value)}
            />
          </Field>
          <ProblemAlert error={decline.error} />
          <Button
            size="lg"
            variant="destructive"
            disabled={decline.isPending}
            onClick={() => decline.mutate(reason.trim() || undefined, { onSuccess: setResult })}
          >
            {t('customerLinks:signing.confirmDecline')}
          </Button>
        </div>
      ) : (
        <Button variant="link" onClick={() => setDeclining(true)}>
          {t('customerLinks:signing.decline')}
        </Button>
      )}
    </div>
  );
}
