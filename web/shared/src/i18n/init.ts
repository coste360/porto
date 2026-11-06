import i18next, { type i18n as I18n } from 'i18next';
import { initReactI18next } from 'react-i18next';

import {
  NAMESPACES,
  SOURCE_LOCALE,
  SUPPORTED_LOCALES,
  resources,
  type Locale,
  type Namespace,
} from './resources';

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

/** Picks it/en from an explicit value (e.g. `?lang=`), then the browser languages. */
export function detectLocale(explicit?: string | null, languages?: readonly string[]): Locale {
  const candidates = [
    explicit ?? undefined,
    ...(languages ?? (typeof navigator !== 'undefined' ? navigator.languages : [])),
  ];
  for (const candidate of candidates) {
    const base = candidate?.toLowerCase().split('-')[0];
    if (isLocale(base)) return base;
  }
  return SOURCE_LOCALE;
}

export type InitI18nOptions = { lng?: Locale; namespaces?: readonly Namespace[] };

/** Creates and initialises an i18next instance (synchronous: resources are bundled). */
export function initI18n(options: InitI18nOptions = {}): I18n {
  const namespaces = options.namespaces ?? NAMESPACES;
  const instance = i18next.createInstance();
  const pick = (locale: Locale) =>
    Object.fromEntries(namespaces.map((ns) => [ns, resources[locale][ns]]));
  void instance.use(initReactI18next).init({
    lng: options.lng ?? SOURCE_LOCALE,
    fallbackLng: SOURCE_LOCALE,
    supportedLngs: [...SUPPORTED_LOCALES],
    ns: [...namespaces],
    defaultNS: 'common',
    nsSeparator: ':',
    keySeparator: '.',
    resources: { it: pick('it'), en: pick('en') },
    interpolation: { escapeValue: false },
    returnNull: false,
    initAsync: false,
  });
  if (typeof document !== 'undefined') {
    document.documentElement.lang = instance.language;
    instance.on('languageChanged', (lng) => {
      document.documentElement.lang = lng;
    });
  }
  return instance;
}
