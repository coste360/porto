import { useState } from 'react';

import {
  useAcceptPublicRenewal,
  useDeclinePublicRenewal,
  type PublicRenewal,
  type PublicRenewalResult,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldLabel, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import { Textarea } from '@porto/shared/ui/textarea';

import { DepositCard, Done, Money, PdfViewer, type LinkRef } from './common';
import { linkPath } from './quoteAccept';

const OPEN: readonly Schemas['RenewalProposalStatus'][] = ['proposed', 'sent'];

/** Renewal proposal (13.8): old and new price, plan choice, accept (→ signing) or decline. */
export function RenewalLinkPage({
  linkRef,
  renewal,
}: {
  linkRef: LinkRef;
  renewal: PublicRenewal;
}) {
  const { t } = useT();
  const accept = useAcceptPublicRenewal(linkRef);
  const decline = useDeclinePublicRenewal(linkRef);
  const [plan, setPlan] = useState<Schemas['PlanChoice']>('standard');
  const [declining, setDeclining] = useState(false);
  const [reason, setReason] = useState('');
  const [result, setResult] = useState<PublicRenewalResult | null>(null);
  const status = result?.status ?? renewal.status;
  const signature = result?.signature_link ?? renewal.signature_link;

  if (!OPEN.includes(status)) {
    return (
      <div className="flex flex-col gap-4">
        <Done title={t(`customerLinks:renewalPage.status.${status}`)} />
        {signature && status === 'accepted' ? (
          <Button asChild size="lg">
            <a href={linkPath(signature)}>{t('customerLinks:quoteAccept.sign')}</a>
          </Button>
        ) : null}
        {result?.deposit ? <DepositCard deposit={result.deposit} /> : null}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{renewal.vessel_name}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-1 text-sm">
          {renewal.resource_codes?.length ? <p>{renewal.resource_codes.join(', ')}</p> : null}
          <p>{`${formatIsoDate(renewal.new_starts_on)} – ${formatIsoDate(renewal.new_ends_on)}`}</p>
          <p>
            {t('customerLinks:renewalPage.oldPrice')} <Money cents={renewal.old_total_cents} />
          </p>
          <p className="text-lg font-semibold">
            {t('customerLinks:renewalPage.newPrice')} <Money cents={renewal.new_total_cents} />
          </p>
          <p className="text-muted-foreground">
            {t('customerLinks:renewalPage.deadline', { date: formatIsoDate(renewal.deadline_on) })}
          </p>
        </CardContent>
      </Card>
      <PdfViewer linkRef={linkRef} title={t('customerLinks:renewalPage.pdf')} />
      {renewal.early_payment_total_cents !== undefined ? (
        <FieldSet>
          <FieldLegend>{t('customerLinks:plan.title')}</FieldLegend>
          <RadioGroup
            value={plan}
            onValueChange={(v) => setPlan(v as Schemas['PlanChoice'])}
            aria-label={t('customerLinks:plan.title')}
          >
            <Field orientation="horizontal">
              <RadioGroupItem id="rplan-standard" value="standard" />
              <FieldLabel htmlFor="rplan-standard">
                {t('customerLinks:plan.standard')} <Money cents={renewal.new_total_cents} />
              </FieldLabel>
            </Field>
            <Field orientation="horizontal">
              <RadioGroupItem id="rplan-early" value="early_payment" />
              <FieldLabel htmlFor="rplan-early">
                {t('customerLinks:plan.early')} <Money cents={renewal.early_payment_total_cents} />
              </FieldLabel>
            </Field>
          </RadioGroup>
        </FieldSet>
      ) : null}
      <ProblemAlert error={accept.error ?? decline.error} />
      {declining ? (
        <div className="flex flex-col gap-2">
          <Field>
            <FieldLabel htmlFor="renewal-reason">
              {t('customerLinks:renewalPage.reason')}
            </FieldLabel>
            <Textarea
              id="renewal-reason"
              value={reason}
              maxLength={500}
              onChange={(e) => setReason(e.target.value)}
            />
          </Field>
          <Button
            size="lg"
            variant="destructive"
            disabled={decline.isPending}
            onClick={() => decline.mutate(reason.trim() || undefined, { onSuccess: setResult })}
          >
            {t('customerLinks:renewalPage.confirmDecline')}
          </Button>
          <Button size="lg" variant="ghost" onClick={() => setDeclining(false)}>
            {t('common:actions.cancel')}
          </Button>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <Button
            size="lg"
            disabled={accept.isPending}
            onClick={() =>
              accept.mutate(plan === 'standard' ? undefined : plan, { onSuccess: setResult })
            }
          >
            {t('customerLinks:renewalPage.accept')}
          </Button>
          <Button size="lg" variant="outline" onClick={() => setDeclining(true)}>
            {t('customerLinks:renewalPage.decline')}
          </Button>
        </div>
      )}
    </div>
  );
}
