import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { OnlineBookingsPage } from './pages/OnlineBookingsPage';
import { OnlineSettingsPage } from './pages/OnlineSettingsPage';

// Routes of M8 online booking (m8-plan §12.3), children of the marina route: the desk list (nav group
// "Operatività") and the configuration page (settings group).

export function onlineRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'online', component: OnlineBookingsPage }),
  ] as const;
}

export function onlineSettingsRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'settings/online',
      component: OnlineSettingsPage,
    }),
  ] as const;
}
