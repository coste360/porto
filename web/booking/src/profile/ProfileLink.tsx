import { useQuery } from '@tanstack/react-query';
import { MapPinnedIcon } from 'lucide-react';

import { queryKeys, unwrap, useApi } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';

import { profileLink as enLink } from '../../../shared/src/i18n/locales/en/publicProfile.json';
import { profileLink as itLink } from '../../../shared/src/i18n/locales/it/publicProfile.json';

/**
 * "Scheda del porto" link of the marina page (/b/{slug}) to /p/{slug} when the profile is published
 * (getPublicMarina profile_published). It shares the query of the marina page; only the link text is imported, so
 * the profile chunk stays out of the booking entry.
 */
export function ProfileLink({ slug }: { slug: string | null }) {
  const { t, i18n } = useT();
  const api = useApi();
  const query = useQuery({
    queryKey: queryKeys.publicMarina(slug ?? ''),
    queryFn: () =>
      unwrap(api.GET('/api/public/v1/{slug}', { params: { path: { slug: slug ?? '' } } })),
    enabled: slug !== null,
    retry: false,
  });
  if (!slug || query.data?.profile_published !== true) return null;
  if (!i18n.exists('publicProfile:profileLink')) {
    i18n.addResource('it', 'publicProfile', 'profileLink', itLink);
    i18n.addResource('en', 'publicProfile', 'profileLink', enLink);
  }
  const lang = i18n.language === 'en' ? '?lang=en' : '';
  return (
    <nav
      className="mx-auto w-full max-w-2xl px-4 pb-6 sm:px-6"
      aria-label={t('publicProfile:profileLink')}
    >
      <a
        href={`/p/${slug}${lang}`}
        className="text-primary inline-flex items-center gap-2 text-sm font-medium underline-offset-4 hover:underline"
        data-testid="profile-link"
      >
        <MapPinnedIcon className="size-4" />
        {t('publicProfile:profileLink')}
      </a>
    </nav>
  );
}
