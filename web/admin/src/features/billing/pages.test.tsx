import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { deskPaymentBody, groupByKind, toSettingsInput } from './lib/catalog';

vi.mock('../operations/public', () => ({ opsMapSearch: () => ({}), PlacementPicker: () => null }));
vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

Element.prototype.scrollIntoView ??= () => undefined;

const API = `/api/admin/v1/marinas/${MARINA_ID}`;
const ALL = [
  'marina.read',
  'marina.configure',
  'customers.read',
  'contracts.read',
  'charges.manage',
  'charges.export',
  'payments.collect',
  'refunds.issue',
];
const base: MockRoute[] = [
  meRoute(makeMe({}, ALL as never)),
  {
    path: API,
    body: { id: MARINA_ID, name: 'Marina Prova', slug: 'prova', timezone: 'Europe/Rome' },
  },
];
const page = (items: unknown[], extra: Record<string, unknown> = {}) => ({
  items,
  next_cursor: null,
  ...extra,
});
const ageing = {
  not_due_cents: 8_000,
  d0_30_cents: 0,
  d31_60_cents: 20_000,
  d61_90_cents: 0,
  d90_plus_cents: 0,
  total_cents: 28_000,
};

describe('billing pages render their data', () => {
  it.each([
    {
      path: 'charges',
      heading: 'Addebiti',
      routes: [{ path: `${API}/charges`, body: page([]) }],
      text: 'Nessun addebito.',
    },
    {
      path: 'receivables',
      heading: 'Scadenzario',
      routes: [
        {
          path: `${API}/receivables`,
          body: page(
            [
              {
                kind: 'charge',
                id: 'c1',
                number: 'A2026-000001',
                due_on: '2026-09-01',
                amount_cents: 20_000,
                residual_cents: 20_000,
                bucket: 'd31_60',
                customer: { id: 'cu1', display_name: 'Rossi Mario' },
              },
            ],
            { totals: ageing },
          ),
        },
      ],
      text: 'A2026-000001',
    },
    {
      path: 'reminders',
      heading: 'Solleciti',
      routes: [{ path: `${API}/reminder-runs`, body: page([]) }],
      text: 'Nessuna esecuzione.',
    },
    {
      path: 'payment-requests',
      heading: 'Richieste di pagamento',
      routes: [{ path: `${API}/payment-requests`, body: page([]) }],
      text: 'Nessuna richiesta.',
    },
    {
      path: 'worklist',
      heading: "Attività dell'ufficio",
      routes: [
        {
          path: `${API}/worklist`,
          body: page([
            {
              kind: 'late_payment',
              source_type: 'payment',
              source_id: 'p1',
              status: 'open',
              amount_cents: 5_000,
              created_at: '2026-11-07T08:00:00Z',
            },
          ]),
        },
      ],
      text: 'Pagamento tardivo (1)',
    },
    {
      path: 'fiscal',
      heading: 'Corrispettivi',
      routes: [
        {
          path: `${API}/fiscal/daily-totals`,
          body: {
            from: '2026-11-01',
            to: '2026-11-07',
            group: 'day',
            total_cents: 12_200,
            rows: [
              {
                period: '2026-11-07',
                vat_rate_bp: 2200,
                method: 'cash',
                net_cents: 10_000,
                vat_cents: 2_200,
                gross_cents: 12_200,
                documents: 1,
              },
            ],
          },
        },
      ],
      text: '2026-11-07',
    },
    {
      path: 'exports',
      heading: 'Da fatturare',
      routes: [
        { path: `${API}/charge-exports`, body: page([]) },
        { path: `${API}/invoice-reconciliations`, body: page([]) },
      ],
      text: 'Nessuna esportazione.',
    },
    {
      path: 'opening-balances',
      heading: 'Saldi di apertura',
      routes: [{ path: `${API}/opening-balances/imports`, body: page([]) }],
      text: 'Nessuna importazione.',
    },
  ])('$path', async ({ path, heading, routes, text }) => {
    renderApp(`/m/${MARINA_ID}/${path}`, [...base, ...(routes as MockRoute[])]);
    expect(await screen.findByRole('heading', { name: heading, level: 1 })).toBeInTheDocument();
    expect(await screen.findByText(text)).toBeInTheDocument();
  });
});

describe('billing settings', () => {
  it('keeps every reminder level and sends the settings with If-Match', async () => {
    const settings = {
      default_due_days: 30,
      auto_payment_request: false,
      reminder_policy: { levels: [{ level: 'due', days: 0, enabled: true }] },
      reminder_hour: 9,
      arrears_policy: { enabled: false, days_overdue: 60, min_amount_cents: 0 },
      export_reminder_days: 15,
      fiscal_driver: 'none',
      receipt_footer: '',
      sepa_lead_days: 5,
      origin_items: [],
      fiscal_drivers: ['none', 'export'],
      version: 7,
    };
    expect(toSettingsInput(settings as never).reminder_policy.levels.map((l) => l.level)).toEqual([
      'pre_due',
      'due',
      'courteous',
      'formal',
      'final_notice',
    ]);
    const { calls } = renderApp(`/m/${MARINA_ID}/settings/billing`, [
      ...base,
      { path: `${API}/billing-settings`, body: settings },
      { path: `${API}/items`, body: page([]) },
      { path: `${API}/cash-registers`, body: { items: [] } },
      { method: 'PUT', path: `${API}/billing-settings`, body: settings },
    ]);
    expect(await screen.findAllByTestId('reminder-level')).toHaveLength(5);
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"7"');
    expect(put.body).not.toHaveProperty('fiscal_drivers');
    expect(put.body).not.toHaveProperty('version');
  });
});

describe('helpers', () => {
  it('builds the desk payment of a transit confirmation only when collecting', () => {
    const v = { collect: false, method: 'cash' as const, amountCents: 5_000, fiscalDocument: true };
    expect(deskPaymentBody(v)).toEqual({});
    expect(deskPaymentBody({ ...v, collect: true })).toEqual({
      payment: { method: 'cash', amount_cents: 5_000 },
      fiscal_document: true,
    });
  });
  it('groups work items by kind in catalogue order', () => {
    const item = (kind: string) => ({
      kind,
      source_type: 's',
      source_id: kind,
      status: 'open',
      created_at: '',
    });
    const groups = groupByKind([item('dispute'), item('payment_failed'), item('dispute')] as never);
    expect(groups.map(([k, g]) => [k, g.length])).toEqual([
      ['payment_failed', 1],
      ['dispute', 2],
    ]);
  });
});
