import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { EmployeesPage } from './pages/EmployeesPage';
import { YardLoadPage } from './pages/YardLoadPage';
import { YardPlannerPage } from './pages/YardPlannerPage';

// Routes of M10 (m10-plan §14), children of the marina route: the planner, the workload and the employees. Owned by PLANNER.

export function yardPlannerRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'yard/planner', component: YardPlannerPage }),
    createRoute({ getParentRoute: () => parent, path: 'yard/load', component: YardLoadPage }),
    createRoute({ getParentRoute: () => parent, path: 'yard/employees', component: EmployeesPage }),
  ] as const;
}
