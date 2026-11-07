import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { Device, PairingCode } from '@porto/shared/api';
import { problem } from '@porto/shared/testing';

import { D, meta, status } from '../features/desktop/test/fixtures';
import { MARINA_ID, makeMe, meRoute, renderApp } from '../test/renderApp';

const DEVICES = `/api/admin/v1/marinas/${MARINA_ID}/devices`;
const CODES = `${DEVICES}/pairing-codes`;

const device = (over: Partial<Device>): Device => ({
  id: 'd1',
  name: 'Telefono banchina',
  platform: 'android',
  kind: 'mobile',
  access: 'browser',
  app_version: '',
  user: { id: 'u2', name: 'Ormeggiatore' },
  token_prefix: 'po_dev_ab',
  last_used_at: null,
  revoked_at: null,
  created_at: '2026-11-01T08:00:00Z',
  ...over,
});

const devices: Device[] = [
  device({}),
  device({
    id: 'd2',
    name: 'Tablet ufficio',
    platform: 'ios',
    access: 'token',
    app_version: '1.0.0',
    revoked_at: '2026-11-02T08:00:00Z',
  }),
];

function code(over: Partial<PairingCode> = {}): PairingCode {
  return {
    code: 'ABCD-EFGH',
    expires_at: new Date(Date.now() + 600_000).toISOString(),
    qr_payload: 'porto://pair?v=1',
    qr_svg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
    pair_url: 'https://192.168.1.20:8443/admin/pair#c=ABCD-EFGH&fp=abc',
    fingerprint: 'abc',
    reachable: true,
    ...over,
  };
}

async function openPairDialog() {
  await userEvent.click(await screen.findByRole('button', { name: 'Abbina un dispositivo' }));
  const dialog = await screen.findByRole('dialog');
  await userEvent.click(within(dialog).getByRole('button', { name: 'Genera il codice' }));
  return dialog;
}

describe('DevicesPage', () => {
  it('lists devices with the access column and state, and revokes one', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/devices`, [
      meRoute(makeMe()),
      { path: DEVICES, body: { items: devices } },
      { method: 'DELETE', path: `${DEVICES}/d1`, status: 204 },
    ]);
    expect(await screen.findByText('Telefono banchina')).toBeInTheDocument();
    const access = screen.getAllByTestId('device-access').map((n) => n.textContent);
    expect(access).toEqual(['Browser', 'App']);
    expect(screen.getByText('Revocato')).toBeInTheDocument();
    expect(screen.getByText('iPhone/iPad · 1.0.0')).toBeInTheDocument();

    // Only the active device can be revoked.
    const revoke = screen.getAllByRole('button', { name: 'Revoca' });
    expect(revoke).toHaveLength(1);
    await userEvent.click(revoke[0]!);
    const confirm = await screen.findByRole('alertdialog');
    await userEvent.click(within(confirm).getByRole('button', { name: 'Revoca' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true));
  });

  it('shows the QR of pair_url with code, fingerprint and the URL for tests', async () => {
    renderApp(`/m/${MARINA_ID}/devices`, [
      meRoute(makeMe()),
      { path: DEVICES, body: { items: [] } },
      { path: `/api/admin/v1/marinas/${MARINA_ID}/members`, body: { items: [] } },
      { method: 'POST', path: CODES, status: 201, body: code() },
    ]);
    const dialog = await openPairDialog();
    const panel = await within(dialog).findByTestId('pairing-panel');
    expect(panel).toHaveAttribute('data-pair-url', code().pair_url);
    expect(
      within(panel).getByRole('img', { name: 'Codice QR per abbinare il dispositivo' }),
    ).toBeInTheDocument();
    expect(within(panel).getByTestId('pairing-code')).toHaveTextContent('ABCD-EFGH');
    expect(within(panel).getByTestId('pairing-fingerprint')).toHaveTextContent('abc');
    expect(within(panel).getByRole('button', { name: 'Nuovo codice' })).toBeInTheDocument();
  });

  it('with the local network off shows the notice and no QR', async () => {
    renderApp(`/m/${MARINA_ID}/devices`, [
      meRoute(makeMe()),
      { path: DEVICES, body: { items: [] } },
      { path: `/api/admin/v1/marinas/${MARINA_ID}/members`, body: { items: [] } },
      { path: '/api/admin/v1/meta', body: meta() },
      { path: `${D}/status`, body: status() },
      {
        method: 'POST',
        path: CODES,
        status: 201,
        body: code({
          reachable: false,
          fingerprint: null,
          pair_url: 'http://127.0.0.1:18090/admin/pair#c=ABCD-EFGH',
        }),
      },
    ]);
    const dialog = await openPairDialog();
    const notice = await within(dialog).findByTestId('pair-unreachable');
    expect(notice).toHaveTextContent('Attiva la rete locale');
    expect(
      await within(notice).findByRole('link', { name: "Apri le impostazioni dell'app desktop" }),
    ).toHaveAttribute('href', '/admin/desktop');
    expect(within(dialog).queryByRole('img')).not.toBeInTheDocument();
    expect(within(dialog).queryByTestId('pairing-panel')).not.toBeInTheDocument();
  });

  it('shows a server error when the code cannot be created', async () => {
    renderApp(`/m/${MARINA_ID}/devices`, [
      meRoute(makeMe()),
      { path: DEVICES, body: { items: [] } },
      { path: `/api/admin/v1/marinas/${MARINA_ID}/members`, body: { items: [] } },
      { method: 'POST', path: CODES, status: 403, body: problem('forbidden', 403) },
    ]);
    const dialog = await openPairDialog();
    expect(await within(dialog).findByRole('alert')).toBeInTheDocument();
  });
});
