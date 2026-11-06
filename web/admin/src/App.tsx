import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import type { i18n as I18n } from 'i18next';
import { useState } from 'react';

import { ApiProvider } from '@porto/shared/api';
import { I18nextProvider } from '@porto/shared/i18n';
import { ThemeProvider } from '@porto/shared/theme';

import { createApp, type CreateAppOptions } from './bootstrap';

export type AppProps = CreateAppOptions & { i18n: I18n };

export function App({ i18n, ...options }: AppProps) {
  const [{ queryClient, api, router }] = useState(() => createApp(options));
  return (
    <I18nextProvider i18n={i18n}>
      <ThemeProvider>
        <QueryClientProvider client={queryClient}>
          <ApiProvider client={api}>
            <RouterProvider router={router} />
          </ApiProvider>
        </QueryClientProvider>
      </ThemeProvider>
    </I18nextProvider>
  );
}
