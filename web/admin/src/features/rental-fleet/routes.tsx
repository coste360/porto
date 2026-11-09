import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { RentalFleetPage } from './pages/RentalFleetPage';

// Routes of M11 (m11-plan §14.1), children of the marina route: the rental fleet. Owned by FLEET.

export function rentalFleetRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'rental/fleet', component: RentalFleetPage }),
  ] as const;
}
