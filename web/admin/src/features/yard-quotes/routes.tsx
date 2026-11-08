import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { YardQuoteNewPage } from './pages/YardQuoteNewPage';
import { YardQuotePage } from './pages/YardQuotePage';
import { YardQuotesPage } from './pages/YardQuotesPage';

// Routes of M10 (m10-plan §14), children of the marina route: the list, the 5-step wizard and the detail. Owned by QUOTE.

export function yardQuotesRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'yard/quotes', component: YardQuotesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'yard/quotes/new',
      component: YardQuoteNewPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'yard/quotes/$quoteId',
      component: YardQuotePage,
    }),
  ] as const;
}
