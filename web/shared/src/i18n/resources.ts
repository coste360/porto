import enAudit from './locales/en/audit.json';
import enAuth from './locales/en/auth.json';
import enBooking from './locales/en/booking.json';
import enCommon from './locales/en/common.json';
import enCustomers from './locales/en/customers.json';
import enDeadlines from './locales/en/deadlines.json';
import enDevices from './locales/en/devices.json';
import enDocuments from './locales/en/documents.json';
import enEditor from './locales/en/editor.json';
import enErrors from './locales/en/errors.json';
import enImports from './locales/en/imports.json';
import enKit from './locales/en/kit.json';
import enMaps from './locales/en/maps.json';
import enMarina from './locales/en/marina.json';
import enPricing from './locales/en/pricing.json';
import enPricingEnums from './locales/en/pricingEnums.json';
import enPricingTools from './locales/en/pricingTools.json';
import enTeam from './locales/en/team.json';
import enVessels from './locales/en/vessels.json';
import enWebhooks from './locales/en/webhooks.json';
import itAudit from './locales/it/audit.json';
import itAuth from './locales/it/auth.json';
import itBooking from './locales/it/booking.json';
import itCommon from './locales/it/common.json';
import itCustomers from './locales/it/customers.json';
import itDeadlines from './locales/it/deadlines.json';
import itDevices from './locales/it/devices.json';
import itDocuments from './locales/it/documents.json';
import itEditor from './locales/it/editor.json';
import itErrors from './locales/it/errors.json';
import itImports from './locales/it/imports.json';
import itKit from './locales/it/kit.json';
import itMaps from './locales/it/maps.json';
import itMarina from './locales/it/marina.json';
import itPricing from './locales/it/pricing.json';
import itPricingEnums from './locales/it/pricingEnums.json';
import itPricingTools from './locales/it/pricingTools.json';
import itTeam from './locales/it/team.json';
import itVessels from './locales/it/vessels.json';
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
  'maps',
  'editor',
  'kit',
  'customers',
  'vessels',
  'documents',
  'deadlines',
  'imports',
  'pricingEnums',
  'pricing',
  'pricingTools',
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
    maps: itMaps,
    editor: itEditor,
    kit: itKit,
    customers: itCustomers,
    vessels: itVessels,
    documents: itDocuments,
    deadlines: itDeadlines,
    imports: itImports,
    pricingEnums: itPricingEnums,
    pricing: itPricing,
    pricingTools: itPricingTools,
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
    maps: enMaps,
    editor: enEditor,
    kit: enKit,
    customers: enCustomers,
    vessels: enVessels,
    documents: enDocuments,
    deadlines: enDeadlines,
    imports: enImports,
    pricingEnums: enPricingEnums,
    pricing: enPricing,
    pricingTools: enPricingTools,
  },
} as const;
