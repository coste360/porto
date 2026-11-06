import enAudit from './locales/en/audit.json';
import enAuth from './locales/en/auth.json';
import enBooking from './locales/en/booking.json';
import enCommon from './locales/en/common.json';
import enDevices from './locales/en/devices.json';
import enErrors from './locales/en/errors.json';
import enMarina from './locales/en/marina.json';
import enTeam from './locales/en/team.json';
import enWebhooks from './locales/en/webhooks.json';
import itAudit from './locales/it/audit.json';
import itAuth from './locales/it/auth.json';
import itBooking from './locales/it/booking.json';
import itCommon from './locales/it/common.json';
import itDevices from './locales/it/devices.json';
import itErrors from './locales/it/errors.json';
import itMarina from './locales/it/marina.json';
import itTeam from './locales/it/team.json';
import itWebhooks from './locales/it/webhooks.json';

export const SUPPORTED_LOCALES = ['it', 'en'] as const;
export type Locale = (typeof SUPPORTED_LOCALES)[number];
/** Italian is the source language and the fallback. */
export const SOURCE_LOCALE: Locale = 'it';

export const NAMESPACES = [
  'common',
  'auth',
  'errors',
  'team',
  'marina',
  'devices',
  'webhooks',
  'audit',
  'booking',
] as const;
export type Namespace = (typeof NAMESPACES)[number];

export const resources = {
  it: {
    common: itCommon,
    auth: itAuth,
    errors: itErrors,
    team: itTeam,
    marina: itMarina,
    devices: itDevices,
    webhooks: itWebhooks,
    audit: itAudit,
    booking: itBooking,
  },
  en: {
    common: enCommon,
    auth: enAuth,
    errors: enErrors,
    team: enTeam,
    marina: enMarina,
    devices: enDevices,
    webhooks: enWebhooks,
    audit: enAudit,
    booking: enBooking,
  },
} as const;
