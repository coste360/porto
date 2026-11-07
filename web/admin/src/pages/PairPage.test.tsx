import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PairSessionResult, Permission } from '@porto/shared/api';
import { problem } from '@porto/shared/testing';

import { detectDevice, normalizeCode, parsePairFragment } from '../features/desktop/pair';
import { MARINA_ID, makeMe, renderApp } from '../test/renderApp';

const PAIR = '/api/device/v1/pair-session';

function result(
  permissions: Permission[],
  kind: 'mobile' | 'desktop' = 'mobile',
  totpRequired = false,
): PairSessionResult {
  return {
    device: {
      id: 'd1',
      name: 'Telefono Android',
      platform: 'android',
      kind,
      access: 'browser',
      app_version: '',
      user: { id: 'u2', name: 'Ormeggiatore' },
      token_prefix: '',
      last_used_at: null,
      revoked_at: null,
      created_at: '2026-11-07T08:00:00Z',
    },
    marina: { id: MARINA_ID, slug: 'prova', name: 'Marina Prova' },
    user: { id: 'u2', name: 'Ormeggiatore' },
    permissions,
    totp_required: totpRequired,
  } as PairSessionResult;
}

const ANDROID =
  'Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36';

afterEach(() => vi.restoreAllMocks());

describe('PairPage', () => {
  it('reads the code from the fragment, clears it and lands on the dock panel', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(ANDROID);
    const { calls, router } = renderApp('/pair#c=abcd-efgh&fp=Zm9vYmFy', [
      { method: 'POST', path: PAIR, status: 201, body: result(['presence.write', 'map.view']) },
      {
        path: '/api/admin/v1/auth/me',
        body: makeMe({}, ['marina.read', 'presence.write', 'map.view']),
      },
    ]);
    await screen.findByTestId('pair-page');
    await waitFor(() => expect(router.state.location.hash).toBe(''));
    expect(screen.getByTestId('pair-fingerprint')).toHaveTextContent('Zm9vYmFy');
    // The code arrived with the QR: no input for it.
    expect(screen.queryByLabelText('Codice')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Nome del dispositivo')).toHaveValue('Telefono Android');

    await userEvent.click(screen.getByRole('button', { name: 'Collega' }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}/dock`));
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.body).toEqual({
      code: 'ABCD-EFGH',
      name: 'Telefono Android',
      platform: 'android',
      kind: 'mobile',
    });
    expect(post?.url.search).toBe('');
    expect(post?.headers.get('Idempotency-Key')).toBeNull();
  });

  it('without permissions for the quay lands on the marina overview', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(ANDROID);
    const { router } = renderApp('/pair#c=ABCDEFGH', [
      { method: 'POST', path: PAIR, status: 201, body: result(['customers.read']) },
      { path: '/api/admin/v1/auth/me', body: makeMe({}, ['marina.read', 'customers.read']) },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Collega' }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`));
  });

  it('asks a user with two-step verification for the code before landing', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(ANDROID);
    const { calls, router } = renderApp('/pair#c=ABCD-EFGH', [
      { method: 'POST', path: PAIR, status: 201, body: result(['presence.write'], 'mobile', true) },
      { method: 'POST', path: `${PAIR}/totp`, status: 204 },
      { path: '/api/admin/v1/auth/me', body: makeMe({}, ['marina.read', 'presence.write']) },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Collega' }));
    await screen.findByTestId('pair-totp');
    expect(router.state.location.pathname).toBe('/pair');
    await userEvent.type(screen.getByLabelText('Codice di verifica'), '12a3456');
    await userEvent.click(screen.getByRole('button', { name: 'Verifica' }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}/dock`));
    const verify = calls.find((c) => c.url.pathname === `${PAIR}/totp`);
    expect(verify?.body).toEqual({ totp_code: '123456' });
  });

  it('explains an expired or used code', async () => {
    renderApp('/pair#c=ABCD-EFGH', [
      { method: 'POST', path: PAIR, status: 400, body: problem('pairing_code_invalid', 400) },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Collega' }));
    expect(await screen.findByTestId('pair-invalid')).toHaveTextContent(
      'Codice scaduto o già usato: chiedi un nuovo QR.',
    );
  });

  it('without a fragment asks for the code and validates it', async () => {
    const { calls } = renderApp('/pair', [
      { method: 'POST', path: PAIR, status: 201, body: result([]) },
    ]);
    const input = await screen.findByLabelText('Codice');
    await userEvent.type(input, 'abc');
    await userEvent.click(screen.getByRole('button', { name: 'Collega' }));
    expect(await screen.findByText(/Inserisci il codice di 8 caratteri/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
    await userEvent.type(input, 'd efgh');
    expect(input).toHaveValue('ABCD-EFGH');
  });
});

describe('pairing helpers', () => {
  it('parses the fragment', () => {
    expect(parsePairFragment('#c=abcd-efgh&fp=xyz')).toEqual({
      code: 'ABCD-EFGH',
      fingerprint: 'xyz',
    });
    expect(parsePairFragment('')).toEqual({ code: '', fingerprint: '' });
  });
  it('normalises codes', () => {
    expect(normalizeCode(' ab cd ef gh ij')).toBe('ABCD-EFGH');
    expect(normalizeCode('ab')).toBe('AB');
  });
  it('detects the platform', () => {
    expect(detectDevice(ANDROID)).toEqual({ platform: 'android', kind: 'mobile' });
    expect(
      detectDevice('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15'),
    ).toEqual({ platform: 'ios', kind: 'mobile' });
    expect(detectDevice('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')).toEqual({
      platform: 'windows',
      kind: 'desktop',
    });
    expect(detectDevice('')).toEqual({ platform: 'other', kind: 'desktop' });
  });
});
