import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { ClassificationsPage } from './classifications/ClassificationsPage';
import { PriceListEditorPage } from './editor/EditorPage';
import { ItemsPage } from './items/ItemsPage';
import { PriceListsPage } from './lists/PriceListsPage';
import { RenewalPage } from './renew/RenewalPage';
import { PricingSettingsPage } from './settings/PricingSettingsPage';

// Pricing routes (m2-plan §7.2): items, classifications, pricing settings, price lists, the document editor and
// the renewal wizard, children of the marina route.

/** Routes of the pricing feature (items, classifications, settings, price lists), children of the marina route. */
export function pricingRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'items', component: ItemsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'pricing/classifications',
      component: ClassificationsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'pricing/settings',
      component: PricingSettingsPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'price-lists', component: PriceListsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'price-lists/$priceListId',
      component: PriceListEditorPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'price-lists/$priceListId/renew',
      component: RenewalPage,
    }),
  ] as const;
}
