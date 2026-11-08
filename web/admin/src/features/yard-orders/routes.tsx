import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { TimesheetsPage } from './pages/TimesheetsPage';
import { WorkOrderPage } from './pages/WorkOrderPage';
import { YardOrdersPage } from './pages/YardOrdersPage';

// Routes of M10 (m10-plan §14), children of the marina route: the board, the work order and the time approval. Owned by ORDERS.

export function yardOrdersRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'yard/orders', component: YardOrdersPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'yard/orders/$orderId',
      component: WorkOrderPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'yard/timesheets',
      component: TimesheetsPage,
    }),
  ] as const;
}
