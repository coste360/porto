import { screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';
import {
  makeDashboard,
  newCustomersTile,
  occupancyTile,
  productionTile,
  yardTile,
} from '../test/fixtures';

const full = makeMe({}, ['marina.read', 'reports.all', 'customers.read']);

function renderDashboard(tiles = [productionTile, occupancyTile, newCustomersTile, yardTile]) {
  return renderApp(`/m/${MARINA_ID}`, [
    ...baseRoutes(full),
    { path: `${API}/dashboard`, body: makeDashboard(tiles) },
    { path: `${API}/deadlines`, body: { items: [], next_cursor: null } },
  ]);
}

const tile = async (key: string) => {
  const all = await screen.findAllByTestId('dashboard-tile');
  const found = all.find((el) => el.getAttribute('data-tile') === key);
  if (!found) throw new Error(`tile ${key} not rendered`);
  return found;
};

describe('TileCard', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('shows value, previous year, delta with trend and the drill link with the period', async () => {
    renderDashboard();
    const prod = await tile('production');
    expect(within(prod).getByText('Produzione')).toBeInTheDocument();
    const value = within(prod).getByTestId('tile-value');
    expect(value).toHaveTextContent('12.345,00 €');
    expect(value).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/production?from=2026-01-01&to=2026-11-08`,
    );
    expect(within(prod).getByTestId('tile-previous')).toHaveTextContent(
      'Anno precedente: 10.000,00 €',
    );
    const delta = within(prod).getByTestId('tile-delta');
    expect(delta).toHaveAttribute('data-trend', 'up');
    expect(delta).toHaveTextContent('In aumento');
    expect(delta).toHaveTextContent('+2.345,00 € (+23,45%)');
    expect(within(prod).getByTestId('tile-sparkline')).toBeInTheDocument();
    expect(within(prod).getByRole('link', { name: /Confronta negli anni/ })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/comparison?indicator=production&from=2026-01-01&to=2026-11-08`,
    );
  });

  it('formats ratios, keeps the drill parameters and marks applied dimensions', async () => {
    renderDashboard();
    const occ = await tile('occupancy');
    expect(within(occ).getByTestId('tile-value')).toHaveTextContent('62,5%');
    expect(within(occ).getByTestId('tile-value')).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/occupancy?variant=physical&family=berth&map_kind=water&from=2026-01-01&to=2026-11-08`,
    );
    expect(within(occ).getByTestId('tile-delta')).toHaveAttribute('data-trend', 'flat');
    expect(within(occ).getByText('Filtri della mappa applicati')).toBeInTheDocument();
    // Vessels-like tiles without a comparable indicator have no comparison link; no previous → no delta.
    const customers = await tile('new_customers');
    expect(within(customers).queryByTestId('tile-previous')).not.toBeInTheDocument();
    expect(within(customers).getByTestId('tile-breakdown')).toHaveTextContent('Italia · 2');
    expect(within(customers).getByTestId('tile-breakdown')).toHaveTextContent('FR · 1');
  });

  it('shows the seam tiles of M10/M11 as unavailable, without link', async () => {
    renderDashboard();
    const yard = await tile('yard_hours');
    expect(yard).toHaveAttribute('data-available', 'false');
    expect(yard).toHaveTextContent('Disponibile con M10');
    expect(within(yard).queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByTestId('dashboard-seams')).toHaveTextContent('Noleggio');
  });

  it('opens the M9 blocked exits tile on its own 30-day period', async () => {
    renderDashboard([
      {
        key: 'storage_exits_blocked',
        unit: 'count',
        available: true,
        dims_applied: false,
        value: 2,
        trend: 'none',
        series: [],
        drill: {
          report: 'storage',
          params: { exits_blocked_only: 'true', from: '2026-11-08', to: '2026-12-07' },
        },
      },
    ]);
    const exits = await tile('storage_exits_blocked');
    expect(within(exits).getByText('Uscite bloccate (30 giorni)')).toBeInTheDocument();
    expect(within(exits).getByTestId('tile-value')).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/storage?exits_blocked_only=true&from=2026-11-08&to=2026-12-07`,
    );
  });
});
