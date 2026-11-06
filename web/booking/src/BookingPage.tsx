import { useQuery } from '@tanstack/react-query';
import { CalendarClockIcon, CompassIcon } from 'lucide-react';

import { ProblemError, queryKeys, unwrap, useApi } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { BrandLogo } from '@porto/shared/ui/patterns/Brand';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { Skeleton } from '@porto/shared/ui/skeleton';

/** Public page of a marina. M0: name, "powered by Porto" and a coming-soon notice. */
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
    <div className="flex min-h-svh flex-col">
      <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-6 p-6">
        {notFound ? (
          <EmptyState
            icon={<CompassIcon />}
            title={t('booking:notFoundTitle')}
            description={t('booking:notFoundDescription')}
          />
        ) : query.isPending ? (
          <div className="flex flex-col gap-3" aria-busy="true" aria-label={t('booking:loading')}>
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-32 w-full" />
          </div>
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
  );
}
