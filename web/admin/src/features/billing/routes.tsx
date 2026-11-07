import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { BillingSettingsPage } from './pages/BillingSettingsPage';
import { CashPage } from './pages/CashPage';
import { ChargePage } from './pages/ChargePage';
import { ChargesPage } from './pages/ChargesPage';
import { ExportPage, ExportWizardPage, ExportsPage, ReconcilePage } from './pages/ExportsPage';
import { FiscalPage } from './pages/FiscalPage';
import { NewChargePage } from './pages/NewChargePage';
import { OpeningBalancesPage } from './pages/OpeningBalancesPage';
import { PaymentPage } from './pages/PaymentPage';
import { PaymentRequestsPage } from './pages/PaymentRequestsPage';
import { PaymentSettingsPage } from './pages/PaymentSettingsPage';
import { PaymentsPage } from './pages/PaymentsPage';
import { ReceivablesPage } from './pages/ReceivablesPage';
import { RemindersPage } from './pages/RemindersPage';
import { WorklistPage } from './pages/WorklistPage';

// Routes of the M5 billing feature (m5-plan §9.1), children of the marina route, nav group "Amministrazione".

export function billingRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'charges', component: ChargesPage }),
    createRoute({ getParentRoute: () => parent, path: 'charges/new', component: NewChargePage }),
    createRoute({ getParentRoute: () => parent, path: 'charges/$chargeId', component: ChargePage }),
    createRoute({ getParentRoute: () => parent, path: 'payments', component: PaymentsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'payments/$paymentId',
      component: PaymentPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'cash', component: CashPage }),
    createRoute({ getParentRoute: () => parent, path: 'fiscal', component: FiscalPage }),
    createRoute({ getParentRoute: () => parent, path: 'receivables', component: ReceivablesPage }),
    createRoute({ getParentRoute: () => parent, path: 'reminders', component: RemindersPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'payment-requests',
      component: PaymentRequestsPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'exports', component: ExportsPage }),
    createRoute({ getParentRoute: () => parent, path: 'exports/new', component: ExportWizardPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'exports/reconcile',
      component: ReconcilePage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'exports/$exportId', component: ExportPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'opening-balances',
      component: OpeningBalancesPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'worklist', component: WorklistPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'settings/billing',
      component: BillingSettingsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'settings/payments',
      component: PaymentSettingsPage,
    }),
  ];
}
