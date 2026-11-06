import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { HarborSettingsPage } from '../harbor-settings/HarborSettingsPage';
import { EditorPage } from './editor/EditorPage';
import { MapPrintPage } from './MapPrintPage';
import { MapsPage } from './MapsPage';
import { MapViewerPage } from './MapViewerPage';

// Harbour map routes (m1-plan §5.1).

/** Routes of the harbour map feature, children of the marina route. */
export function mapRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'maps', component: MapsPage }),
    createRoute({ getParentRoute: () => parent, path: 'maps/$mapId', component: MapViewerPage }),
    createRoute({ getParentRoute: () => parent, path: 'maps/$mapId/edit', component: EditorPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'maps/$mapId/print',
      component: MapPrintPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'harbor-settings',
      component: HarborSettingsPage,
    }),
  ] as const;
}
