import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { CustomerPage } from './customers/CustomerPage';
import { CustomersPage } from './customers/CustomersPage';
import { NewCustomerPage } from './customers/NewCustomerPage';
import { DeadlinesPage } from './deadlines/DeadlinesPage';
import { NewVesselPage } from './vessels/NewVesselPage';
import { VesselPage } from './vessels/VesselPage';
import { VesselsPage } from './vessels/VesselsPage';

// Registry routes (m1-plan §5.1): customers, vessels and deadlines, children of the marina route.

type NewVesselSearch = { customer?: string };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Routes of the registry feature (customers, vessels, deadlines), children of the marina route. */
export function registryRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'customers', component: CustomersPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'customers/new',
      component: NewCustomerPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'customers/$customerId',
      component: CustomerPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'vessels', component: VesselsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'vessels/new',
      // The customer page opens the form with that customer as the owner.
      validateSearch: (search: Record<string, unknown>): NewVesselSearch =>
        typeof search.customer === 'string' && UUID.test(search.customer)
          ? { customer: search.customer }
          : {},
      component: NewVesselPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'vessels/$vesselId',
      component: VesselPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'deadlines', component: DeadlinesPage }),
  ] as const;
}
