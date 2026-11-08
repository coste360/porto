import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import {
  makeDashboard,
  occupancyTile,
  productionTile,
  yardTile,
} from '../../dashboard/test/fixtures';
import { API, baseRoutes } from '../../desk/test/fixtures';

const width = window.innerWidth;
function phone(w = 412) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: w });
}

const routes = (permissions: Parameters<typeof makeMe>[1]) => [
  ...baseRoutes(makeMe({}, permissions)),
  {
    path: `${API}/movements`,
    body: { arrivals_expected: [], departures_expected: [], date: '2026-11-08' },
  },
  {
    path: `${API}/dashboard`,
    body: makeDashboard([productionTile, occupancyTile, yardTile], {
      period: { from: '2026-11-01', to: '2026-11-08' },
    }),
  },
];

describe('DirectionDashboardTab', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
    phone();
  });
  afterEach(() => {
    vi.useRealTimers();
    phone(width);
  });

  it('shows the compact tiles of the month, switches to year to date, keeps today counts', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/direction`,
      routes(['marina.read', 'reports.all', 'contracts.read']),
    );
    const section = await screen.findByTestId('direction-tiles');
    await waitFor(() => expect(within(section).getAllByTestId('dashboard-tile')).toHaveLength(2));
    expect(within(section).queryByTestId('tile-sparkline')).not.toBeInTheDocument();
    expect(within(section).queryByText('Disponibile con M10')).not.toBeInTheDocument();
    expect(screen.getAllByTestId('direction-count')).toHaveLength(3);
    const first = calls.find((c) => c.url.pathname.endsWith('/dashboard'))!.url.searchParams;
    expect(first.get('from')).toBe('2026-11-01');
    expect(first.get('to')).toBe('2026-11-08');
    const link = within(section).getAllByTestId('tile-value')[0]!;
    expect(link).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/production?from=2026-11-01&to=2026-11-08`,
    );
    await userEvent.click(within(section).getByRole('radio', { name: 'Da inizio anno' }));
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.pathname.endsWith('/dashboard') &&
            c.url.searchParams.get('from') === '2026-01-01',
        ),
      ).toBe(true),
    );
  });

  it('shows no tiles to a role without report permissions', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/direction`,
      routes(['marina.read', 'marina.configure', 'contracts.read']),
    );
    expect(await screen.findByTestId('direction-dashboard')).toBeInTheDocument();
    expect(screen.queryByTestId('direction-tiles')).not.toBeInTheDocument();
    expect(calls.some((c) => c.url.pathname.endsWith('/dashboard'))).toBe(false);
  });
});
