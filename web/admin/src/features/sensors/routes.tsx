import { createRoute } from '@tanstack/react-router';
import { RadioTowerIcon } from 'lucide-react';

import type { MarinaRoute } from '../../router';
import { registerDockSlots } from '../phone/public';
import { AlertsTab } from '../phone/dock/AlertsTab';
import { SensorHistoryPage, type SensorHistorySearch } from './pages/SensorHistoryPage';
import { SensorsPage } from './pages/SensorsPage';

// Routes of M12 (m12-plan §16.1), children of the marina route: the Sensori page (alerts, devices, gateways,
// sources, settings) and the per-resource history. The dock phone screen gets the "Avvisi" tab (DockSlots.extraTabs,
// m12-plan §9.3). Owned by RECONCILE.

let dockTabRegistered = false;

const RESOURCE_TYPES = ['berth', 'dynamic_area', 'pedestal'] as const;
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

export function validateHistorySearch(search: Record<string, unknown>): SensorHistorySearch {
  const out: SensorHistorySearch = {};
  const rt = search.resource_type;
  if (typeof rt === 'string' && (RESOURCE_TYPES as readonly string[]).includes(rt))
    out.resource_type = rt as SensorHistorySearch['resource_type'];
  if (typeof search.resource_id === 'string' && search.resource_id.length <= 36)
    out.resource_id = search.resource_id;
  for (const k of ['from', 'to'] as const) {
    const v = search[k];
    if (typeof v === 'string' && DATE_RE.test(v)) out[k] = v;
  }
  return out;
}

export function sensorsRoutes(parent: MarinaRoute) {
  if (!dockTabRegistered) {
    dockTabRegistered = true;
    registerDockSlots({
      extraTabs: [
        {
          value: 'alerts',
          labelKey: 'sensors:tabs.alerts',
          icon: <RadioTowerIcon />,
          permission: 'map.view',
          render: (date) => <AlertsTab today={date} />,
        },
      ],
    });
  }
  return [
    createRoute({ getParentRoute: () => parent, path: 'sensors', component: SensorsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'sensors/history',
      validateSearch: validateHistorySearch,
      component: SensorHistoryPage,
    }),
  ] as const;
}
