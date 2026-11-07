import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { DockPage } from '../dock/DockPage';
import { ContractPage } from './contracts/ContractPage';
import { ContractsPage } from './contracts/ContractsPage';
import { ContractWizardPage } from './contracts/ContractWizardPage';
import { InquiriesPage } from './inquiries/InquiriesPage';
import { isIsoDate } from './lib/dates';
import { PLANNER_SPANS, type PlannerSpan } from './lib/planner';
import { validatePrefillSearch } from './lib/prefill';
import { PlannerPage } from './planner/PlannerPage';
import { QuoteNewPage } from './quotes/QuoteNewPage';
import { QuotePage } from './quotes/QuotePage';
import { QuotesPage } from './quotes/QuotesPage';
import { OccupancySettingsPage } from './settings/OccupancySettingsPage';
import { TransitPage } from './transits/TransitPage';
import { TransitsPage } from './transits/TransitsPage';

// Desk routes (m3-plan §8.1): planner, quotes, contracts, transits, inquiries, the phone dock panel and the
// occupancy settings, children of the marina route.

export type PlannerSearch = {
  map?: string;
  from?: string;
  days?: PlannerSpan;
  group?: string;
  structure?: string;
};

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function validatePlannerSearch(search: Record<string, unknown>): PlannerSearch {
  const out: PlannerSearch = {};
  const id = (v: unknown) => (typeof v === 'string' && ID.test(v) ? v : undefined);
  const map = id(search.map);
  const group = id(search.group);
  const structure = id(search.structure);
  if (map) out.map = map;
  if (group) out.group = group;
  if (structure) out.structure = structure;
  if (typeof search.from === 'string' && isIsoDate(search.from)) out.from = search.from;
  const days = Number(search.days);
  if ((PLANNER_SPANS as readonly number[]).includes(days)) out.days = days as PlannerSpan;
  return out;
}

/** Routes of the desk, children of the marina route. */
export function deskRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'planner',
      validateSearch: validatePlannerSearch,
      component: PlannerPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'quotes', component: QuotesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'quotes/new',
      validateSearch: validatePrefillSearch,
      component: QuoteNewPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'quotes/$quoteId', component: QuotePage }),
    createRoute({ getParentRoute: () => parent, path: 'contracts', component: ContractsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'contracts/new',
      validateSearch: validatePrefillSearch,
      component: ContractWizardPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'contracts/$contractId',
      component: ContractPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'transits', component: TransitsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'transits/$contractId',
      component: TransitPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'inquiries', component: InquiriesPage }),
    createRoute({ getParentRoute: () => parent, path: 'dock', component: DockPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'occupancy-settings',
      component: OccupancySettingsPage,
    }),
  ] as const;
}
