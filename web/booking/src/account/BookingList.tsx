import { CalendarDaysIcon, ChevronRightIcon, SailboatIcon } from 'lucide-react';
import { useEffect } from 'react';

import { isProblem, useAccountBookings, type AccountBookingSummary } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { localized } from '../common/i18nText';
import { Money } from '../common/Money';
import { StatusBadge } from './status';

/** The customer's bookings, newest first (any channel). */
export function BookingList({
  slug,
  session,
  onOpen,
  onSessionLost,
}: {
  slug: string;
  session: string;
  onOpen: (contractId: string) => void;
  onSessionLost: () => void;
}) {
  const { t } = useT();
  const q = useAccountBookings(slug, session);
  const lost = q.isError && isProblem(q.error) && q.error.status === 404;
  useEffect(() => {
    if (lost) onSessionLost();
  }, [lost, onSessionLost]);

  if (q.isPending) return <Skeleton className="h-40 w-full" />;
  if (q.isError) return <ProblemAlert error={q.error} />;
  return (
    <section className="flex flex-col gap-3" aria-labelledby="account-list-title">
      <h2 id="account-list-title" className="text-lg font-semibold">
        {t('account:list.title')}
      </h2>
      {q.data.items.length === 0 ? (
        <EmptyState icon={<SailboatIcon />} title={t('account:list.empty')} />
      ) : (
        <ul className="flex flex-col gap-3" data-testid="account-bookings">
          {q.data.items.map((b) => (
            <li key={b.contract_id}>
              <BookingRow booking={b} onOpen={onOpen} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function BookingRow({
  booking: b,
  onOpen,
}: {
  booking: AccountBookingSummary;
  onOpen: (id: string) => void;
}) {
  const { t, i18n } = useT();
  return (
    <Card>
      <CardContent className="flex flex-col gap-3 pt-6 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{b.reference ?? b.code}</span>
            <StatusBadge status={b.status} />
          </div>
          <p className="text-muted-foreground flex items-center gap-1 text-sm">
            <CalendarDaysIcon className="size-4" aria-hidden />
            {t('account:dates', {
              arrival: formatIsoDate(b.arrival),
              departure: formatIsoDate(b.departure),
            })}
          </p>
          <p className="truncate text-sm">
            {b.vessel_name}
            {b.category ? ` · ${localized(b.category, i18n.language)}` : ''}
          </p>
          {b.residual_cents > 0 ? (
            <p className="text-sm">
              {t('account:fields.residual')}: <Money cents={b.residual_cents} />
            </p>
          ) : null}
        </div>
        <Button variant="outline" onClick={() => onOpen(b.contract_id)} data-testid="account-open">
          {t('account:list.open')}
          <ChevronRightIcon />
        </Button>
      </CardContent>
    </Card>
  );
}
