import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { problem } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { firstRunSchema } from './schemas';
import { D, backups, desktopRoutes, lanOn, meta, status } from './test/fixtures';

describe('DesktopSettingsPage', () => {
  it('switches the local network on with the chosen address and port', async () => {
    const { calls } = renderApp('/desktop', [
      meRoute(makeMe()),
      ...desktopRoutes(),
      { method: 'PUT', path: `${D}/lan`, body: lanOn },
    ]);
    const card = await screen.findByTestId('desktop-lan-card');
    expect(within(card).getByText(/La rete locale è spenta/)).toBeInTheDocument();
    await userEvent.click(within(card).getByRole('switch', { name: 'Attiva la rete locale' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.body).toEqual({ enabled: true, port: 8443, address: '' });
    expect(put?.headers.get('X-CSRF-Token')).toBe('csrf-test');
    // The form remounts on the applied address: query the card again.
    expect(await screen.findByTestId('lan-url')).toHaveTextContent('https://192.168.1.20:8443');
    expect(screen.getByText(/I telefoni e gli altri computer funzionano solo/)).toBeVisible();
  });

  it('shows the problem when the port is taken', async () => {
    renderApp('/desktop', [
      meRoute(makeMe()),
      ...desktopRoutes(),
      {
        method: 'PUT',
        path: `${D}/lan`,
        status: 409,
        body: problem('lan_port_unavailable', 409),
      },
    ]);
    const card = await screen.findByTestId('desktop-lan-card');
    await userEvent.click(within(card).getByRole('switch', { name: 'Attiva la rete locale' }));
    expect(
      await within(card).findByText(/La porta della rete locale è già in uso/),
    ).toBeInTheDocument();
  });

  it('lists the backups, takes one now and restores one after confirmation', async () => {
    const { calls } = renderApp('/desktop', [
      meRoute(makeMe()),
      ...desktopRoutes(),
      { method: 'POST', path: `${D}/backups`, status: 201, body: backups.items[0] },
      {
        method: 'POST',
        path: `${D}/backups/${backups.items[1]!.id}/restore`,
        status: 202,
      },
    ]);
    const card = await screen.findByTestId('desktop-backup-card');
    expect(await within(card).findByText('Giornaliero')).toBeInTheDocument();
    expect(within(card).getByText('Chiusura')).toBeInTheDocument();
    expect(within(card).getByText(/12,3\s?MB/)).toBeInTheDocument();

    await userEvent.click(within(card).getByRole('button', { name: 'Fai un backup ora' }));
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.pathname === `${D}/backups`)).toBe(
        true,
      ),
    );

    const rows = within(card).getAllByRole('row');
    await userEvent.click(within(rows[2]!).getByRole('button', { name: 'Ripristina' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/Prima del ripristino viene fatto un backup/)).toBeVisible();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Ripristina' }));
    expect(await screen.findByTestId('desktop-restarting')).toBeInTheDocument();
    expect(calls.some((c) => c.url.pathname.endsWith('-exit.tar.gz/restore'))).toBe(true);
  });

  it('reveals the secret key only after the password, and forgets it on close', async () => {
    const { calls } = renderApp('/desktop', [
      meRoute(makeMe()),
      ...desktopRoutes({ status: status({ secret_store: 'file' }) }),
      {
        method: 'POST',
        path: `${D}/secret-key/reveal`,
        body: { secret_key: 'k'.repeat(43) },
      },
    ]);
    const card = await screen.findByTestId('desktop-secret-card');
    expect(within(card).getByTestId('secret-file-fallback')).toBeInTheDocument();
    await userEvent.click(within(card).getByRole('button', { name: 'Mostra la chiave' }));
    const dialog = await screen.findByRole('dialog');
    const submit = within(dialog).getByRole('button', { name: 'Mostra la chiave' });
    expect(submit).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText('Conferma la password'), 'password-segreta');
    await userEvent.click(submit);
    expect(await within(dialog).findByDisplayValue('k'.repeat(43))).toBeInTheDocument();
    expect(calls.find((c) => c.url.pathname === `${D}/secret-key/reveal`)?.body).toEqual({
      password: 'password-segreta',
    });
    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await userEvent.click(within(card).getByRole('button', { name: 'Mostra la chiave' }));
    expect(await screen.findByLabelText('Conferma la password')).toHaveValue('');
    expect(screen.queryByDisplayValue('k'.repeat(43))).not.toBeInTheDocument();
  });

  it('shows invalid_credentials on a wrong password', async () => {
    renderApp('/desktop', [
      meRoute(makeMe()),
      ...desktopRoutes(),
      {
        method: 'POST',
        path: `${D}/secret-key/reveal`,
        status: 400,
        body: problem('invalid_credentials', 400),
      },
    ]);
    const card = await screen.findByTestId('desktop-secret-card');
    await userEvent.click(within(card).getByRole('button', { name: 'Mostra la chiave' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Conferma la password'), 'sbagliata');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Mostra la chiave' }));
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });

  it('is only in the app window: a local-network browser gets an explanation', async () => {
    renderApp('/desktop', [
      meRoute(makeMe()),
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, status: 404, body: problem('not_found', 404) },
    ]);
    expect(
      await screen.findByText(/disponibili solo nella finestra dell'app desktop/),
    ).toBeInTheDocument();
    expect(screen.queryByTestId('desktop-lan-card')).not.toBeInTheDocument();
  });

  it('is owner only', async () => {
    renderApp('/desktop', [meRoute(makeMe({ organizations: [] })), ...desktopRoutes()]);
    await waitFor(() => expect(screen.queryByTestId('desktop-lan-card')).not.toBeInTheDocument());
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
  });
});

