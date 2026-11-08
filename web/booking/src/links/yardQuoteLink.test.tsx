import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';

import { ApiProvider, createApiClient, type PublicLink } from '@porto/shared/api';
import { createFetchMock, renderWithProviders, type MockRoute } from '@porto/shared/testing';

import { LinkPage } from './index';

const SLUG = 'porto-demo';
const TOKEN = 'ABCDEFGHIJKLMNOP1234';
const L = `/api/public/v1/${SLUG}/links/${TOKEN}`;

const link: PublicLink = {
  purpose: 'quote',
  marina: { name: 'Marina Prova', default_locale: 'it', locales: ['it'] },
  expires_at: '2026-12-07T23:00:00Z',
  yard_quote: {
    number: 'PC2026-000012',
    kind: 'job',
    status: 'sent',
    valid_until: '2026-12-07',
    vessel_name: 'Maestrale',
    lines: [
      {
        description: 'Alaggio',
        qty_milli: 12000,
        unit_cents: 1800,
        net_cents: 21600,
        vat_rate_bp: 2200,
      },
      {
        description: 'Lavaggio carena',
        qty_milli: 56100,
        unit_cents: 400,
        net_cents: 22440,
        vat_rate_bp: 2200,
      },
    ],
    net_cents: 170260,
    vat_cents: 37457,
    gross_cents: 207717,
    deposit_amount_cents: 62315,
    billing_mode: 'actual',
    conditions: 'Saldo alla consegna.',
    can_accept: true,
  },
};

function renderLink(routes: MockRoute[]) {
  const mock = createFetchMock(routes);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  renderWithProviders(
    <ApiProvider client={api}>
      <LinkPage slug={SLUG} purpose="quote" token={TOKEN} />
    </ApiProvider>,
  );
  return mock;
}

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:doc');
  URL.revokeObjectURL = vi.fn();
});

it('shows a yard quote from its link and accepts it with the conditions', async () => {
  const mock = renderLink([
    { path: L, body: link },
    { path: `${L}/document`, body: new Blob(['%PDF-'], { type: 'application/pdf' }) },
    {
      method: 'POST',
      path: `${L}/yard-quote/accept`,
      body: { quote_number: 'PC2026-000012', work_order_number: 'C2026-000034' },
    },
  ]);
  await screen.findByTestId('yard-quote-link');
  expect(screen.getAllByTestId('yard-quote-link-line')).toHaveLength(2);
  expect(screen.getByTestId('yard-quote-link-gross')).toHaveTextContent('2.077,17');
  const accept = screen.getAllByRole('button').at(-1)!;
  expect(accept).toBeDisabled();
  await userEvent.click(screen.getByRole('checkbox'));
  expect(accept).toBeEnabled();
  await userEvent.click(accept);
  await waitFor(() => expect(screen.queryByTestId('yard-quote-link')).toBeNull());
  const call = mock.calls.find((c) => c.url.pathname.endsWith('/yard-quote/accept'))!;
  expect(call.body).toEqual({ accept_conditions: true });
});
