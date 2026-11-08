// Top-level return page of the provider checkout (m8-plan §10, §12.1): /b/{slug}/r/{bookingId}#st=…&rt=…. It
// polls the status with the read-only status token (or the booking token when this tab started the booking)
// until the booking is confirmed, received for approval or failed. The browser return never confirms anything
// (INV-09): only the provider webhook or the manual driver does. "Torna al sito" goes to return_to only when its
// origin is in the marina's allowed embed origins.
import { useQuery } from '@tanstack/react-query';
import { ArrowLeftIcon, SearchXIcon } from 'lucide-react';
import { useEffect, useState } from 'react';

import {
  ProblemError,
  queryKeys,
  unwrap,
  useApi,
  useBooking,
  useBookingConfig,
  useBookingStatus,
  type BookingRef,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { Confirmation } from '../common/Confirmation';
import { outcomeOf, type Outcome } from '../common/outcome';
import { allowedReturnTo } from '../common/embed';
import { parseReturnFragment, VERIFY_TIMEOUT_MS, type ReturnFragment } from './returnFragment';
import { loadState } from './state';

function useFragment(): ReturnFragment {
  // The status token stays in memory only: the fragment is wiped from the address bar once read.
  const [f] = useState(() => parseReturnFragment(window.location.hash));
  useEffect(() => {
    if (window.location.hash) {
      window.history.replaceState(
        window.history.state,
        '',
        window.location.pathname + window.location.search,
      );
    }
  }, []);
  return f;
}

export default function ReturnPage({ slug, bookingId }: { slug: string; bookingId: string }) {
  const { t } = useT();
  const api = useApi();
  const fragment = useFragment();
  const [stored] = useState(() => loadState(slug).booking);
  const bookingToken = stored && stored.id === bookingId ? stored.token : undefined;
  const statusToken = fragment.statusToken ?? undefined;
  const status = useBookingStatus(slug, bookingId, { bookingToken, statusToken });
  const ref: BookingRef | null = bookingToken ? { slug, bookingId, token: bookingToken } : null;
  const booking = useBooking(ref);
  const config = useBookingConfig(slug);
  const marina = useQuery({
    queryKey: queryKeys.publicMarina(slug),
    queryFn: () => unwrap(api.GET('/api/public/v1/{slug}', { params: { path: { slug } } })),
    retry: false,
  });
  const [startedAt] = useState(() => Date.now());
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(id);
  }, []);

  const returnTo = allowedReturnTo(fragment.returnTo, marina.data?.embed_origins);
  const home = `/b/${slug}`;
  const back = (
    <Button asChild variant="outline">
      <a href={returnTo ?? home} data-testid="return-back">
        <ArrowLeftIcon />
        {returnTo ? t('checkout:return.backToSite') : t('checkout:return.backToMarina')}
      </a>
    </Button>
  );

  let body;
  if (!bookingToken && !statusToken) {
    body = (
      <EmptyState
        icon={<SearchXIcon />}
        title={t('checkout:return.invalidTitle')}
        description={t('checkout:return.invalidDescription')}
        action={back}
      />
    );
  } else if (status.isPending) {
    body = <Skeleton className="h-48 w-full" aria-label={t('checkout:return.loading')} />;
  } else if (status.isError) {
    const gone = status.error instanceof ProblemError && status.error.status === 404;
    body = (
      <EmptyState
        icon={<SearchXIcon />}
        title={gone ? t('checkout:return.invalidTitle') : t('errors:title')}
        description={gone ? t('checkout:return.invalidDescription') : t('errors:internal_error')}
        action={back}
      />
    );
  } else {
    let outcome: Outcome = outcomeOf(status.data.status, status.data.verifying);
    if (outcome === 'failed' && fragment.cancelled) outcome = 'cancelled';
    if (outcome === 'verifying' && now - startedAt > VERIFY_TIMEOUT_MS) outcome = 'timeout';
    const stay = booking.data
      ? { arrival: booking.data.arrival, departure: booking.data.departure }
      : undefined;
    body = (
      <Confirmation
        outcome={outcome}
        reference={status.data.reference}
        stay={stay}
        marinaName={marina.data?.name}
        arrivalInfo={config.data?.arrival_info}
        actions={outcome === 'verifying' ? null : back}
      />
    );
  }

  return (
    <main
      className="mx-auto flex min-h-svh w-full max-w-2xl flex-col gap-6 p-4 sm:p-6"
      data-testid="return-page"
    >
      {marina.data ? (
        <h1 className="text-2xl font-semibold tracking-tight">{marina.data.name}</h1>
      ) : null}
      {body}
    </main>
  );
}
