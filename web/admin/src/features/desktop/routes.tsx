import { createRoute } from '@tanstack/react-router';

import type { PlainRoute, RootRoute } from '../../router';
import { DesktopSettingsPage } from './DesktopSettingsPage';
import { FirstRunPage } from './FirstRunPage';
import { MoveOnlinePage } from './MoveOnlinePage';

/**
 * Desktop edition routes (m6a-plan §7): the first run is outside the auth guard (child of the root route); the
 * settings and the move-online guide are inside it (children of the plain layout, not marina-scoped).
 */
export function desktopRoutes(root: RootRoute, plain: PlainRoute) {
  const firstRun = createRoute({
    getParentRoute: () => root,
    path: 'desktop/first-run',
    component: FirstRunPage,
  });
  const settings = createRoute({
    getParentRoute: () => plain,
    path: 'desktop',
    component: DesktopSettingsPage,
  });
  const online = createRoute({
    getParentRoute: () => plain,
    path: 'desktop/online',
    component: MoveOnlinePage,
  });
  return { publicRoutes: [firstRun] as const, plainRoutes: [settings, online] as const };
}
