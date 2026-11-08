import enAccount from './locales/en/account.json';
import enAudit from './locales/en/audit.json';
import enCheckout from './locales/en/checkout.json';
import enOnline from './locales/en/online.json';
import enStorage from './locales/en/storage.json';
import enMaintenance from './locales/en/maintenance.json';
import enUtilities from './locales/en/utilities.json';
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
import enDock from './locales/en/dock.json';
import enDesk from './locales/en/desk.json';
import enOpsMap from './locales/en/opsMap.json';
import enOccupancyEnums from './locales/en/occupancyEnums.json';
import itAccount from './locales/it/account.json';
import itAudit from './locales/it/audit.json';
import itCheckout from './locales/it/checkout.json';
import itOnline from './locales/it/online.json';
import itStorage from './locales/it/storage.json';
import itMaintenance from './locales/it/maintenance.json';
import itUtilities from './locales/it/utilities.json';
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
import itDock from './locales/it/dock.json';
import itDesk from './locales/it/desk.json';
import itOpsMap from './locales/it/opsMap.json';
import itOccupancyEnums from './locales/it/occupancyEnums.json';
import enContractsM4 from './locales/en/contractsM4.json';
import enRenewals from './locales/en/renewals.json';
import enCharter from './locales/en/charter.json';
import enRights from './locales/en/rights.json';
import enCommonCharges from './locales/en/commonCharges.json';
import enTemplates from './locales/en/templates.json';
import enSignature from './locales/en/signature.json';
import enCommunications from './locales/en/communications.json';
import enDocsEnums from './locales/en/docsEnums.json';
import enCustomerLinks from './locales/en/customerLinks.json';
import itContractsM4 from './locales/it/contractsM4.json';
import itRenewals from './locales/it/renewals.json';
import itCharter from './locales/it/charter.json';
import itRights from './locales/it/rights.json';
import itCommonCharges from './locales/it/commonCharges.json';
import itTemplates from './locales/it/templates.json';
import itSignature from './locales/it/signature.json';
import itCommunications from './locales/it/communications.json';
import itDocsEnums from './locales/it/docsEnums.json';
import itCustomerLinks from './locales/it/customerLinks.json';
import enBillingEnums from './locales/en/billingEnums.json';
import enCharges from './locales/en/charges.json';
import enPayments from './locales/en/payments.json';
import enCash from './locales/en/cash.json';
import enFiscal from './locales/en/fiscal.json';
import enReceivables from './locales/en/receivables.json';
import enReminders from './locales/en/reminders.json';
import enExports from './locales/en/exports.json';
import enBillingSettings from './locales/en/billingSettings.json';
import enPaymentProviders from './locales/en/paymentProviders.json';
import enCustomerBilling from './locales/en/customerBilling.json';
import enWorklist from './locales/en/worklist.json';
import enPayPublic from './locales/en/payPublic.json';
import itBillingEnums from './locales/it/billingEnums.json';
import itCharges from './locales/it/charges.json';
import itPayments from './locales/it/payments.json';
import itCash from './locales/it/cash.json';
import itFiscal from './locales/it/fiscal.json';
import itReceivables from './locales/it/receivables.json';
import itReminders from './locales/it/reminders.json';
import itExports from './locales/it/exports.json';
import itBillingSettings from './locales/it/billingSettings.json';
import itPaymentProviders from './locales/it/paymentProviders.json';
import itCustomerBilling from './locales/it/customerBilling.json';
import itWorklist from './locales/it/worklist.json';
import itPayPublic from './locales/it/payPublic.json';
import enDesktop from './locales/en/desktop.json';
import itDesktop from './locales/it/desktop.json';
import enOffline from './locales/en/offline.json';
import itOffline from './locales/it/offline.json';
import enPhone from './locales/en/phone.json';
import itPhone from './locales/it/phone.json';
import enReports from './locales/en/reports.json';
import itReports from './locales/it/reports.json';
import enDashboard from './locales/en/dashboard.json';
import itDashboard from './locales/it/dashboard.json';

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
  'occupancyEnums',
  'opsMap',
  'desk',
  'dock',
  'contractsM4',
  'renewals',
  'charter',
  'rights',
  'commonCharges',
  'templates',
  'signature',
  'communications',
  'docsEnums',
  'customerLinks',
  // M5 (m5-plan §9.3)
  'billingEnums',
  'charges',
  'payments',
  'cash',
  'fiscal',
  'receivables',
  'reminders',
  'exports',
  'billingSettings',
  'paymentProviders',
  'customerBilling',
  'worklist',
  'payPublic',
  // M6a (m6a-plan §7)
  'desktop',
  'offline',
  'phone',
  // M7 (m7-plan §10.2)
  'reports',
  'dashboard',
  // M8 (m8-plan §12)
  'checkout',
  'account',
  'online',
  // M9 (m9-plan §12)
  'storage',
  'maintenance',
  'utilities',
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
    occupancyEnums: itOccupancyEnums,
    opsMap: itOpsMap,
    desk: itDesk,
    dock: itDock,
    contractsM4: itContractsM4,
    renewals: itRenewals,
    charter: itCharter,
    rights: itRights,
    commonCharges: itCommonCharges,
    templates: itTemplates,
    signature: itSignature,
    communications: itCommunications,
    docsEnums: itDocsEnums,
    customerLinks: itCustomerLinks,
    billingEnums: itBillingEnums,
    charges: itCharges,
    payments: itPayments,
    cash: itCash,
    fiscal: itFiscal,
    receivables: itReceivables,
    reminders: itReminders,
    exports: itExports,
    billingSettings: itBillingSettings,
    paymentProviders: itPaymentProviders,
    customerBilling: itCustomerBilling,
    worklist: itWorklist,
    payPublic: itPayPublic,
    desktop: itDesktop,
    offline: itOffline,
    phone: itPhone,
    reports: itReports,
    dashboard: itDashboard,
    checkout: itCheckout,
    account: itAccount,
    online: itOnline,
    storage: itStorage,
    maintenance: itMaintenance,
    utilities: itUtilities,
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
    occupancyEnums: enOccupancyEnums,
    opsMap: enOpsMap,
    desk: enDesk,
    dock: enDock,
    contractsM4: enContractsM4,
    renewals: enRenewals,
    charter: enCharter,
    rights: enRights,
    commonCharges: enCommonCharges,
    templates: enTemplates,
    signature: enSignature,
    communications: enCommunications,
    docsEnums: enDocsEnums,
    customerLinks: enCustomerLinks,
    billingEnums: enBillingEnums,
    charges: enCharges,
    payments: enPayments,
    cash: enCash,
    fiscal: enFiscal,
    receivables: enReceivables,
    reminders: enReminders,
    exports: enExports,
    billingSettings: enBillingSettings,
    paymentProviders: enPaymentProviders,
    customerBilling: enCustomerBilling,
    worklist: enWorklist,
    payPublic: enPayPublic,
    desktop: enDesktop,
    offline: enOffline,
    phone: enPhone,
    reports: enReports,
    dashboard: enDashboard,
    checkout: enCheckout,
    account: enAccount,
    online: enOnline,
    storage: enStorage,
    maintenance: enMaintenance,
    utilities: enUtilities,
  },
} as const;
