import '@porto/shared/styles/globals.css';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { StrictMode, Suspense } from 'react';
import { createRoot } from 'react-dom/client';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import { I18nextProvider, detectLocale } from '@porto/shared/i18n';
import { ThemeProvider } from '@porto/shared/theme';

import { initBookingI18n } from './i18n';
import { Page } from './Page';
import { registerBookingServiceWorker } from './pwa';
import { routeFromPath } from './routes';
import { slugFromPath } from './slug';

const lang = new URLSearchParams(window.location.search).get('lang');
const i18n = initBookingI18n(detectLocale(lang));
const route = routeFromPath(window.location.pathname, slugFromPath);
const queryClient = new QueryClient();
const api = createApiClient();

const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <QueryClientProvider client={queryClient}>
            <ApiProvider client={api}>
              <Suspense fallback={null}>
                <Page route={route} />
              </Suspense>
            </ApiProvider>
          </QueryClientProvider>
        </ThemeProvider>
      </I18nextProvider>
    </StrictMode>,
  );
}

if (import.meta.env.PROD) registerBookingServiceWorker();
