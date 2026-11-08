import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { reportSearchMiddleware, validateReportSearch } from './lib/reportSearch';
import { ReportPage } from './pages/ReportPage';
import { ReportsIndexPage } from './pages/ReportsIndexPage';

// Routes of the M7 reports (m7-plan §10.1), children of the marina route, nav group "Analisi". The comparison
// page (reports/comparison) is registered by dashboardRoutes; a static segment wins over $report.

export function reportsRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'reports', component: ReportsIndexPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'reports/$report',
      component: ReportPage,
      validateSearch: validateReportSearch,
      search: { middlewares: [reportSearchMiddleware] },
    }),
  ] as const;
}
