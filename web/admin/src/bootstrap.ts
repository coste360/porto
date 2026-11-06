import { QueryClient } from '@tanstack/react-query';
import type { RouterHistory } from '@tanstack/react-router';

import { createApiClient, queryKeys, type ApiClient } from '@porto/shared/api';

import { createAppRouter, type AppRouter } from './router';

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 15_000,
        // Problems such as 403/404 are answers: do not retry them.
        retry: (count, error) =>
          count < 2 &&
          !(error instanceof Error && 'status' in error && Number(error.status) >= 400),
      },
    },
  });
}

export type CreateAppOptions = {
  queryClient?: QueryClient;
  api?: ApiClient;
  history?: RouterHistory;
};

/** Builds the API client, query client and router, wired so a 401 sends the user to login. */
export function createApp({ queryClient = createQueryClient(), api, history }: CreateAppOptions) {
  let router: AppRouter | null = null;
  const client =
    api ??
    createApiClient({
      onUnauthorized: () => {
        queryClient.setQueryData(queryKeys.me, null);
        const current = router?.state.location.href;
        void router?.navigate({
          to: '/login',
          search: current && !current.startsWith('/login') ? { redirect: current } : {},
        });
      },
    });
  router = createAppRouter({ queryClient, api: client }, history);
  return { queryClient, api: client, router };
}
