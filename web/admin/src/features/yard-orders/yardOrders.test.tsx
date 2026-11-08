import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { FinalAccount, Timesheet, WorkOrder, YardBoard } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';

const WO = '0190a000-0000-7000-8000-00000000c001';
const PERMS = ['marina.read', 'yard.read', 'yard.manage', 'charges.manage', 'map.view'] as const;

const order: WorkOrder = {
  id: WO,
  number: 'C2027-000001',
  customer_id: 'c1',
  vessel_id: 'v1',
  vessel_name: 'Maestrale',
  status: 'completed',
  billing_mode: 'actual',
  completion_bp: 10000,
  hours_approved_milli: 14500,
  hours_estimated_milli: 14000,
  position_note: '',
  notes: '',
  lines: [],
  warnings: [],
  version: 4,
  created_at: '2027-06-01T07:00:00Z',
  updated_at: '2027-06-01T07:00:00Z',
};

const board: YardBoard = {
  today: '2027-06-10',
  columns: [
    { status: 'planned', cards: [] },
    {
      status: 'completed',
      cards: [
        {
          work_order_id: WO,
          number: 'C2027-000001',
          vessel_id: 'v1',
          vessel_name: 'Maestrale',
          loa_cm: 1200,
          completion_bp: 10000,
          planned_delivery_on: '2027-06-08',
          delay_days: 2,
          status: 'completed',
          hours_approved_milli: 14500,
          hours_estimated_milli: 14000,
        },
      ],
    },
  ],
};

const account: FinalAccount = {
  work_order_id: WO,
  billing_mode: 'actual',
  lines: [
    {
      key: 'item:lab|wt:a',
      source: 'labour',
      item_id: 'lab',
      item_code: 'LAB-HAND',
      work_type_id: 'a',
      qty_milli: 9000,
      revenue_cents: 34200,
      cost_cents: 20600,
      margin_cents: 13600,
      vat_rate_bp: 2200,
      vat_cents: 7524,
      gross_cents: 41724,
      budget_qty_milli: 8400,
      budget_net_cents: 31920,
      delta_net_cents: 2280,
    },
  ],
  by_work_type: [
    {
      work_type_id: 'a',
      revenue_cents: 34200,
      cost_cents: 20600,
      margin_cents: 13600,
      margin_bp: 3977,
    },
  ],
  vat: [],
  totals: {
    revenue_cents: 190090,
    cost_cents: 84510,
    margin_cents: 105580,
    margin_bp: 5554,
    revenue_labour_cents: 0,
    revenue_material_cents: 0,
    revenue_service_cents: 0,
    revenue_external_cents: 0,
    cost_labour_cents: 0,
    cost_material_cents: 0,
    cost_service_cents: 0,
    cost_external_cents: 0,
    hours_labour_milli: 14500,
    vat_cents: 41820,
    gross_cents: 231910,
  },
  quote_net_cents: 170260,
  delta_net_cents: 19830,
  charged_gross_cents: 62315,
  remaining_gross_cents: 169595,
};

const routes = (
  extra: { method?: string; path: string; status?: number; body?: unknown }[] = [],
) => [
  ...baseRoutes(makeMe({}, [...PERMS])),
  ...extra,
  { path: `${API}/yard/board`, body: board },
  { path: `${API}/yard/work-orders/${WO}/account`, body: account },
  {
    path: `${API}/yard/work-orders/${WO}/billings`,
    body: { items: [], charged_gross_cents: 62315 },
  },
  { path: `${API}/yard/work-orders/${WO}/tasks`, body: { items: [] } },
  { path: `${API}/yard/work-orders/${WO}`, body: order },
  { path: `${API}/yard/work-orders`, body: { items: [order], next_cursor: null } },
  { path: `${API}/yard/work-types`, body: { items: [] } },
  { path: `${API}/yard/employees`, body: { items: [], next_cursor: null } },
  { path: `${API}/equipment-bookings`, body: { date: '2027-06-10', bookings: [], requests: [] } },
];

describe('yard orders pages', () => {
  it('shows the board by status with completion, delay and hours', async () => {
    renderApp(`/m/${MARINA_ID}/yard/orders`, routes());
    const card = await screen.findByTestId('board-card-C2027-000001');
    expect(card).toHaveTextContent('Maestrale');
    expect(card).toHaveTextContent('100%');
    expect(card).toHaveTextContent('2 giorni di ritardo');
    expect(card).toHaveTextContent('Ore 14,5 / 14');
    expect(screen.getByText('Nessuna commessa')).toBeInTheDocument();
  });

  it('shows the final account and previews then creates the final billing', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/yard/orders/${WO}`,
      routes([
        {
          method: 'POST',
          path: `${API}/yard/work-orders/${WO}/billings`,
          body: {
            dry_run: true,
            kind: 'final',
            gross_cents: 169595,
            lines: [
              {
                plan_key: 'item:lab|wt:a',
                description: 'LAB-HAND',
                net_cents: 1000,
                vat_cents: 220,
                gross_cents: 1220,
                vat_rate_bp: 2200,
              },
            ],
          },
        },
      ]),
    );
    await screen.findByTestId('work-order-page');
    await userEvent.click(screen.getByRole('tab', { name: 'Consuntivo' }));
    const acc = await screen.findByTestId('final-account');
    expect(acc).toHaveTextContent('1.900,90');
    expect(acc).toHaveTextContent('55,54%');
    expect(within(acc).getByTestId('account-line-LAB-HAND')).toHaveTextContent('22,80');
    await userEvent.click(screen.getByRole('tab', { name: 'Addebiti' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Anteprima' }));
    expect(await screen.findByTestId('billing-preview')).toHaveTextContent('1.695,95');
    await waitFor(() =>
      expect(
        calls.some((c) => c.method === 'POST' && c.url.searchParams.get('dry_run') === 'true'),
      ).toBe(true),
    );
  });

  it('approves the selected entries', async () => {
    const entry: Timesheet = {
      id: 'e1',
      employee_id: 'emp',
      work_order_id: WO,
      work_order_number: 'C2027-000001',
      work_date: '2027-06-01',
      hours_milli: 4000,
      source: 'manual',
      status: 'submitted',
      note: '',
      late: false,
      version: 2,
      created_at: '2027-06-01T07:00:00Z',
      updated_at: '2027-06-01T07:00:00Z',
    };
    const { calls } = renderApp(
      `/m/${MARINA_ID}/yard/timesheets`,
      routes([
        { path: `${API}/timesheets`, body: { items: [entry], next_cursor: null } },
        {
          method: 'POST',
          path: `${API}/timesheets/approve`,
          body: { items: [{ ...entry, status: 'approved' }] },
        },
      ]),
    );
    const row = await screen.findByTestId('entry-e1');
    await userEvent.click(within(row).getByRole('checkbox'));
    await userEvent.click(screen.getByRole('button', { name: 'Approva selezionate (1)' }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.method === 'POST' && c.url.pathname.endsWith('/timesheets/approve')),
      ).toBe(true),
    );
  });
});
