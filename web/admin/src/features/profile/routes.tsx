import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { ProfilePage } from './pages/ProfilePage';

// Routes of M12 (m12-plan §16.2), children of the marina route: the public profile panel. Owned by PUBLIC.

export function profileRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'profile', component: ProfilePage }),
  ] as const;
}