describe('FirstRunPage', () => {
  it('validates the owner form before calling the server', async () => {
    const { calls } = renderApp('/desktop/first-run#t=first-run-token', [
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, body: status({ first_run_pending: true }) },
    ]);
    const page = await screen.findByTestId('first-run-page');
    await userEvent.click(await within(page).findByRole('button', { name: "Crea l'account" }));
    expect(await within(page).findAllByText('Campo obbligatorio.')).not.toHaveLength(0);
    await userEvent.type(within(page).getByLabelText("Nome dell'azienda"), 'Porto Prova SRL');
    await userEvent.type(within(page).getByLabelText('Il tuo nome'), 'Titolare');
    await userEvent.type(within(page).getByLabelText('Email'), 'titolare@porto.test');
    await userEvent.type(within(page).getByLabelText('Password'), 'corta');
    await userEvent.type(within(page).getByLabelText('Ripeti la password'), 'diversa');
    await userEvent.click(within(page).getByRole('button', { name: "Crea l'account" }));
    expect(
      await within(page).findByText('La password deve avere almeno 12 caratteri.'),
    ).toBeInTheDocument();
    expect(within(page).getByText('Le password non coincidono.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('creates the owner with the fragment token and clears the fragment', async () => {
    const { calls, router } = renderApp('/desktop/first-run#t=first-run-token', [
      meRoute(null),
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, body: status({ first_run_pending: true }) },
      { method: 'POST', path: `${D}/first-run`, status: 201, body: { user_id: 'u1' } },
    ]);
    const page = await screen.findByTestId('first-run-page');
    await waitFor(() => expect(router.state.location.hash).toBe(''));
    await userEvent.type(
      await within(page).findByLabelText("Nome dell'azienda"),
      'Porto Prova SRL',
    );
    await userEvent.type(within(page).getByLabelText('Il tuo nome'), 'Titolare');
    await userEvent.type(within(page).getByLabelText('Email'), 'titolare@porto.test');
    await userEvent.type(within(page).getByLabelText('Password'), 'una-password-lunga');
    await userEvent.type(within(page).getByLabelText('Ripeti la password'), 'una-password-lunga');
    await userEvent.click(within(page).getByRole('checkbox'));
    await userEvent.click(within(page).getByRole('button', { name: "Crea l'account" }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
      token: 'first-run-token',
      organization: 'Porto Prova SRL',
      name: 'Titolare',
      email: 'titolare@porto.test',
      password: 'una-password-lunga',
      update_check: false,
    });
  });

  it('without a token explains the link is not valid', async () => {
    renderApp('/desktop/first-run', [
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, body: status({ first_run_pending: true }) },
    ]);
    expect(await screen.findByTestId('first-run-no-token')).toBeInTheDocument();
  });

  it('rejects mismatching passwords in the schema', () => {
    const r = firstRunSchema.safeParse({
      organization: 'O',
      name: 'N',
      email: 'a@b.it',
      password: 'abcdefghijkl',
      confirm: 'abcdefghijkm',
      update_check: true,
    });
    expect(r.success).toBe(false);
    expect(r.error?.issues[0]?.message).toBe('common:validation.passwordMismatch');
  });
});

