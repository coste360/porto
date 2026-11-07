import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { Charge } from '@porto/shared/api';
import type { MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';

vi.mock('../operations/public', () => ({ opsMapSearch: () => ({}), PlacementPicker: () => null }));
vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

// cmdk (combobox) scrolls the active option into view; jsdom has no layout.
Element.prototype.scrollIntoView ??= () => undefined;

const API = `/api/admin/v1/marinas/${MARINA_ID}`;
const base = (permissions: string[]): MockRoute[] => [
  meRoute(makeMe({}, ['marina.read', 'customers.read', ...permissions] as never)),
  {
    path: API,
    body: { id: MARINA_ID, name: 'Marina Prova', slug: 'prova', timezone: 'Europe/Rome' },
  },
];

const charge = (id: string, number: string, due: string, residual: number, over = {}) => ({
  id,
  number,
  origin_type: 'manual',
  origin_id: id,
  issued_on: '2026-01-01',
  due_on: due,
  total_cents: residual,
  net_cents: residual,
  vat_cents: 0,
  paid_amount_cents: 0,
  credited_cents: 0,
  residual_cents: residual,
  payment_status: 'open',
  invoicing_status: 'to_invoice',
  fiscal_mode: 'invoice',
  description: 'Servizio',
  customer: { id: 'cu1', display_name: 'Rossi Mario' },
  version: 1,
  ...over,
});

const value = (el: HTMLElement) => (el as HTMLInputElement).value;

describe('record payment with the matching table', () => {
  it('auto-fills oldest first, clamps rows, shows the credit and sends the matches', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/payments`, [
      ...base(['payments.collect', 'charges.manage']),
      { path: `${API}/payments`, body: { items: [], next_cursor: null } },
      {
        path: `${API}/customers`,
        body: { items: [{ id: 'cu1', display_name: 'Rossi Mario' }], next_cursor: null },
      },
      {
        path: `${API}/charges`,
        body: {
          items: [
            charge('c3', 'A2026-000003', '2026-03-01', 5_000),
            charge('c1', 'A2026-000001', '2026-01-01', 10_000),
            charge('c2', 'A2026-000002', '2026-01-01', 2_500),
            charge('c4', 'A2026-000004', '2026-01-01', 7_000, {
              payment_status: 'paid',
              residual_cents: 0,
            }),
          ],
          next_cursor: null,
        },
      },
      {
        method: 'POST',
        path: `${API}/payments`,
        status: 201,
        body: { id: 'p1', number: 'P2026-000001' },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Registra pagamento' }));
    const dialog = await screen.findByTestId('record-payment-dialog');
    await userEvent.click(within(dialog).getByRole('combobox', { name: /Cliente/ }));
    await userEvent.click(await screen.findByRole('option', { name: 'Rossi Mario' }));
    expect(await within(dialog).findAllByTestId('match-row')).toHaveLength(3);
    fireEvent.change(within(dialog).getByLabelText('Importo'), { target: { value: '130' } });
    expect(value(within(dialog).getByLabelText('Importo per A2026-000001'))).toBe('100,00');
    expect(value(within(dialog).getByLabelText('Importo per A2026-000002'))).toBe('25,00');
    expect(value(within(dialog).getByLabelText('Importo per A2026-000003'))).toBe('5,00');
    // A row never takes more than what the payment leaves.
    fireEvent.change(within(dialog).getByLabelText('Importo per A2026-000003'), {
      target: { value: '999' },
    });
    await waitFor(() =>
      expect(value(within(dialog).getByLabelText('Importo per A2026-000003'))).toBe('5,00'),
    );
    expect(within(dialog).getByTestId('match-credit')).toHaveTextContent('0,00');
    // A larger amount: rows capped at the residual, the rest is credit.
    fireEvent.change(within(dialog).getByLabelText('Importo'), { target: { value: '200' } });
    expect(value(within(dialog).getByLabelText('Importo per A2026-000003'))).toBe('50,00');
    expect(within(dialog).getByTestId('match-credit')).toHaveTextContent('25,00');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Registra' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.headers.get('Idempotency-Key')).toBeTruthy();
    expect(post.body).toMatchObject({
      customer_id: 'cu1',
      kind: 'payment',
      amount_cents: 20_000,
      matches: [
        { charge_id: 'c1', amount_cents: 10_000 },
        { charge_id: 'c2', amount_cents: 2_500 },
        { charge_id: 'c3', amount_cents: 5_000 },
      ],
    });
  });
});

const session = {
  id: 's1',
  register_id: 'r1',
  status: 'open',
  opened_at: '2026-11-07T07:00:00Z',
  business_date: '2026-11-07',
  float_cents: 10_000,
  version: 4,
  movements: [],
  summary: {
    expected: [
      { method: 'cash', amount_cents: 15_000 },
      { method: 'card_pos', amount_cents: 4_000 },
    ],
    counted: [],
    difference_cents: 0,
    payments_cents: 9_000,
    refunds_cents: 0,
    movements_in_cents: 0,
    movements_out_cents: 0,
  },
};

describe('cash close wizard', () => {
  it('shows the live difference and closes with the counted amounts', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/cash`, [
      ...base(['payments.collect']),
      {
        path: `${API}/cash-registers`,
        body: {
          items: [
            { id: 'r1', code: 'main', name: 'Cassa', active: true, is_default: true, version: 1 },
          ],
        },
      },
      { path: `${API}/cash-sessions/current`, body: session },
      {
        method: 'POST',
        path: `${API}/cash-sessions/s1/close`,
        body: {
          ...session,
          status: 'closed',
          summary: { ...session.summary, difference_cents: -500 },
        },
      },
    ]);
    expect(await screen.findByTestId('cash-session')).toBeInTheDocument();
    expect(screen.getAllByTestId('cash-expected')).toHaveLength(2);
    await userEvent.click(screen.getAllByRole('button', { name: 'Chiudi cassa' })[0]!);
    const dialog = await screen.findByTestId('close-session-dialog');
    expect(within(dialog).getByTestId('close-difference')).toHaveTextContent('-190,00');
    fireEvent.change(within(dialog).getByLabelText('Contato Contanti'), {
      target: { value: '145' },
    });
    fireEvent.change(within(dialog).getByLabelText('Contato Carta (POS)'), {
      target: { value: '40' },
    });
    expect(within(dialog).getByTestId('close-difference')).toHaveTextContent('-5,00');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Chiudi cassa' }));
    expect(await within(dialog).findByText(/Cassa chiusa/)).toBeInTheDocument();
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.headers.get('If-Match')).toBe('"4"');
    expect(post.body).toEqual({
      counted: [
        { method: 'cash', amount_cents: 14_500 },
        { method: 'card_pos', amount_cents: 4_000 },
      ],
    });
    expect(within(dialog).getByRole('button', { name: 'Report Z (PDF)' })).toBeInTheDocument();
  });

  it('offers to open the register when no session is open', async () => {
    renderApp(`/m/${MARINA_ID}/cash`, [
      ...base(['payments.collect']),
      { path: `${API}/cash-registers`, body: { items: [] } },
      {
        path: `${API}/cash-sessions/current`,
        status: 404,
        body: { type: 'session_not_open', title: 'x', status: 404 },
      },
    ]);
    expect(await screen.findByRole('button', { name: 'Apri cassa' })).toBeInTheDocument();
  });
});

