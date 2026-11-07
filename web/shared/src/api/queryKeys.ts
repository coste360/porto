/**
 * TanStack Query keys. Everything about one marina lives under ['marina', id] so that
 * SSE topics can invalidate a whole area at once (see sse.ts).
 */
export const queryKeys = {
  me: ['me'] as const,
  meta: ['meta'] as const,
  eventTypes: ['event-types'] as const,
  organizations: ['organizations'] as const,
  orgMarinas: (orgId: string) => ['organizations', orgId, 'marinas'] as const,
  orgOwners: (orgId: string) => ['organizations', orgId, 'owners'] as const,
  marina: (marinaId: string) => ['marina', marinaId, 'detail'] as const,
  team: (marinaId: string) => ['marina', marinaId, 'team'] as const,
  members: (marinaId: string) => ['marina', marinaId, 'team', 'members'] as const,
  apiKeys: (marinaId: string) => ['marina', marinaId, 'team', 'api-keys'] as const,
  devices: (marinaId: string) => ['marina', marinaId, 'team', 'devices'] as const,
  webhooks: (marinaId: string) => ['marina', marinaId, 'webhooks'] as const,
  webhookDeliveries: (marinaId: string, endpointId: string) =>
    ['marina', marinaId, 'webhooks', endpointId, 'deliveries'] as const,
  audit: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'audit', filters] as const,
  publicMarina: (slug: string) => ['public', slug] as const,

  // M6a desktop edition (loopback only, m6a-plan §7): not marina-scoped, never invalidated by SSE.
  desktop: {
    all: ['desktop'] as const,
    status: ['desktop', 'status'] as const,
    settings: ['desktop', 'settings'] as const,
    lan: ['desktop', 'lan'] as const,
    backups: ['desktop', 'backups'] as const,
  },

  // M1 harbour: everything under ['marina', id, 'maps'] (invalidated by map.changed).
  maps: (marinaId: string) => ['marina', marinaId, 'maps'] as const,
  map: (marinaId: string, mapId: string) => ['marina', marinaId, 'maps', 'detail', mapId] as const,
  berths: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'maps', 'berths', filters] as const,
  berthGroups: (marinaId: string) => ['marina', marinaId, 'maps', 'berth-groups'] as const,
  harborSettings: (marinaId: string) => ['marina', marinaId, 'maps', 'harbor-settings'] as const,

  // M1 registry: everything under ['marina', id, 'registry'] (invalidated by registry.changed).
  registry: (marinaId: string) => ['marina', marinaId, 'registry'] as const,
  customers: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'customers', filters] as const,
  customer: (marinaId: string, customerId: string) =>
    ['marina', marinaId, 'registry', 'customer', customerId] as const,
  vessels: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'vessels', filters] as const,
  vessel: (marinaId: string, vesselId: string) =>
    ['marina', marinaId, 'registry', 'vessel', vesselId] as const,
  documents: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'documents', filters] as const,
  deadlines: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'registry', 'deadlines', filters] as const,
  deadlinePolicies: (marinaId: string) =>
    ['marina', marinaId, 'registry', 'deadline-policies'] as const,

  // M2 pricing: everything under ['marina', id, 'pricing'] (invalidated by pricing.changed).
  pricing: (marinaId: string) => ['marina', marinaId, 'pricing'] as const,
  pricingSettings: (marinaId: string) => ['marina', marinaId, 'pricing', 'settings'] as const,
  revenueClasses: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'revenue-classes', filters] as const,
  items: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'items', filters] as const,
  item: (marinaId: string, itemId: string) =>
    ['marina', marinaId, 'pricing', 'item', itemId] as const,
  customerCategoryCounts: (marinaId: string) =>
    ['marina', marinaId, 'pricing', 'customer-categories'] as const,
  berthCategories: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'berth-categories', filters] as const,
  priceLists: (marinaId: string, filters: Record<string, string | undefined> = {}) =>
    ['marina', marinaId, 'pricing', 'price-lists', filters] as const,
  priceList: (marinaId: string, priceListId: string) =>
    ['marina', marinaId, 'pricing', 'price-list', priceListId] as const,
  priceMatrix: (
    marinaId: string,
    priceListId: string,
    params: Record<string, string | undefined> = {},
  ) => ['marina', marinaId, 'pricing', 'price-list', priceListId, 'matrix', params] as const,
  priceCoverage: (
    marinaId: string,
    priceListId: string,
    params: Record<string, string | undefined> = {},
  ) => ['marina', marinaId, 'pricing', 'price-list', priceListId, 'coverage', params] as const,

  // M3 operational map: everything under ['marina', id, 'ops'] (invalidated by occupancy.changed and
  // presence.changed, narrowed by map id). Omitted trailing arguments give prefix keys for invalidation.
  ops: (marinaId: string) => ['marina', marinaId, 'ops'] as const,
  dayView: (marinaId: string, mapId?: string, date?: string) =>
    trim(['marina', marinaId, 'ops', 'day', mapId, date]),
  mapOccupancy: (marinaId: string, mapId?: string, from?: string, to?: string) =>
    trim(['marina', marinaId, 'ops', 'occupancy', mapId, from, to]),
  allocation: (marinaId: string, allocationId?: string) =>
    trim(['marina', marinaId, 'ops', 'allocation', allocationId]),
  occupancySettings: (marinaId: string) => ['marina', marinaId, 'ops', 'settings'] as const,
  closures: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'ops', 'closures', filters]),

  // M3 desk: quotes, contracts, transits, inquiries, releases, movements and presences under
  // ['marina', id, 'desk'] (invalidated by contract.changed, occupancy.changed and presence.changed).
  desk: (marinaId: string) => ['marina', marinaId, 'desk'] as const,
  quotes: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'quotes', filters]),
  quote: (marinaId: string, quoteId?: string) =>
    trim(['marina', marinaId, 'desk', 'quote', quoteId]),
  contracts: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'contracts', filters]),
  contract: (marinaId: string, contractId?: string) =>
    trim(['marina', marinaId, 'desk', 'contract', contractId]),
  transits: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'transits', filters]),
  inquiries: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'inquiries', filters]),
  releases: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'releases', filters]),
  movements: (marinaId: string, date?: string, mapId?: string) =>
    trim(['marina', marinaId, 'desk', 'movements', date, mapId]),
  presences: (marinaId: string, date?: string) =>
    trim(['marina', marinaId, 'desk', 'presences', date]),

  // M4 contracts (m4-plan §8.2): instalments, renewals, charter, addenda, waiting list and contract settings under
  // the desk root (invalidated by contract.changed).
  installments: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'installments', filters]),
  contractInstallments: (marinaId: string, contractId?: string) =>
    trim(['marina', marinaId, 'desk', 'contract-installments', contractId]),
  addenda: (marinaId: string, contractId?: string) =>
    trim(['marina', marinaId, 'desk', 'addenda', contractId]),
  renewals: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'renewals', filters]),
  campaign: (marinaId: string, campaignId?: string) =>
    trim(['marina', marinaId, 'desk', 'campaign', campaignId]),
  campaignProposals: (
    marinaId: string,
    campaignId?: string,
    filters?: Record<string, string | undefined>,
  ) => trim(['marina', marinaId, 'desk', 'campaign-proposals', campaignId, filters]),
  proposal: (marinaId: string, proposalId?: string) =>
    trim(['marina', marinaId, 'desk', 'proposal', proposalId]),
  charters: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'charters', filters]),
  fleets: (marinaId: string) => ['marina', marinaId, 'desk', 'fleets'] as const,
  fleet: (marinaId: string, fleetId?: string) =>
    trim(['marina', marinaId, 'desk', 'fleet', fleetId]),
  waitlist: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'desk', 'waitlist', filters]),
  contractSettings: (marinaId: string) =>
    ['marina', marinaId, 'desk', 'contract-settings'] as const,

  // M4 rights: everything under ['marina', id, 'rights'] (invalidated by rights.changed).
  rights: (marinaId: string) => ['marina', marinaId, 'rights'] as const,
  rightsList: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'rights', 'list', filters]),
  right: (marinaId: string, rightId?: string) =>
    trim(['marina', marinaId, 'rights', 'right', rightId]),
  rightHistory: (marinaId: string, berthId?: string) =>
    trim(['marina', marinaId, 'rights', 'history', berthId]),
  mapRights: (marinaId: string, mapId?: string, date?: string) =>
    trim(['marina', marinaId, 'rights', 'map', mapId, date]),
  transfers: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'rights', 'transfers', filters]),
  transfer: (marinaId: string, transferId?: string) =>
    trim(['marina', marinaId, 'rights', 'transfer', transferId]),
  mandates: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'rights', 'mandates', filters]),
  mandate: (marinaId: string, mandateId?: string) =>
    trim(['marina', marinaId, 'rights', 'mandate', mandateId]),
  feeTables: (marinaId: string, year?: string) =>
    trim(['marina', marinaId, 'rights', 'fee-tables', year]),
  feeRuns: (marinaId: string, year?: string) =>
    trim(['marina', marinaId, 'rights', 'fee-runs', year]),
  feeRun: (marinaId: string, feeRunId?: string) =>
    trim(['marina', marinaId, 'rights', 'fee-run', feeRunId]),
  millesimalTables: (marinaId: string) =>
    ['marina', marinaId, 'rights', 'millesimal-tables'] as const,
  millesimalTable: (marinaId: string, tableId?: string) =>
    trim(['marina', marinaId, 'rights', 'millesimal-table', tableId]),
  budgets: (marinaId: string) => ['marina', marinaId, 'rights', 'budgets'] as const,
  budget: (marinaId: string, budgetId?: string) =>
    trim(['marina', marinaId, 'rights', 'budget', budgetId]),
  commonRuns: (marinaId: string, budgetId?: string) =>
    trim(['marina', marinaId, 'rights', 'common-runs', budgetId]),
  commonRun: (marinaId: string, runId?: string) =>
    trim(['marina', marinaId, 'rights', 'common-run', runId]),
  holderInstallments: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'rights', 'holder-installments', filters]),
  concession: (marinaId: string) => ['marina', marinaId, 'rights', 'concession'] as const,

  // M4 documents, signatures, communications and imports under ['marina', id, 'docs'] (invalidated by
  // documents.changed). The M1 registry documents keep queryKeys.documents.
  docsArea: (marinaId: string) => ['marina', marinaId, 'docs'] as const,
  templateKinds: (marinaId: string) => ['marina', marinaId, 'docs', 'template-kinds'] as const,
  templates: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'docs', 'templates', filters]),
  template: (marinaId: string, templateId?: string) =>
    trim(['marina', marinaId, 'docs', 'template', templateId]),
  templateVersions: (marinaId: string, templateId?: string) =>
    trim(['marina', marinaId, 'docs', 'template-versions', templateId]),
  generatedDocs: (marinaId: string, subjectType?: string, subjectId?: string) =>
    trim(['marina', marinaId, 'docs', 'generated', subjectType, subjectId]),
  generatedDoc: (marinaId: string, documentId?: string) =>
    trim(['marina', marinaId, 'docs', 'generated-doc', documentId]),
  signatures: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'docs', 'signatures', filters]),
  signature: (marinaId: string, requestId?: string) =>
    trim(['marina', marinaId, 'docs', 'signature', requestId]),
  signatureSettings: (marinaId: string) =>
    ['marina', marinaId, 'docs', 'signature-settings'] as const,
  communications: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'docs', 'communications', filters]),
  communication: (marinaId: string, campaignId?: string) =>
    trim(['marina', marinaId, 'docs', 'communication', campaignId]),
  communicationRecipients: (marinaId: string, campaignId?: string) =>
    trim(['marina', marinaId, 'docs', 'communication-recipients', campaignId]),
  imports: (marinaId: string) => ['marina', marinaId, 'docs', 'imports'] as const,
  importRun: (marinaId: string, runId?: string) =>
    trim(['marina', marinaId, 'docs', 'import', runId]),
  customerMessages: (marinaId: string, customerId?: string) =>
    trim(['marina', marinaId, 'docs', 'customer-messages', customerId]),

  // M4 customer magic-link pages (booking SPA): never under a marina id, the token is the only credential.
  publicLink: (slug: string, token: string) => ['public', slug, 'link', token] as const,

  // M5 billing: everything under ['marina', id, 'billing'] (invalidated by billing.changed, m5-plan §9.2).
  billing: (marinaId: string) => ['marina', marinaId, 'billing'] as const,
  charges: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'charges', filters]),
  charge: (marinaId: string, chargeId?: string) =>
    trim(['marina', marinaId, 'billing', 'charge', chargeId]),
  payments: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'payments', filters]),
  payment: (marinaId: string, paymentId?: string) =>
    trim(['marina', marinaId, 'billing', 'payment', paymentId]),
  refunds: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'refunds', filters]),
  credits: (marinaId: string) => ['marina', marinaId, 'billing', 'credits'] as const,
  receivables: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'receivables', filters]),
  ageing: (marinaId: string) => ['marina', marinaId, 'billing', 'ageing'] as const,
  cashRegisters: (marinaId: string) => ['marina', marinaId, 'billing', 'cash-registers'] as const,
  cashSessions: (marinaId: string, registerId?: string) =>
    trim(['marina', marinaId, 'billing', 'cash-sessions', registerId]),
  cashSession: (marinaId: string, sessionId?: string) =>
    trim(['marina', marinaId, 'billing', 'cash-session', sessionId]),
  fiscalTotals: (marinaId: string, range?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'fiscal-totals', range]),
  fiscalDocs: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'fiscal-docs', filters]),
  chargeExports: (marinaId: string) => ['marina', marinaId, 'billing', 'exports'] as const,
  chargeExport: (marinaId: string, exportId?: string) =>
    trim(['marina', marinaId, 'billing', 'export', exportId]),
  reconciliations: (marinaId: string) =>
    ['marina', marinaId, 'billing', 'reconciliations'] as const,
  chargeImports: (marinaId: string) => ['marina', marinaId, 'billing', 'charge-imports'] as const,
  paymentRequests: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'payment-requests', filters]),
  paymentRequest: (marinaId: string, requestId?: string) =>
    trim(['marina', marinaId, 'billing', 'payment-request', requestId]),
  reminderRuns: (marinaId: string) => ['marina', marinaId, 'billing', 'reminder-runs'] as const,
  reminderRun: (marinaId: string, runId?: string) =>
    trim(['marina', marinaId, 'billing', 'reminder-run', runId]),
  exclusions: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'exclusions', filters]),
  paymentProviders: (marinaId: string) =>
    ['marina', marinaId, 'billing', 'payment-providers'] as const,
  providerSettings: (marinaId: string, code?: string) =>
    trim(['marina', marinaId, 'billing', 'provider-settings', code]),
  billingSettings: (marinaId: string) =>
    ['marina', marinaId, 'billing', 'billing-settings'] as const,
  customerDashboard: (marinaId: string, customerId?: string) =>
    trim(['marina', marinaId, 'billing', 'customer-dashboard', customerId]),
  customerStatement: (
    marinaId: string,
    customerId?: string,
    range?: Record<string, string | undefined>,
  ) => trim(['marina', marinaId, 'billing', 'customer-statement', customerId, range]),
  customerCredits: (marinaId: string, customerId?: string) =>
    trim(['marina', marinaId, 'billing', 'customer-credits', customerId]),
  sepaMandates: (marinaId: string, customerId?: string) =>
    trim(['marina', marinaId, 'billing', 'sepa-mandates', customerId]),
  mapPayments: (marinaId: string, mapId?: string, date?: string) =>
    trim(['marina', marinaId, 'billing', 'map-payments', mapId, date]),
  worklist: (marinaId: string, filters?: Record<string, string | undefined>) =>
    trim(['marina', marinaId, 'billing', 'worklist', filters]),

  // M5 public pay page (booking SPA): never under a marina id, the token is the only credential.
  publicPay: (slug: string, token: string) => ['public', slug, 'pay', token] as const,
};

