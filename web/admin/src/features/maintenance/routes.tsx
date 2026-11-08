import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { EquipmentPage } from './pages/EquipmentPage';
import { MaintenancePage } from './pages/MaintenancePage';
import { PlansPage } from './pages/PlansPage';
import { TaskPage } from './pages/TaskPage';

// Routes of M9 maintenance (m9-plan §12.2), children of the marina route: the tasks board, the task detail, the
// plans and the equipment registry.

export function maintenanceRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'maintenance', component: MaintenancePage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'maintenance/tasks/$taskId',
      component: TaskPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'maintenance/plans', component: PlansPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'maintenance/equipment',
      component: EquipmentPage,
    }),
  ] as const;
}
