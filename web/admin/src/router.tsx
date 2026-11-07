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

import { ensureMe, findMarina } from './auth/me';
import { AppShell } from './components/AppShell';
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
import { ResetPasswordPage } from './pages/ResetPasswordPage';
import { TeamPage } from './pages/TeamPage';
import { TotpSetupPage } from './pages/TotpSetupPage';
import { WebhooksPage } from './pages/WebhooksPage';
import { safeRedirect } from './lib/hash';
import { berthImportRoutes } from './features/berth-import/routes';
import { mapRoutes } from './features/maps/routes';
import { pricingRoutes } from './features/pricing/routes';
import { pricingToolRoutes } from './features/pricing-tools/routes';
import { registryRoutes } from './features/registry/routes';

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
  beforeLoad: ({ context, params }) => {
    // Unknown and foreign marinas look the same (the API answers 404 too).
    const marina = findMarina(context.me, params.marinaId);
    if (!marina) throw notFound();
    return { marina };
  },
  component: AppShell,
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
  authRoute.addChildren([
    totpSetupRoute,
    plainRoute.addChildren([pickerRoute, accountRoute, organizationRoute]),
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