describe('local mode', () => {
  it('payment settings show the notice with the move-online link', async () => {
    renderApp(`/m/${MARINA_ID}/settings/payments`, [
      meRoute(makeMe({}, ['marina.read', 'providers.configure'])),
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `/api/admin/v1/marinas/${MARINA_ID}/payment-providers`, body: { items: [] } },
    ]);
    const notice = await screen.findByTestId('local-mode-notice');
    expect(notice).toHaveAttribute('data-feature', 'onlinePayments');
    expect(
      within(notice).getByRole('link', { name: 'Come passare a un server online' }),
    ).toHaveAttribute('href', '/admin/desktop/online');
  });

  it('server edition shows no notice', async () => {
    renderApp(`/m/${MARINA_ID}/settings`, [
      meRoute(makeMe()),
      { path: '/api/admin/v1/meta', body: meta({ edition: 'server', local_mode: false }) },
    ]);
    await screen.findByRole('heading', { name: 'Impostazioni del porto' });
    expect(screen.queryByTestId('local-mode-notice')).not.toBeInTheDocument();
  });

  it('move-online guide: steps for everybody, buttons only for the owner in the window', async () => {
    renderApp('/desktop/online', [
      meRoute(makeMe({ organizations: [] })),
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, status: 404, body: problem('not_found', 404) },
    ]);
    expect(await screen.findByTestId('online-step-server')).toHaveTextContent('porto restore');
    expect(screen.queryByRole('button', { name: 'Fai un backup ora' })).not.toBeInTheDocument();
  });

  it('move-online guide: owner in the window can back up now', async () => {
    renderApp('/desktop/online', [meRoute(makeMe()), ...desktopRoutes()]);
    expect(await screen.findByRole('button', { name: 'Fai un backup ora' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Mostra la chiave' })).toHaveAttribute(
      'href',
      '/admin/desktop',
    );
  });
});

describe('UpdateBanner and user menu', () => {
  it('shows the update banner in the window and dismisses it for that version', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}`, [
      meRoute(makeMe()),
      ...desktopRoutes({
        status: status({ update: { version: '1.2.0', url: 'https://example.test/porto' } }),
      }),
      { method: 'PATCH', path: `${D}/settings`, body: {} },
    ]);
    const banner = await screen.findByTestId('update-banner');
    expect(banner).toHaveTextContent('È disponibile Porto 1.2.0.');
    await userEvent.click(within(banner).getByRole('button', { name: 'Non ora' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({
        dismissed_update: '1.2.0',
      }),
    );
  });

  it('no banner without an update, and none outside the window', async () => {
    renderApp(`/m/${MARINA_ID}`, [
      meRoute(makeMe()),
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, status: 404, body: problem('not_found', 404) },
    ]);
    await screen.findByTestId('dashboard-local-mode-card');
    expect(screen.queryByTestId('update-banner')).not.toBeInTheDocument();
  });

  it('the user menu has the desktop entry for the owner in the window', async () => {
    renderApp('/account', [meRoute(makeMe()), ...desktopRoutes()]);
    await userEvent.click(await screen.findByRole('button', { name: /Menu utente|Owner Prova/ }));
    expect(await screen.findByRole('menuitem', { name: 'App desktop' })).toBeInTheDocument();
  });
});
