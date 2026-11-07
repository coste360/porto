import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { CommunicationPage, CommunicationsPage } from './CommunicationsPage';
import { ImportsPage } from './ImportsPage';
import { SignatureSettingsPage, SignaturesPage } from './SignaturesPage';
import { TemplatePage } from './TemplatePage';
import { TemplatesPage } from './TemplatesPage';

// Routes of the documents feature (m4-plan §8.1), children of the marina route. CONTRACT writes the paths; WEB
// owns the pages.

export function documentsRoutes(parent: MarinaRoute) {
  return [
    createRoute({ getParentRoute: () => parent, path: 'templates', component: TemplatesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'templates/$templateId',
      component: TemplatePage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'signatures', component: SignaturesPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'communications',
      component: CommunicationsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'communications/$commCampaignId',
      component: CommunicationPage,
    }),
    createRoute({ getParentRoute: () => parent, path: 'imports', component: ImportsPage }),
    createRoute({
      getParentRoute: () => parent,
      path: 'settings/signature',
      component: SignatureSettingsPage,
    }),
  ];
}
