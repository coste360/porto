import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { ComparisonPage } from '../reports/comparison/ComparisonPage';
import { reportSearchMiddleware, validateReportSearch } from '../reports/lib/reportSearch';

// Routes of the M7 dashboard feature (m7-plan §10.1): the manager dashboard itself is DashboardPage at
// /m/$marinaId; this adds the period comparison page.

export function dashboardRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'reports/comparison',
      component: ComparisonPage,
      validateSearch: validateReportSearch,
      search: { middlewares: [reportSearchMiddleware] },
    }),
  ] as const;
}