describe('export wizard', () => {
  it('previews totals and warnings, confirms and offers the downloads', async () => {
    const preview = {
      count: 3,
      total_cents: 32_200,
      totals_by_vat: [
        { vat_rate_bp: 2200, net_cents: 20_000, vat_cents: 4_400, gross_cents: 24_400 },
        { vat_rate_bp: 1000, net_cents: 7_091, vat_cents: 709, gross_cents: 7_800 },
      ],
      totals_by_origin: [{ origin_type: 'transit', count: 3, total_cents: 32_200 }],
      warnings: [{ code: 'customer_fiscal_incomplete', customer_id: 'cu9', charge_ids: ['x'] }],
    };
    const { calls } = renderApp(`/m/${MARINA_ID}/exports/new`, [
      ...base(['charges.export']),
      { method: 'POST', path: `${API}/charge-exports/preview`, body: preview },
      {
        method: 'POST',
        path: `${API}/charge-exports`,
        status: 201,
        body: {
          id: 'e1',
          number: 'E2026-0001',
          kind: 'export',
          delivery: 'file',
          delivery_status: 'none',
          charge_count: 3,
          line_count: 4,
          total_cents: 32_200,
          created_at: '2026-11-07T10:00:00Z',
          version: 1,
        },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Anteprima' }));
    const p = await screen.findByTestId('export-preview');
    expect(p).toHaveTextContent('3');
    expect(p).toHaveTextContent('322,00');
    expect(screen.getAllByTestId('vat-row')).toHaveLength(2);
    expect(screen.getByTestId('export-warnings')).toHaveTextContent(
      'Cliente con dati fiscali incompleti (1 addebiti).',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Conferma esportazione' }));
    expect(await screen.findByTestId('export-done')).toHaveTextContent('Addebiti esportati: 3');
    const posts = calls.filter((c) => c.method === 'POST');
    expect(posts[1]!.body).toEqual(posts[0]!.body);
    expect((posts[0]!.body as { period_from: string }).period_from).toMatch(/^\d{4}-\d{2}-01$/);
    expect(posts[1]!.headers.get('Idempotency-Key')).toBeTruthy();
    for (const name of ['Scarica CSV', 'Scarica Excel', 'Riepilogo PDF'])
      expect(screen.getByRole('button', { name })).toBeInTheDocument();
  });
});

describe('payment provider settings', () => {
  it('is disabled by default, masks secrets and never resends an empty secret', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/settings/payments`, [
      ...base(['providers.configure']),
      {
        path: `${API}/payment-providers`,
        body: {
          items: [
            { code: 'manual', enabled: true, capabilities: caps },
            { code: 'stripe', enabled: false, capabilities: caps },
          ],
        },
      },
      {
        path: `${API}/payment-providers/stripe/settings`,
        body: {
          provider_code: 'stripe',
          enabled: false,
          mode: 'test',
          has_secret_key: true,
          has_webhook_secret: false,
          sepa_enabled: false,
          methods: ['card'],
          statement_descriptor: 'MARINA',
          webhook_url: 'https://porto.example/webhooks/payments/stripe/m1',
          version: 2,
        },
      },
      {
        method: 'PUT',
        path: `${API}/payment-providers/stripe/settings`,
        body: { provider_code: 'stripe', version: 3 },
      },
    ]);
    expect(await screen.findByTestId('offline-banner')).toHaveTextContent(
      'Porto funziona anche senza pagamenti online',
    );
    expect(await screen.findByTestId('provider-state')).toHaveTextContent('Non attivo');
    const sk = screen.getByLabelText('Chiave segreta') as HTMLInputElement;
    expect(sk.type).toBe('password');
    expect(sk.value).toBe('');
    expect(sk.placeholder).toMatch(/Impostata/);
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"2"');
    expect(put.body).not.toHaveProperty('secret_key');
    expect(put.body).not.toHaveProperty('webhook_secret');
    expect(put.body).toMatchObject({ enabled: false, mode: 'test', methods: ['card'] });
  });
});

const caps = {
  checkout: true,
  mandate_setup: true,
  mandate_charge: true,
  refund: true,
  status_check: true,
  methods: ['card'],
};

describe('charge detail', () => {
  it('offers adjust instead of edit on an exported charge', async () => {
    renderApp(`/m/${MARINA_ID}/charges/c1`, [
      ...base(['charges.manage', 'payments.collect', 'charges.export']),
      {
        path: `${API}/charges/c1`,
        body: charge('c1', 'A2026-000001', '2026-01-31', 12_200, {
          invoicing_status: 'exported',
          lines: [],
          vat_summary: [
            { vat_rate_bp: 2200, net_cents: 10_000, vat_cents: 2_200, gross_cents: 12_200 },
          ],
        }) as unknown as Charge,
      },
    ]);
    expect(
      await screen.findByRole('heading', { name: 'Addebito A2026-000001' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Rettifica' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Modifica' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Annulla addebito' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Estremi fattura' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Incassa' })).toBeInTheDocument();
    expect(screen.getByTestId('charge-residual')).toHaveTextContent('122,00');
  });
});
