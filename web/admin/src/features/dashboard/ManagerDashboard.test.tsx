import { screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';
import { pickOption } from '../pricing/test/dom';
import { makeDashboard, occupancyTile, productionTile } from './test/fixtures';

const routes = (permissions: Parameters<typeof makeMe>[1]) => [
  ...baseRoutes(makeMe({}, permissions)),
  { path: `${API}/dashboard`, body: makeDashboard([productionTile, occupancyTile]) },
  { path: `${API}/deadlines`, body: { items: [], next_cursor: null } },
];

describe('ManagerDashboard', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('replaces the placeholder: year to date with the previous year by default', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}`, routes(['marina.read', 'reports.all']));
    expect(await screen.findByTestId('manager-dashboard')).toBeInTheDocument();
    expect(await screen.findAllByTestId('dashboard-tile')).toHaveLength(2);
    expect(screen.queryByText(/prossime versioni/)).not.toBeInTheDocument();
    const q = calls.find((c) => c.url.pathname.endsWith('/dashboard'))!.url.searchParams;
    expect(q.get('from')).toBe('2026-01-01');
    expect(q.get('to')).toBe('2026-11-08');
    expect(q.get('compare')).toBeNull();
    expect(q.get('align')).toBeNull();
  });

  it('asks the period of a preset and the chosen comparison', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}`, routes(['marina.read', 'reports.operational']));
    await screen.findAllByTestId('dashboard-tile');
    await pickOption('Periodo', 'Mese in corso');
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.pathname.endsWith('/dashboard') &&
            c.url.searchParams.get('from') === '2026-11-01',
        ),
      ).toBe(true),
    );
    await pickOption('Allineamento', 'Per giorno della settimana');
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('align') === 'weekday')).toBe(true),
    );
    await pickOption('Confronto', 'Nessun confronto');
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('compare') === 'none')).toBe(true),
    );
    expect(screen.queryByRole('combobox', { name: 'Allineamento' })).not.toBeInTheDocument();
  });

  it('narrows the tiles by pier, group and category and keeps them in the drill link', async () => {
    const drilled = {
      ...occupancyTile,
      drill: {
        report: 'occupancy' as const,
        params: { variant: 'physical', family: 'berth', structure_id: 's1' },
      },
    };
    const { calls } = renderApp(`/m/${MARINA_ID}`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'reports.all', 'map.view', 'pricing.read'])),
      { path: `${API}/dashboard`, body: makeDashboard([drilled]) },
      { path: `${API}/deadlines`, body: { items: [], next_cursor: null } },
      {
        path: `${API}/berth-categories`,
        body: {
          items: [
            {
              id: 'c1',
              code: 'B',
              name: { it: 'Classe B' },
              applies_to: [],
              sort_order: 1,
              archived: false,
              usage: {},
              version: 1,
            },
          ],
          unknown_in_use: [],
        },
      },
    ]);
    await screen.findAllByTestId('dashboard-tile');
    await pickOption('Pontile', 'Pontile A');
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('structure_id') === 's1')).toBe(true),
    );
    await pickOption('Categoria', 'Classe B');
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url.pathname.endsWith('/dashboard') &&
            c.url.searchParams.get('structure_id') === 's1' &&
            c.url.searchParams.get('category') === 'B',
        ),
      ).toBe(true),
    );
    const tile = (await screen.findAllByTestId('dashboard-tile'))[0]!;
    expect(within(tile).getByTestId('tile-value').getAttribute('href')).toContain(
      'structure_id=s1',
    );
  });

  it('is not rendered for roles without report permissions', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}`, routes(['marina.read', 'presence.write']));
    expect(await screen.findByRole('heading', { name: 'Marina Prova' })).toBeInTheDocument();
    expect(screen.queryByTestId('manager-dashboard')).not.toBeInTheDocument();
    expect(calls.some((c) => c.url.pathname.endsWith('/dashboard'))).toBe(false);
    expect(screen.getByText('Scadenze e attività del giorno del porto.')).toBeInTheDocument();
  });

  it('describes the manager dashboard, not the team settings, in the subtitle', async () => {
    renderApp(`/m/${MARINA_ID}`, routes(['marina.read', 'reports.operational']));
    expect(await screen.findByRole('heading', { name: 'Marina Prova' })).toBeInTheDocument();
    expect(screen.getByText(/confrontato con l'anno precedente/)).toBeInTheDocument();
    expect(screen.queryByText(/gestisci il team/)).not.toBeInTheDocument();
  });
});
