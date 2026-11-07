import '@porto/shared/styles/globals.css';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import { I18nextProvider, detectLocale } from '@porto/shared/i18n';
import { ThemeProvider } from '@porto/shared/theme';

import { BookingPage } from './BookingPage';
import { initBookingI18n } from './i18n';
import { LinkPage } from './links';
import { PayPage } from './pay';
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
              {route.page === 'link' ? (
                <LinkPage slug={route.slug} purpose={route.purpose} token={route.token} />
              ) : route.page === 'pay' ? (
                <PayPage slug={route.slug} token={route.token} ret={route.ret} />
              ) : (
                <BookingPage slug={route.slug} />
              )}
            </ApiProvider>
          </QueryClientProvider>
        </ThemeProvider>
      </I18nextProvider>
    </StrictMode>,
  );
}
