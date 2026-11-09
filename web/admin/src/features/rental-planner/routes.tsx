import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { RentalBookingPage } from './pages/RentalBookingPage';
import { RentalBookingsPage } from './pages/RentalBookingsPage';
import { RentalPlannerPage } from './pages/RentalPlannerPage';

// Routes of M11 (m11-plan §14.2), children of the marina route: the rental planner, the bookings list and the
// booking page. Owned by BOOKING.

export function rentalPlannerRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'rental/planner',
      component: RentalPlannerPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'rental/bookings',
      component: RentalBookingsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'rental/bookings/$bookingId',
      component: RentalBookingPage,
    }),
  ] as const;
}
