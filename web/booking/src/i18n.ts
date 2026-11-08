// i18n of the booking SPA with only its namespaces (m4-plan §8.1). The shared initI18n bundles every namespace of
// the admin app, which breaks the 180 KB booking budget (spec 04); this instance has the same settings as
// initI18n and imports just common, errors, booking, customerLinks and docsEnums. The locale files are read from
// the shared workspace directly until @porto/shared exports a per-namespace entry (requested from CONTRACT).
import i18next, { type i18n as I18n } from 'i18next';
import { initReactI18next } from 'react-i18next';

import { SOURCE_LOCALE, SUPPORTED_LOCALES, type Locale } from '@porto/shared/i18n';

import enBooking from '../../shared/src/i18n/locales/en/booking.json';
import enCommon from '../../shared/src/i18n/locales/en/common.json';
import enCustomerLinks from '../../shared/src/i18n/locales/en/customerLinks.json';
import enDocsEnums from '../../shared/src/i18n/locales/en/docsEnums.json';
import enErrors from '../../shared/src/i18n/locales/en/errors.json';
import itBooking from '../../shared/src/i18n/locales/it/booking.json';
import itCommon from '../../shared/src/i18n/locales/it/common.json';
import itCustomerLinks from '../../shared/src/i18n/locales/it/customerLinks.json';
import itDocsEnums from '../../shared/src/i18n/locales/it/docsEnums.json';
import itErrors from '../../shared/src/i18n/locales/it/errors.json';
import enBillingEnums from '../../shared/src/i18n/locales/en/billingEnums.json';
import enPayPublic from '../../shared/src/i18n/locales/en/payPublic.json';
import itBillingEnums from '../../shared/src/i18n/locales/it/billingEnums.json';
import itPayPublic from '../../shared/src/i18n/locales/it/payPublic.json';
import enAccount from '../../shared/src/i18n/locales/en/account.json';
import enCheckout from '../../shared/src/i18n/locales/en/checkout.json';
import itAccount from '../../shared/src/i18n/locales/it/account.json';
import itCheckout from '../../shared/src/i18n/locales/it/checkout.json';
import enYardQuoteLink from '../../shared/src/i18n/locales/en/yardQuoteLink.json';
import itYardQuoteLink from '../../shared/src/i18n/locales/it/yardQuoteLink.json';

export const BOOKING_NAMESPACES = [
  'common',
  'errors',
  'booking',
  'customerLinks',
  'docsEnums',
  // M5 public pay page (m5-plan §9.3)
  'payPublic',
  'billingEnums',
  // M8 booking flow and customer area (m8-plan §12)
  'checkout',
  'account',
  // M10 yard quote link (m10-plan §14.1)
  'yardQuoteLink',
] as const;

const resources = {
  it: {
    common: itCommon,
    errors: itErrors,
    booking: itBooking,
    customerLinks: itCustomerLinks,
    docsEnums: itDocsEnums,
    payPublic: itPayPublic,
    billingEnums: itBillingEnums,
    checkout: itCheckout,
    account: itAccount,
    yardQuoteLink: itYardQuoteLink,
  },
  en: {
    common: enCommon,
    errors: enErrors,
    booking: enBooking,
    customerLinks: enCustomerLinks,
    docsEnums: enDocsEnums,
    payPublic: enPayPublic,
    billingEnums: enBillingEnums,
    checkout: enCheckout,
    account: enAccount,
    yardQuoteLink: enYardQuoteLink,
  },
};

export function initBookingI18n(lng: Locale): I18n {
  const instance = i18next.createInstance();
  void instance.use(initReactI18next).init({
    lng,
    fallbackLng: SOURCE_LOCALE,
    supportedLngs: [...SUPPORTED_LOCALES],
    ns: [...BOOKING_NAMESPACES],
    defaultNS: 'common',
    nsSeparator: ':',
    keySeparator: '.',
    resources,
    interpolation: { escapeValue: false },
    returnNull: false,
    initAsync: false,
  });
  if (typeof document !== 'undefined') {
    document.documentElement.lang = instance.language;
    instance.on('languageChanged', (l) => {
      document.documentElement.lang = l;
    });
  }
  return instance;
}
