import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { ApiProvider, createApiClient, type PayPage as PayPageData } from '@porto/shared/api';
import {
  createFetchMock,
  problem,
  renderWithProviders,
  type MockRoute,
} from '@porto/shared/testing';

import { PayPage } from './index';
import { payView } from './state';

const SLUG = 'porto-demo';
const TOKEN = 'ABCDEFGHIJKLMNOP1234';
const P = `/api/public/v1/${SLUG}/pay/${TOKEN}`;

const page = (over: Partial<PayPageData> = {}): PayPageData => ({
  marina: { name: 'Marina Prova', default_locale: 'it', locales: ['it'] },
  amount_cents: 45_000,
  currency: 'EUR',
  status: 'open',
  charges: [
    {
      number: 'A2026-000001',
      description: 'Ormeggio novembre',
      due_on: '2026-11-30',
      residual_cents: 30_000,
    },
    {
      number: 'A2026-000002',
      description: 'Corrente',
      due_on: '2026-12-31',
      residual_cents: 15_000,
    },
  ],
  bank: {
    iban: 'IT60X0542811101000000123456',
    holder: 'Marina Prova SRL',
    bank: 'Banca Prova',
    reference: 'A2026-000001 A2026-000002',
  },
  online: false,
  methods: [],
  ...over,
});

function renderPay(routes: MockRoute[], ret = false) {
  const mock = createFetchMock(routes);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  renderWithProviders(
    <ApiProvider client={api}>
      <PayPage slug={SLUG} token={TOKEN} ret={ret} />
    </ApiProvider>,
  );
  return mock;
}

describe('pay page view state', () => {
  it('never shows a payment as received without the server', () => {
    expect(payView('open', false)).toBe('open');
    expect(payView('open', true)).toBe('verifying');
    expect(payView('open', false, true)).toBe('verifying');
    expect(payView('paid', false, true)).toBe('paid');
    expect(payView('expired', false)).toBe('expired');
    expect(payView('cancelled', true)).toBe('cancelled');
  });
});

describe('public pay page', () => {
  it('manual only: amount, charges and bank details, no online button', async () => {
    renderPay([{ path: P, body: page() }]);
    expect(await screen.findByTestId('pay-amount')).toHaveTextContent('450,00');
    expect(screen.getAllByTestId('pay-charge')).toHaveLength(2);
    const bank = screen.getByTestId('pay-bank');
    expect(within(bank).getByText('IT60X0542811101000000123456')).toBeInTheDocument();
    expect(within(bank).getByText('A2026-000001 A2026-000002')).toBeInTheDocument();
    expect(within(bank).getByRole('button', { name: 'Copia IBAN' })).toBeInTheDocument();
    expect(screen.queryByTestId('pay-online')).toBeNull();
  });

  it('online: starts the checkout with the chosen method and redirects', async () => {
    const assign = window.location.assign;
    const visited: string[] = [];
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, assign: (u: string) => visited.push(u) },
    });
    const mock = renderPay([
      { path: P, body: page({ online: true, methods: ['card', 'sepa_debit'] }) },
      {
        method: 'POST',
        path: `${P}/checkout`,
        body: { redirect_url: 'https://checkout.example/session' },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Paga online con carta' }));
    await waitFor(() => expect(visited).toEqual(['https://checkout.example/session']));
    expect(mock.calls.find((c) => c.method === 'POST')?.body).toEqual({ method: 'card' });
    expect(screen.getByRole('button', { name: 'Paga con addebito SEPA' })).toBeInTheDocument();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, assign },
    });
  });

  it('shows the final state of a paid or expired request', async () => {
    renderPay([{ path: P, body: page({ status: 'paid' }) }]);
    expect(await screen.findByTestId('pay-paid')).toHaveTextContent('Pagamento ricevuto');
    expect(screen.queryByTestId('pay-bank')).toBeNull();
  });

  it('answers an unknown or revoked link with the same message', async () => {
    renderPay([{ path: P, status: 404, body: problem('not_found', 404) }]);
    expect(await screen.findByText('Link non valido')).toBeInTheDocument();
  });

  it('return page: verifying until the webhook confirms, then received', async () => {
    let status: 'open' | 'paid' = 'open';
    const mock = createFetchMock([]);
    const routes = (url: URL) => url.pathname === `${P}/status`;
    const fetchFn: typeof fetch = async (input, init) => {
      const req = input instanceof Request ? input : new Request(String(input), init);
      if (routes(new URL(req.url)))
        return new Response(JSON.stringify({ status, verifying: status === 'open' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      return mock.fetch(input, init);
    };
    const api = createApiClient({ fetch: fetchFn, baseUrl: 'http://localhost' });
    const { queryClient } = renderWithProviders(
      <ApiProvider client={api}>
        <PayPage slug={SLUG} token={TOKEN} ret />
      </ApiProvider>,
    );
    expect(await screen.findByTestId('pay-verifying')).toHaveTextContent('Pagamento in verifica');
    status = 'paid';
    await queryClient.refetchQueries();
    expect(await screen.findByTestId('pay-paid')).toHaveTextContent('Pagamento ricevuto');
  });
});
