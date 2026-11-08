import { createRoute } from '@tanstack/react-router';
import { GaugeIcon } from 'lucide-react';

import type { MarinaRoute } from '../../router';
import { registerDockSlots } from '../phone/public';
import { ReadingsTab } from '../phone/readings/ReadingsTab';
import { ConsumptionsPage } from './pages/ConsumptionsPage';
import { ReadingsPage } from './pages/ReadingsPage';
import { SettingsPage } from './pages/SettingsPage';
import { UtilitiesPage } from './pages/UtilitiesPage';

// Routes of M9 utilities (m9-plan §12.2), children of the marina route: meters, readings, consumptions and
// settings. The dock phone screen gets the "Letture" tab (DockSlots.extraTabs) for users who record readings.

let dockTabRegistered = false;

export function utilitiesRoutes(parent: MarinaRoute) {
  if (!dockTabRegistered) {
    dockTabRegistered = true;
    registerReadingsTab();
  }
  return routesOf(parent);
}

function registerReadingsTab() {
  registerDockSlots({
    extraTabs: [
      {
        value: 'readings',
        labelKey: 'utilities:dock.readings',
        icon: <GaugeIcon />,
        permission: 'presence.write',
        render: (date) => <ReadingsTab today={date} />,
      },
    ],
  });
}

function routesOf(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'utilities', component: UtilitiesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'utilities/readings',
      component: ReadingsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'utilities/consumptions',
      component: ConsumptionsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'utilities/settings',
      component: SettingsPage,
    }),
  ] as const;
}