/** Drops the trailing undefined segments, so a key with fewer arguments is a prefix of the full one. */
function trim(key: readonly unknown[]): readonly unknown[] {
  let end = key.length;
  while (end > 0 && key[end - 1] === undefined) end--;
  return key.slice(0, end);
}

export type QueryKey = readonly unknown[];

/** The data of an SSE message (internal/http/sse Message): ids, enums and dates only. */
export type StreamMessage = {
  topic: string;
  type: string;
  marina_id: string;
  entity_ids: string[];
  map_id?: string;
  resource_ids?: string[];
  nights?: { from: string; to: string };
};

/** Topics whose invalidation is narrowed by the message content and debounced (M3). */
export const NARROWED_TOPICS = [
  'occupancy.changed',
  'presence.changed',
  'contract.changed',
] as const;

/**
 * Query keys invalidated by one message (m3-plan §7.5). The M3 topics narrow the day view and the map occupancy
 * to the message's map (all maps when absent); other topics fall back to keysForTopic.
 */
export function keysForMessage(msg: StreamMessage, marinaId: string): QueryKey[] {
  const mapId = msg.map_id || undefined;
  switch (msg.topic) {
    case 'occupancy.changed':
      return [
        queryKeys.dayView(marinaId, mapId),
        queryKeys.mapOccupancy(marinaId, mapId),
        queryKeys.allocation(marinaId),
        queryKeys.movements(marinaId),
        queryKeys.releases(marinaId),
      ];
    case 'presence.changed':
      return [
        queryKeys.dayView(marinaId, mapId),
        queryKeys.movements(marinaId),
        queryKeys.presences(marinaId),
      ];
    case 'contract.changed':
      return [
        queryKeys.quotes(marinaId),
        queryKeys.quote(marinaId),
        queryKeys.contracts(marinaId),
        queryKeys.contract(marinaId),
        queryKeys.transits(marinaId),
        queryKeys.inquiries(marinaId),
        queryKeys.movements(marinaId),
        queryKeys.dayView(marinaId),
        // M4: instalments, renewals, charter, addenda, waiting list (m4-plan §7.5).
        queryKeys.installments(marinaId),
        queryKeys.contractInstallments(marinaId),
        queryKeys.addenda(marinaId),
        queryKeys.renewals(marinaId),
        queryKeys.campaign(marinaId),
        queryKeys.campaignProposals(marinaId),
        queryKeys.proposal(marinaId),
        queryKeys.charters(marinaId),
        queryKeys.fleets(marinaId),
        queryKeys.fleet(marinaId),
        queryKeys.waitlist(marinaId),
      ];
    default:
      return keysForTopic(msg.topic, marinaId);
  }
}

