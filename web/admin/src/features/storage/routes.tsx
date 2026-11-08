import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { MovementsPage } from './pages/MovementsPage';
import { ScenarioComparePage } from './pages/ScenarioComparePage';
import { ScenarioEditorPage } from './pages/ScenarioEditorPage';
import { ScenariosPage } from './pages/ScenariosPage';
import { StorageAreasPage } from './pages/StorageAreasPage';
import { StorageContractNewPage } from './pages/StorageContractNewPage';
import { StoragePlannerPage } from './pages/StoragePlannerPage';
import { validateDateSearch, validateCompareSearch, validateNewContractSearch } from './lib/search';

// Routes of M9 dry storage (m9-plan §12.1), children of the marina route. Static segments (movements, contracts)
// win over $areaId.

export function storageRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'storage', component: StorageAreasPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'storage/movements',
      component: MovementsPage,
      validateSearch: validateDateSearch,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'storage/contracts/new',
      component: StorageContractNewPage,
      validateSearch: validateNewContractSearch,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'storage/$areaId',
      component: StoragePlannerPage,
      validateSearch: validateDateSearch,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'storage/$areaId/scenarios',
      component: ScenariosPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'storage/$areaId/scenarios/$scenarioId',
      component: ScenarioEditorPage,
      validateSearch: validateDateSearch,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'storage/$areaId/scenarios/$scenarioId/compare',
      component: ScenarioComparePage,
      validateSearch: validateCompareSearch,
    }),
  ] as const;
}
