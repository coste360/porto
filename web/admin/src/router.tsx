import type { QueryClient } from '@tanstack/react-query';
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  notFound,
  redirect,
  type RouterHistory,
} from '@tanstack/react-router';

import type { ApiClient } from '@porto/shared/api';
import { marinaBlockingEntry, noteMarinaSwitchBlocked } from '@porto/shared/offline';

import { ensureMe, findMarina } from './auth/me';
import { MarinaLayout } from './components/MarinaLayout';
import { PlainLayout } from './components/PlainLayout';
import { RootLayout } from './components/RootLayout';
import { AccountPage } from './pages/AccountPage';
import { ApiKeysPage } from './pages/ApiKeysPage';
import { AuditPage } from './pages/AuditPage';
import { DashboardPage } from './pages/DashboardPage';
import { DevicesPage } from './pages/DevicesPage';
import { ForgotPasswordPage } from './pages/ForgotPasswordPage';
import { LoginPage } from './pages/LoginPage';
import { MarinaPickerPage } from './pages/MarinaPickerPage';
import { MarinaSettingsPage } from './pages/MarinaSettingsPage';
import { NotFoundPage } from './pages/NotFoundPage';
import { OrganizationPage } from './pages/OrganizationPage';
import { PairPage } from './pages/PairPage';
import { ResetPasswordPage } from './pages/ResetPasswordPage';
import { TeamPage } from './pages/TeamPage';
import { TotpSetupPage } from './pages/TotpSetupPage';
import { WebhooksPage } from './pages/WebhooksPage';
import { safeRedirect } from './lib/hash';
import { berthImportRoutes } from './features/berth-import/routes';
import { deskRoutes } from './features/desk/routes';
import { mapRoutes } from './features/maps/routes';
import { operationsRoutes } from './features/operations/routes';
import { pricingRoutes } from './features/pricing/routes';
import { pricingToolRoutes } from './features/pricing-tools/routes';
import { registryRoutes } from './features/registry/routes';
import { contractsM4Routes } from './features/contracts-m4/routes';
import { documentsRoutes } from './features/documents/routes';
import { rightsRoutes } from './features/rights/routes';
import { billingRoutes } from './features/billing/routes';
import { desktopRoutes } from './features/desktop/routes';
import { phoneRoutes } from './features/phone/routes';
import { reportsRoutes } from './features/reports/routes';
import { dashboardRoutes } from './features/dashboard/routes';
import { onlineRoutes, onlineSettingsRoutes } from './features/online/routes';
import { storageRoutes } from './features/storage/routes';
import { maintenanceRoutes } from './features/maintenance/routes';
import { utilitiesRoutes } from './features/utilities/routes';
import { CertificateHelpPage } from './features/help/CertificateHelpPage';

export type RouterContext = { queryClient: QueryClient; api: ApiClient };

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFoundPage,
});

type LoginSearch = { redirect?: string };

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'login',
  validateSearch: (search: Record<string, unknown>): LoginSearch =>
    typeof search.redirect === 'string' ? { redirect: safeRedirect(search.redirect) } : {},
  beforeLoad: async ({ context, search }) => {
    const me = await ensureMe(context.queryClient, context.api);
    if (me && !me.totp_enrolment_required) throw redirect({ to: safeRedirect(search.redirect) });
  },
  component: LoginPage,
});

const forgotPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'forgot-password',
  component: ForgotPasswordPage,
});

const resetPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'reset-password',
  component: ResetPasswordPage,
});

/** Browser pairing by QR (M6a): public, the code arrives in the URL fragment. */
const pairRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'pair',
  component: PairPage,
});

/** Public help page for installing the local CA on phones (M6b, m6b-plan §8.5). */
const certificateHelpRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'help/certificate',
  component: CertificateHelpPage,
});

/** The root route, parent of the public desktop routes (features/desktop/routes.tsx). */
export type RootRoute = typeof rootRoute;

/** Every route below needs a session; enrolment-required sessions only reach TOTP setup. */
const authRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'auth',
  beforeLoad: async ({ context, location }) => {
    const me = await ensureMe(context.queryClient, context.api);
    if (!me) throw redirect({ to: '/login', search: { redirect: location.href } });
    if (me.totp_enrolment_required && !location.pathname.endsWith('/totp-setup')) {
      throw redirect({ to: '/totp-setup' });
    }
    return { me };
  },
});

