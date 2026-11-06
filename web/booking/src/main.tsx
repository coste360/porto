import '@porto/shared/styles/globals.css';

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import { I18nextProvider, detectLocale, initI18n } from '@porto/shared/i18n';
import { ThemeProvider } from '@porto/shared/theme';

import { BookingPage } from './BookingPage';
import { slugFromPath } from './slug';

const lang = new URLSearchParams(window.location.search).get('lang');
const i18n = initI18n({ lng: detectLocale(lang), namespaces: ['common', 'errors', 'booking'] });
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
              <BookingPage slug={slugFromPath(window.location.pathname)} />
            </ApiProvider>
          </QueryClientProvider>
        </ThemeProvider>
      </I18nextProvider>
    </StrictMode>,
  );
}
