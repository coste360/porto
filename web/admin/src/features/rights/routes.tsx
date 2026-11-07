import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { CommonChargesPage } from './CommonChargesPage';
import { ConcessionPage } from './ConcessionPage';
import { FeesPage } from './FeesPage';
import { HolderInstallmentsPage } from './HolderInstallmentsPage';
import { MandatePage, MandatesPage } from './MandatesPage';
import { RightPage } from './RightPage';
import { RightsPage } from './RightsPage';
import { TransferPage, TransfersPage } from './TransfersPage';

// Routes of the rights feature (m4-plan §8.1), children of the marina route. CONTRACT writes the paths; WEB
// owns the pages.

/** `?right=<id>` opens the transfer wizard for that right (link from the right page). */
export function validateTransfersSearch(search: Record<string, unknown>): { right?: string } {
  return typeof search.right === 'string' && search.right ? { right: search.right } : {};
}

export function rightsRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'rights', component: RightsPage }),
    createRoute({ getParentRoute: () => parent, path: 'rights/$rightId', component: RightPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'transfers',
      validateSearch: validateTransfersSearch,
      component: TransfersPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'transfers/$transferId',
      component: TransferPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'mandates', component: MandatesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'mandates/$mandateId',
      component: MandatePage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'fees', component: FeesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'common-charges',
      component: CommonChargesPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'holder-installments',
      component: HolderInstallmentsPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'concession', component: ConcessionPage }),
  ];
}
