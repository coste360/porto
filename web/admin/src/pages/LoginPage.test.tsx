import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { problem } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../test/renderApp';

async function fillCredentials(email = 'owner@porto.test', password = 'prova-password-123') {
  await userEvent.type(await screen.findByLabelText('Email'), email);
  await userEvent.type(screen.getByLabelText('Password'), password);
  await userEvent.click(screen.getByRole('button', { name: 'Accedi' }));
}

describe('LoginPage', () => {
  it('signs in and goes to the marina', async () => {
    const me = makeMe();
    const { router, calls } = renderApp('/login', [
      meRoute(null),
      { method: 'POST', path: '/api/admin/v1/auth/login', body: me },
    ]);
    await fillCredentials();
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`));
    const login = calls.find((c) => c.url.pathname === '/api/admin/v1/auth/login');
    expect(login?.body).toEqual({ email: 'owner@porto.test', password: 'prova-password-123' });
    expect(login?.headers.get('X-CSRF-Token')).toBe('csrf-test');
  });

  it('honours the redirect parameter', async () => {
    const { router } = renderApp(`/login?redirect=${encodeURIComponent(`/m/${MARINA_ID}/audit`)}`, [
      meRoute(null),
      { method: 'POST', path: '/api/admin/v1/auth/login', body: makeMe() },
    ]);
    await fillCredentials();
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}/audit`));
  });

  it('shows invalid credentials', async () => {
    renderApp('/login', [
      meRoute(null),
      {
        method: 'POST',
        path: '/api/admin/v1/auth/login',
        status: 401,
        body: problem('invalid_credentials', 401),
      },
    ]);
    await fillCredentials('owner@porto.test', 'wrong');
    expect(await screen.findByText('Email o password non corretti.')).toBeInTheDocument();
  });

  it('validates the fields before calling the API', async () => {
    const { calls } = renderApp('/login', [meRoute(null)]);
    await userEvent.click(await screen.findByRole('button', { name: 'Accedi' }));
    expect(await screen.findAllByText('Campo obbligatorio.')).toHaveLength(2);
    expect(calls.some((c) => c.url.pathname.endsWith('/auth/login'))).toBe(false);
  });

  it('asks for the TOTP code when required, then signs in', async () => {
    let attempts = 0;
    const me = makeMe();
    const { router, calls } = renderApp('/login', [
      meRoute(null),
      {
        method: 'POST',
        path: (url) => url.pathname === '/api/admin/v1/auth/login' && attempts++ === 0,
        status: 401,
        body: problem('totp_required', 401),
      },
      { method: 'POST', path: '/api/admin/v1/auth/login', body: me },
    ]);
    await fillCredentials();
    expect(await screen.findByText('Verifica in due passaggi')).toBeInTheDocument();
    await userEvent.keyboard('123456');
    await userEvent.click(screen.getByRole('button', { name: 'Verifica' }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}`));
    const last = calls.filter((c) => c.url.pathname.endsWith('/auth/login')).at(-1);
    expect(last?.body).toEqual({
      email: 'owner@porto.test',
      password: 'prova-password-123',
      totp_code: '123456',
    });
  });

  it('accepts a recovery code instead of the TOTP code', async () => {
    let attempts = 0;
    const { calls } = renderApp('/login', [
      meRoute(null),
      {
        method: 'POST',
        path: (url) => url.pathname === '/api/admin/v1/auth/login' && attempts++ === 0,
        status: 401,
        body: problem('totp_required', 401),
      },
      { method: 'POST', path: '/api/admin/v1/auth/login', body: makeMe() },
    ]);
    await fillCredentials();
    await userEvent.click(await screen.findByRole('button', { name: 'Usa un codice di recupero' }));
    await userEvent.type(screen.getByLabelText('Codice di recupero'), 'abcd-efgh');
    await userEvent.click(screen.getByRole('button', { name: 'Verifica' }));
    await waitFor(() => {
      const last = calls.filter((c) => c.url.pathname.endsWith('/auth/login')).at(-1);
      expect(last?.body).toMatchObject({ recovery_code: 'abcd-efgh' });
    });
  });
});
