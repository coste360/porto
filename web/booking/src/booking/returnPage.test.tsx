import { screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import { createFetchMock, renderWithProviders, type MockRoute } from '@porto/shared/testing';

import { config, SLUG } from '../test/fixtures';
import ReturnPage from './ReturnPage';
import { parseReturnFragment } from './returnFragment';

const P = `/api/public/v1/${SLUG}`;
const ID = 'bk-0001-aaaa';
const marina = {
  slug: SLUG,
  name: 'Marina Prova',
  default_locale: 'it',
  locales: ['it'],
  booking_enabled: true,
  embed_origins: ['http://localhost:18081'],
};

function render(statusBodies: unknown[], extra: MockRoute[] = []) {
  let n = 0;
  const routes: MockRoute[] = [
    { path: P, body: marina },
    { path: `${P}/booking/config`, body: config },
    ...extra,
  ];
  const mock = createFetchMock(routes);
  const fetchImpl: typeof fetch = async (input, init) => {
    const req = input instanceof Request ? input : new Request(String(input), init);
    if (new URL(req.url).pathname === `${P}/bookings/${ID}/status`) {
      mock.calls.push({
        method: 'GET',
        url: new URL(req.url),
        headers: req.headers,
        body: undefined,
      });
      const body = statusBodies[Math.min(n++, statusBodies.length - 1)];
      return new Response(JSON.stringify(body), {
        headers: { 'Content-Type': 'application/json' },
      });
    }
    return mock.fetch(input, init);
  };
  const api = createApiClient({ fetch: fetchImpl, baseUrl: 'http://localhost' });
  renderWithProviders(
    <ApiProvider client={api}>
      <ReturnPage slug={SLUG} bookingId={ID} />
    </ApiProvider>,
  );
  return mock;
}

const st = (status: string, verifying = false) => ({
  booking_id: ID,
  reference: 'K7M2Q9XA',
  status,
  verifying,
});

describe('return page', () => {
  beforeEach(() => window.sessionStorage.clear());

  it('parses the fragment', () => {
    expect(parseReturnFragment('#st=abc&rt=http%3A%2F%2Flocalhost%3A18081%2F&cancelled=1')).toEqual(
      {
        statusToken: 'abc',
        returnTo: 'http://localhost:18081/',
        cancelled: true,
      },
    );
    expect(parseReturnFragment('')).toEqual({
      statusToken: null,
      returnTo: null,
      cancelled: false,
    });
  });

  it('polls with the status token until confirmed, wipes the fragment and offers the way back', async () => {
    window.history.replaceState(
      null,
      '',
      `/b/${SLUG}/r/${ID}#st=status-tok&rt=${encodeURIComponent('http://localhost:18081/host.html')}`,
    );
    const mock = render([st('pending_payment', true), st('confirmed')]);
    expect(await screen.findByText('Pagamento in verifica…')).toBeInTheDocument();
    expect(window.location.hash).toBe('');
    const outcome = await screen.findByTestId('booking-outcome', {}, { timeout: 6000 });
    await waitFor(() => expect(outcome).toHaveAttribute('data-outcome', 'confirmed'), {
      timeout: 6000,
    });
    expect(within(outcome).getByText('Prenotazione confermata')).toBeInTheDocument();
    expect(within(outcome).getByTestId('booking-reference')).toHaveTextContent('K7M2Q9XA');
    const back = await screen.findByTestId('return-back');
    await waitFor(() => expect(back).toHaveAttribute('href', 'http://localhost:18081/host.html'));
    expect(back).toHaveTextContent('Torna al sito');
    const call = mock.calls.find((c) => c.url.pathname.endsWith('/status'))!;
    expect(call.headers.get('Porto-Status-Token')).toBe('status-tok');
    expect(call.url.href).not.toContain('status-tok');
  });

  it('hides return_to when its origin is not allowed', async () => {
    window.history.replaceState(
      null,
      '',
      `/b/${SLUG}/r/${ID}#st=s&rt=${encodeURIComponent('https://evil.example/')}`,
    );
    render([st('awaiting_approval')]);
    const outcome = await screen.findByTestId('booking-outcome');
    expect(outcome).toHaveAttribute('data-outcome', 'received');
    expect(screen.getByTestId('return-back')).toHaveAttribute('href', `/b/${SLUG}`);
  });

  it('shows the cancelled checkout', async () => {
    window.history.replaceState(null, '', `/b/${SLUG}/r/${ID}#st=s&cancelled=1`);
    render([st('held')]);
    const outcome = await screen.findByTestId('booking-outcome');
    expect(outcome).toHaveAttribute('data-outcome', 'cancelled');
  });

  it('without any token shows the invalid link page', async () => {
    window.history.replaceState(null, '', `/b/${SLUG}/r/${ID}`);
    render([]);
    expect(await screen.findByText('Prenotazione non trovata')).toBeInTheDocument();
  });
});
