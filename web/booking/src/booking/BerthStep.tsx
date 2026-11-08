import { AnchorIcon, ChevronRightIcon, PhoneIcon, SearchXIcon } from 'lucide-react';

import type { BookingAvailability, BookingConfig } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { localized } from '../common/i18nText';
import { Money } from '../common/Money';
import { formatLength } from '../common/units';
import type { SearchInput } from './state';

/** Step 2 "Posto": one card per berth category with yes/no and a "from" price, or the empty state. */
export function BerthStep({
  config,
  search,
  availability,
  error,
  loading,
  notice,
  onSelect,
  onEditSearch,
}: {
  config: BookingConfig;
  search: SearchInput;
  availability: BookingAvailability | undefined;
  error: unknown;
  loading: boolean;
  notice: string | null;
  onSelect: (category: string) => void;
  onEditSearch: () => void;
}) {
  const { t, i18n } = useT();
  const summary = (
    <div className="bg-muted/50 flex flex-wrap items-center justify-between gap-2 rounded-lg p-3 text-sm">
      <span>
        {t('checkout:berth.summary', {
          arrival: formatIsoDate(search.arrival),
          departure: formatIsoDate(search.departure),
          loa: formatLength(search.loa_cm, i18n.language, config.show_feet),
        })}
      </span>
      <Button variant="link" size="sm" className="h-auto p-0" onClick={onEditSearch}>
        {t('checkout:berth.editSearch')}
      </Button>
    </div>
  );

  if (loading) {
    return (
      <div
        className="flex flex-col gap-3"
        aria-busy="true"
        aria-label={t('checkout:berth.searching')}
      >
        {summary}
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-20 w-full" />
      </div>
    );
  }
  if (error || !availability) {
    return (
      <div className="flex flex-col gap-3">
        {summary}
        <ProblemAlert error={error} />
      </div>
    );
  }

  const available = availability.categories.filter((c) => c.available);
  if (available.length === 0) {
    const reason = availability.reason ?? 'no_space';
    const phone = config.arrival_info?.phone;
    return (
      <div className="flex flex-col gap-3" data-testid="booking-empty">
        {summary}
        <EmptyState
          icon={<SearchXIcon />}
          title={t('checkout:berth.emptyTitle')}
          description={t(`checkout:berth.reason.${reason}`, {
            min: config.rules.min_nights,
            max: config.rules.max_nights,
          })}
          action={
            phone ? (
              <Button asChild variant="outline">
                <a href={`tel:${phone.replace(/[^\d+]/g, '')}`}>
                  <PhoneIcon />
                  {t('checkout:berth.contact', { phone })}
                </a>
              </Button>
            ) : undefined
          }
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {summary}
      {notice ? (
        <Alert>
          <AlertDescription>{notice}</AlertDescription>
        </Alert>
      ) : null}
      <ul className="flex flex-col gap-3" aria-label={t('checkout:berth.listLabel')}>
        {availability.categories.map((c) => (
          <li key={c.code}>
            <button
              type="button"
              disabled={!c.available}
              onClick={() => onSelect(c.code)}
              data-testid="booking-category"
              data-category={c.code}
              className="border-border bg-card hover:bg-accent focus-visible:ring-ring flex w-full items-center gap-3 rounded-lg border p-4 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
            >
              <AnchorIcon className="text-primary size-5 shrink-0" aria-hidden />
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="font-medium">{localized(c.name, i18n.language)}</span>
                <span className="text-muted-foreground text-sm">
                  {c.available ? (
                    c.from_cents !== undefined ? (
                      <>
                        {t('checkout:berth.from')}{' '}
                        <Money cents={c.from_cents} currency={config.currency} />
                      </>
                    ) : (
                      t('checkout:berth.available')
                    )
                  ) : (
                    t('checkout:berth.unavailable')
                  )}
                </span>
                {c.available && c.approval_required ? (
                  <Badge variant="secondary" className="w-fit">
                    {t('checkout:berth.onRequest')}
                  </Badge>
                ) : null}
              </span>
              {c.available ? <ChevronRightIcon className="size-5 shrink-0" aria-hidden /> : null}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
