import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import {
  createFetchMock,
  problem,
  renderWithProviders,
  type MockRoute,
} from '@porto/shared/testing';

import { AccountLinkPage, AccountPage } from './index';
import { fragmentToken, readSession, writeSession } from './session';

const SLUG = 'marina-test';
const P = `/api/public/v1/${SLUG}`;
const TOKEN = 'A'.repeat(52);

function render(ui: React.ReactElement, routes: MockRoute[]) {
  const mock = createFetchMock(routes);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  renderWithProviders(<ApiProvider client={api}>{ui}</ApiProvider>);
  return mock;
}

describe('customer area login', () => {
  beforeEach(() => window.sessionStorage.clear());

  it('reads only well-formed fragment tokens', () => {
    expect(fragmentToken(`#t=${TOKEN}`)).toBe(TOKEN);
    expect(fragmentToken('#t=<script>')).toBeNull();
    expect(fragmentToken('')).toBeNull();
  });

  it('answers the same way for any email and never says whether it is known', async () => {
    const mock = render(<AccountPage slug={SLUG} contractId={null} />, [
      { method: 'POST', path: `${P}/account/login`, status: 202, body: { status: 'accepted' } },
    ]);
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'anna@example.test' } });
    fireEvent.click(screen.getByTestId('account-login-submit'));
    expect(await screen.findByTestId('account-link-sent')).toHaveTextContent(
      "Se l'indirizzo è registrato",
    );
    expect(mock.calls[0]?.body).toEqual({ email: 'anna@example.test', locale: 'it' });
  });

  it('validates the email before calling the server', async () => {
    const mock = render(<AccountPage slug={SLUG} contractId={null} />, []);
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'nope' } });
    fireEvent.click(screen.getByTestId('account-login-submit'));
    await waitFor(() =>
      expect(screen.getByLabelText('Email')).toHaveAttribute('aria-invalid', 'true'),
    );
    expect(mock.calls).toHaveLength(0);
  });

  it('exchanges the fragment token, wipes it from the address bar and stores the session', async () => {
    window.history.replaceState(null, '', `/b/${SLUG}/a#t=${TOKEN}`);
    const mock = render(<AccountLinkPage slug={SLUG} />, [
      {
        method: 'POST',
        path: `${P}/account/session`,
        status: 201,
        body: { session_token: 'S'.repeat(52), expires_at: '2999-01-01T00:00:00Z' },
      },
      { path: `${P}/account/bookings`, body: { items: [] } },
    ]);
    expect(window.location.hash).toBe('');
    expect(await screen.findByText('Non ci sono prenotazioni.')).toBeInTheDocument();
    expect(window.location.pathname).toBe(`/b/${SLUG}/account`);
    expect(mock.calls[0]?.body).toEqual({ token: TOKEN });
    expect(readSession(SLUG)).toBe('S'.repeat(52));
    const list = mock.calls.find((c) => c.url.pathname === `${P}/account/bookings`);
    expect(list?.headers.get('Porto-Customer-Session')).toBe('S'.repeat(52));
  });

  it('shows the invalid link page for a used or expired token', async () => {
    window.history.replaceState(null, '', `/b/${SLUG}/a#t=${TOKEN}`);
    render(<AccountLinkPage slug={SLUG} />, [
      {
        method: 'POST',
        path: `${P}/account/session`,
        status: 404,
        body: problem('link_invalid', 404),
      },
    ]);
    expect(await screen.findByText('Link non valido')).toBeInTheDocument();
    expect(window.location.hash).toBe('');
    expect(readSession(SLUG)).toBeNull();
  });

  it('drops an expired session and asks to sign in again', async () => {
    writeSession(SLUG, { token: 'S'.repeat(52), expiresAt: '2999-01-01T00:00:00Z' });
    render(<AccountPage slug={SLUG} contractId={null} />, [
      { path: `${P}/account/bookings`, status: 404, body: problem('link_invalid', 404) },
    ]);
    expect(await screen.findByText('La sessione è scaduta: accedi di nuovo.')).toBeInTheDocument();
    expect(readSession(SLUG)).toBeNull();
  });

  it('ignores a stored session past its absolute expiry', () => {
    writeSession(SLUG, { token: 'S'.repeat(52), expiresAt: '2000-01-01T00:00:00Z' });
    expect(readSession(SLUG)).toBeNull();
  });
});
