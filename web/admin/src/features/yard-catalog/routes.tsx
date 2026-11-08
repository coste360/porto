import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { YardCatalogPage } from './pages/YardCatalogPage';

// Routes of M10 (m10-plan §14), children of the marina route: the catalogue tabs. Owned by QUOTE.

export function yardCatalogRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'yard/catalog', component: YardCatalogPage }),
  ] as const;
}
