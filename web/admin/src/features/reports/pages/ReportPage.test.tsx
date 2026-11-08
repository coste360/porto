import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ChargesReport, ReceivablesReport } from '@porto/shared/api';
import type { MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';

const envelope = {
  filters: {},
  ignored_filters: [],
  notes: [],
  report: 'charges' as const,
  period: { from: '2026-01-01', to: '2026-03-31' },
  today: '2026-11-08',
};

const charges: ChargesReport = {
  ...envelope,
  basis: 'issued',
  group_by: 'origin',
  page: { page: 1, page_size: 100, total_records: 2 },
  totals: {
    charges: 2,
    lines: 2,
    net_cents: 150000,
    vat_cents: 33000,
    gross_cents: 183000,
    charged_cents: 183000,
  },
  groups: [
    {
      key: 'transit',
      label: 'Transito',
      totals: {
        charges: 2,
        lines: 2,
        net_cents: 150000,
        vat_cents: 33000,
        gross_cents: 183000,
        charged_cents: 183000,
      },
    },
  ],
  records: [
    {
      charge_id: 'c1',
      number: 'A-1',
      seq: 1,
      issued_on: '2026-01-05',
      origin_type: 'transit',
      customer_name: 'Mario Rossi',
      net_cents: 100000,
      vat_cents: 22000,
      gross_cents: 122000,
      payment_status: 'open',
      invoicing_status: 'to_invoice',
      ref: { type: 'charge', id: 'c1', label: 'A-1' },
    },
    {
      charge_id: 'c2',
      number: 'A-2',
      seq: 1,
      issued_on: '2026-02-10',
      origin_type: 'transit',
      net_cents: 50000,
      vat_cents: 11000,
      gross_cents: 61000,
      payment_status: 'paid',
      invoicing_status: 'exported',
      ref: { type: 'charge', id: 'c2' },
    },
  ],
};

const receivables: ReceivablesReport = {
  ...envelope,
  report: 'receivables',
  section: 'reminders',
  page: { page: 1, page_size: 100, total_records: 1 },
  totals: {
    as_of: '2026-11-08',
    count: 3,
    open_cents: 90000,
    overdue_cents: 40000,
    buckets: {
      not_due_cents: 50000,
      d0_30_cents: 40000,
      d31_60_cents: 0,
      d61_90_cents: 0,
      d90_plus_cents: 0,
      total_cents: 90000,
    },
    future_cents: 0,
    reminders_sent: 1,
  },
  groups: [],
  records: [
    {
      section: 'reminders',
      kind: 'reminder',
      id: 'r1',
      number: 'A-9',
      reminder_level: 'formal',
      sent_on: '2026-03-02',
      ref: { type: 'charge', id: 'c9' },
    },
  ],
};

const render = (permissions: Parameters<typeof makeMe>[1], path: string, extra: MockRoute[] = []) =>
  renderApp(path, [
    ...baseRoutes(makeMe({}, permissions)),
    ...extra,
    {
      path: (u) => u.pathname === `${API}/reports/charges` && u.searchParams.has('format'),
      body: 'csv',
      headers: { 'Content-Type': 'text/csv; charset=utf-8' },
    },
    { path: `${API}/reports/charges`, body: charges },
    { path: `${API}/reports/receivables`, body: receivables },
  ]);

const ALL = ['marina.read', 'reports.all'] as const;

describe('ReportPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
    URL.createObjectURL = vi.fn(() => 'blob:report');
    URL.revokeObjectURL = vi.fn();
  });
  afterEach(() => vi.useRealTimers());

  it('shows the records and a Totale row equal to the totals', async () => {
    const { calls } = render(
      [...ALL],
      `/m/${MARINA_ID}/reports/charges?from=2026-01-01&to=2026-03-31&group_by=origin`,
    );
    const rows = await screen.findAllByTestId('report-record');
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByRole('link', { name: '05/01/2026' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/charges/c1`,
    );
    expect(rows[0]).toHaveTextContent('Mario Rossi');
    const total = screen.getByTestId('report-total-row');
    expect(total).toHaveTextContent(/^Totale/);
    // The Totale cells are the totals of the response (not a sum made by the page).
    expect(screen.getByTestId('report-total-cell-net_cents').textContent).toBe(
      screen.getByTestId('report-total-net_cents').textContent,
    );
    expect(screen.getByTestId('report-total-cell-net_cents')).toHaveTextContent('1.500,00 €');
    expect(screen.getByTestId('report-total-cell-gross_cents')).toHaveTextContent('1.830,00 €');
    expect(screen.getByTestId('report-groups')).toHaveTextContent('Transito');
    const q = calls.find((c) => c.url.pathname.endsWith('/reports/charges'))!.url.searchParams;
    expect(q.get('from')).toBe('2026-01-01');
    expect(q.get('to')).toBe('2026-03-31');
    expect(q.get('group_by')).toBe('origin');
    expect(q.get('format')).toBeNull();
  });

  it('defaults the period to year to date and exports the same query as CSV and Excel', async () => {
    const { calls } = render([...ALL], `/m/${MARINA_ID}/reports/charges?basis=competence`);
    await screen.findAllByTestId('report-record');
    const first = calls.find((c) => c.url.pathname.endsWith('/reports/charges'))!.url.searchParams;
    expect(first.get('from')).toBe('2026-01-01');
    expect(first.get('to')).toBe('2026-11-08');
    expect(first.get('basis')).toBe('competence');
    await userEvent.click(screen.getByRole('button', { name: 'Esporta CSV' }));
    await userEvent.click(screen.getByRole('button', { name: 'Esporta Excel' }));
    await waitFor(() =>
      expect(
        calls
          .filter((c) => c.url.searchParams.has('format'))
          .map((c) => c.url.searchParams.get('format')),
      ).toEqual(['csv', 'xlsx']),
    );
    for (const c of calls.filter((x) => x.url.searchParams.has('format'))) {
      expect(c.url.searchParams.get('basis')).toBe('competence');
      expect(c.url.searchParams.get('from')).toBe('2026-01-01');
      expect(c.url.searchParams.get('to')).toBe('2026-11-08');
      expect(c.url.searchParams.get('lang')).toBe('it');
      expect(c.url.searchParams.get('page')).toBeNull();
    }
  });

  it('keeps the section and the drill dimensions in the request', async () => {
    const { calls, router } = render(
      [...ALL],
      `/m/${MARINA_ID}/reports/receivables?overdue=true&customer_id=cu1`,
    );
    await screen.findAllByTestId('report-record');
    expect(screen.getByTestId('report-active-filters')).toHaveTextContent('Cliente: cu1');
    await userEvent.click(screen.getByRole('radio', { name: 'Solleciti' }));
    await waitFor(() =>
      expect(router.state.location.search).toMatchObject({ section: 'reminders' }),
    );
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.searchParams.get('section') === 'reminders' &&
            c.url.searchParams.get('overdue') === 'true' &&
            c.url.searchParams.get('customer_id') === 'cu1',
        ),
      ).toBe(true),
    );
    expect(await screen.findByText('Inviato il')).toBeInTheDocument();
  });

  it('refuses a money report to the operational role', async () => {
    render(['marina.read', 'reports.operational'], `/m/${MARINA_ID}/reports/charges`);
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
    expect(screen.queryByTestId('report-page')).not.toBeInTheDocument();
  });
});

describe('ReportsIndexPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('lists only the operational reports to the operational role', async () => {
    render(['marina.read', 'reports.operational'], `/m/${MARINA_ID}/reports`);
    await screen.findByTestId('reports-index');
    expect(screen.getByTestId('report-card-occupancy')).toBeInTheDocument();
    expect(screen.getByTestId('report-card-comparison')).toBeInTheDocument();
    expect(screen.queryByTestId('report-card-charges')).not.toBeInTheDocument();
    // M10: the yard report needs reports.yard or reports.all; rental is still announced.
    expect(screen.queryByTestId('report-card-yard')).not.toBeInTheDocument();
    expect(screen.getByTestId('report-card-rental')).toHaveTextContent('M11');
  });

  it('lists every report to reports.all', async () => {
    render([...ALL], `/m/${MARINA_ID}/reports`);
    await screen.findByTestId('reports-index');
    expect(screen.getAllByTestId(/^report-card-/)).toHaveLength(16 + 3 + 1 + 2);
  });
});
