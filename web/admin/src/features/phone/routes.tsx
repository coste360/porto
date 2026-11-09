import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { DirectionPage } from './DirectionPage';
import { DockPage } from './dock/DockPage';
import { MaintenancePhonePage } from './maintenance/MaintenancePhonePage';
import { RentalPhonePage } from './rental/RentalPhonePage';
import { StoragePhonePage } from './storage/StoragePhonePage';
import { YardPhonePage } from './yard/YardPhonePage';
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
    // M9 (m9-plan §12): storage (PLANNER) and maintenance (UTILITIES) phone screens.
    createRoute({
      getParentRoute: () => parent,
      path: 'storage-phone',
      component: StoragePhonePage,
      staticData: { shell: 'phone' },
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'maintenance-phone',
      component: MaintenancePhonePage,
      staticData: { shell: 'phone' },
    }),
    // M10 (m10-plan §14.3): yard phone screen (PLANNER) for yard and yard_mgr.
    createRoute({
      getParentRoute: () => parent,
      path: 'yard-phone',
      component: YardPhonePage,
      staticData: { shell: 'phone' },
    }),
    // M11 (m11-plan §14.3): rental phone screen (HANDOVER) for the rental role.
    createRoute({
      getParentRoute: () => parent,
      path: 'rental-phone',
      component: RentalPhonePage,
      staticData: { shell: 'phone' },
    }),
  ] as const;
}
