import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { PeripheralSettings } from '@porto/shared/desktop/hooks';
import { problem } from '@porto/shared/testing';

import { makeMe, meRoute, renderApp } from '../../test/renderApp';
import { remotePairSchema, toPairRequest } from './remote';
import { D, desktopRoutes, lanOn, meta, status } from './test/fixtures';

const peripherals: PeripheralSettings = {
  receipt: { driver: 'fake', width: 80 },
  label: { driver: 'none' },
  reader: { driver: 'keyboard' },
  fiscal: { driver: 'none' },
};

const bridge = {
  receipt: { driver: 'fake', ready: true, message: 'ready' },
  label: { driver: 'none', ready: false, message: 'not_configured' },
  reader: { driver: 'keyboard', ready: true, message: 'ready' },
  fiscal: { driver: 'none', ready: false, message: 'not_configured' },
};

const remoteStatus = status({
  mode: 'remote',
  kiosk: true,
  remote: {
    url: 'https://porto.example.it',
    marina: { id: 'm1', slug: 'online', name: 'Marina Online' },
    reachable: false,
    paired_at: '2026-11-01T09:00:00Z',
    version_mismatch: false,
  },
});

describe('remote pairing form', () => {
  it('prefers the link and validates the PIN', () => {
    const ok = remotePairSchema.safeParse({
      link: 'porto://pair?v=1&url=https%3A%2F%2Fx&code=ABCD-1234',
      url: '',
      code: '',
      fp: '',
      name: 'Banco',
      pin: '246810',
      confirm: '246810',
    });
    expect(ok.success).toBe(true);
    if (ok.success) {
      expect(toPairRequest('tok', ok.data)).toEqual({
        first_run_token: 'tok',
        pin: '246810',
        name: 'Banco',
        link: 'porto://pair?v=1&url=https%3A%2F%2Fx&code=ABCD-1234',
      });
    }
    const bad = remotePairSchema.safeParse({
      link: '',
      url: 'https://x',
      code: '',
      fp: '',
      name: '',
      pin: '12',
      confirm: '13',
    });
    expect(bad.success).toBe(false);
  });
});

describe('RemotePairPage', () => {
  it('pairs with the first-run token, the link and the PIN, then waits for the restart', async () => {
    const { calls, router } = renderApp('/desktop/remote#t=first-run-token', [
      ...desktopRoutes({ status: status({ first_run_pending: true }) }),
      { method: 'POST', path: `${D}/remote/pair`, status: 202 },
    ]);
    const page = await screen.findByTestId('remote-pair-page');
    await waitFor(() => expect(router.state.location.hash).toBe(''));
    await userEvent.type(
      within(page).getByLabelText('Collegamento'),
      'porto://pair?v=1&url=https%3A%2F%2Fporto.example.it&code=ABCD-1234',
    );
    await userEvent.type(within(page).getByLabelText("PIN dell'app"), '246810');
    await userEvent.type(within(page).getByLabelText('Ripeti il PIN'), '246810');
    await userEvent.click(within(page).getByRole('button', { name: 'Collega' }));
    expect(await screen.findByTestId('remote-pair-done')).toBeInTheDocument();
    const post = calls.find((c) => c.url.pathname === `${D}/remote/pair`);
    expect(post?.body).toMatchObject({
      first_run_token: 'first-run-token',
      pin: '246810',
      link: 'porto://pair?v=1&url=https%3A%2F%2Fporto.example.it&code=ABCD-1234',
    });
  });

  it('shows both fingerprints on a certificate mismatch', async () => {
    renderApp('/desktop/remote#t=tok', [
      ...desktopRoutes(),
      {
        method: 'POST',
        path: `${D}/remote/pair`,
        status: 502,
        body: {
          ...problem('remote_pin_mismatch', 502),
          details: { expected: 'aaaa', presented: 'bbbb' },
        },
      },
    ]);
    const page = await screen.findByTestId('remote-pair-page');
    await userEvent.type(
      within(page).getByLabelText('Indirizzo del Porto online'),
      'https://192.168.1.5:8443',
    );
    await userEvent.type(within(page).getByLabelText('Codice'), 'ABCD-1234');
    await userEvent.type(within(page).getByLabelText("PIN dell'app"), '246810');
    await userEvent.type(within(page).getByLabelText('Ripeti il PIN'), '246810');
    await userEvent.click(within(page).getByRole('button', { name: 'Collega' }));
    const alert = await screen.findByTestId('remote-pin-mismatch');
    expect(alert).toHaveTextContent('aaaa');
    expect(alert).toHaveTextContent('bbbb');
  });

  it('refuses without the first-run token', async () => {
    renderApp('/desktop/remote', desktopRoutes());
    expect(await screen.findByTestId('remote-pair-no-token')).toBeInTheDocument();
  });
});

describe('FirstRunPage (M6b)', () => {
  it('offers the remote pairing and says when the device was revoked', async () => {
    renderApp(
      '/desktop/first-run#t=tok&revoked=1',
      desktopRoutes({ status: status({ first_run_pending: true }) }),
    );
    expect(await screen.findByTestId('first-run-revoked')).toHaveTextContent(
      'Dispositivo revocato',
    );
    expect(screen.getByTestId('first-run-remote')).toHaveAttribute(
      'href',
      '/admin/desktop/remote#t=tok',
    );
  });
});

