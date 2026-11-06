import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { BerthImportPage } from './BerthImportPage';

// Berth CSV import route (m1-plan §5.1, §5.5).

/** Route of the berth CSV import wizard, child of the marina route. */
export function berthImportRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'maps/$mapId/import-berths',
      component: BerthImportPage,
    }),
  ] as const;
}
