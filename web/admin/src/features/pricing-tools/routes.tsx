import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { CoveragePage } from './coverage/CoveragePage';
import { MatrixPage } from './matrix/MatrixPage';
import { MatrixPrintPage } from './matrix/MatrixPrintPage';
import { validateMatrixSearch } from './matrix/matrixSearch';
import { SimulatorPage } from './simulator/SimulatorPage';

// Pricing tool routes (m2-plan §7.2): simulator, matrix, printable matrix and coverage check, flat children of
// the marina route (the price list pages of WEB-PRICELISTS use the other paths under price-lists/).

type SimulatorSearch = { probe?: string };

/** Routes of the pricing tools (simulator, matrix, coverage), children of the marina route. */
export function pricingToolRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'pricing/simulator',
      // A matrix cell opens the simulator with its probe (base64url JSON, decoded by the page).
      validateSearch: (search: Record<string, unknown>): SimulatorSearch =>
        typeof search.probe === 'string' && search.probe.length <= 8192
          ? { probe: search.probe }
          : {},
      component: SimulatorPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'price-lists/$priceListId/matrix',
      validateSearch: validateMatrixSearch,
      component: MatrixPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'price-lists/$priceListId/matrix/print',
      validateSearch: validateMatrixSearch,
      component: MatrixPrintPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'price-lists/$priceListId/coverage',
      component: CoveragePage,
    }),
  ] as const;
}