/** SSE topics (plan §4.5) and the query keys each one invalidates. */
export function keysForTopic(topic: string, marinaId: string): QueryKey[] {
  switch (topic) {
    case 'marina.changed':
      return [queryKeys.marina(marinaId), queryKeys.organizations, queryKeys.me];
    case 'team.changed':
      return [queryKeys.team(marinaId), queryKeys.me];
    case 'map.changed':
      // maps, map(id), berths, berth-groups and harbor-settings all live under the maps key.
      return [queryKeys.maps(marinaId)];
    case 'registry.changed':
      return [queryKeys.registry(marinaId)];
    case 'pricing.changed':
      // settings, revenue classes, items, categories, lists, matrices and coverage live under pricing.
      return [queryKeys.pricing(marinaId)];
    case 'rights.changed':
      // rights, map rights, transfers, mandates, fees, millesimal tables, budgets, runs, holder instalments and
      // the concession live under rights (M4).
      return [queryKeys.rights(marinaId)];
    case 'documents.changed':
      // templates, generated documents, signatures, communications, imports and customer messages (M4).
      return [queryKeys.docsArea(marinaId)];
    case 'billing.changed':
      // Charges, payments, cash, fiscal, exports, requests, reminders, providers, settings, dashboards, the
      // payment lens and the worklist live under billing (M5); installment.charged also refreshes the instalment
      // lists of contracts and holders (m5-plan §7.5).
      return [
        queryKeys.billing(marinaId),
        queryKeys.installments(marinaId),
        queryKeys.contractInstallments(marinaId),
        queryKeys.holderInstallments(marinaId),
      ];
    default:
      return [];
  }
}
