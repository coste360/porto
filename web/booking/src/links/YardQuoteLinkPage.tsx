import { useState } from 'react';

import { useAcceptPublicYardQuote, type PublicYardQuote } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate, formatQtyMilli } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';

import { Done, Money, PdfViewer, type LinkRef } from './common';

/** A yard quote from its link (m10-plan §4.3, §14.1): the lines and totals, the conditions and the acceptance. */
export function YardQuoteLinkPage({
  linkRef,
  quote,
}: {
  linkRef: LinkRef;
  quote: PublicYardQuote;
}) {
  const { t, i18n } = useT();
  const accept = useAcceptPublicYardQuote(linkRef);
  const [agreed, setAgreed] = useState(false);
  const done = accept.data;
  if (done)
    return (
      <Done
        title={t('yardQuoteLink:accepted')}
        {...(done.work_order_number
          ? { description: t('yardQuoteLink:acceptedOrder', { number: done.work_order_number }) }
          : {})}
      />
    );
  return (
    <div className="flex flex-col gap-4" data-testid="yard-quote-link">
      <Card>
        <CardHeader>
          <CardTitle>{t('yardQuoteLink:number', { number: quote.number })}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-1 text-sm">
          <p className="font-medium">{quote.vessel_name}</p>
          {quote.work_order_number ? (
            <p>{t('yardQuoteLink:variationOf', { number: quote.work_order_number })}</p>
          ) : null}
          <p>{t('yardQuoteLink:validUntil', { date: formatIsoDate(quote.valid_until) })}</p>
          {quote.planned_start_on && quote.planned_delivery_on ? (
            <p>
              {t('yardQuoteLink:planned', {
                start: formatIsoDate(quote.planned_start_on),
                end: formatIsoDate(quote.planned_delivery_on),
              })}
            </p>
          ) : null}
          <p className="text-muted-foreground">{t(`yardQuoteLink:status.${quote.status}`)}</p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('yardQuoteLink:lines')}</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="flex flex-col gap-2 text-sm">
            {quote.lines.map((l, i) => (
              <li key={i} className="flex justify-between gap-2" data-testid="yard-quote-link-line">
                <span>{`${l.description} · ${formatQtyMilli(l.qty_milli, i18n.language)}`}</span>
                <Money cents={l.net_cents} />
              </li>
            ))}
          </ul>
          <dl className="mt-3 grid grid-cols-2 gap-1 text-sm">
            <dt>{t('yardQuoteLink:net')}</dt>
            <dd className="text-right">
              <Money cents={quote.net_cents} />
            </dd>
            <dt>{t('yardQuoteLink:vat')}</dt>
            <dd className="text-right">
              <Money cents={quote.vat_cents} />
            </dd>
            <dt className="font-semibold">{t('yardQuoteLink:gross')}</dt>
            <dd className="text-right font-semibold" data-testid="yard-quote-link-gross">
              <Money cents={quote.gross_cents} />
            </dd>
            {quote.deposit_amount_cents > 0 ? (
              <>
                <dt>{t('yardQuoteLink:deposit')}</dt>
                <dd className="text-right">
                  <Money cents={quote.deposit_amount_cents} />
                </dd>
              </>
            ) : null}
          </dl>
        </CardContent>
      </Card>
      <PdfViewer linkRef={linkRef} title={t('yardQuoteLink:pdf')} />
      {quote.conditions ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('yardQuoteLink:conditions')}</CardTitle>
          </CardHeader>
          <CardContent className="text-sm whitespace-pre-line">{quote.conditions}</CardContent>
        </Card>
      ) : null}
      <ProblemAlert error={accept.error} />
      {quote.can_accept ? (
        <div className="flex flex-col gap-3">
          {quote.conditions ? (
            <Field orientation="horizontal">
              <Checkbox
                id="yq-agree"
                checked={agreed}
                onCheckedChange={(c) => setAgreed(c === true)}
              />
              <FieldLabel htmlFor="yq-agree">{t('yardQuoteLink:acceptConditions')}</FieldLabel>
            </Field>
          ) : null}
          <Button
            size="lg"
            disabled={accept.isPending || (!!quote.conditions && !agreed)}
            onClick={() => accept.mutate(agreed)}
          >
            {t('yardQuoteLink:accept')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
