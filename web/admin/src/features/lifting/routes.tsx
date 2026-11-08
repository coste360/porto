import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { LiftingPage } from './pages/LiftingPage';

// Routes of M10 (m10-plan §14), children of the marina route: the lifting day calendar. Owned by PLANNER.

export function liftingRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'yard/lifting', component: LiftingPage }),
  ] as const;
}