const totpSetupRoute = createRoute({
  getParentRoute: () => authRoute,
  path: 'totp-setup',
  component: TotpSetupPage,
});

const plainRoute = createRoute({
  getParentRoute: () => authRoute,
  id: 'plain',
  component: PlainLayout,
});

/** The plain (non-marina) layout route, parent of the desktop settings routes. */
export type PlainRoute = typeof plainRoute;

const desktop = desktopRoutes(rootRoute, plainRoute);

const pickerRoute = createRoute({
  getParentRoute: () => plainRoute,
  path: '/',
  component: MarinaPickerPage,
});

const accountRoute = createRoute({
  getParentRoute: () => plainRoute,
  path: 'account',
  component: AccountPage,
});

const organizationRoute = createRoute({
  getParentRoute: () => plainRoute,
  path: 'org/$organizationId',
  component: OrganizationPage,
});

const marinaRoute = createRoute({
  getParentRoute: () => authRoute,
  path: 'm/$marinaId',
  beforeLoad: async ({ context, params }) => {
    // Unknown and foreign marinas look the same (the API answers 404 too).
    const marina = findMarina(context.me, params.marinaId);
    if (!marina) throw notFound();
    // Unsent offline ops of another marina: back there, whatever link led here (D9, INV-84).
    const blocking = await marinaBlockingEntry(context.me.user.id, marina.id);
    if (blocking && findMarina(context.me, blocking)) {
      noteMarinaSwitchBlocked();
      throw redirect({ to: '/m/$marinaId', params: { marinaId: blocking } });
    }
    return { marina };
  },
  // AppShell, or PhoneShell for the phone screens (M6b, staticData.shell).
  component: MarinaLayout,
});

/** The marina route, parent of the M1 feature route factories (features/<name>/routes.tsx). */
export type MarinaRoute = typeof marinaRoute;

const dashboardRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: '/',
  component: DashboardPage,
});
const settingsRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: 'settings',
  component: MarinaSettingsPage,
});
const teamRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: 'team',
  component: TeamPage,
});
const apiKeysRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: 'api-keys',
  component: ApiKeysPage,
});
const devicesRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: 'devices',
  component: DevicesPage,
});
const webhooksRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: 'webhooks',
  component: WebhooksPage,
});
const auditRoute = createRoute({
  getParentRoute: () => marinaRoute,
  path: 'audit',
  component: AuditPage,
});

export const routeTree = rootRoute.addChildren([
  loginRoute,
  forgotPasswordRoute,
  resetPasswordRoute,
  pairRoute,
  certificateHelpRoute,
  ...desktop.publicRoutes,
  authRoute.addChildren([
    totpSetupRoute,
    plainRoute.addChildren([pickerRoute, accountRoute, organizationRoute, ...desktop.plainRoutes]),
    marinaRoute.addChildren([
      dashboardRoute,
      settingsRoute,
      teamRoute,
      apiKeysRoute,
      devicesRoute,
      webhooksRoute,
      auditRoute,
      ...mapRoutes(marinaRoute),
      ...registryRoutes(marinaRoute),
      ...berthImportRoutes(marinaRoute),
      ...pricingRoutes(marinaRoute),
      ...pricingToolRoutes(marinaRoute),
      ...operationsRoutes(marinaRoute),
      ...deskRoutes(marinaRoute),
      ...contractsM4Routes(marinaRoute),
      ...rightsRoutes(marinaRoute),
      ...documentsRoutes(marinaRoute),
      ...billingRoutes(marinaRoute),
      ...phoneRoutes(marinaRoute),
      // M7 (m7-plan §10.1)
      ...dashboardRoutes(marinaRoute),
      ...reportsRoutes(marinaRoute),
      // M8 (m8-plan §12.3)
      ...onlineRoutes(marinaRoute),
      ...onlineSettingsRoutes(marinaRoute),
      // M9 (m9-plan §12)
      ...storageRoutes(marinaRoute),
      ...maintenanceRoutes(marinaRoute),
      ...utilitiesRoutes(marinaRoute),
    ]),
  ]),
]);

export function createAppRouter(context: RouterContext, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context,
    basepath: '/admin',
    defaultPreload: 'intent',
    scrollRestoration: true,
    ...(history ? { history } : {}),
  });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

declare module '@tanstack/react-router' {
  interface Register {
    router: AppRouter;
  }
}

export { marinaRoute, organizationRoute, resetPasswordRoute, loginRoute };
