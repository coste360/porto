import { QueryClientProvider, type QueryClient } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory } from '@tanstack/react-router';
import { render } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';

import { ApiProvider, createApiClient, type Me } from '@porto/shared/api';
import { initI18n } from '@porto/shared/i18n';
import { ThemeProvider } from '@porto/shared/theme';
import { createFetchMock, createTestQueryClient, type MockRoute } from '@porto/shared/testing';

import { createApp } from '../bootstrap';

export const MARINA_ID = '0190a000-0000-7000-8000-000000000001';
export const ORG_ID = '0190a000-0000-7000-8000-0000000000aa';

export function makeMe(
  overrides: Partial<Me> = {},
  permissions?: Me['marinas'][number]['permissions'],
): Me {
  return {
    user: {
      id: 'u1',
      email: 'owner@porto.test',
      name: 'Owner Prova',
      locale: 'it',
      totp_enabled: false,
    },
    auth_method: 'session',
    totp_enrolment_required: false,
    organizations: [{ id: ORG_ID, name: 'Porto Prova SRL', role: 'owner' }],
    marinas: [
      {
        id: MARINA_ID,
        organization_id: ORG_ID,
        slug: 'prova',
        name: 'Marina Prova',
        roles: ['owner'],
        permissions: permissions ?? [
          'marina.read',
          'marina.configure',
          'users.manage',
          'devices.manage',
          'devices.pair_self',
          'audit.read',
        ],
      },
    ],
    ...overrides,
  };
}

/** Renders the whole admin app at `path` (under /admin) against mocked API routes; `seed` prepares the query
 * cache before the first render (a cache restored offline, a stale `me`). */
export function renderApp(path: string, routes: MockRoute[], seed?: (qc: QueryClient) => void) {
  const mock = createFetchMock(routes);
  const queryClient = createTestQueryClient();
  seed?.(queryClient);
  const api = createApiClient({
    fetch: mock.fetch,
    baseUrl: 'http://localhost',
    readCsrf: () => 'csrf-test',
  });
  const history = createMemoryHistory({ initialEntries: [`/admin${path}`] });
  const { router } = createApp({ queryClient, api, history });
  const i18n = initI18n({ lng: 'it' });
  const result = render(
    <I18nextProvider i18n={i18n}>
      <ThemeProvider defaultTheme="light">
        <QueryClientProvider client={queryClient}>
          <ApiProvider client={api}>
            <RouterProvider router={router} />
          </ApiProvider>
        </QueryClientProvider>
      </ThemeProvider>
    </I18nextProvider>,
  );
  return { ...result, router, calls: mock.calls, queryClient };
}

export const meRoute = (me: Me | null): MockRoute =>
  me
    ? { path: '/api/admin/v1/auth/me', body: me }
    : {
        path: '/api/admin/v1/auth/me',
        status: 401,
        body: { type: 'unauthorized', title: 'Unauthorized', status: 401 },
      };