describe('KioskExitPage', () => {
  it('leaves kiosk mode with the app PIN in remote mode', async () => {
    const { calls } = renderApp('/desktop/kiosk-exit', [
      { path: '/api/admin/v1/meta', body: meta({ edition: 'server', local_mode: false }) },
      { path: `${D}/status`, body: remoteStatus },
      { method: 'POST', path: `${D}/kiosk/exit`, status: 204 },
    ]);
    const page = await screen.findByTestId('kiosk-exit-page');
    await userEvent.type(await within(page).findByLabelText("PIN dell'app"), '135790');
    await userEvent.click(within(page).getByRole('button', { name: 'Esci' }));
    expect(await screen.findByTestId('kiosk-exit-done')).toBeInTheDocument();
    const post = calls.find((c) => c.url.pathname === `${D}/kiosk/exit`);
    expect(post?.headers.get('Porto-App-Pin')).toBe('135790');
  });

  it('asks for the owner credentials in local mode', async () => {
    const { calls } = renderApp('/desktop/kiosk-exit', [
      ...desktopRoutes(),
      {
        method: 'POST',
        path: `${D}/kiosk/exit`,
        status: 401,
        body: problem('invalid_credentials', 401),
      },
    ]);
    const page = await screen.findByTestId('kiosk-exit-page');
    await userEvent.type(
      await within(page).findByLabelText('Email del titolare'),
      'owner@porto.test',
    );
    await userEvent.type(within(page).getByLabelText('Password del titolare'), 'secret');
    await userEvent.click(within(page).getByRole('button', { name: 'Esci' }));
    await waitFor(() => expect(calls.some((c) => c.url.pathname === `${D}/kiosk/exit`)).toBe(true));
    const post = calls.find((c) => c.url.pathname === `${D}/kiosk/exit`);
    expect(post?.body).toEqual({ email: 'owner@porto.test', password: 'secret' });
    expect(post?.headers.get('Porto-App-Pin')).toBeNull();
    expect(await within(page).findByRole('alert')).toBeInTheDocument();
  });
});

describe('DesktopSettingsPage (M6b)', () => {
  it('in remote mode shows the online instance and asks the PIN before testing a peripheral', async () => {
    const { calls } = renderApp('/desktop', [
      meRoute(makeMe()),
      { path: '/api/admin/v1/meta', body: meta({ edition: 'server', local_mode: false }) },
      { path: `${D}/status`, body: remoteStatus },
      { path: `${D}/peripherals`, body: peripherals },
      { path: '/api/bridge/v1/status', body: bridge },
      { method: 'POST', path: '/api/bridge/v1/test', status: 204 },
    ]);
    const remote = await screen.findByTestId('desktop-remote-card');
    expect(within(remote).getByText(/Marina Online/)).toBeInTheDocument();
    expect(within(remote).getByTestId('remote-unreachable')).toBeInTheDocument();
    expect(screen.queryByTestId('desktop-lan-card')).not.toBeInTheDocument();

    const card = await screen.findByTestId('desktop-peripherals-card');
    await userEvent.click(within(card).getAllByRole('button', { name: 'Prova' })[0]!);
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText("PIN dell'app"), '135790');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Conferma' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname === '/api/bridge/v1/test')).toBe(true),
    );
    const post = calls.find((c) => c.url.pathname === '/api/bridge/v1/test');
    expect(post?.headers.get('Porto-App-Pin')).toBe('135790');
    expect(post?.body).toEqual({ device: 'receipt' });
    expect(screen.getByTestId('kiosk-active')).toBeInTheDocument();
  });

  it('in local mode saves without a PIN and offers the CA download for a constrained CA', async () => {
    const { calls } = renderApp('/desktop', [
      meRoute(makeMe()),
      ...desktopRoutes({
        lan: {
          ...lanOn,
          ca_constrained: true,
          ca_download_url: 'https://192.168.1.20:8443/lan/porto-ca.crt',
        },
      }),
      { path: `${D}/peripherals`, body: peripherals },
      { path: '/api/bridge/v1/status', body: bridge },
      { method: 'PUT', path: `${D}/kiosk`, body: { enabled: true } },
    ]);
    const lan = await screen.findByTestId('lan-ca-download');
    expect(
      within(lan).getByRole('link', { name: 'Scarica il certificato per i telefoni' }),
    ).toHaveAttribute('href', 'https://192.168.1.20:8443/lan/porto-ca.crt');
    const kiosk = await screen.findByTestId('desktop-kiosk-card');
    await userEvent.click(within(kiosk).getByRole('switch'));
    await waitFor(() => expect(calls.some((c) => c.url.pathname === `${D}/kiosk`)).toBe(true));
    const put = calls.find((c) => c.url.pathname === `${D}/kiosk`);
    expect(put?.headers.get('Porto-App-Pin')).toBeNull();
    expect(put?.headers.get('X-CSRF-Token')).toBe('csrf-test');
    expect(await screen.findByTestId('desktop-peripherals-card')).toBeInTheDocument();
  });
});
