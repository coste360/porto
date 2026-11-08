import {
  CalendarPlusIcon,
  CircleCheckBigIcon,
  CircleXIcon,
  ExternalLinkIcon,
  HourglassIcon,
  MailCheckIcon,
  PhoneIcon,
  RadioIcon,
} from 'lucide-react';
import type { ReactNode } from 'react';

import type { BookingConfig } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Spinner } from '@porto/shared/ui/spinner';

import { localized } from './i18nText';
import { buildIcs, downloadIcs } from './ics';
import type { Outcome } from './outcome';

const icons: Record<Outcome, ReactNode> = {
  verifying: <HourglassIcon className="text-muted-foreground size-10" aria-hidden />,
  timeout: <HourglassIcon className="text-muted-foreground size-10" aria-hidden />,
  confirmed: <CircleCheckBigIcon className="text-primary size-10" aria-hidden />,
  received: <MailCheckIcon className="text-primary size-10" aria-hidden />,
  failed: <CircleXIcon className="text-destructive size-10" aria-hidden />,
  cancelled: <CircleXIcon className="text-destructive size-10" aria-hidden />,
};

/** Arrival instructions of the marina (VHF channel, berthing-master phone, rules, free text). */
export function ArrivalInfo({ info }: { info: BookingConfig['arrival_info'] }) {
  const { t, i18n } = useT();
  if (!info) return null;
  const text = localized(info.text, i18n.language);
  if (!info.vhf_channel && !info.phone && !info.rules_url && !text) return null;
  return (
    <div className="flex flex-col gap-2 text-sm" data-testid="arrival-info">
      <p className="font-medium">{t('checkout:arrival.title')}</p>
      {info.vhf_channel ? (
        <p className="flex items-center gap-2">
          <RadioIcon className="size-4" aria-hidden />
          {t('checkout:arrival.vhf', { channel: info.vhf_channel })}
        </p>
      ) : null}
      {info.phone ? (
        <p className="flex items-center gap-2">
          <PhoneIcon className="size-4" aria-hidden />
          <a className="underline" href={`tel:${info.phone.replace(/[^\d+]/g, '')}`}>
            {t('checkout:arrival.phone', { phone: info.phone })}
          </a>
        </p>
      ) : null}
      {text ? <p className="text-muted-foreground whitespace-pre-line">{text}</p> : null}
      {info.rules_url && /^https?:\/\//.test(info.rules_url) ? (
        <a
          className="flex items-center gap-1 underline"
          href={info.rules_url}
          target="_blank"
          rel="noopener noreferrer"
        >
          <ExternalLinkIcon className="size-4" aria-hidden />
          {t('checkout:arrival.rules')}
        </a>
      ) : null}
    </div>
  );
}

/**
 * Outcome card of a booking: "pagamento in verifica" (spinner), "Prenotazione confermata" (reference, arrival
 * info, .ics), "Richiesta ricevuta" (approval), or the failure. Never personal data: the reference and the dates.
 */
export function Confirmation({
  outcome,
  reference,
  stay,
  marinaName,
  arrivalInfo,
  actions,
}: {
  outcome: Outcome;
  reference: string;
  stay?: { arrival: string; departure: string } | undefined;
  marinaName?: string | undefined;
  arrivalInfo?: BookingConfig['arrival_info'];
  actions?: ReactNode;
}) {
  const { t } = useT();
  return (
    <Card data-testid="booking-outcome" data-outcome={outcome}>
      <CardHeader className="flex flex-col items-center gap-3 text-center">
        {outcome === 'verifying' ? <Spinner className="size-10" /> : icons[outcome]}
        <CardTitle role="status" aria-live="polite" className="text-xl">
          {t(`checkout:outcome.${outcome}.title`)}
        </CardTitle>
        <CardDescription>{t(`checkout:outcome.${outcome}.description`)}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {reference ? (
          <p className="text-center text-sm">
            {t('checkout:outcome.reference')}{' '}
            <span
              className="font-mono text-base font-semibold tracking-widest"
              data-testid="booking-reference"
            >
              {reference}
            </span>
          </p>
        ) : null}
        {outcome === 'confirmed' && stay ? (
          <p className="text-muted-foreground text-center text-sm">
            {t('checkout:payment.stay', {
              arrival: formatIsoDate(stay.arrival),
              departure: formatIsoDate(stay.departure),
            })}
          </p>
        ) : null}
        {outcome === 'confirmed' || outcome === 'received' ? (
          <ArrivalInfo info={arrivalInfo} />
        ) : null}
        {outcome === 'confirmed' ? (
          <p className="text-muted-foreground text-sm">{t('checkout:outcome.accountHint')}</p>
        ) : null}
        <div className="flex flex-col gap-2 sm:flex-row sm:justify-center">
          {outcome === 'confirmed' && stay ? (
            <Button
              variant="outline"
              onClick={() =>
                downloadIcs(
                  `porto-${reference}.ics`,
                  buildIcs({
                    uid: reference,
                    arrival: stay.arrival,
                    departure: stay.departure,
                    summary: t('checkout:outcome.icsSummary', {
                      marina: marinaName ?? '',
                      reference,
                    }),
                    stamp: new Date(),
                  }),
                )
              }
            >
              <CalendarPlusIcon />
              {t('checkout:outcome.addToCalendar')}
            </Button>
          ) : null}
          {actions}
        </div>
      </CardContent>
    </Card>
  );
}
