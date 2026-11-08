import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, meRoute, renderApp } from './test/renderApp';

describe('router guards', () => {
  it('sends anonymous users to login, keeping where they were going', async () => {
    const { router } = renderApp(`/m/${MARINA_ID}/team`, [meRoute(null)]);
    expect(await screen.findByRole('button', { name: 'Accedi' })).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
    expect(router.state.location.search).toEqual({ redirect: `/m/${MARINA_ID}/team` });
  });

  it('forces TOTP enrolment when the policy requires it', async () => {
    const { router } = renderApp(`/m/${MARINA_ID}`, [
      meRoute(makeMe({ totp_enrolment_required: true })),
    ]);
    expect(await screen.findByText('Attiva la verifica in due passaggi')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/totp-setup');
  });

  it('redirects a single-marina user from the picker to the marina', async () => {
    const { router } = renderApp('/', [meRoute(makeMe())]);
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`));
    expect(await screen.findByRole('heading', { name: 'Marina Prova' })).toBeInTheDocument();
  });

  it('shows not found for a marina the user cannot access', async () => {
    renderApp('/m/other-marina', [meRoute(makeMe())]);
    expect(await screen.findByText('Pagina non trovata.')).toBeInTheDocument();
  });

  it('serves the QR pairing and desktop first-run pages without a session (M6a)', async () => {
    const pair = renderApp('/pair', [meRoute(null)]);
    expect(await screen.findByText('Collega questo dispositivo')).toBeInTheDocument();
    expect(pair.router.state.location.pathname).toBe('/pair');
    pair.unmount();
    const firstRun = renderApp('/desktop/first-run', [meRoute(null)]);
    expect(await screen.findByText('Benvenuto in Porto')).toBeInTheDocument();
    expect(firstRun.router.state.location.pathname).toBe('/desktop/first-run');
  });

  it('keeps the desktop settings behind the session guard (M6a)', async () => {
    const { router } = renderApp('/desktop', [meRoute(null)]);
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'));
  });

  it('shows the forbidden state when the role lacks the permission', async () => {
    renderApp(`/m/${MARINA_ID}/team`, [meRoute(makeMe({}, ['marina.read']))]);
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
  });
});
