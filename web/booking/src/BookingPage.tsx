import { useQuery } from '@tanstack/react-query';
import { CalendarClockIcon, CompassIcon } from 'lucide-react';
import { lazy, Suspense } from 'react';

import { ProblemError, queryKeys, unwrap, useApi, useBookingConfig } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { BrandLogo } from '@porto/shared/ui/patterns/Brand';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { EmbedProvider } from './common/EmbedContext';
import { useEmbed } from './common/useEmbed';

// The booking flow is its own chunk (m8-plan §12.1 budget): the entry carries only this page shell.
const BookingFlow = lazy(() => import('./booking'));

function Loading() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3" aria-busy="true" aria-label={t('booking:loading')}>
      <Skeleton className="h-10 w-2/3" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}

/**
 * Public page of a marina (/b/{slug}): the marina name, then the online booking flow when booking is enabled
 * (a coming-soon notice otherwise). In embed mode (?embed=1 from the widget) the header can be hidden and the
 * page reports its height to the verified parent page.
 */
export function BookingPage({ slug }: { slug: string | null }) {
  const { t } = useT();
  const api = useApi();
  const query = useQuery({
    queryKey: queryKeys.publicMarina(slug ?? ''),
    queryFn: () =>
      unwrap(api.GET('/api/public/v1/{slug}', { params: { path: { slug: slug ?? '' } } })),
    enabled: slug !== null,
    retry: (count, error) => !(error instanceof ProblemError && error.status === 404) && count < 2,
  });

  const notFound = slug === null || query.isError;

  return (
    <EmbedProvider allowed={query.data?.embed_origins}>
      <div className="flex min-h-svh flex-col">
        <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-6 p-4 sm:p-6">
          {notFound ? (
            <EmptyState
              icon={<CompassIcon />}
              title={t('booking:notFoundTitle')}
              description={t('booking:notFoundDescription')}
            />
          ) : query.isPending ? (
            <Loading />
          ) : query.data.booking_enabled ? (
            <EnabledPage slug={query.data.slug} name={query.data.name} />
          ) : (
            <>
              <h1 className="text-3xl font-semibold tracking-tight">{query.data.name}</h1>
              <EmptyState
                icon={<CalendarClockIcon />}
                title={t('booking:comingSoon')}
                description={t('booking:comingSoonDescription')}
              />
            </>
          )}
        </main>
        <footer className="text-muted-foreground flex items-center justify-center gap-1 p-4 text-xs">
          <BrandLogo variant="symbol" size="sm" />
          <span>{t('common:product.poweredBy')}</span>
        </footer>
      </div>
    </EmbedProvider>
  );
}

function EnabledPage({ slug, name }: { slug: string; name: string }) {
  const { embedded } = useEmbed();
  const config = useBookingConfig(slug);
  const hideHeader = embedded && config.data?.theme.hide_header_in_embed === true;
  return (
    <>
      {hideHeader ? (
        <h1 className="sr-only">{name}</h1>
      ) : (
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{name}</h1>
      )}
      <Suspense fallback={<Loading />}>
        <BookingFlow slug={slug} />
      </Suspense>
    </>
  );
}
