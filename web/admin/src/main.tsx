import '@porto/shared/styles/globals.css';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { detectLocale, initI18n } from '@porto/shared/i18n';

import { App } from './App';

const i18n = initI18n({ lng: detectLocale() });
const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App i18n={i18n} />
    </StrictMode>,
  );
}
