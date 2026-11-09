import { screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';
import { makeDashboard, rentalRevenueTile, rentalUtilisationTile } from '../test/fixtures';

// M11 rental tiles (m11-plan §11): available, formatted by unit and drilling into the rental report views.

const me = makeMe({}, ['marina.read', 'reports.all']);

describe('rental tiles', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('shows utilisation in percent and revenue in euro with their drill links', async () => {
    renderApp(`/m/${MARINA_ID}`, [
      ...baseRoutes(me),
      { path: `${API}/dashboard`, body: makeDashboard([rentalUtilisationTile, rentalRevenueTile]) },
      { path: `${API}/deadlines`, body: { items: [], next_cursor: null } },
    ]);
    const tiles = await screen.findAllByTestId('dashboard-tile');
    const util = tiles.find((el) => el.getAttribute('data-tile') === 'rental_utilisation');
    const rev = tiles.find((el) => el.getAttribute('data-tile') === 'rental_revenue');
    if (!util || !rev) throw new Error('rental tiles not rendered');
    expect(util).toHaveAttribute('data-available', 'true');
    expect(within(util).getByText('Utilizzo della flotta a noleggio')).toBeInTheDocument();
    expect(within(util).getByTestId('tile-value')).toHaveTextContent('25');
    expect(within(util).getByTestId('tile-value')).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/rental?view=utilisation&from=2026-01-01&to=2026-11-08`,
    );
    expect(within(rev).getByTestId('tile-value')).toHaveTextContent('21.086,00 €');
    expect(within(rev).getByTestId('tile-value')).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/reports/rental?view=revenue&from=2026-01-01&to=2026-11-08`,
    );
  });
});
