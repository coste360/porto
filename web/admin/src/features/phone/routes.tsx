import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { DirectionPage } from './DirectionPage';
import { DockPage } from './dock/DockPage';
import { validateDirectionSearch, validateDockSearch } from './lib/tabs';

// Phone screens (m6b-plan §6): children of the marina route rendered inside PhoneShell (staticData.shell). The
// dock keeps the M3 path /m/$marinaId/dock (and data-testid="dock-page"); the tab is in the search (?tab=).

export function phoneRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'dock',
      component: DockPage,
      validateSearch: validateDockSearch,
      staticData: { shell: 'phone' },
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'direction',
      component: DirectionPage,
      validateSearch: validateDirectionSearch,
      staticData: { shell: 'phone' },
    }),
  ] as const;
}
