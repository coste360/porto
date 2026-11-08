import { screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { MaintenanceReport, StorageReport } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';

const envelope = {
  filters: {},
  ignored_filters: [],
  notes: [],
  period: { from: '2026-03-01', to: '2026-03-31' },
  today: '2026-11-08',
  page: { page: 1, page_size: 100, total_records: 1 },
};

const storage: StorageReport = {
  ...envelope,
  report: 'storage',
  totals: {
    available_m2_nights_milli: 31_000_000,
    occupied_m2_nights_milli: 7_750_000,
    hull_m2_nights_milli: 5_000_000,
    occupancy_bp: 2500,
    boats: 1,
    exits_blocked: 1,
  },
  groups: [],
  records: [
    {
      allocation_id: 'a1',
      area_id: 'ar1',
      area_code: 'CAPANNONE',
      map_kind: 'shed',
      contract_code: 'RIM-2026-0001',
      contract_kind: 'storage',
      nights: 31,
      hull_m2_nights_milli: 5_000_000,
      footprint_m2_nights_milli: 7_750_000,
      exit_on: '2026-03-20',
      exit_blocked: true,
      ref: { type: 'contract', id: 'c1', label: 'RIM-2026-0001' },
    },
  ],
};

const maintenance: MaintenanceReport = {
  ...envelope,
  report: 'maintenance',
  totals: {
    tasks: 1,
    open: 1,
    scheduled: 0,
    in_progress: 0,
    done: 0,
    cancelled: 0,
    overdue: 1,
    internal_minutes: 0,
  },
  groups: [],
  records: [
    {
      task_id: 't1',
      element: { element_type: 'pedestal', element_id: 'p1', code: 'P12' },
      source: 'report',
      priority: 'urgent',
      status: 'open',
      due_on: '2026-03-01',
      overdue_days: 5,
      ref: { type: 'maintenance_task', id: 't1', label: 'P12' },
    },
  ],
};

const render = (path: string) =>
  renderApp(path, [
    ...baseRoutes(makeMe({}, ['marina.read', 'reports.operational'])),
    { path: `${API}/reports/storage`, body: storage },
    { path: `${API}/reports/maintenance`, body: maintenance },
  ]);

describe('M9 report pages', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('storage: occupancy, blocked exits and the Totale from the totals; operational filters only', async () => {
    const { calls } = render(
      `/m/${MARINA_ID}/reports/storage?from=2026-03-01&to=2026-03-31&exits_blocked_only=true`,
    );
    const rows = await screen.findAllByTestId('report-record');
    expect(rows).toHaveLength(1);
    expect(within(rows[0]!).getByRole('link', { name: 'CAPANNONE' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/contracts/c1`,
    );
    expect(rows[0]).toHaveTextContent('Sì');
    expect(screen.getByTestId('report-total-cell-footprint_m2_nights_milli').textContent).toBe(
      screen.getByTestId('report-total-occupied_m2_nights_milli').textContent,
    );
    const q = calls.find((c) => c.url.pathname.endsWith('/reports/storage'))!.url.searchParams;
    expect(q.get('exits_blocked_only')).toBe('true');
  });

  it('maintenance: reads the nested element code and links to the maintenance page', async () => {
    render(`/m/${MARINA_ID}/reports/maintenance?overdue_only=true`);
    const rows = await screen.findAllByTestId('report-record');
    expect(within(rows[0]!).getByRole('link', { name: 'P12' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/maintenance`,
    );
    expect(rows[0]).toHaveTextContent('Colonnina');
    expect(rows[0]).toHaveTextContent('Urgente');
  });
});
