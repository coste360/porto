import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { ChartersPage } from './ChartersPage';
import { ContractSettingsPage } from './ContractSettingsPage';
import { InstallmentsPage } from './InstallmentsPage';
import { RenewalCampaignPage } from './RenewalCampaignPage';
import { RenewalsPage } from './RenewalsPage';
import { WaitlistPage } from './WaitlistPage';

// Routes of the contracts-m4 feature (m4-plan §8.1), children of the marina route. CONTRACT writes the paths; WEB
// owns the pages.

export function contractsM4Routes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'installments',
      component: InstallmentsPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'renewals', component: RenewalsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'renewals/$campaignId',
      component: RenewalCampaignPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'charters', component: ChartersPage }),
    createRoute({ getParentRoute: () => parent, path: 'waitlist', component: WaitlistPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'settings/contracts',
      component: ContractSettingsPage,
    }),
  ];
}
