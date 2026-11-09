import { CalendarCheckIcon, CompassIcon, EyeIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useMarinaProfilePreview,
  usePublicOccupancy,
  usePublicProfile,
  usePublicProfileMap,
  usePublicProfilePrices,
  type PublicProfile,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { BrandLogo } from '@porto/shared/ui/patterns/Brand';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { Skeleton } from '@porto/shared/ui/skeleton';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { registerProfileTexts } from './i18n';
import { hasHours, photoSrc, previewMarinaId } from './lib';
import {
  Contacts,
  Features,
  Hours,
  Occupancy,
  Photos,
  Prices,
  Section,
  Services,
} from './sections';
import { SimpleMap } from './SimpleMap';

type Lang = 'it' | 'en';

function Loading() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3" aria-busy="true" aria-label={t('publicProfile:loading')}>
      <Skeleton className="h-10 w-2/3" />
      <Skeleton className="aspect-video w-full" />
      <Skeleton className="h-32 w-full" />
    </div>
  );
}

function LanguageSwitch({
  locales,
  lang,
  onChange,
}: {
  locales: string[];
  lang: Lang;
  onChange: (l: Lang) => void;
}) {
  const { t } = useT();
  if (!locales.includes('en')) return null;
  return (
    <ToggleGroup
      type="single"
      value={lang}
      onValueChange={(v) => (v === 'it' || v === 'en' ? onChange(v) : undefined)}
      aria-label={t('publicProfile:language')}
      variant="outline"
      size="sm"
      data-testid="profile-lang"
    >
      <ToggleGroupItem value="it" lang="it">
        {t('publicProfile:languageIt')}
      </ToggleGroupItem>
      <ToggleGroupItem value="en" lang="en">
        {t('publicProfile:languageEn')}
      </ToggleGroupItem>
    </ToggleGroup>
  );
}

function Body({
  p,
  preview,
  lang,
  setLang,
}: {
  p: PublicProfile;
  preview: string | null;
  lang: Lang;
  setLang: (l: Lang) => void;
}) {
  const { t } = useT();
  const live = preview === null;
  const map = usePublicProfileMap(p.slug, live && p.sections.map);
  const prices = usePublicProfilePrices(p.slug, lang, live && p.sections.prices);
  const occupancy = usePublicOccupancy(p.slug, live && p.sections.occupancy);
  const src = photoSrc(preview);
  return (
    <>
      <header className="flex flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold" data-testid="profile-name">
            {p.name}
          </h1>
          <LanguageSwitch locales={p.locales} lang={lang} onChange={setLang} />
        </div>
        {p.description ? <p className="text-muted-foreground">{p.description}</p> : null}
        <Photos photos={p.photos ?? []} srcOf={src} />
        {p.booking_url ? (
          <div>
            <Button asChild size="lg">
              <a
                href={`${p.booking_url}${lang === 'en' ? '?lang=en' : ''}`}
                data-testid="profile-book"
              >
                <CalendarCheckIcon />
                {t('publicProfile:book')}
              </a>
            </Button>
          </div>
        ) : null}
      </header>
      <Section id="contacts" title={t('publicProfile:contacts')}>
        <Contacts p={p} />
      </Section>
      {hasHours(p) ? (
        <Section id="hours" title={t('publicProfile:hours')}>
          <Hours p={p} />
        </Section>
      ) : null}
      <Section id="features" title={t('publicProfile:features')}>
        <Features p={p} />
      </Section>
      <Services p={p} />
      {p.access_notes || (p.access_images ?? []).length > 0 || p.lights ? (
        <Section id="access" title={t('publicProfile:access')}>
          {p.access_notes ? <p className="whitespace-pre-line">{p.access_notes}</p> : null}
          <Photos photos={p.access_images ?? []} srcOf={src} />
          {p.lights ? (
            <>
              <h3 className="font-medium">{t('publicProfile:lights')}</h3>
              <p className="whitespace-pre-line">{p.lights}</p>
            </>
          ) : null}
        </Section>
      ) : null}
      {p.sections.map && map.data ? (
        <Section id="map" title={t('publicProfile:map')}>
          <SimpleMap map={map.data} />
          {map.data.poi.length > 0 ? (
            <p className="text-muted-foreground text-sm">{t('publicProfile:mapLegend')}</p>
          ) : null}
        </Section>
      ) : null}
      {p.sections.prices && prices.data ? (
        <Section id="prices" title={t('publicProfile:prices')}>
          <Prices m={prices.data} />
        </Section>
      ) : null}
      {p.sections.occupancy && occupancy.data ? (
        <Section id="occupancy" title={t('publicProfile:occupancy')}>
          <Occupancy o={occupancy.data} />
        </Section>
      ) : null}
    </>
  );
}

/**
 * Public marina profile /p/{slug} (m12-plan §12.3): hero with photos, contacts, hours, features and depths,
 * services, access, simplified map, transit prices and aggregate occupancy (refetched every 60 s), "Prenota" to
 * /b/{slug} when online booking is enabled, it/en switch. The head (title, canonical, JSON-LD) is injected by the
 * server. With ?preview=1&marina={id} it shows the draft through the panel session.
 */
export function ProfilePage({ slug }: { slug: string }) {
  const { t, i18n } = useT();
  useState(() => {
    registerProfileTexts(i18n);
    return true;
  });
  const preview = typeof window === 'undefined' ? null : previewMarinaId(window.location.search);
  const [lang, setLangState] = useState<Lang>(i18n.language === 'en' ? 'en' : 'it');
  const setLang = (l: Lang) => {
    setLangState(l);
    void i18n.changeLanguage(l);
    const url = new URL(window.location.href);
    if (l === 'en') url.searchParams.set('lang', 'en');
    else url.searchParams.delete('lang');
    window.history.replaceState(null, '', url);
  };
  const pub = usePublicProfile(slug, lang, preview === null);
  const draft = useMarinaProfilePreview(preview, lang);
  const query = preview === null ? pub : draft;
  const profile: PublicProfile | undefined = preview === null ? pub.data : draft.data?.profile;
  return (
    <div className="flex min-h-svh flex-col">
      <main
        className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6 p-4 sm:p-6"
        data-testid="public-profile"
        data-slug={slug}
      >
        {preview !== null ? (
          <Alert data-testid="profile-preview-banner">
            <EyeIcon />
            <AlertDescription>{t('publicProfile:preview')}</AlertDescription>
          </Alert>
        ) : null}
        {query.isPending ? (
          <Loading />
        ) : query.isError || !profile ? (
          <EmptyState icon={<CompassIcon />} title={t('publicProfile:notFound')} />
        ) : (
          <Body p={profile} preview={preview} lang={lang} setLang={setLang} />
        )}
      </main>
      <footer className="mx-auto flex w-full max-w-3xl justify-end p-4 sm:p-6">
        <BrandLogo variant="symbol" size="sm" />
      </footer>
    </div>
  );
}
